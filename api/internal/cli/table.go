// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 ponack

package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

func NewTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func healthLabel(score int) string {
	switch {
	case score < 0:
		return "unknown"
	case score >= 80:
		return "healthy"
	case score >= 50:
		return "degraded"
	default:
		return "unhealthy"
	}
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func kvLine(w io.Writer, key, value string) {
	fmt.Fprintf(w, "%-22s %s\n", key+":", value)
}

func strOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func planSummary(add, change, destroy *int) string {
	if add == nil && change == nil && destroy == nil {
		return ""
	}
	a, c, d := 0, 0, 0
	if add != nil {
		a = *add
	}
	if change != nil {
		c = *change
	}
	if destroy != nil {
		d = *destroy
	}
	parts := []string{}
	if a > 0 {
		parts = append(parts, fmt.Sprintf("+%d", a))
	}
	if c > 0 {
		parts = append(parts, fmt.Sprintf("~%d", c))
	}
	if d > 0 {
		parts = append(parts, fmt.Sprintf("-%d", d))
	}
	return strings.Join(parts, " ")
}
