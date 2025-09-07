// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Package-level variables for regexes make the parser stateless.
var (
	commandRegex = regexp.MustCompile(`^\$ `)
	commentRegex = regexp.MustCompile(`^\$ #`)
)

// ScenarioTest represents a simple shell-style test.
type ScenarioTest struct {
	Name        string
	Description string
	Setup       map[string]string
	Script      []Step
}

// Step represents one item in the test script, either a command or a comment.
type Step struct {
	IsCommand      bool
	Content        string // The raw command string or comment text
	ExpectedOutput string
}

// ParseFile parses a simple shell-style test file.
func ParseFile(filename string) (*ScenarioTest, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read test file: %w", err)
	}
	return ParseContent(string(content))
}

// ParseContent parses test content from a string.
func ParseContent(content string) (*ScenarioTest, error) {
	test := &ScenarioTest{
		Setup:  make(map[string]string),
		Script: make([]Step, 0),
	}

	lines := strings.Split(content, "\n")
	var currentSection string
	var inTestBlock, inSetupBlock bool
	var yamlBuffer, outputBuffer, cmdBuffer strings.Builder
	var currentStep *Step

	// Helper to finalize the previous step
	finalizeStep := func() {
		if currentStep != nil && currentStep.IsCommand {
			currentStep.ExpectedOutput = strings.TrimSuffix(outputBuffer.String(), "\n")
			test.Script = append(test.Script, *currentStep)
		}
		outputBuffer.Reset()
		currentStep = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inSetupBlock {
				// End of setup YAML block, parse it
				if err := yaml.Unmarshal([]byte(yamlBuffer.String()), &test.Setup); err != nil {
					return nil, fmt.Errorf("failed to parse setup YAML: %w", err)
				}
				inSetupBlock = false
			} else if inTestBlock {
				// End of test script block
				finalizeStep()
				inTestBlock = false
			} else if currentSection == "setup" {
				inSetupBlock = true
				yamlBuffer.Reset()
			} else if currentSection == "test" {
				inTestBlock = true
			}
			continue
		}

		if inSetupBlock {
			yamlBuffer.WriteString(line + "\n")
			continue
		}

		if inTestBlock {
			if commandRegex.MatchString(line) {
				// This is a new command or comment line, finalize the previous step
				finalizeStep()

				if commentRegex.MatchString(line) {
					// It's a comment, add it directly
					test.Script = append(test.Script, Step{
						IsCommand: false,
						Content:   strings.TrimSpace(strings.TrimPrefix(line, "$ #")),
					})
				} else {
					// It's a new command
					cmdContent := strings.TrimSpace(strings.TrimPrefix(line, "$"))
					if strings.HasSuffix(cmdContent, "»") {
						// Single-line command
						cmdContent = strings.TrimSuffix(cmdContent, "»")
						currentStep = &Step{IsCommand: true, Content: cmdContent}
					} else {
						// Start of a multi-line command
						cmdBuffer.WriteString(cmdContent + "\n")
						currentStep = &Step{IsCommand: true} // Content is pending
					}
				}
			} else {
				// This line is either output or part of a multi-line command
				if currentStep != nil && currentStep.IsCommand {
					if currentStep.Content == "" { // We are building a multi-line command
						cmdBuffer.WriteString(line + "\n")
						if strings.HasSuffix(line, "»") {
							// End of multi-line command
							fullCmd := strings.TrimSuffix(cmdBuffer.String(), "»\n")
							currentStep.Content = fullCmd
							cmdBuffer.Reset()
						}
					} else { // We are building expected output
						outputBuffer.WriteString(line + "\n")
					}
				}
			}
			continue
		}

		// --- Metadata Parsing ---
		if strings.HasPrefix(line, "# ") && test.Name == "" {
			test.Name = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		} else if test.Name != "" && test.Description == "" && strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "**") {
			test.Description = strings.TrimSpace(line)
		} else if strings.HasPrefix(line, "**Setup:**") {
			currentSection = "setup"
		} else if strings.HasPrefix(line, "**Test:**") {
			currentSection = "test"
		}
	}

	return test, nil
}
