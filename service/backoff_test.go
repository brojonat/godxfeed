package service

import (
	"testing"
	"time"
)

func TestBackoff_Progression(t *testing.T) {
	b := &backoff{
		base:   1 * time.Second,
		max:    60 * time.Second,
		jitter: func() float64 { return 0.5 }, // fixed: delay = 75% of base
	}

	// With jitter fixed at 0.5, delay = 0.5*base + 0.5*0.5*base = 0.75*base
	want := []time.Duration{
		750 * time.Millisecond,   // 1s * 0.75
		1500 * time.Millisecond,  // 2s * 0.75
		3 * time.Second,          // 4s * 0.75
		6 * time.Second,          // 8s * 0.75
		12 * time.Second,         // 16s * 0.75
		24 * time.Second,         // 32s * 0.75
		45 * time.Second,         // 60s cap * 0.75
		45 * time.Second,         // still capped
	}

	for i, w := range want {
		got := b.next()
		if got != w {
			t.Errorf("attempt %d: got %v, want %v", i+1, got, w)
		}
	}
}

func TestBackoff_Reset(t *testing.T) {
	b := &backoff{
		base:   1 * time.Second,
		max:    60 * time.Second,
		jitter: func() float64 { return 0.0 }, // delay = 50% of base
	}

	b.next() // 500ms
	b.next() // 1s
	b.next() // 2s
	b.reset()

	got := b.next()
	want := 500 * time.Millisecond // back to first step
	if got != want {
		t.Errorf("after reset: got %v, want %v", got, want)
	}
}
