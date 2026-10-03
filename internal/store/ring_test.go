package store

import (
	"math"
	"testing"
)

func TestRing(t *testing.T) {
	r := NewRing(3, math.NaN())
	r.Put(2, 20)
	if !math.IsNaN(r.Get(1)) || r.Get(2) != 20 || !math.IsNaN(r.Get(0)) {
		t.Fatalf("after Put(2): %v %v", r.Get(1), r.Get(2))
	}
	r.Put(5, 50) // 3 and 4 were skipped, 2 falls out of a 3-slot ring
	if !math.IsNaN(r.Get(2)) || !math.IsNaN(r.Get(3)) || !math.IsNaN(r.Get(4)) || r.Get(5) != 50 || !math.IsNaN(r.Get(6)) {
		t.Fatalf("after Put(5): %v", r.Range(nil, 2, 6))
	}
	if seq, v := r.Last(); seq != 5 || v != 50 {
		t.Errorf("Last = %d %v", seq, v)
	}
	if got := r.Range(nil, 3, 5); len(got) != 3 || got[2] != 50 {
		t.Errorf("Range = %v", got)
	}
	if big := r.Resize(10); big.Cap() != 10 || big.Get(5) != 50 {
		t.Errorf("grow lost data")
	}
	small := NewRing(10, 0.0)
	for s := uint64(1); s <= 10; s++ {
		small.Put(s, float64(s))
	}
	s2 := small.Resize(4)
	if s2.Get(7) != 7 || s2.Get(6) != 0 || s2.Get(10) != 10 {
		t.Errorf("shrink kept wrong window: %v", s2.Range(nil, 5, 10))
	}
}
