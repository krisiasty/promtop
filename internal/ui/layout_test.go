package ui

import "testing"

// Every view, in every mode, at every size, healthy and failing, must render
// exactly h lines of exactly w cells.
func TestEveryFrameFits(t *testing.T) {
	healthy := newFixture(t, 120)
	down := newFixture(t, 120)
	down.fail(7)
	for _, fx := range []*fixture{healthy, down} {
		for _, w := range []int{80, 99, 100, 120, 123, 141, 142, 145, 146, 200} {
			for _, h := range []int{24, 36, 60} {
				for v := ViewTable; v <= ViewRaw; v++ {
					m := fx.model(w, h)
					m.view = v
					assertFrame(t, m.render(), w, h)
					m.famOpen, m.rawWrap, m.showRate = true, true, false
					assertFrame(t, m.render(), w, h)
					if v <= ViewGraph {
						m.detailOpen = true
						assertFrame(t, m.render(), w, h)
						m.detailOpen = false
					}
					if fx == down {
						m.errOpen = true
						assertFrame(t, m.render(), w, h)
					}
				}
			}
		}
	}
}
