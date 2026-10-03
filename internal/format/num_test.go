package format

import (
	"math"
	"testing"
	"time"
)

func TestNum(t *testing.T) {
	cases := []struct {
		v    float64
		u    Unit
		want string
	}{
		{269, None, "269"},
		{18, None, "18.0"},
		{1, None, "1.00"},
		{0.394, None, "0.394"},
		{0.297, None, "0.297"},
		{6.9e-4, None, "0.0007"},
		{0.00044, None, "0.0004"},
		{0.0078, None, "0.0078"},
		{-0.00078, None, "-0.0008"},
		{0.00004, None, "0"},
		{-0.00004, None, "0"},
		{0, None, "0"},
		{1347, None, "1347"},
		{12500, None, "12.5k"},
		{2.5e6, None, "2.50M"},
		{3.2e9, None, "3.20G"},
		{100.4 * (1 << 20), Bytes, "100.4Mi"},
		{735.1 * (1 << 10), Bytes, "735.1Ki"},
		{19.93 * (1 << 30), Bytes, "19.93Gi"},
		{512, Bytes, "512B"},
		{0.00024, Seconds, "0.24ms"},
		{0.0468, Seconds, "46.8ms"},
		{0.5, Seconds, "500.0ms"},
		{0.005, Seconds, "5.00ms"},
		{0.0001, Seconds, "0.10ms"},
		{9.83e-5, Seconds, "98.30µs"},
		{1.024e-6, Seconds, "1.02µs"},
		{6.4e-8, Seconds, "64.0ns"},
		{1, Seconds, "1.00s"},
		{0, Seconds, "0"},
		{math.NaN(), None, "—"},
		{math.Inf(1), None, "+Inf"},
	}
	for _, c := range cases {
		if got := Num(c.v, c.u); got != c.want {
			t.Errorf("Num(%v, %v) = %q, want %q", c.v, c.u, got, c.want)
		}
	}
}

func TestSigned(t *testing.T) {
	cases := []struct {
		d    float64
		u    Unit
		want string
	}{
		{7, None, "+7.00"},
		{-355.2 * 1024, Bytes, "−355.2Ki"},
		{-6.9e-4, None, "−0.0007"},
		{0.00001, None, "0"},
		{-0.00001, None, "0"},
		{0, None, "0"},
		{0.000001, Seconds, "+1.00µs"},
		{math.NaN(), None, "—"},
	}
	for _, c := range cases {
		if got := Signed(c.d, c.u); got != c.want {
			t.Errorf("Signed(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestIntSizeRaw(t *testing.T) {
	checks := map[string]string{
		Int(20083):          "20,083",
		Int(83):             "83",
		Int(1234567):        "1,234,567",
		Int(-4200):          "-4,200",
		Size(3900000):       "3.9 MB",
		Size(999):           "999 B",
		Raw(12798715):       "12798715",
		Raw(4244.448):       "4244.448",
		Raw(0.47521):        "0.4752",
		RawDelta(-19955082): "-19955082",
		RawDelta(223):       "+223",
		RawDelta(0.4752):    "+0.4752",
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestDurations(t *testing.T) {
	checks := map[string]string{
		Dur(18 * time.Second):                                    "18s",
		Dur(225 * time.Second):                                   "3m45s",
		Dur(300 * time.Second):                                   "5m",
		Dur(7500 * time.Second):                                  "2h5m",
		Dur(-3 * time.Second):                                    "0s",
		Interval(time.Second):                                    "1s",
		Interval(90 * time.Second):                               "1m30s",
		Interval(2 * time.Minute):                                "2m",
		Interval(500 * time.Millisecond):                         "500ms",
		Millis(150 * time.Millisecond):                           "150ms",
		Millis(1234 * time.Millisecond):                          "1.23s",
		Clock(time.Date(2026, 9, 26, 12, 36, 59, 0, time.Local)): "12:36:59",
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestSINaN(t *testing.T) {
	if SI(math.NaN()) != "—" || SI(math.Inf(1)) != "+Inf" {
		t.Error("SI must not panic on NaN/Inf")
	}
}

func TestDurDays(t *testing.T) {
	if got := Dur(5*24*time.Hour + 6*time.Hour + 7*time.Minute); got != "5d6h" {
		t.Errorf("Dur = %q, want 5d6h", got)
	}
	if got := Dur(3 * 24 * time.Hour); got != "3d" {
		t.Errorf("Dur = %q, want 3d", got)
	}
}

func TestTimestamps(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 36, 59, 0, time.Local)
	epoch := func(t time.Time) float64 { return float64(t.Unix()) }
	checks := []struct{ got, want string }{
		{Value(epoch(now.Add(-time.Hour)), Timestamp, now), "11:36:59"},
		{Value(epoch(time.Date(2026, 9, 21, 13, 33, 20, 0, time.Local)), Timestamp, now), "Sep 21"},
		{Value(epoch(time.Date(2027, 3, 1, 0, 0, 0, 0, time.Local)), Timestamp, now), "Mar 2027"},
		{Value(42, Timestamp, now), "42.00s"}, // not epoch-like: plain seconds
		{Value(math.NaN(), Timestamp, now), "—"},
		{Value(1.5, Seconds, now), "1.50s"}, // other units unchanged
		{Delta(3, Timestamp), "+3s"},
		{Delta(-3*3600-300, Timestamp), Minus + "3h5m"},
		{Delta(0, Timestamp), "0"},
		{Delta(0.2, Timestamp), "0"},
		{Delta(7, None), "+7.00"},
		{TimeFull(epoch(time.Date(2026, 9, 21, 13, 33, 20, 0, time.Local)), now), "2026-09-21 13:33:20 · 4d23h ago"},
		{TimeFull(epoch(now.Add(12*24*time.Hour)), now), "2026-10-08 12:36:59 · in 12d"},
	}
	for i, c := range checks {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
	if !IsEpoch(1.79e9) || IsEpoch(5) || IsEpoch(math.Inf(1)) {
		t.Error("IsEpoch")
	}
}
