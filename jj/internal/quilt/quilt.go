// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package quilt

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type PatchInfo struct {
	Name string
}

type Manager struct {
	rootPath   string
	patchesDir string
	seriesFile string
}

// NewManager creates a new Manager for the given root path.
//
// The patches directory and series file names can be overridden via the
// QUILT_PATCHES and QUILT_SERIES environment variables respectively. Absolute
// paths are used as-is, matching quilt(1) behavior:
// https://man7.org/linux/man-pages/man1/quilt.1.html
func NewManager(rootPath string) *Manager {
	patchesDirName := "patches"
	if v := os.Getenv("QUILT_PATCHES"); v != "" {
		patchesDirName = v
	}
	seriesFileName := "series"
	if v := os.Getenv("QUILT_SERIES"); v != "" {
		seriesFileName = v
	}
	patchesDir := patchesDirName
	if !filepath.IsAbs(patchesDir) {
		patchesDir = filepath.Join(rootPath, patchesDirName)
	}
	seriesFile := seriesFileName
	if !filepath.IsAbs(seriesFile) {
		seriesFile = filepath.Join(patchesDir, seriesFileName)
	}

	return &Manager{
		rootPath:   rootPath,
		patchesDir: patchesDir,
		seriesFile: seriesFile,
	}
}

// PatchesDir returns the resolved patches directory path.
func (q *Manager) PatchesDir() string {
	return q.patchesDir
}

// SeriesFile returns the resolved series file path.
func (q *Manager) SeriesFile() string {
	return q.seriesFile
}

// GetPatchesToPop returns patches that should be popped based on count or all flag
func (q *Manager) GetPatchesToPop(count int, all bool) ([]PatchInfo, error) {
	patches, err := q.readSeries()
	if err != nil {
		return nil, err
	}

	if len(patches) == 0 {
		return []PatchInfo{}, nil
	}

	if all {
		count = len(patches)
	}

	if count > len(patches) {
		count = len(patches)
	}

	if count <= 0 {
		return []PatchInfo{}, nil
	}

	// Return patches from the end (most recent patches first)
	result := make([]PatchInfo, count)
	startIdx := len(patches) - count

	for i := range count {
		result[i] = PatchInfo{Name: patches[startIdx+i]}
	}

	return result, nil
}

// ReadPatch reads a patch file and separates description from diff content
func (q *Manager) ReadPatch(patchName string) (content, description string, err error) {
	patchPath := filepath.Join(q.patchesDir, patchName)

	data, err := os.ReadFile(patchPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read patch file: %w", err)
	}

	content, description = q.separatePatchDescription(string(data))
	return content, description, nil
}

// RemovePatch removes a patch file and updates the series file
func (q *Manager) RemovePatch(patchName string) error {
	patchPath := filepath.Join(q.patchesDir, patchName)

	// Remove the patch file
	if err := os.Remove(patchPath); err != nil {
		return fmt.Errorf("failed to remove patch file: %w", err)
	}

	// Update series file to remove this patch
	if err := q.removeFromSeries(patchName); err != nil {
		return fmt.Errorf("failed to update series file: %w", err)
	}

	return nil
}

// WritePatchFiles writes patch files and updates the series file
func (q *Manager) WritePatchFiles(names []string, content []string) error {
	// Write each patch file
	for i, name := range names {
		patchPath := filepath.Join(q.patchesDir, name)
		if err := os.WriteFile(patchPath, []byte(content[i]), 0644); err != nil {
			return fmt.Errorf("failed to write patch %s: %w", name, err)
		}
	}
	// Update series file
	if err := q.addToSeries(names); err != nil {
		return fmt.Errorf("failed to update series file: %w", err)
	}
	return nil
}

// readSeries reads the series file and returns the list of patches
func (q *Manager) readSeries() ([]string, error) {
	file, err := os.Open(q.seriesFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer file.Close()

	var patches []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line != "" && !strings.HasPrefix(line, "#") {
			patches = append(patches, line)
		}
	}

	return patches, scanner.Err()
}

// addToSeries adds patches to the series file
func (q *Manager) addToSeries(patchNames []string) error {
	file, err := os.OpenFile(q.seriesFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, name := range patchNames {
		if _, err := file.WriteString(name + "\n"); err != nil {
			return err
		}
	}

	return nil
}

// removeFromSeries removes a patch from the series file
func (q *Manager) removeFromSeries(patchName string) error {
	patches, err := q.readSeries()
	if err != nil {
		return err
	}

	// Remove the specified patch
	var newPatches []string
	for _, patch := range patches {
		if patch != patchName {
			newPatches = append(newPatches, patch)
		}
	}

	// Rewrite the series file
	return q.writeSeries(newPatches)
}

// writeSeries writes the complete series file
func (q *Manager) writeSeries(patches []string) error {
	file, err := os.Create(q.seriesFile)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, patch := range patches {
		if _, err := file.WriteString(patch + "\n"); err != nil {
			return err
		}
	}
	return nil
}

// separatePatchDescription separates patch description from diff content
// This is similar to the Python version's _separatepatchdescription function
func (*Manager) separatePatchDescription(patchContent string) (content, description string) {
	lines := strings.Split(patchContent, "\n")

	var descLines []string
	diffStartIdx := 0

	// Find where the diff starts
	for i, line := range lines {
		if strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "diff --git") ||
			strings.HasPrefix(line, "rename from") ||
			strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "Index: ") {
			diffStartIdx = i
			if strings.HasPrefix(line, "Index: ") && i+2 < len(lines) {
				diffStartIdx = i + 2
			}
			break
		}
		descLines = append(descLines, strings.TrimRight(line, " \t"))
	}

	// Extract diff content
	if diffStartIdx < len(lines) {
		diffLines := lines[diffStartIdx:]
		content = strings.TrimLeft(strings.Join(diffLines, "\n"), " \n\t")
	}

	// Extract description
	description = strings.TrimRight(strings.Join(descLines, "\n"), " \n\t")

	return content, description
}

// copyFile copies a file from src to dst
func (*Manager) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	return err
}

// CanonicalizeDiffPaths rewrites diff header paths to be subdir-relative.
// Accepts patches with paths in either form:
//
//	--- a/<rootRelRepo>/file  →  --- a/file
//	--- <rootRelRepo>/file    →  --- file
//	--- a/file                →  unchanged
//
// The optional a/ or b/ prefix is preserved. The trailing slash after
// rootRelRepo is consumed, avoiding the double-slash artifact.
func CanonicalizeDiffPaths(content, rootRelRepo string) string {
	rootRelRepo = strings.TrimSuffix(filepath.ToSlash(rootRelRepo), "/")
	if rootRelRepo == "" || rootRelRepo == "." {
		return content
	}
	// Match --- or +++ at start of line, then whitespace, optional a/ or b/ prefix,
	// then the rootRelRepo followed by a slash.
	re := regexp.MustCompile(`(?m)^(---|\+\+\+)(\s+)((?:a|b)/)?` + regexp.QuoteMeta(rootRelRepo) + `/`)
	return re.ReplaceAllString(content, "${1}${2}${3}")
}

// ApplyPatch applies a patch to the working directory using git apply.
func ApplyPatch(patchContent, rootPath string) error {
	cmd := exec.Command("git", "apply", "-")
	cmd.Dir = rootPath
	cmd.Stdin = bytes.NewBufferString(patchContent)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to apply patch: %s\nstderr: %s", err, stderr.String())
	}
	return nil
}

// ApplyPatchReverse applies a patch in reverse to remove changes using git apply.
func ApplyPatchReverse(patchContent, rootPath string) error {
	cmd := exec.Command("git", "apply", "--reverse", "-")
	cmd.Dir = rootPath
	cmd.Stdin = bytes.NewBufferString(patchContent)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to reverse patch: %s\nstderr: %s", err, stderr.String())
	}
	return nil
}

// FormatGitDiff converts jj diff output to quilt format.
func FormatGitDiff(diff, rootPath string) string {
	// Normalize the rootPath by removing any trailing slash, just like the Python version.
	rootPrefix := strings.TrimSuffix(rootPath, "/")
	var result strings.Builder
	// Use a scanner to properly handle different line endings and preserve content.
	scanner := bufio.NewScanner(strings.NewReader(diff))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index ") {
			// Skip these lines entirely as Quilt's default diff generation doesn't
			// include them (as it doesn't use git to compute diffs).
			continue
		} else if strings.HasPrefix(line, "--- a/"+rootPrefix) {
			// Quilts '-p ab' flag creates diffs which don't contain the Quilt root
			// directory, and instead use a/ and b/ for the root of the base and changed
			// file respectively.
			path := strings.TrimPrefix(line, "--- a/"+rootPrefix+"/")
			result.WriteString("--- a/" + path + "\n")
		} else if strings.HasPrefix(line, "+++ b/"+rootPrefix) {
			// Quilts '-p ab' flag creates diffs which don't contain the Quilt root
			// directory, and instead use a/ and b/ for the root of the base and changed
			// file respectively.
			path := strings.TrimPrefix(line, "+++ b/"+rootPrefix+"/")
			result.WriteString("+++ b/" + path + "\n")
		} else if strings.HasPrefix(line, "@@ ") {
			// Quilt generates diffs that strip trailing whitespace from context lines.
			// jj's diffs keep the trailing whitespace by default, so we remove that
			// here. This seems to only be configured for context lines, not all lines.
			result.WriteString(strings.TrimRight(line, " \t") + "\n")
		} else {
			result.WriteString(line + "\n")
		}
	}
	return result.String()
}
