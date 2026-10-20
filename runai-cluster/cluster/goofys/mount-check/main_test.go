// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestMounted(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		info string
		want bool
	}{
		{name: "empty mount table", path: "/storage"},
		{name: "unmounted emptyDir", path: "/storage", info: "30 20 8:1 / /storage rw - ext4 /dev/root rw\n"},
		{name: "mounted bucket", path: "/storage", info: "31 30 0:40 / /storage rw shared:1 - fuse.goofys bucket rw\n", want: true},
		{name: "empty bucket", path: "/storage", info: "31 30 0:40 / /storage rw - fuse.goofys empty-bucket rw\n", want: true},
		{name: "different FUSE filesystem", path: "/storage", info: "31 30 0:40 / /storage rw - fuse.sshfs remote rw\n"},
		{name: "parent path is insufficient", path: "/storage/data", info: "31 30 0:40 / /storage rw - fuse.goofys bucket rw\n"},
		{name: "child path is insufficient", path: "/storage", info: "31 30 0:40 / /storage/data rw - fuse.goofys bucket rw\n"},
		{name: "source is not a mount path", path: "/storage", info: "31 30 0:40 / /other rw - fuse.goofys /storage rw\n"},
		{name: "several buckets", path: "/second", info: "31 30 0:40 / /first rw - fuse.goofys first rw\n32 30 0:41 / /second rw - fuse.goofys second rw\n", want: true},
		{name: "escaped mount path", path: "/data set\tline\nback\\slash", info: "31 30 0:40 / /data\\040set\\011line\\012back\\134slash rw - fuse.goofys bucket rw\n", want: true},
		{name: "escape replacement is not recursive", path: `/data\040set`, info: "31 30 0:40 / /data\\134040set rw - fuse.goofys bucket rw\n", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mounted(strings.NewReader(tc.info), tc.path)
			if err != nil || got != tc.want {
				t.Fatalf("mounted() = %t, %v; want %t, nil", got, err, tc.want)
			}
		})
	}
}

func TestMountedFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader io.Reader
	}{
		{name: "truncated entry", reader: strings.NewReader("31 30 0:40 / /storage")},
		{name: "missing separator", reader: strings.NewReader("31 30 0:40 / /storage rw shared:1 fuse.goofys bucket rw")},
		{name: "read error", reader: iotest.ErrReader(io.ErrUnexpectedEOF)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ready, err := mounted(tc.reader, "/storage")
			if ready || err == nil {
				t.Fatalf("mounted() = %t, %v; want false and an error", ready, err)
			}
		})
	}
}

func TestCheckRequiresOneAbsolutePath(t *testing.T) {
	for _, args := range [][]string{nil, {"relative"}, {"/one", "/two"}} {
		var stderr bytes.Buffer
		if code := check(args, &stderr); code != 2 || stderr.Len() == 0 {
			t.Fatalf("check(%q) = %d, %q; want usage error", args, code, stderr.String())
		}
	}
}
