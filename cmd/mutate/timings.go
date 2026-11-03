package main

import (
	"fmt"
	"os"
	"time"
)

// phaseTimer is opt-in so regular conversions do no timing work. Measurements
// are written only to stderr, preserving stdout/JSON CLI contracts.
type phaseTimer struct {
	enabled   bool
	operation string
	start     time.Time
	last      time.Time
}

func startPhaseTimer(operation string) phaseTimer {
	if os.Getenv("MUTATE_TIMINGS") != "1" {
		return phaseTimer{}
	}
	now := time.Now()
	return phaseTimer{enabled: true, operation: operation, start: now, last: now}
}

func (t *phaseTimer) mark(phase string) {
	if !t.enabled {
		return
	}
	now := time.Now()
	fmt.Fprintf(os.Stderr, "[timings] %s %s=%s total=%s\n",
		t.operation, phase, now.Sub(t.last).Round(time.Microsecond),
		now.Sub(t.start).Round(time.Microsecond))
	t.last = now
}
