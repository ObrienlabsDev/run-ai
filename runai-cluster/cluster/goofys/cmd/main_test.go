// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jacobsa/fuse"
	"github.com/kahing/goofys/api/common"
	"github.com/kahing/goofys/internal"
)

func goofysCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestGoofysProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "GOOFYS_TEST_PROCESS=1")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd
}

func TestCheckReady(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	if err := os.WriteFile(ready, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		args      []string
		wantReady bool
	}{
		{name: "ready", args: []string{"--check-ready", ready}, wantReady: true},
		{name: "not ready", args: []string{"--check-ready", ready + "-missing"}},
		{name: "directory", args: []string{"--check-ready", filepath.Dir(ready)}},
		{name: "relative path", args: []string{"--check-ready", "ready"}},
		{name: "empty path", args: []string{"--check-ready="}},
		{name: "unexpected mount arguments", args: []string{"--check-ready", ready, "bucket", "/data"}},
		{name: "conflicting modes", args: []string{"--check-ready", ready, "--ready-file", ready}},
		{name: "daemon mode", args: []string{"--ready-file", ready, "bucket", "/data"}},
		{name: "marker inside mount", args: []string{"-f", "--ready-file", ready, "bucket", filepath.Dir(ready)}},
		{name: "marker at mount point", args: []string{"-f", "--ready-file", ready, "bucket", ready}},
		{name: "version remains supported", args: []string{"--version"}, wantReady: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := goofysCommand(ctx, tc.args...).Run()
			if ctx.Err() != nil {
				t.Fatal("goofys command timed out")
			}
			if (err == nil) != tc.wantReady {
				t.Fatalf("command success = %v, want %v", err == nil, tc.wantReady)
			}
		})
	}
}

func TestReadinessFollowsMount(t *testing.T) {
	for _, succeeds := range []bool{false, true} {
		t.Run(fmt.Sprintf("mount succeeds=%v", succeeds), func(t *testing.T) {
			dir := t.TempDir()
			ready := filepath.Join(dir, "ready")
			if err := os.Mkdir(filepath.Join(dir, "mount"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ready, nil, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := goofysCommand(ctx, "-f", "--ready-file", ready, "bucket", filepath.Join(dir, "mount"))
			cmd.Env = append(cmd.Env, "GOOFYS_TEST_MOUNT_DIR="+dir, fmt.Sprintf("GOOFYS_TEST_MOUNT_SUCCEEDS=%v", succeeds))
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cancel()
				if err := cmd.Wait(); err == nil && succeeds {
					t.Error("mounted process exited unexpectedly")
				}
			})
			waitForReadinessFile(t, filepath.Join(dir, "entered"))
			if _, err := os.Stat(ready); !os.IsNotExist(err) {
				t.Fatal("previous readiness marker survived into the mount attempt")
			}
			if err := goofysCommand(ctx, "--check-ready", ready).Run(); err == nil {
				t.Fatal("probe succeeded before mounting completed")
			}
			if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if succeeds {
				waitForReadinessFile(t, ready)
				if err := goofysCommand(ctx, "--check-ready", ready).Run(); err != nil {
					t.Fatal("probe failed after mounting succeeded")
				}
			} else {
				waitForReadinessFile(t, filepath.Join(dir, "failed"))
				if err := goofysCommand(ctx, "--check-ready", ready).Run(); err == nil {
					t.Fatal("failed mount satisfied the probe")
				}
			}
		})
	}
}

func TestReadinessPublicationFailure(t *testing.T) {
	for _, unmountFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("unmount fails=%v", unmountFails), func(t *testing.T) {
			dir := t.TempDir()
			mountPoint := filepath.Join(dir, "mount")
			if err := os.Mkdir(mountPoint, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ready := filepath.Join(dir, "missing-directory", "ready")
			cmd := goofysCommand(ctx, "-f", "--ready-file", ready, "bucket", mountPoint)
			cmd.Env = append(cmd.Env, "GOOFYS_TEST_MOUNT_DIR="+dir, "GOOFYS_TEST_MOUNT_SUCCEEDS=true",
				fmt.Sprintf("GOOFYS_TEST_UNMOUNT_FAILS=%v", unmountFails))
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err == nil || ctx.Err() != nil {
				t.Fatal("goofys must fail promptly when it cannot publish readiness")
			}
			unmounted, err := os.ReadFile(filepath.Join(dir, "unmounted"))
			if err != nil || string(unmounted) != mountPoint {
				t.Fatal("readiness publication failure did not attempt to unmount the mounted path")
			}
			if _, err := os.Stat(ready); !os.IsNotExist(err) {
				t.Fatal("failed publication left a readiness marker")
			}
			if !strings.Contains(stderr.String(), "create readiness file:") {
				t.Fatal("original readiness publication error was lost")
			}
			if strings.Contains(stderr.String(), "test unmount failed") != unmountFails {
				t.Fatal("unmount failure was not logged as expected")
			}
		})
	}
}

func waitForReadinessFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for test process")
}

func TestGoofysProcess(t *testing.T) {
	if os.Getenv("GOOFYS_TEST_PROCESS") != "1" {
		return
	}
	if dir := os.Getenv("GOOFYS_TEST_MOUNT_DIR"); dir != "" {
		unmount = func(path string, flags int) error {
			if flags != 0 {
				return fmt.Errorf("unexpected unmount flags: %d", flags)
			}
			if err := os.WriteFile(filepath.Join(dir, "unmounted"), []byte(path), 0600); err != nil {
				return err
			}
			if os.Getenv("GOOFYS_TEST_UNMOUNT_FAILS") == "true" {
				return fmt.Errorf("test unmount failed")
			}
			return nil
		}
		mount = func(context.Context, string, *common.FlagStorage) (*internal.Goofys, *fuse.MountedFileSystem, error) {
			if err := os.WriteFile(filepath.Join(dir, "entered"), nil, 0600); err != nil {
				return nil, nil, err
			}
			for {
				if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if os.Getenv("GOOFYS_TEST_MOUNT_SUCCEEDS") != "true" {
				if err := os.WriteFile(filepath.Join(dir, "failed"), nil, 0600); err != nil {
					return nil, nil, err
				}
				return nil, nil, fmt.Errorf("test mount failed")
			}
			return &internal.Goofys{}, &fuse.MountedFileSystem{}, nil
		}
	}
	separator := slices.Index(os.Args, "--")
	os.Args = append([]string{"goofys"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}
