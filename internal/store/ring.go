// Package store keeps promtop's in-memory history: one timestamp ring shared
// by all series plus one value ring per series, aligned by scrape sequence
// number (1 = first successful scrape).
package store

// Ring holds one value per scrape sequence number in a fixed-size circular
// buffer. Sequences never written, skipped or already evicted read as the
// ring's empty value (NaN for float rings).
type Ring[T any] struct {
	buf   []T
	last  uint64
	empty T
}

// NewRing returns a ring with room for capacity sequences.
func NewRing[T any](capacity int, empty T) *Ring[T] {
	r := &Ring[T]{buf: make([]T, max(1, capacity)), empty: empty}
	for i := range r.buf {
		r.buf[i] = empty
	}
	return r
}

// Cap is the number of sequences the ring retains.
func (r *Ring[T]) Cap() int { return len(r.buf) }

// Put stores v at seq; sequences skipped since the last Put become empty.
func (r *Ring[T]) Put(seq uint64, v T) {
	c := uint64(len(r.buf))
	gap := r.last + 1
	if lo := seq + 1; lo > c && gap < lo-c {
		gap = lo - c
	}
	for s := gap; s < seq; s++ {
		r.buf[s%c] = r.empty
	}
	r.buf[seq%c] = v
	if seq > r.last {
		r.last = seq
	}
}

// Get returns the value stored for seq, or empty if unknown or evicted.
func (r *Ring[T]) Get(seq uint64) T {
	c := uint64(len(r.buf))
	if seq == 0 || seq > r.last || r.last-seq >= c {
		return r.empty
	}
	return r.buf[seq%c]
}

// Last returns the highest sequence written and its value.
func (r *Ring[T]) Last() (uint64, T) { return r.last, r.Get(r.last) }

// Range appends the values for sequences from..to (inclusive) to dst.
func (r *Ring[T]) Range(dst []T, from, to uint64) []T {
	for s := from; from > 0 && s <= to; s++ {
		dst = append(dst, r.Get(s))
	}
	return dst
}

// Resize returns a ring of the new capacity holding the most recent values.
func (r *Ring[T]) Resize(capacity int) *Ring[T] {
	n := NewRing(capacity, r.empty)
	if r.last == 0 {
		return n
	}
	keep := uint64(min(capacity, len(r.buf))) //nolint:gosec // lengths are non-negative
	lo := uint64(1)
	if r.last+1 > keep {
		lo = r.last + 1 - keep
	}
	for s := lo; s <= r.last; s++ {
		n.Put(s, r.Get(s))
	}
	return n
}
