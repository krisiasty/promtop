package ui

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/stats"
	"github.com/krisiasty/promtop/internal/store"
)

const (
	familyPaneW    = 44
	seriesListMax  = 12
	labelValuesMax = 8
	pivotRowsMax   = 12
)

// familyPaneWidth gives metric names room to fit while preserving at least
// the original 99-cell detail pane at standard terminal widths.
func familyPaneWidth(w int, fams []*store.FamilyInfo) int {
	if w <= 146 {
		return familyPaneW
	}
	nameW, countW := 0, 5
	for _, f := range fams {
		nameW = max(nameW, format.Width(f.Name))
		countW = max(countW, format.Width(strconv.Itoa(f.Samples)))
	}
	needed := 2 + 2 + nameW + 4 + countW // borders, cursor, name, type, count
	return clamp(needed, familyPaneW, min(w/2, w-102))
}

func (m *Model) viewSeries(lay layout) body {
	th := &m.th
	fams := m.st.Families()
	total := 0
	for _, f := range fams {
		total += f.Samples
	}
	b := body{status: format.Int(len(fams)) + " families · " + format.Int(total) + " series", statusStyle: th.fg}
	if len(fams) == 0 {
		b.lines = center([]string{th.dim.Render("no metric families")}, lay.w, lay.h)
		return b
	}
	fi := clamp(m.famIdx, 0, len(fams)-1)
	vis := lay.h - 4
	switch {
	case lay.w >= 146:
		paneW := familyPaneWidth(lay.w, fams)
		left := m.familyBox(fams, fi, paneW, vis, false)
		right := m.familyDetail(fams[fi], lay.w-paneW-3, true)
		for i := 0; i < lay.h; i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			b.lines = append(b.lines, fit(l, paneW)+"   "+r)
		}
	case !m.famOpen:
		b.lines = m.familyBox(fams, fi, lay.w, vis, true)
	default:
		b.lines = append([]string{(&row{}).add("esc", th.cyan).add(" ‹ families", th.dim).String(lay.w)},
			m.familyDetail(fams[fi], lay.w, false)...)
	}
	return b
}

func (m *Model) familyBox(fams []*store.FamilyInfo, fi, w, vis int, narrow bool) []string {
	th := &m.th
	off := clampOffset(fi, vis, len(fams))
	b := box{w: w, title: "families · " + strconv.Itoa(fi+1) + "/" + strconv.Itoa(len(fams)), titleStyle: th.dim,
		border: th.line, topRightStyle: th.cyan, markStyle: th.cyan}
	if off > 0 {
		b.topRight = "▲ " + strconv.Itoa(off) + " more"
	}
	if rest := len(fams) - off - vis; rest > 0 {
		b.bottomRight = "▼ " + strconv.Itoa(rest) + " more"
	}
	in := b.inner()
	countW := 5
	for _, f := range fams {
		countW = max(countW, format.Width(strconv.Itoa(f.Samples)))
	}
	nameW := in - 2 - 4 - countW
	if narrow {
		nameW = min(40, w-24)
	}
	for i := off; i < min(len(fams), off+vis); i++ {
		f := fams[i]
		r := &row{}
		cur := "  "
		if i == fi {
			r.bg, cur = th.c.sel, "▸ "
		}
		tag, tst := th.typeTag(f.Type)
		r.add(cur, th.cyan).left(f.Name, nameW, th.fg).left(tag, 4, tst)
		if narrow {
			keys := strings.Join(f.LabelNames, ", ")
			if keys == "" {
				keys = "—"
			}
			r.space(1).left(keys, in-2-nameW-4-1-6, th.prev)
		}
		r.right(strconv.Itoa(f.Samples), in-r.width(), th.dim)
		b.lines = append(b.lines, r.String(in))
	}
	return b.render()
}

func (m *Model) familyDetail(fi *store.FamilyInfo, w int, wide bool) []string {
	th := &m.th
	var lines []string
	if wide {
		lines = append(lines, "")
	}
	_, tst := th.typeTag(fi.Type)
	lines = append(lines,
		(&row{}).add(fi.Name, th.cyanBold).space(2).add(fi.Type.String(), tst).space(2).add(strconv.Itoa(fi.Samples)+" series", th.dim).String(w),
		(&row{}).left("# "+fi.Help, w, th.dim).String(w))
	if wide {
		lines = append(lines, "")
	}
	lines = append(lines, m.labelBox(fi, w)...)
	var panel []string
	switch {
	case fi.Type.IsHistogram():
		panel = m.bucketRateBox(fi, w)
	case len(fi.LabelNames) == 2:
		panel = m.pivotBox(fi, w)
	}
	if panel == nil {
		panel = m.seriesListBox(fi, w)
	}
	if wide {
		lines = append(lines, "")
	}
	return append(lines, panel...)
}

func (m *Model) labelBox(fi *store.FamilyInfo, w int) []string {
	th := &m.th
	b := box{w: w, pad: 1, title: "label cardinality", titleStyle: th.dim, border: th.line}
	in := b.inner()
	labelW := 10
	for _, name := range fi.LabelNames {
		labelW = max(labelW, format.Width(name))
	}
	labelW = min(labelW, in-40) // reserve 20 cells for values after count and bar
	for _, name := range fi.LabelNames {
		vals := fi.LabelValues[name]
		shown := strings.Join(vals[:min(len(vals), labelValuesMax)], " ")
		if len(vals) > labelValuesMax {
			shown += " …"
		}
		b.lines = append(b.lines, (&row{}).left(name, labelW, th.amber).right(format.Int(len(vals)), 5, th.hi).space(3).
			left(format.Bar(math.Min(1, float64(len(vals))/12), 12), 12, th.magenta).left(shown, in-labelW-20, th.prev).String(in))
	}
	if len(fi.LabelNames) == 0 {
		b.lines = []string{(&row{}).left("no labels — single series", in, th.dim).String(in)}
	}
	return b.render()
}

func (m *Model) pivotBox(fi *store.FamilyInfo, maxW int) []string {
	th := &m.th
	k1, k2 := fi.LabelNames[0], fi.LabelNames[1]
	rowsV := fi.LabelValues[k1]
	colsV := slices.Clone(fi.LabelValues[k2])
	slices.Sort(colsV)
	if len(rowsV) > pivotRowsMax || len(colsV) > (maxW-4-10-11)/10 {
		return nil
	}
	cells := map[[2]string]*store.Series{}
	for _, s := range fi.Series {
		a, _ := labelValue(s.Labels, k1)
		c, _ := labelValue(s.Labels, k2)
		cells[[2]string{a, c}] = s
	}
	counter := fi.Type == expo.Counter
	var rateFrom, rateTo uint64
	var rateTimes []time.Time
	if counter {
		rateFrom, rateTo, rateTimes = m.minuteRateWindow()
	}
	value := func(s *store.Series) float64 {
		if counter {
			return stats.Rate1m(s.Values(rateFrom, rateTo), rateTimes)
		}
		return s.Last()
	}
	statusCol := k2 == "code" || k2 == "status" || k2 == "status_code"
	cellStyle := func(v string) lipgloss.Style {
		if statusCol && len(v) == 3 {
			switch v[0] {
			case '2':
				return th.green
			case '4':
				return th.amber
			case '5':
				return th.red
			}
		}
		return th.fg
	}
	corner := k1 + "\\" + k2
	rowW, colW, totalW := 10, 10, 11
	if maxW > 120 {
		rowW = max(rowW, format.Width(corner))
		for _, rv := range rowsV {
			rowW = max(rowW, format.Width(rv))
		}
		for _, cv := range colsV {
			colW = max(colW, format.Width(cv))
		}
		if needed := rowW + colW*len(colsV) + totalW; needed > maxW-4 {
			return nil
		}
		extra := maxW - 4 - (rowW + colW*len(colsV) + totalW)
		each := extra / (len(colsV) + 2)
		rowW += each
		colW += each
		totalW += each + extra%(len(colsV)+2)
	}
	cw := rowW + colW*len(colsV) + totalW
	b := box{w: cw + 4, pad: 1, title: fi.Name + " by " + k1 + " × " + k2, titleStyle: th.dim, border: th.line}
	if counter {
		b.title = "rate(" + fi.Name + "[1m]) by " + k1 + " × " + k2
	}
	over := max(0, format.Width(corner)-rowW) // the corner may overflow into the first column at standard widths
	h := &row{}
	h.add(format.PadRight(corner, rowW), th.dim)
	for j, c := range colsV {
		w := colW
		if j == 0 {
			w -= over
		}
		h.right(c, w, th.dim)
	}
	h.right("Σ", totalW, th.dim)
	b.lines = append(b.lines, h.String(cw))
	// Totals stay NaN ("—") until a defined cell contributes, so unavailable
	// rates never read as a measured zero.
	add := func(t *float64, v float64) {
		if math.IsNaN(*t) {
			*t = v
		} else {
			*t += v
		}
	}
	colT := make([]float64, len(colsV))
	for j := range colT {
		colT[j] = math.NaN()
	}
	all := math.NaN()
	for _, rv := range rowsV {
		r := &row{}
		r.left(rv, rowW, th.fg)
		t := math.NaN()
		for j, cv := range colsV {
			s := cells[[2]string{rv, cv}]
			if s == nil {
				r.right("·", colW, th.axis)
				continue
			}
			v := value(s)
			if !math.IsNaN(v) {
				add(&t, v)
				add(&colT[j], v)
			}
			r.right(format.SI(v), colW, cellStyle(cv))
		}
		if !math.IsNaN(t) {
			add(&all, t)
		}
		b.lines = append(b.lines, r.right(format.SI(t), totalW, th.hiBold).String(cw))
	}
	b.lines = append(b.lines, th.line.Render(strings.Repeat("─", cw)))
	sr := &row{}
	sr.left("Σ", rowW, th.dim)
	for _, v := range colT {
		sr.right(format.SI(v), colW, th.hi)
	}
	suffix := ""
	if counter && !math.IsNaN(all) {
		suffix = "/s"
	}
	b.lines = append(b.lines, sr.right(format.SI(all)+suffix, totalW, th.cyanBold).String(cw))
	return b.render()
}

func (m *Model) seriesListBox(fi *store.FamilyInfo, w int) []string {
	th := &m.th
	title := "value"
	if len(fi.LabelNames) > 0 {
		title = "series by " + fi.LabelNames[0]
	}
	if len(fi.Series) > seriesListMax {
		title += " · first 12 of " + format.Int(len(fi.Series))
	}
	b := box{w: w, pad: 1, title: title, titleStyle: th.dim, border: th.line}
	in := b.inner()
	key := func(s *store.Series) string {
		if len(fi.LabelNames) > 0 {
			if v, ok := labelValue(s.Labels, fi.LabelNames[0]); ok {
				return fi.LabelNames[0] + "=\"" + v + "\""
			}
		}
		return s.Name
	}
	keyW := 22
	for _, s := range fi.Series[:min(len(fi.Series), seriesListMax)] {
		keyW = max(keyW, format.Width(key(s)))
	}
	keyW = min(keyW, in-27) // 12-cell value, 3-cell gap, at least 12 trend cells
	trendW := in - keyW - 15
	var rateFrom, rateTo uint64
	var rateTimes []time.Time
	for _, s := range fi.Series[:min(len(fi.Series), seriesListMax)] {
		v := m.num(s.Last(), s.Unit)
		if s.Type == expo.Counter {
			if rateTimes == nil {
				rateFrom, rateTo, rateTimes = m.minuteRateWindow()
			}
			v = format.SI(stats.Rate1m(s.Values(rateFrom, rateTo), rateTimes)) + "/s"
		}
		disp, _ := m.values(s)
		b.lines = append(b.lines, (&row{}).left(key(s), keyW, th.fg).right(v, 12, th.hiBold).space(3).
			add(format.Spark(disp, trendW, math.NaN(), math.NaN()), th.cyan).String(in))
	}
	return b.render()
}

func (m *Model) bucketRateBox(fi *store.FamilyInfo, w int) []string {
	th := &m.th
	b := box{w: w, pad: 1, title: "rate(_bucket[1m]) — cumulative", titleStyle: th.dim, border: th.line}
	src := store.HeatSource{Family: fi.Name, Type: fi.Type, Unit: fi.Unit, Hists: fi.Hists}
	if src.IsGauge() {
		b.title = "bucket counts · current distribution"
	}
	in := b.inner()
	if len(fi.Hists) == 0 || len(fi.Hists[0].LE) == 0 {
		return b.render()
	}
	keyW := 22
	for _, le := range fi.Hists[0].LE {
		if !math.IsInf(le, 1) {
			keyW = max(keyW, format.Width("le=\""+strconv.FormatFloat(le, 'f', -1, 64)+"\""))
		}
	}
	keyW = min(keyW, in-27)
	trendW := in - keyW - 15
	from, to := m.st.Window(m.window)
	trendFrom := max(1, from-1)
	trendTimes := m.st.Times(trendFrom, to)
	rateFrom, rateTo, rateTimes := m.minuteRateWindow()
	for k, le := range fi.Hists[0].LE {
		var trend []float64
		value := format.SI(math.NaN())
		if src.IsGauge() { // snapshots need no previous scrape
			if trend = src.Bucket(k, from, to); len(trend) > 0 {
				value = format.SI(trend[len(trend)-1])
			}
		} else {
			if trend = src.BucketRates(k, trendFrom, to, trendTimes); trendFrom < from {
				trend = trend[1:]
			}
			value = format.SI(src.BucketRate1m(k, rateFrom, rateTo, rateTimes)) + "/s"
		}
		label := "+Inf"
		if !math.IsInf(le, 1) {
			label = strconv.FormatFloat(le, 'f', -1, 64)
		}
		b.lines = append(b.lines, (&row{}).left("le=\""+label+"\"", keyW, th.fg).right(value, 12, th.hiBold).
			space(3).add(format.Spark(trend, trendW, math.NaN(), math.NaN()), th.cyan).String(in))
	}
	return b.render()
}

// minuteRateWindow includes a sample exactly one minute before the latest.
// Store.Window uses an exclusive cutoff; Rate1m itself clips older samples.
func (m *Model) minuteRateWindow() (uint64, uint64, []time.Time) {
	from, to := m.st.Window(time.Minute + time.Nanosecond)
	return from, to, m.st.Times(from, to)
}

func labelValue(ls []expo.Label, name string) (string, bool) {
	for _, l := range ls {
		if l.Name == name {
			return l.Value, true
		}
	}
	return "", false
}
