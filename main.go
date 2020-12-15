package main

import (
	"context"
	"encoding/csv"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"golang.org/x/sync/semaphore"
)

const mediainfoTemplate string = `General;%OverallBitRate%,
Video;%Format%,%Width%,%Height%,%BitRate_Maximum%,%BitRate%,%BitRate_Nominal%`

var (
	maxSem int64 = 150 // A sane value to avoid hitting file open limits

	videoFileRegex    *regexp.Regexp = regexp.MustCompile(`\.mp4$|\.mkv$|\.avi$|\.mov$`)
	subtitleFileRegex *regexp.Regexp = regexp.MustCompile(`\.srt$|\.idx$|\.sub$`)

	outputFile io.Writer = os.Stdout
)

func main() {
	// Get our directory to traverse
	dirPath := os.Args[1]

	// Mediainfo cannot handle a template that grabs from more than one section as a commandline argument
	// However, it supports multi-section templates when read in from a file
	// Writing the template to file means we can avoid calling mediainfo
	// more than once for a given file, so while this is gross, it's notably faster
	templateTempFile, err := ioutil.TempFile("", "mediaauditTemplate")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(templateTempFile.Name())
	templateTempFile.WriteString(mediainfoTemplate)
	templateTempFile.Close()

	var csvLock sync.Mutex
	sem := semaphore.NewWeighted(maxSem)

	// Add a header to our CSV output
	writer := csv.NewWriter(outputFile)
	var headers []string
	headers = append(headers, "Name")
	headers = append(headers, reportHeaders...)
	writer.Write(headers) // Don't bother flushing here, the goroutine will flush for us

	// Traverse the given directory
	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		// Make sure we actually want to check the file
		switch {
		case err != nil:
			log.Printf("Prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		case info.IsDir():
			return nil
		case subtitleFileRegex.MatchString(info.Name()):
			return nil
		case !videoFileRegex.MatchString(info.Name()):
			// We're not sure what we're skipping here, so log to stderr
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
				return
			}

			// Calculate the size of the file
			report.SizeMB = math.Round((float64(info.Size())/1048576)*100) / 100

			// Add the entry to our output
			var values []string
			values = append(values, info.Name())
			values = append(values, report.ToSlice()...)

			// Now write it
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
