package frameinterp

import (
	"image"
	"math"
	"time"
)

// Timeline anchors a received tick to a local monotonic clock. Duplicate packets
// do not restart interpolation; missing ticks never cause extrapolation.
type Timeline struct {
	frame uint32
	at    time.Duration
	valid bool
}

func (t *Timeline) Observe(frame uint32, now time.Duration) {
	if !t.valid || frame != t.frame {
		t.frame, t.at, t.valid = frame, now, true
	}
}

func (t *Timeline) Alpha(now, tick time.Duration) float64 {
	if !t.valid || tick <= 0 {
		return 1
	}
	return min(1, max(0, float64(now-t.at)/float64(tick)))
}

// Track keeps network endpoints separate from mutable legacy drawable fields.
type Track struct {
	prev, cur           image.Point
	prevFrame, curFrame uint32
	valid               bool
}

func (t *Track) Observe(frame uint32, pos image.Point) {
	if !t.valid {
		*t = Track{prev: pos, cur: pos, prevFrame: frame, curFrame: frame, valid: true}
		return
	}
	if frame != t.curFrame {
		t.prev, t.prevFrame = t.cur, t.curFrame
	}
	t.cur, t.curFrame = pos, frame
}

func (t Track) At(frame uint32, alpha float64, maxStep int) (image.Point, bool) {
	if !t.valid || t.curFrame != frame || t.curFrame != t.prevFrame+1 {
		return image.Point{}, false
	}
	d := t.cur.Sub(t.prev)
	// Use wide arithmetic even in the 32-bit client.
	if int64(d.X)*int64(d.X)+int64(d.Y)*int64(d.Y) > int64(maxStep)*int64(maxStep) {
		return image.Point{}, false
	}
	return Lerp(t.prev, t.cur, alpha), true
}

func Lerp(prev, cur image.Point, alpha float64) image.Point {
	alpha = min(1, max(0, alpha))
	d := cur.Sub(prev)
	return image.Pt(prev.X+int(math.Round(float64(d.X)*alpha)), prev.Y+int(math.Round(float64(d.Y)*alpha)))
}
