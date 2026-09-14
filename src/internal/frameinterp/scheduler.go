// Package frameinterp contains the clock and position arithmetic used by the
// client renderer, without dependencies on the game, C, or a display server.
package frameinterp

import "time"

// Scheduler measures ordinary frames as well as extra frames. A hitch therefore
// cannot leave an estimate which prevents all future measurements.
type Scheduler struct {
	start, drawCost, swapCost time.Duration
	valid                     bool
}

func (s *Scheduler) Reset() { *s = Scheduler{} }

func (s *Scheduler) ObserveDraw(start, end time.Duration) {
	s.start, s.drawCost, s.valid = start, max(0, end-start), true
}

func (s *Scheduler) ObservePresent(start, end time.Duration) {
	s.swapCost = max(0, end-start)
}

// Run spends an existing tick's wait budget on repaints. It never extends the
// deadline to make room for a frame. Draw must report whether it painted, and
// record its timing using ObserveDraw; present records ObservePresent likewise.
func (s *Scheduler) Run(deadline, step time.Duration, now func() time.Duration, sleep func(time.Duration), draw func() bool, present func()) {
	for {
		t := now()
		left := deadline - t
		if left <= 0 {
			return
		}
		if step <= 0 || !s.valid {
			sleep(left)
			return
		}
		// Space frame starts on the clock, rather than sleeping for a full
		// interval after every completed draw and blocking buffer swap.
		wait := max(0, s.start+step-t)
		cost := s.drawCost + s.swapCost
		if wait+cost > left {
			sleep(left)
			return
		}
		if wait > 0 {
			sleep(wait)
		}
		// Sleep may overshoot, or execute a queued loop hook. Check again.
		left = deadline - now()
		if left <= 0 {
			return
		}
		if cost > left || !draw() {
			if left = deadline - now(); left > 0 {
				sleep(left)
			}
			return
		}
		present()
	}
}
