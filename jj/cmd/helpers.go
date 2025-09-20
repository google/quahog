// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package cmd

import "io"

type IO struct {
	Out io.Writer
	Err io.Writer
}

func pluralize[T any](s []T, plural string) string {
	if len(s) > 1 {
		return plural
	}
	return ""
}
