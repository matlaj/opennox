package frameinterp

import (
	"fmt"
	"testing"
	"time"
)

// Use the host's integer-millisecond remaining budget and the renderer's duration
// clock. A constant swap cost hides the interaction with real refresh boundaries.
func TestSchedulerDisplayCadence(t *testing.T) {
	for _, hz := range []int{60, 144, 165} {
		for _, cost := range []time.Duration{2 * time.Millisecond, 4 * time.Millisecond} {
			for _, remote := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dHz/draw=%v/remote=%v", hz, cost, remote), func(t *testing.T) {
					const step = time.Second / 165
					refresh := time.Second / time.Duration(hz)
					var s Scheduler
					s.SetVSync(true)
					var now, remoteDeadline time.Duration
					var presents []time.Duration
					var ordinary, extra int
					sleep := func(dt time.Duration) { now += dt + 100*time.Microsecond }
					clock := func() time.Duration { return now }
					draw := func() bool {
						start := now
						now += cost
						s.ObserveDraw(start, now)
						return true
					}
					present := func() {
						start := now
						now = (now/refresh + 1) * refresh
						s.ObservePresent(start, now)
						presents = append(presents, now)
					}
					for tick := 1; tick <= 180; tick++ {
						remoteDeadline = NextTick(remoteDeadline, now, time.Second/30)
						now += 2 * time.Millisecond // simulation/input, once per tick
						s.WaitFrame(step, clock, sleep)
						draw()
						ordinary++
						present()
						left := time.Duration(int64(float64(tick)*(1000.0/30.0))-int64(now/time.Millisecond)) * time.Millisecond
						if remote {
							left = remoteDeadline - now
						}
						if left > 0 {
							s.Run(now+left, step, clock, sleep, func() bool { extra++; return draw() }, present)
						}
					}
					for i := 1; i < len(presents); i++ {
						if presents[i-1] < time.Second { // allow clock warmup
							continue
						}
						if gap := presents[i] - presents[i-1]; gap != refresh {
							t.Fatalf("missed a refresh at %v: gap=%v, want=%v", presents[i], gap, refresh)
						}
					}
					if ordinary != 180 || ordinary+extra != len(presents) {
						t.Fatal("rendered an undisplayed frame or skipped a tick draw")
					}
					if now < 6*time.Second-refresh || now > 6*time.Second+refresh {
						t.Fatalf("simulation clock drifted: 180 ticks took %v", now)
					}
				})
			}
		}
	}
}

func TestSchedulerHitchRecovery(t *testing.T) {
	const ms = time.Millisecond
	const step = 16 * ms
	var s Scheduler
	var now time.Duration
	clock := func() time.Duration { return now }
	sleep := func(dt time.Duration) { now += dt }
	s.WaitFrame(step, clock, sleep)
	s.ObserveDraw(0, 40*ms)
	now = 40 * ms
	s.ObservePresent(now, now)
	frames := 0
	draw := func() bool {
		start := now
		now += 2 * ms
		s.ObserveDraw(start, now)
		frames++
		return true
	}
	present := func() { s.ObservePresent(now, now) }
	s.Run(50*ms, step, clock, sleep, draw, present)
	if frames != 0 || now != 50*ms {
		t.Fatalf("started an expensive extra frame: frames=%d, time=%v", frames, now)
	}
	// The next ordinary frame refreshes the estimate independently of repaints.
	s.WaitFrame(step, clock, sleep)
	draw()
	present()
	frames = 0
	s.Run(83*ms, step, clock, sleep, draw, present)
	if frames != 1 || now != 83*ms {
		t.Fatalf("did not recover: frames=%d, time=%v", frames, now)
	}
}

func TestSchedulerOversleep(t *testing.T) {
	const ms = time.Millisecond
	var s Scheduler
	now := time.Duration(0)
	clock := func() time.Duration { return now }
	s.WaitFrame(16*ms, clock, func(dt time.Duration) { now += dt })
	s.ObserveDraw(0, 2*ms)
	now = 2 * ms
	s.ObservePresent(now, now)
	s.Run(33*ms, 16*ms, clock, func(dt time.Duration) { now += dt + 30*ms }, func() bool {
		t.Fatal("started a frame after oversleeping its deadline")
		return false
	}, func() {})
}

func TestSchedulerCapWithoutVSync(t *testing.T) {
	const step = time.Second / 165
	var s Scheduler
	var now, prev time.Duration
	clock := func() time.Duration { return now }
	sleep := func(dt time.Duration) { now += dt }
	for i := 0; i < 100; i++ {
		s.WaitFrame(step, clock, sleep)
		start := now
		now += time.Millisecond
		s.ObserveDraw(start, now)
		s.ObservePresent(now, now)
		if i > 0 && now-prev != step {
			t.Fatalf("cap interval: got %v, want %v", now-prev, step)
		}
		prev = now
	}
	s.Run(now+step, 0, clock, sleep, func() bool { t.Fatal("disabled repaint"); return false }, func() {})
}

func TestNextTickAfterHitch(t *testing.T) {
	const step = time.Second / 30
	next := NextTick(0, 0, step)
	if next != step {
		t.Fatalf("first deadline: %v", next)
	}
	if got := NextTick(next, next+time.Millisecond, step); got != 2*step {
		t.Fatalf("small overrun shifted the next tick: %v", got)
	}
	const now = time.Second
	if got := NextTick(next, now, step); got != now+step {
		t.Fatalf("hitch queued catch-up ticks: %v", got)
	}
}
