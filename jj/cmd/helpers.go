// Copyright 2025 Google LLC
// SPDX-License-Identifier: Apache-2.0

package cmd

func pluralize[T any](s []T, plural string) string {
	if len(s) > 1 {
		return plural
	}
	return ""
}
