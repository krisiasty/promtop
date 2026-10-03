package ui

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
)

type seg struct {
	text string
	st   lipgloss.Style
}

type rawRow struct {
	num  string // line number; "" on continuation rows
	cont bool
	segs []seg
}

func (m *Model) viewRaw(lay layout) body {
	th := &m.th
	rows := m.rawRows(lay.w - 11)
	vis := lay.h - 3
	off := clamp(m.rawOff, 0, max(0, len(rows)-vis))
	title := "last scrape · text exposition format"
	if m.rawWrap {
		title += " · wrap"
	}
	b := box{w: lay.w, title: title, titleStyle: th.dim, border: th.line, topRightStyle: th.cyan, markStyle: th.cyan}
	if off > 0 {
		b.topRight = "▲ " + format.Int(off) + " more"
	}
	if rest := len(rows) - off - vis; rest > 0 {
		b.bottomRight = "▼ " + format.Int(rest) + " more"
	}
	for _, r := range rows[off:min(len(rows), off+vis)] {
		ln := &row{}
		gutter := "  "
		if r.cont {
			gutter = " ↪"
		}
		ln.right(r.num, 5, th.axis).add(gutter, th.gutter)
		for _, s := range r.segs {
			ln.add(s.text, s.st)
		}
		b.lines = append(b.lines, ln.String(b.inner()))
	}
	lines := 0
	if p := m.st.Last(); p != nil {
		lines = len(p.Lines)
	}
	status := "rows " + strconv.Itoa(min(len(rows), off+1)) + "–" + strconv.Itoa(min(len(rows), off+vis)) + " of " +
		strconv.Itoa(len(rows)) + " · " + strconv.Itoa(lines) + " lines · changed samples highlighted"
	return body{lines: b.render(), status: status, statusStyle: th.fg}
}

func (m *Model) rawMaxOff() int {
	if m.w < minW || m.h < minH {
		return 0
	}
	return max(0, len(m.rawRows(m.w-11))-(m.bodyHeight()-3))
}

type rawCache struct {
	seq        uint64
	w          int
	wrap, dark bool
	ok         bool
	rows       []rawRow
}

// rawRows lays the payload out in visual rows of rw content cells, cached
// until the next scrape or a width, wrap or theme change (rendering and
// every scroll step need it, and a large payload is expensive to lay out).
func (m *Model) rawRows(rw int) []rawRow {
	c := &m.raw
	if !c.ok || c.seq != m.st.Seq() || c.w != rw || c.wrap != m.rawWrap || c.dark != m.th.dark {
		*c = rawCache{seq: m.st.Seq(), w: rw, wrap: m.rawWrap, dark: m.th.dark, ok: true, rows: m.layoutRaw(rw)}
	}
	return c.rows
}

func (m *Model) layoutRaw(rw int) []rawRow {
	if rw <= 0 {
		return nil
	}
	var out []rawRow
	for i, segs := range m.rawSegments() {
		num := strconv.Itoa(i + 1)
		if m.rawWrap {
			for j, part := range wrapSegs(segs, rw) {
				r := rawRow{segs: part}
				if j == 0 {
					r.num = num
				} else {
					r.cont = true
				}
				out = append(out, r)
			}
			continue
		}
		total := 0
		for _, s := range segs {
			total += format.Width(s.text)
		}
		if total > rw {
			segs = append(wrapSegs(segs, rw-1)[0], seg{"›", m.th.amber})
		}
		out = append(out, rawRow{num: num, segs: segs})
	}
	return out
}

// rawSegments colours each payload line; changed sample values are
// highlighted and followed by their delta.
func (m *Model) rawSegments() [][]seg {
	th := &m.th
	p := m.st.Last()
	if p == nil {
		return nil
	}
	bySample := map[int]*expo.Sample{}
	for _, f := range p.Families {
		for i := range f.Samples {
			bySample[f.Samples[i].Line] = &f.Samples[i]
		}
	}
	out := make([][]seg, len(p.Lines))
	for i, raw := range p.Lines {
		ln := strings.ReplaceAll(raw, "\t", " ") // a blank in the syntax
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "# TYPE"):
			out[i] = []seg{{ln, th.magenta}}
		case t == "" || strings.HasPrefix(t, "#"):
			out[i] = []seg{{ln, th.dim}}
		default:
			out[i] = m.sampleSegments(ln, bySample[i])
		}
	}
	return out
}

func (m *Model) sampleSegments(ln string, s *expo.Sample) []seg {
	th := &m.th
	end := strings.IndexAny(ln, "{ ")
	if end < 0 {
		return []seg{{ln, th.cyan}}
	}
	name, rest := ln[:end], ln[end:]
	labels := ""
	if l := strings.TrimLeft(rest, " "); strings.HasPrefix(l, "{") { // blanks may precede the labels
		e := len(rest) - len(l) + labelsEnd(l)
		labels, rest = rest[:e], rest[e:]
	}
	segs := []seg{{name, th.cyan}, {labels, th.prev}}
	if s == nil {
		return append(segs, seg{rest, th.fg})
	}
	prev, ok := m.st.PrevValue(expo.Key(s.Name, s.Labels))
	if !ok || prev == s.Value || math.IsNaN(prev) || math.IsNaN(s.Value) {
		return append(segs, seg{rest, th.fg})
	}
	return append(segs, seg{rest, th.changed}, seg{"   " + format.RawDelta(s.Value-prev), th.dim})
}

// labelsEnd returns the index just past the label set's closing brace,
// ignoring braces inside quoted values.
func labelsEnd(s string) int {
	quoted := false
	for i := 1; i < len(s); i++ {
		switch {
		case quoted && s[i] == '\\':
			i++
		case s[i] == '"':
			quoted = !quoted
		case !quoted && s[i] == '}':
			return i + 1
		}
	}
	return len(s)
}

// wrapSegs splits segments into rows of at most w cells. A rune wider than
// an entire row becomes U+FFFD; nonpositive widths return one empty row.
func wrapSegs(segs []seg, w int) [][]seg {
	rows := [][]seg{nil}
	if w <= 0 {
		return rows
	}
	n := 0
	for _, s := range segs {
		t := s.text
		for t != "" {
			if n >= w {
				rows, n = append(rows, nil), 0
			}
			part := format.Cut(t, w-n)
			if part == "" { // a wide rune does not fit at the end of the row
				if n > 0 {
					rows, n = append(rows, nil), 0
					continue
				}
				_, size := utf8.DecodeRuneInString(t)
				part, t = string(utf8.RuneError), t[size:]
			} else {
				t = t[len(part):]
			}
			rows[len(rows)-1] = append(rows[len(rows)-1], seg{part, s.st})
			n += format.Width(part)
		}
	}
	return rows
}
