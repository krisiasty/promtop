package ui

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/krisiasty/promtop/internal/format"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// plain strips ANSI escape sequences.
func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

var red = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))

func TestRowFixedWidth(t *testing.T) {
	r := &row{}
	r.add("ab", red).left("NAME", 6, red).right("12", 5, red)
	got := r.String(20)
	if format.Width(got) != 20 || plain(got) != "abNAME     12       " {
		t.Errorf("row = %q (%d cells)", plain(got), format.Width(got))
	}
	long := (&row{}).add(strings.Repeat("x", 30), red).String(10)
	if format.Width(long) != 10 {
		t.Errorf("overflow not cut: %d cells", format.Width(long))
	}
	if got := plain((&row{}).left("http_requests_total", 10, red).String(10)); got != "http_requ…" {
		t.Errorf("left truncation = %q", got)
	}
}

func TestBoxRender(t *testing.T) {
	b := box{w: 30, pad: 1, title: "stats", topRight: "▲ 2 more", bottomRight: "▼ 14 more", lines: []string{"hello"}}
	got := b.render()
	want := []string{
		"╭─ stats " + strings.Repeat("─", 9) + " ▲ 2 more ─╮",
		"│ hello" + strings.Repeat(" ", 21) + " │",
		"╰" + strings.Repeat("─", 16) + " ▼ 14 more ─╯",
	}
	for i := range want {
		if plain(got[i]) != want[i] || format.Width(got[i]) != 30 {
			t.Errorf("line %d = %q, want %q", i, plain(got[i]), want[i])
		}
	}
	if b.inner() != 26 {
		t.Errorf("inner = %d", b.inner())
	}
}

func TestGrid(t *testing.T) {
	lines := grid([][2]string{{"current", "269"}, {"min", "186"}, {"previous", "262"}}, 2, 59, 10, red, red)
	if len(lines) != 2 {
		t.Fatalf("rows = %d", len(lines))
	}
	if got := plain(lines[0]); got != "current                  269   min                      186" {
		t.Errorf("grid row = %q", got)
	}
	if format.Width(lines[1]) != 59 {
		t.Errorf("short row width = %d", format.Width(lines[1]))
	}
}

func TestCenterAndOverlay(t *testing.T) {
	c := center([]string{"ab"}, 6, 3)
	if len(c) != 3 || c[1] != "  ab" || c[0] != "" {
		t.Errorf("center = %q", c)
	}
	base := []string{"aaaaaaaaaa", "bbbbbbbbbb", "cccccccccc"}
	got := overlay(base, []string{"XX", "YY", "ZZ", "WW"}, 3, 1, 10)
	want := []string{"aaaaaaaaaa", "bbbXXbbbbb", "cccYYccccc"}
	for i := range want {
		if plain(got[i]) != want[i] {
			t.Errorf("overlay line %d = %q, want %q", i, plain(got[i]), want[i])
		}
	}
	if len(got) != 3 {
		t.Errorf("overlay must clip to the base height, got %d lines", len(got))
	}
}
