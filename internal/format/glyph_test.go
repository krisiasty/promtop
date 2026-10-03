package format

import (
	"math"
	"strings"
	"testing"
	"time"
)

var nan = math.NaN()

func TestSpark(t *testing.T) {
	if got := Spark([]float64{0, 1, 2, 3, 4, 5, 6, 7}, 8, nan, nan); got != "▁▂▃▄▅▆▇█" {
		t.Errorf("ramp = %q", got)
	}
	if got := Spark([]float64{5, 5, 5, 5}, 4, nan, nan); got != "▄▄▄▄" {
		t.Errorf("flat = %q", got)
	}
	if got := Spark([]float64{nan, nan}, 2, nan, nan); got != "  " {
		t.Errorf("all NaN = %q", got)
	}
	if got := Spark([]float64{0, 10}, 2, 0, 20); got != "▁▅" {
		t.Errorf("fixed scale = %q", got)
	}
}

func TestBar(t *testing.T) {
	if got := Bar(1, 28); got != strings.Repeat("█", 28) {
		t.Errorf("full = %q", got)
	}
	if got := Bar(0, 28); got != "▏" {
		t.Errorf("empty = %q", got)
	}
	if got := Bar(0.5, 3); got != "█▌" {
		t.Errorf("half = %q", got)
	}
}

// brailleEach plots vals with BrailleTimed, sample i in dot column i.
func brailleEach(vals []float64, w, h int, lo, hi float64) []string {
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	window := time.Duration(2*w-1) * time.Second
	times := make([]time.Time, len(vals))
	for i := range times {
		times[i] = start.Add(time.Duration(i) * time.Second)
	}
	return BrailleTimed(vals, times, w, h, lo, hi, start.Add(window), window)
}

func TestBraille(t *testing.T) {
	got := brailleEach([]float64{5, 5, 5, 5}, 2, 1, 0, 10)
	if len(got) != 1 || got[0] != "⠤⠤" {
		t.Errorf("flat line = %q", got)
	}
	// A NaN run breaks the line: the gap column stays empty.
	got = brailleEach([]float64{5, 5, nan, nan, 5, 5}, 3, 1, 0, 10)
	if got[0] != "⠤⠀⠤" {
		t.Errorf("gap = %q, want %q", got[0], "⠤⠀⠤")
	}
	if n := len(brailleEach(make([]float64, 100), 132, 22, 0, 1)); n != 22 {
		t.Errorf("rows = %d", n)
	}
}

func TestTimeColumnAndBrailleTimed(t *testing.T) {
	latest := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	window := 5 * time.Minute
	for _, tc := range []struct {
		at   time.Time
		want int
	}{
		{latest.Add(-window), 0},
		{latest.Add(-4 * time.Minute), 2},
		{latest.Add(-time.Minute), 8},
		{latest, 10},
		{latest.Add(-window - time.Second), -1},
	} {
		if got := TimeColumn(tc.at, latest, window, 11); got != tc.want {
			t.Errorf("TimeColumn(%v) = %d, want %d", tc.at, got, tc.want)
		}
	}
	blank := rune(0x2800)
	startup := []rune(BrailleTimed([]float64{5}, []time.Time{latest}, 5, 1, 0, 10, latest, window)[0])
	for i, cell := range startup {
		if i < 4 && cell != blank || i == 4 && cell == blank {
			t.Errorf("startup plotted in cell %d: %q", i, string(startup))
		}
	}
	gap := []rune(BrailleTimed([]float64{5, 5}, []time.Time{latest.Add(-4 * time.Minute), latest},
		5, 1, 0, 10, latest, window)[0])
	for i, cell := range gap {
		if (i == 1 || i == 4) && cell == blank || (i != 1 && i != 4) && cell != blank {
			t.Errorf("gap plotted in cell %d: %q", i, string(gap))
		}
	}
}

func TestAxis(t *testing.T) {
	ticks, labels := Axis(41, 5*time.Minute)
	if want := "┬" + strings.Repeat("─", 9) + "┬" + strings.Repeat("─", 9) + "┬" + strings.Repeat("─", 9) + "┬" + strings.Repeat("─", 9) + "┬"; ticks != want {
		t.Errorf("ticks = %q", ticks)
	}
	if want := "-5m    -3m45s    -2m30s    -1m15s     now"; labels != want {
		t.Errorf("labels = %q", labels)
	}
	ticks, _ = Axis(132, 5*time.Minute)
	if []rune(ticks)[33] != '┬' { // matches design render 03: 32 dashes between the first two ticks
		t.Errorf("second tick not at cell 33: %q", ticks)
	}
}

func TestYRange(t *testing.T) {
	lo, hi := YRange(186, 332)
	if math.Abs(lo-174.32) > 1e-9 || math.Abs(hi-343.68) > 1e-9 {
		t.Errorf("YRange = %v, %v", lo, hi)
	}
	lo, hi = YRange(0, 0)
	if lo != -0.08 || hi != 0.08 {
		t.Errorf("flat zero = %v, %v", lo, hi)
	}
}

// ±Inf samples must not reach int() unclamped (int(±Inf) and int(NaN) are
// implementation-defined: 0 on arm64, the most negative int on amd64). They
// plot at the edge of the scale; auto-scaling uses the finite samples only.
func TestGlyphsWithInfinities(t *testing.T) {
	inf := math.Inf(1)
	if got := Spark([]float64{1, inf, 2}, 3, nan, nan); got != "▁██" {
		t.Errorf("auto-scaled spark with +Inf = %q, want %q", got, "▁██")
	}
	if got := Spark([]float64{-inf, inf}, 2, 0, 10); got != "▁█" {
		t.Errorf("fixed-scale spark with ±Inf = %q, want %q", got, "▁█")
	}
	if got := Spark([]float64{inf, inf}, 2, nan, nan); got != "▄▄" {
		t.Errorf("spark without finite samples = %q, want the flat %q", got, "▄▄")
	}
	if got := brailleEach([]float64{inf, inf}, 1, 1, 0, 10); got[0] != "⠉" {
		t.Errorf("+Inf plots on the top row: %q", got[0])
	}
	if got := brailleEach([]float64{-inf, -inf}, 1, 1, 0, 10); got[0] != "⣀" {
		t.Errorf("-Inf plots on the bottom row: %q", got[0])
	}
	if got := brailleEach([]float64{1, 2}, 1, 1, math.Inf(-1), inf); len(got) != 1 || []rune(got[0])[0] < 0x2800 {
		t.Errorf("infinite scale = %q", got)
	}
	if got := Bar(inf, 4); got != "████" {
		t.Errorf("Bar(+Inf) = %q", got)
	}
}
