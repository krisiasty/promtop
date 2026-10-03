package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/krisiasty/promtop/internal/format"
)

var plainStyle = lipgloss.NewStyle()

// row builds one terminal line from styled segments of known cell width.
type row struct {
	b     strings.Builder
	n     int
	bg    color.Color // optional background applied to every segment
	faint bool
}

func (r *row) style(st lipgloss.Style) lipgloss.Style {
	if r.bg != nil {
		st = st.Background(r.bg)
	}
	if r.faint {
		st = st.Faint(true)
	}
	return st
}

// add appends plain text s rendered in st.
func (r *row) add(s string, st lipgloss.Style) *row {
	if s == "" {
		return r
	}
	s = oneLine(s)
	r.b.WriteString(r.style(st).Render(s))
	r.n += format.Width(s)
	return r
}

// left appends s truncated (…) and padded to exactly w cells.
func (r *row) left(s string, w int, st lipgloss.Style) *row {
	return r.add(format.PadRight(format.Truncate(oneLine(s), w), w), st)
}

// right appends s truncated (…) and right-aligned in exactly w cells.
func (r *row) right(s string, w int, st lipgloss.Style) *row {
	return r.add(format.PadLeft(format.Truncate(oneLine(s), w), w), st)
}

var lineEscaper = strings.NewReplacer("\n", `\n`, "\t", `\t`)

// oneLine shows a newline (decoded from a label value's \n escape) or a tab
// as its escape, so scraped text can never add lines to the frame or cells
// of unknown width, and "a b" stays distinguishable from "a<tab>b".
func oneLine(s string) string {
	if !strings.ContainsAny(s, "\n\t") {
		return s
	}
	return lineEscaper.Replace(s)
}

// space appends n blank cells (with the row background).
func (r *row) space(n int) *row {
	if n > 0 {
		r.add(strings.Repeat(" ", n), plainStyle)
	}
	return r
}

// raw appends an already-styled string.
func (r *row) raw(s string) *row {
	r.b.WriteString(s)
	r.n += format.Width(s)
	return r
}

func (r *row) width() int { return r.n }

// String pads the row to w cells and cuts anything beyond.
func (r *row) String(w int) string {
	if r.n < w {
		r.space(w - r.n)
	}
	return fit(r.b.String(), w)
}

// fit forces a (possibly styled) line to exactly w cells.
func fit(s string, w int) string {
	n := format.Width(s)
	if n > w {
		s = lipgloss.NewStyle().MaxWidth(w).Render(s)
		n = format.Width(s)
	}
	if n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}

// box is a rounded panel with an inset title and optional markers embedded in
// the top-right and bottom-right border ("▲ 3 more", "▼ 14 more", "82 / 83").
type box struct {
	w, pad        int
	title         string
	titleStyle    lipgloss.Style
	topRight      string
	topRightStyle lipgloss.Style
	bottomRight   string
	markStyle     lipgloss.Style
	border        lipgloss.Style
	lines         []string
}

func (b box) inner() int { return b.w - 2 - 2*b.pad }

func (b box) render() []string {
	bs := b.border
	out := make([]string, 0, len(b.lines)+2)
	top := &row{}
	top.add("╭─", bs)
	used := 3
	title := ""
	if b.title != "" {
		room := b.w - 8
		if b.topRight != "" {
			room -= format.Width(b.topRight) + 3
		}
		title = format.Truncate(b.title, max(1, room))
		used += format.Width(title) + 2
	}
	if b.topRight != "" {
		used += format.Width(b.topRight) + 3
	}
	if title != "" {
		top.space(1).add(title, b.titleStyle).space(1)
	}
	top.add(strings.Repeat("─", max(0, b.w-used)), bs)
	if b.topRight != "" {
		top.space(1).add(b.topRight, b.topRightStyle).space(1).add("─", bs)
	}
	top.add("╮", bs)
	out = append(out, top.String(b.w))
	for _, ln := range b.lines {
		r := &row{}
		r.add("│", bs).space(b.pad).raw(fit(ln, b.inner())).space(b.pad).add("│", bs)
		out = append(out, r.String(b.w))
	}
	bot := &row{}
	bot.add("╰", bs)
	if b.bottomRight != "" {
		bot.add(strings.Repeat("─", max(0, b.w-5-format.Width(b.bottomRight))), bs).
			space(1).add(b.bottomRight, b.markStyle).space(1).add("─", bs)
	} else {
		bot.add(strings.Repeat("─", max(0, b.w-2)), bs)
	}
	bot.add("╯", bs)
	out = append(out, bot.String(b.w))
	return out
}

// grid lays key/value pairs out row-major in ncol columns three spaces apart.
// Keys are left-aligned in keyW cells, values right-aligned in the rest.
func grid(items [][2]string, ncol, width, keyW int, kst, vst lipgloss.Style) []string {
	const gap = 3
	avail := width - gap*(ncol-1)
	base, extra := avail/ncol, avail%ncol
	var out []string
	for i := 0; i < len(items); i += ncol {
		r := &row{}
		for c := 0; c < ncol; c++ {
			cw := base
			if c < extra {
				cw++
			}
			if c > 0 {
				r.space(gap)
			}
			if i+c >= len(items) {
				r.space(cw)
				continue
			}
			r.left(items[i+c][0], keyW, kst).space(1).right(items[i+c][1], cw-keyW-1, vst) // a key never touches its value
		}
		out = append(out, r.String(width))
	}
	return out
}

// center places lines in the middle of a w×h area (lines shorter than h).
func center(lines []string, w, h int) []string {
	out := make([]string, h)
	top := max(0, (h-len(lines))/2)
	for i, ln := range lines {
		if top+i >= h {
			break
		}
		out[top+i] = strings.Repeat(" ", max(0, (w-format.Width(ln))/2)) + ln
	}
	return out
}

// overlay draws box over base at (x, y). The result keeps base's height
// (the popup is clipped) and every line is exactly w cells.
func overlay(base, box []string, x, y, w int) []string {
	bg := lipgloss.NewLayer(strings.Join(base, "\n"))
	fg := lipgloss.NewLayer(strings.Join(box, "\n")).X(x).Y(y).Z(1)
	out := strings.Split(lipgloss.NewCompositor(bg, fg).Render(), "\n")
	for len(out) < len(base) {
		out = append(out, "")
	}
	out = out[:len(base)]
	for i := range out {
		out[i] = fit(out[i], w)
	}
	return out
}
