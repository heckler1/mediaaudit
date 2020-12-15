package main

import (
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

var reportHeaders []string = []string{"Codec", "SizeMB", "BitrateType", "BitrateMbps", "Width", "Height"}

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
