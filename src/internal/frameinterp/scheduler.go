// Package frameinterp contains the clock and position arithmetic used by the
// client renderer, without dependencies on the game, C, or a display server.
package frameinterp

import "time"

// Scheduler keeps a presentation cadence across simulation ticks. Both ordinary
// frames and repaints use WaitFrame/ObserveDraw/ObservePresent.
type Scheduler struct {
	drawCost, swapCost time.Duration
	next, step         time.Duration
	valid              bool
	vsync              bool
}

func (s *Scheduler) Reset() { *s = Scheduler{} }

func (s *Scheduler) SetVSync(v bool) {
	if s.vsync != v {
		s.valid = false
		s.vsync = v
	}
}

func (s *Scheduler) ObserveDraw(start, end time.Duration) {
	s.drawCost = max(0, end-start)
}

func (s *Scheduler) ObservePresent(start, end time.Duration) {
	s.swapCost = max(0, end-start)
	if !s.valid || s.next+s.step <= end {
		// First frame, or a hitch: don't try to catch up missed presentations.
		s.next = end + s.step
	} else {
		s.next += s.step
	}
	s.valid = true
}

func (s *Scheduler) configure(step time.Duration) {
	if s.step != step {
		*s = Scheduler{step: step, vsync: s.vsync}
	}
}

func (s *Scheduler) startAt() time.Duration {
	// Submit slightly before the target when VSync is active. Arriving on
	// the refresh boundary (or a small timer overshoot) misses that refresh.
	margin := time.Duration(0)
	if s.vsync {
		margin = min(time.Millisecond, s.step/8)
	}
	return s.next - s.drawCost - s.swapCost - margin
}

// WaitFrame reserves the CPU draw and the buffer swap before the next presentation
// target. A blocking VSync swap is part of that interval, not an extra sleep after it.
func (s *Scheduler) WaitFrame(step time.Duration, now func() time.Duration, sleep func(time.Duration)) {
	s.configure(step)
	if step > 0 && s.valid {
		if dt := s.startAt() - now(); dt > 0 {
			sleep(dt)
		}
	}
}

// Run fills the idle part of a tick. A frame already due before the next tick may
// finish across its boundary, by at most one estimated presentation interval.
// Requiring a VSync swap to finish before every 30 Hz boundary drops refreshes
// periodically on displays such as 144/165 Hz. No second frame starts after the
// deadline, and expensive draws are left to the ordinary tick to avoid overload.
func (s *Scheduler) Run(deadline, step time.Duration, now func() time.Duration, sleep func(time.Duration), draw func() bool, present func()) {
	s.configure(step)
	for {
		t := now()
		left := deadline - t
		if left <= 0 {
			return
		}
		start := max(t, s.startAt())
		if step <= 0 || !s.valid || start >= deadline || start+s.drawCost+s.swapCost > deadline+step {
			sleep(left)
			return
		}
		if start > t {
			sleep(start - t)
		}
		// Sleep can overshoot or service a loop hook. Do not start a stale frame.
		if now() >= deadline {
			return
		}
		if !draw() {
			if left = deadline - now(); left > 0 {
				sleep(left)
			}
			return
		}
		present()
	}
}

// NextTick preserves a tick cadence when a presentation finishes just after its
// deadline. Reset after a full missed tick or a clock discontinuity, rather than
// accumulating catch-up work. A zero previous deadline starts a new cadence.
func NextTick(previous, now, step time.Duration) time.Duration {
	if previous == 0 || now >= previous+step || now < previous-step {
		return now + step
	}
	return previous + step
}
