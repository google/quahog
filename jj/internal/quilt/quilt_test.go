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
