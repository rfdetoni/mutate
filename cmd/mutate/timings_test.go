package main

import "testing"

func TestPhaseTimerOptIn(t *testing.T) {
	t.Setenv("MUTATE_TIMINGS", "")
	disabled := startPhaseTimer("build")
	if disabled.enabled {
		t.Fatal("profiling must be disabled by default")
	}
	disabled.mark("unpack")

	t.Setenv("MUTATE_TIMINGS", "1")
	enabled := startPhaseTimer("build")
	if !enabled.enabled || enabled.start.IsZero() {
		t.Fatal("profiling flag did not enable phase measurement")
	}
}
