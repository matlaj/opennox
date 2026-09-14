package frameinterp

import (
	"testing"
	"time"
)

func TestSchedulerHitchRecovery(t *testing.T) {
	const ms = time.Millisecond
	var s Scheduler
	now := 20 * ms
	s.ObserveDraw(0, now) // A slow frame leaves no budget for an extra frame.
	frames := 0
	draw := func() bool {
		start := now
		now += 2 * ms
		s.ObserveDraw(start, now)
		frames++
		return true
	}
	sleep := func(dt time.Duration) { now += dt }
	s.Run(33*ms, 16*ms, func() time.Duration { return now }, sleep, draw, func() {})
	if frames != 0 || now != 33*ms {
		t.Fatalf("overran first tick: frames=%d, time=%v", frames, now)
	}
	// The next ordinary repaint refreshes the estimate even though no extra
	// frames have been possible since the hitch.
	draw()
	frames = 0
	s.Run(66*ms, 16*ms, func() time.Duration { return now }, sleep, draw, func() {})
	if frames != 1 || now != 66*ms {
		t.Fatalf("did not recover: frames=%d, time=%v", frames, now)
	}
}

func TestSchedulerOversleep(t *testing.T) {
	var s Scheduler
	const ms = time.Millisecond
	s.ObserveDraw(0, 2*ms)
	now := 2 * ms
	s.Run(33*ms, 16*ms, func() time.Duration { return now }, func(dt time.Duration) {
		now += dt + 30*ms
	}, func() bool {
		t.Fatal("started a frame after oversleeping its deadline")
		return false
	}, func() {})
}

func TestSchedulerIncludesSwapAndDoesNotAddItToFrameSpacing(t *testing.T) {
	const ms = time.Millisecond
	var s Scheduler
	s.ObserveDraw(0, 2*ms)
	s.ObservePresent(2*ms, 16*ms)
	now := 16 * ms
	frames := 0
	s.Run(32*ms, 16*ms, func() time.Duration { return now }, func(dt time.Duration) {
		now += dt
	}, func() bool {
		start := now
		now += 2 * ms
		s.ObserveDraw(start, now)
		frames++
		return true
	}, func() {
		start := now
		now += 14 * ms
		s.ObservePresent(start, now)
	})
	if frames != 1 || now != 32*ms {
		t.Fatalf("swap was not budgeted correctly: frames=%d, time=%v", frames, now)
	}
}
