package stats

import (
	"fmt"
	"math"
	"testing"
	"time"
)

var nan = math.NaN()

func secs(n ...int) []time.Time {
	base := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	out := make([]time.Time, len(n))
	for i, s := range n {
		out[i] = base.Add(time.Duration(s) * time.Second)
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestIncrease(t *testing.T) {
	if Increase(10, 14) != 4 || Increase(10, 4) != 4 {
		t.Error("Increase must treat a drop as a restart from zero")
	}
}

func TestRatesAcrossGapsAndResets(t *testing.T) {
	got := Rates([]float64{0, 10, nan, 20, 5}, secs(0, 1, 2, 3, 4))
	want := []float64{nan, 10, nan, 5, 5}
	for i := range want {
		if math.IsNaN(want[i]) != math.IsNaN(got[i]) || !math.IsNaN(want[i]) && !near(got[i], want[i]) {
			t.Fatalf("Rates = %v, want %v", got, want)
		}
	}
}

func TestSummarize(t *testing.T) {
	s := Summarize([]float64{1, nan, 2, 3, 4})
	if s.N != 4 || s.Cur != 4 || s.Prev != 3 || s.Delta != 1 || s.Min != 1 || s.Max != 4 ||
		!near(s.Mean, 2.5) || !near(s.Median, 2.5) || !near(s.P99, 3.97) || !near(s.Stddev, math.Sqrt(1.25)) {
		t.Errorf("Summarize = %+v", s)
	}
	empty := Summarize([]float64{nan})
	if empty.N != 0 || !math.IsNaN(empty.Cur) || !math.IsNaN(empty.P99) {
		t.Errorf("empty = %+v", empty)
	}
	one := Summarize([]float64{7})
	if one.Cur != 7 || !math.IsNaN(one.Prev) || !math.IsNaN(one.Delta) {
		t.Errorf("single = %+v", one)
	}
}

func TestSummarizeLatestSample(t *testing.T) {
	for _, tc := range []struct {
		name             string
		vals             []float64
		cur, prev, delta float64 // Summarize: NaN is absent, the last number is current
		nanPrev          float64 // LatestNaN: the latest number becomes previous
	}{
		{"empty", nil, nan, nan, nan, nan},
		{"all NaN", []float64{nan, nan}, nan, nan, nan, nan},
		{"leading NaN", []float64{nan, 7}, 7, nan, nan, 7},
		{"single trailing NaN", []float64{7, nan}, 7, nan, nan, 7},
		{"trailing NaN", []float64{3, 7, nan}, 7, 3, 4, 7},
		{"consecutive NaNs", []float64{3, 7, nan, nan}, 7, 3, 4, 7},
		{"recovery", []float64{3, 7, nan, nan, 9}, 9, 7, 2, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Summarize(tc.vals)
			if !same(s.Cur, tc.cur) || !same(s.Prev, tc.prev) || !same(s.Delta, tc.delta) {
				t.Errorf("current/previous/delta = %v/%v/%v, want %v/%v/%v", s.Cur, s.Prev, s.Delta, tc.cur, tc.prev, tc.delta)
			}
			n := s.LatestNaN()
			if !math.IsNaN(n.Cur) || !math.IsNaN(n.Delta) || !same(n.Prev, tc.nanPrev) {
				t.Errorf("explicit NaN current/previous/delta = %v/%v/%v, want NaN/%v/NaN", n.Cur, n.Prev, n.Delta, tc.nanPrev)
			}
			n.Cur, n.Prev, n.Delta = s.Cur, s.Prev, s.Delta
			if fmt.Sprint(n) != fmt.Sprint(s) {
				t.Errorf("explicit NaN changed window aggregates: %+v, want %+v", n, s)
			}
		})
	}
	for _, vals := range [][]float64{{3, 7, nan}, {3, 7, nan, nan}} {
		s := Summarize(vals)
		if s.N != 2 || s.Min != 3 || s.Max != 7 || s.Mean != 5 || s.Median != 5 || s.P50 != 5 || s.Stddev != 2 ||
			!near(s.P90, 6.6) || !near(s.P95, 6.8) || !near(s.P99, 6.96) {
			t.Errorf("NaNs changed window aggregates: %+v", s)
		}
	}
}

// same compares floats, treating NaN as equal to NaN.
func same(a, b float64) bool { return math.IsNaN(a) && math.IsNaN(b) || a == b }

func TestRate1m(t *testing.T) {
	vals := make([]float64, 121)
	ts := make([]time.Time, 121)
	for i := range vals {
		vals[i] = float64(i * 2)
		ts[i] = secs(i)[0]
	}
	if got := Rate1m(vals, ts); !near(got, 2) {
		t.Errorf("Rate1m = %v, want 2", got)
	}
	if !math.IsNaN(Rate1m([]float64{1}, secs(0))) {
		t.Error("one sample has no rate")
	}
}

func TestHistogramQuantile(t *testing.T) {
	le := []float64{0.1, 0.5, 1, math.Inf(1)}
	cum := []float64{10, 60, 90, 100}
	if got := HistogramQuantile(0.5, le, cum); !near(got, 0.42) {
		t.Errorf("p50 = %v, want 0.42", got)
	}
	if got := HistogramQuantile(0.99, le, cum); got != 1 {
		t.Errorf("p99 in +Inf bucket = %v, want 1 (previous bound)", got)
	}
	if got := HistogramQuantile(0.05, le, cum); !near(got, 0.05) {
		t.Errorf("p5 = %v, want 0.05", got)
	}
	if !math.IsNaN(HistogramQuantile(0.5, le, []float64{0, 0, 0, 0})) {
		t.Error("empty histogram must be NaN")
	}
}

func TestDistribution(t *testing.T) {
	bins := Distribution([]float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, nan}, 10)
	if len(bins) != 10 || bins[0].Lo != 0 || bins[9].Hi != 10 || bins[9].Count != 2 || bins[0].Count != 1 {
		t.Errorf("bins = %+v", bins)
	}
	if Distribution([]float64{nan}, 10) != nil {
		t.Error("no data → nil")
	}
}

// ±Inf must neither widen the range (bw would be Inf/NaN, and int(NaN) is a
// negative index on amd64) nor be counted.
func TestDistributionIgnoresInfinities(t *testing.T) {
	bins := Distribution([]float64{1, math.Inf(1), math.Inf(-1), 2, math.NaN()}, 4)
	if len(bins) != 4 {
		t.Fatalf("bins = %+v", bins)
	}
	total := 0
	for _, b := range bins {
		total += b.Count
		if math.IsNaN(b.Lo) || math.IsInf(b.Lo, 0) || math.IsNaN(b.Hi) || math.IsInf(b.Hi, 0) {
			t.Errorf("non-finite bin bounds: %+v", bins)
		}
	}
	if total != 2 || bins[0].Count != 1 || bins[3].Count != 1 || bins[0].Lo != 1 || bins[3].Hi != 2 {
		t.Errorf("only the finite values count: %+v", bins)
	}
	if got := Distribution([]float64{math.Inf(1), math.Inf(-1)}, 4); got != nil {
		t.Errorf("no finite data = %+v, want nil", got)
	}
	st := Summarize([]float64{1, math.Inf(1), 2})
	if st.N != 3 || !math.IsInf(st.Max, 1) || st.Min != 1 {
		t.Errorf("Summarize keeps infinities as values: %+v", st)
	}
}

func TestMismatchedLengthsDoNotPanic(t *testing.T) {
	r := Rates([]float64{0, 1, 2}, secs(0, 1))
	if len(r) != 3 || !math.IsNaN(r[2]) || r[1] != 1 {
		t.Errorf("Rates with short ts = %v", r)
	}
	if got := Rate1m([]float64{0, 1, 2}, secs(0, 1)); got != 1 {
		t.Errorf("Rate1m with short ts = %v, want 1", got)
	}
}
