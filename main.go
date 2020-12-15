package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/semaphore"
)

const template string = `General;%OverallBitRate%,
Video;%Format%,%Width%,%Height%,%BitRate_Maximum%,%BitRate%,%BitRate_Nominal%`

var (
	csvLock sync.Mutex

	maxSem int64 = 200

	videoFileRegex    *regexp.Regexp = regexp.MustCompile(`\.mp4$|\.mkv$|\.avi$|\.mov$`)
	subtitleFileRegex *regexp.Regexp = regexp.MustCompile(`\.srt$|\.idx$|\.sub$`)

	reportHeaders []string = []string{"Codec", "SizeMB", "BitrateType", "BitrateMbps", "Width", "Height"}
)

func main() {
	// Get our directory to traverse
	dirPath := os.Args[1]

	// Because mediainfo's inline template handling is trash, we write a temporary template file to load in
	// This means we can avoid calling mediainfo more than once for a given file, so it's worth the trash
	templateTempFile, err := ioutil.TempFile("", "mediaauditTemplate")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(templateTempFile.Name())
	templateTempFile.WriteString(template)

	// Prep our semaphore to prevent too many open files
	sem := semaphore.NewWeighted(maxSem)

	// Add a header to our csv output
	writer := csv.NewWriter(os.Stdout)
	var headers []string
	headers = append(headers, "Name")
	headers = append(headers, reportHeaders...)
	writer.Write(headers)

	// Traverse the given directory
	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		switch {
		case err != nil:
			log.Printf("Prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		case info.IsDir():
			return nil
		case subtitleFileRegex.MatchString(info.Name()):
			return nil
		case !videoFileRegex.MatchString(info.Name()):
			// Dunno what we're skipping here, so log to stderr
			log.Printf("Skipping non-video file: %q\n", info.Name())
			return nil
		}

		// Acquire a semaphore
		sem.Acquire(context.TODO(), 1)
		go func(path string, info os.FileInfo) {
			defer sem.Release(1)
			// Get the report from mediainfo
			report, err := getReport(path, templateTempFile.Name())
			if err != nil {
				log.Println(err.Error())
			}
			// Calculate the size of the file
			report.SizeMB = math.Round((float64(info.Size())/1048576)*100) / 100

			// Add the entry to our output
			var values []string
			values = append(values, info.Name())
			values = append(values, report.ToSlice()...)
			csvLock.Lock()
			defer csvLock.Unlock()
			writer.Write(values)
			writer.Flush()
			if writer.Error() != nil {
				log.Printf("Failed to flush writes to CSV when checking %q: %s\n", info.Name(), err.Error())
			}
		}(path, info)
		return nil
	})

	// Wait for all goroutines to finish
	sem.Acquire(context.TODO(), maxSem)
}

type Report struct {
	Name        string
	Codec       string
	SizeMB      float64
	BitrateType string
	BitrateMbps float64
	Width       int
	Height      int
}

func (r *Report) ToSlice() []string {
	return []string{r.Codec, fmt.Sprintf("%.2f", r.SizeMB), r.BitrateType, fmt.Sprintf("%.3f", r.BitrateMbps), fmt.Sprintf("%d", r.Width), fmt.Sprintf("%d", r.Height)}
}

func getReport(path, templateFilePath string) (mediaInfo *Report, err error) {
	cmd := exec.Command("mediainfo", `--output=file://`+templateFilePath, path)
	bytes, err := cmd.Output()
	if err != nil {
		return &Report{}, err
	}

	info := strings.Split(
		strings.TrimSuffix(string(bytes), "\n"),
		",",
	)
	if len(info) != 7 {
		return &Report{}, fmt.Errorf("Missing full info for file %q, %v", path, info)
	}
	codec := info[1]

	width, err := strconv.Atoi(info[2])
	if err != nil {
		return &Report{}, err
	}

	height, err := strconv.Atoi(info[3])
	if err != nil {
		return &Report{}, err
	}

	bitrateType := ""
	bitrateString := "0"
	if info[4] != "" {
		bitrateType = "Variable"
		bitrateString = info[4]
	} else if info[5] != "" {
		bitrateType = "Constant"
		bitrateString = info[5]
	} else if info[6] != "" {
		bitrateType = "Nominal"
		bitrateString = info[6]
	} else if info[0] != "" {
		bitrateType = "Overall"
		bitrateString = info[0]
	} else {
		return &Report{}, fmt.Errorf("Unable to get bitrate for file %q: %v", path, info)
	}

	bitrateInt, err := strconv.Atoi(bitrateString)
	if err != nil {
		return &Report{}, err
	}

	bitrateMbps := math.Round((float64(bitrateInt)/1048576)*1000) / 1000

	return &Report{
		Codec:       codec,
		BitrateType: bitrateType,
		BitrateMbps: bitrateMbps,
		Width:       width,
		Height:      height,
	}, nil
}
