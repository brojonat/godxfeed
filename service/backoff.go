package service

import (
	"math/rand/v2"
	"time"
)

type backoff struct {
	attempt int
	base    time.Duration
	max     time.Duration
	jitter  func() float64 // returns [0.0, 1.0)
}

func newBackoff() *backoff {
	return &backoff{
		base:   1 * time.Second,
		max:    60 * time.Second,
		jitter: rand.Float64,
	}
}

func (b *backoff) next() time.Duration {
	b.attempt++
	shift := min(b.attempt-1, 6) // cap shift to avoid overflow
	delay := b.base << shift     // 1s, 2s, 4s, 8s, 16s, 32s, 64s
	if delay > b.max {
		delay = b.max
	}
	// Jitter: [50%, 100%) of the computed delay.
	half := float64(delay) * 0.5
	delay = time.Duration(half + half*b.jitter())
	return delay
}

func (b *backoff) reset() {
	b.attempt = 0
}
