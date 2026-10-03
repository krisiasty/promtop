package format

import (
	"math"
	"strings"
	"time"
)

var (
	sparkRunes  = []rune("▁▂▃▄▅▆▇█")
	eighthRunes = []rune("▏▎▍▌▋▊▉")
	brailleBits = [4][2]int{{1, 8}, {2, 16}, {4, 32}, {64, 128}}
)

// Spark renders vals into w cells of ▁…█, averaging the samples that fall in
// each cell. lo/hi fix the scale; pass NaN for both to scale to the finite
// data (±Inf then plot at the edges). Cells with no numeric sample are blank.
func Spark(vals []float64, w int, lo, hi float64) string {
	n := len(vals)
	if n == 0 || w <= 0 {
		return ""
	}
	if math.IsNaN(lo) || math.IsNaN(hi) {
		lo, hi = math.Inf(1), math.Inf(-1)
		for _, v := range vals {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
		}
		if lo > hi { // no finite sample
			lo, hi = 0, 0
		}
	}
	var b strings.Builder
	for i := 0; i < w; i++ {
		a := i * n / w
		e := max(a+1, (i+1)*n/w)
		sum, c := 0.0, 0
		for j := a; j < e && j < n; j++ {
			if !math.IsNaN(vals[j]) {
				sum += vals[j]
				c++
			}
		}
		if c == 0 {
			b.WriteByte(' ')
			continue
		}
		k := 3
		if hi != lo {
			k = clampInt((sum/float64(c)-lo)/(hi-lo)*7, 0, 7)
		}
		b.WriteRune(sparkRunes[k])
	}
	return b.String()
}

// Bar renders frac (clamped to 0..1) of w cells using █ plus an eighth-block
// remainder. It is never empty: the minimum is one eighth (▏).
func Bar(frac float64, w int) string {
	if math.IsNaN(frac) {
		frac = 0
	}
	e := max(1, int(math.Round(math.Max(0, math.Min(1, frac))*float64(w)*8)))
	s := strings.Repeat("█", e/8)
	if r := e % 8; r > 0 {
		s += string(eighthRunes[r-1])
	}
	return s
}

// TimeColumn maps a sample to a column between latest-window and latest.
// Samples outside the window, or an invalid axis, return -1.
func TimeColumn(at, latest time.Time, window time.Duration, columns int) int {
	if columns <= 0 || window <= 0 || at.IsZero() || latest.IsZero() {
		return -1
	}
	start := latest.Add(-window)
	if at.Before(start) || at.After(latest) {
		return -1
	}
	return clampInt(float64(at.Sub(start))/float64(window)*float64(columns-1), 0, columns-1)
}

// BrailleTimed plots samples at their actual times within the selected window.
// Empty dot columns stay blank, including gaps between successful scrapes.
func BrailleTimed(vals []float64, times []time.Time, w, h int, lo, hi float64, latest time.Time, window time.Duration) []string {
	if w <= 0 || h <= 0 {
		return nil
	}
	grid := make([][]int, h)
	for i := range grid {
		grid[i] = make([]int, w)
	}
	dx, dy := w*2, h*4
	mins, maxs, lasts := make([]float64, dx), make([]float64, dx), make([]float64, dx)
	seen := make([]bool, dx)
	for i := 0; i < min(len(vals), len(times)); i++ {
		v := vals[i]
		x := TimeColumn(times[i], latest, window, dx)
		if x < 0 || math.IsNaN(v) {
			continue
		}
		if !seen[x] {
			mins[x], maxs[x], seen[x] = v, v, true
		} else {
			mins[x], maxs[x] = math.Min(mins[x], v), math.Max(maxs[x], v)
		}
		lasts[x] = v
	}
	span := hi - lo
	if span == 0 || math.IsNaN(span) {
		span = 1
	}
	y := func(v float64) int { return clampInt((1-(v-lo)/span)*float64(dy-1), 0, dy-1) }
	py := -1
	for x := range dx {
		if !seen[x] {
			py = -1
			continue
		}
		y1, y2 := y(maxs[x]), y(mins[x])
		if py >= 0 {
			y1, y2 = min(y1, py), max(y2, py)
		}
		for yy := y1; yy <= y2; yy++ {
			grid[yy>>2][x>>1] |= brailleBits[yy&3][x&1]
		}
		py = y(lasts[x])
	}
	return renderBraille(grid)
}

func renderBraille(grid [][]int) []string {
	out := make([]string, len(grid))
	for r := range grid {
		var b strings.Builder
		for _, c := range grid[r] {
			b.WriteRune(rune(0x2800 + c)) //nolint:gosec // c is a braille dot mask in 0..255
		}
		out[r] = b.String()
	}
	return out
}

// clampInt rounds f and clamps it to lo..hi in float space, so ±Inf and NaN
// (NaN → lo) never reach an implementation-defined float→int conversion.
func clampInt(f float64, lo, hi int) int {
	if math.IsNaN(f) {
		return lo
	}
	return int(math.Max(float64(lo), math.Min(float64(hi), math.Round(f))))
}

// Axis returns a w-cell time axis: the tick line ("┬───┬…", five ticks) and
// the label line ("-5m … -3m45s … now") for the given window.
func Axis(w int, window time.Duration) (ticks, labels string) {
	if w < 1 {
		return "", ""
	}
	a := []rune(strings.Repeat("─", w))
	l := []rune(strings.Repeat(" ", w))
	for i := 0; i <= 4; i++ {
		p := int(math.Round(float64(i*(w-1)) / 4))
		a[p] = '┬'
		s := "now"
		if i < 4 {
			s = "-" + Dur(time.Duration(float64(window)*(1-float64(i)/4)))
		}
		rs := []rune(s)
		st := p - len(rs)/2
		switch i {
		case 0:
			st = p
		case 4:
			st = p - len(rs) + 1
		}
		for j, r := range rs {
			if st+j >= 0 && st+j < w {
				l[st+j] = r
			}
		}
	}
	return string(a), string(l)
}

// YRange pads min..max by 8% on both sides; flat data gets ±10% of |max|
// (or ±1 when max is 0) before padding.
func YRange(mn, mx float64) (float64, float64) {
	r := mx - mn
	if r == 0 {
		r = math.Abs(mx) * 0.1
		if r == 0 {
			r = 1
		}
	}
	return mn - r*0.08, mx + r*0.08
}
