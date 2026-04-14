// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package quilt

import "testing"

func TestNewManagerPaths(t *testing.T) {
	tests := []struct {
		name           string
		quiltPatches   string
		quiltSeries    string
		wantPatchesDir string
		wantSeriesFile string
	}{
		{
			name:           "defaults",
			wantPatchesDir: "/root/patches",
			wantSeriesFile: "/root/patches/series",
		},
		{
			name:           "custom patches dir",
			quiltPatches:   "custom-patches",
			wantPatchesDir: "/root/custom-patches",
			wantSeriesFile: "/root/custom-patches/series",
		},
		{
			name:           "custom series file",
			quiltSeries:    "my-series",
			wantPatchesDir: "/root/patches",
			wantSeriesFile: "/root/patches/my-series",
		},
		{
			name:           "both custom",
			quiltPatches:   "p",
			quiltSeries:    "s",
			wantPatchesDir: "/root/p",
			wantSeriesFile: "/root/p/s",
		},
		{
			name:           "patches abspath",
			quiltPatches:   "/tmp/abs-patches",
			wantPatchesDir: "/tmp/abs-patches",
			wantSeriesFile: "/tmp/abs-patches/series",
		},
		{
			name:           "series abspath",
			quiltSeries:    "/tmp/abs-series",
			wantPatchesDir: "/root/patches",
			wantSeriesFile: "/tmp/abs-series",
		},
		{
			name:           "both abspath",
			quiltPatches:   "/tmp/p",
			quiltSeries:    "/tmp/s",
			wantPatchesDir: "/tmp/p",
			wantSeriesFile: "/tmp/s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.quiltPatches != "" {
				t.Setenv("QUILT_PATCHES", tt.quiltPatches)
			} else {
				t.Setenv("QUILT_PATCHES", "")
			}
			if tt.quiltSeries != "" {
				t.Setenv("QUILT_SERIES", tt.quiltSeries)
			} else {
				t.Setenv("QUILT_SERIES", "")
			}

			m := NewManager("/root")

			if got := m.PatchesDir(); got != tt.wantPatchesDir {
				t.Errorf("PatchesDir() = %q, want %q", got, tt.wantPatchesDir)
			}
			if got := m.SeriesFile(); got != tt.wantSeriesFile {
				t.Errorf("SeriesFile() = %q, want %q", got, tt.wantSeriesFile)
			}
		})
	}
}

func TestCanonicalizeDiffPaths(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		rootRelRepo string
		want        string
	}{
		{
			name:        "already canonical with a/b prefix",
			content:     "--- a/file.py\n+++ b/file.py\n",
			rootRelRepo: "foo",
			want:        "--- a/file.py\n+++ b/file.py\n",
		},
		{
			name:        "already canonical bare",
			content:     "--- file.py\n+++ file.py\n",
			rootRelRepo: "foo",
			want:        "--- file.py\n+++ file.py\n",
		},
		{
			name:        "repo-relative with a/b prefix",
			content:     "--- a/foo/file.py\n+++ b/foo/file.py\n",
			rootRelRepo: "foo",
			want:        "--- a/file.py\n+++ b/file.py\n",
		},
		{
			name:        "repo-relative bare",
			content:     "--- foo/file.py\n+++ foo/file.py\n",
			rootRelRepo: "foo",
			want:        "--- file.py\n+++ file.py\n",
		},
		{
			name:        "nested rootRelRepo",
			content:     "--- a/path/to/import/x.py\n+++ b/path/to/import/x.py\n",
			rootRelRepo: "path/to/import",
			want:        "--- a/x.py\n+++ b/x.py\n",
		},
		{
			name:        "mixed --- and +++ in one patch",
			content:     "--- a/foo/file.py\n+++ b/foo/file.py\n@@ -1,1 +1,1 @@\n-old\n+new\n",
			rootRelRepo: "foo",
			want:        "--- a/file.py\n+++ b/file.py\n@@ -1,1 +1,1 @@\n-old\n+new\n",
		},
		{
			name:        "substring but not prefix",
			content:     "--- a/other/foo/file.py\n+++ b/other/foo/file.py\n",
			rootRelRepo: "foo",
			want:        "--- a/other/foo/file.py\n+++ b/other/foo/file.py\n",
		},
		{
			name:        "body context line with dashes",
			content:     "--- a/foo/file.py\n+++ b/foo/file.py\n@@ -1,2 +1,2 @@\n-some deleted line\n+some added line\n",
			rootRelRepo: "foo",
			want:        "--- a/file.py\n+++ b/file.py\n@@ -1,2 +1,2 @@\n-some deleted line\n+some added line\n",
		},
		{
			name:        "rootRelRepo is dot",
			content:     "--- a/file.py\n+++ b/file.py\n",
			rootRelRepo: ".",
			want:        "--- a/file.py\n+++ b/file.py\n",
		},
		{
			name:        "rootRelRepo empty",
			content:     "--- a/file.py\n+++ b/file.py\n",
			rootRelRepo: "",
			want:        "--- a/file.py\n+++ b/file.py\n",
		},
		{
			name:        "rootRelRepo with trailing slash",
			content:     "--- a/foo/file.py\n+++ b/foo/file.py\n",
			rootRelRepo: "foo/",
			want:        "--- a/file.py\n+++ b/file.py\n",
		},
		{
			name: "multi-file patch",
			content: "--- a/foo/file1.py\n+++ b/foo/file1.py\n@@ -1,1 +1,1 @@\n-old\n+new\n" +
				"--- a/foo/file2.py\n+++ b/foo/file2.py\n@@ -1,1 +1,1 @@\n-old2\n+new2\n",
			rootRelRepo: "foo",
			want: "--- a/file1.py\n+++ b/file1.py\n@@ -1,1 +1,1 @@\n-old\n+new\n" +
				"--- a/file2.py\n+++ b/file2.py\n@@ -1,1 +1,1 @@\n-old2\n+new2\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalizeDiffPaths(tt.content, tt.rootRelRepo)
			if got != tt.want {
				t.Errorf("CanonicalizeDiffPaths() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}
