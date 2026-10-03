package format

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Width is the display width of s in terminal cells; ANSI sequences count 0.
func Width(s string) int { return lipgloss.Width(s) }

// Truncate cuts plain text to at most w cells, ending with "…" when cut.
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	return Cut(s, w-1) + "…"
}

// Cut cuts plain text to at most w cells without adding an ellipsis.
func Cut(s string, w int) string {
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := Width(string(r))
		if used+rw > w {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String()
}

// PadRight appends spaces until s is w cells wide (s may contain ANSI).
func PadRight(s string, w int) string {
	if d := w - Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// PadLeft prepends spaces until s is w cells wide (s may contain ANSI).
func PadLeft(s string, w int) string {
	if d := w - Width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}
