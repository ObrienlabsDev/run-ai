// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func mounted(reader io.Reader, path string) (bool, error) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			return false, fmt.Errorf("invalid mountinfo entry")
		}
		separator := 6
		for separator < len(fields) && fields[separator] != "-" {
			separator++
		}
		if separator+3 >= len(fields) {
			return false, fmt.Errorf("invalid mountinfo separator")
		}
		mountPath := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(fields[4])
		if mountPath == path && fields[separator+1] == "fuse.goofys" {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read mountinfo: %w", err)
	}
	return false, nil
}

func check(args []string, stderr io.Writer) int {
	if len(args) != 1 || !filepath.IsAbs(args[0]) {
		return report(stderr, 2, "usage: s3-mount-check /absolute/mount/path")
	}
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return report(stderr, 2, fmt.Sprintf("open mountinfo: %v", err))
	}
	ready, readErr := mounted(file, filepath.Clean(args[0]))
	closeErr := file.Close()
	if readErr != nil {
		return report(stderr, 2, readErr.Error())
	}
	if closeErr != nil {
		return report(stderr, 2, fmt.Sprintf("close mountinfo: %v", closeErr))
	}
	if !ready {
		return report(stderr, 1, "S3 FUSE mount is not ready")
	}
	return 0
}

func report(stderr io.Writer, code int, message string) int {
	if _, err := fmt.Fprintln(stderr, message); err != nil {
		return 2
	}
	return code
}

func main() {
	os.Exit(check(os.Args[1:], os.Stderr))
}
