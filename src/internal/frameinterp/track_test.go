package frameinterp

import (
	"image"
	"testing"
	"time"
)

func TestTrackDuplicateAndBatchedUpdates(t *testing.T) {
	var track Track
	track.Observe(10, image.Pt(100, 100))
	track.Observe(11, image.Pt(110, 100))
	track.Observe(11, image.Pt(120, 100))
	if p, ok := track.At(11, 0.5, 128); !ok || p != image.Pt(110, 100) {
		t.Fatalf("duplicate tick lost previous endpoint: %v, %v", p, ok)
	}
	track.Observe(12, image.Pt(130, 100))
	if p, ok := track.At(12, 0.5, 128); !ok || p != image.Pt(125, 100) {
		t.Fatalf("batched ticks lost latest pair: %v, %v", p, ok)
	}
}

func TestTrackDiscontinuities(t *testing.T) {
	var track Track
	track.Observe(10, image.Pt(100, 100))
	track.Observe(12, image.Pt(120, 100))
	if _, ok := track.At(12, 0.5, 128); ok {
		t.Fatal("interpolated across missing tick")
	}
	track.Observe(13, image.Pt(500, 500))
	if _, ok := track.At(13, 0.5, 128); ok {
		t.Fatal("interpolated a teleport")
	}
	track.Observe(1, image.Pt(510, 500))
	if _, ok := track.At(1, 0.5, 128); ok {
		t.Fatal("interpolated across frame reset")
	}
	track.Observe(2, image.Pt(520, 500))
	if _, ok := track.At(3, 0.5, 128); ok {
		t.Fatal("interpolated stale object")
	}
}

func TestTimelineDuplicateAndMissingTicks(t *testing.T) {
	var clock Timeline
	const tick = 30 * time.Millisecond
	clock.Observe(40, time.Second)
	clock.Observe(40, time.Second+tick/2)
	if a := clock.Alpha(time.Second+tick/2, tick); a != 0.5 {
		t.Fatalf("duplicate tick reset clock: %v", a)
	}
	if a := clock.Alpha(time.Second+10*tick, tick); a != 1 {
		t.Fatalf("extrapolated while waiting for packets: %v", a)
	}
	clock.Observe(44, 2*time.Second)
	if a := clock.Alpha(2*time.Second, tick); a != 0 {
		t.Fatalf("new snapshot retained old clock: %v", a)
	}
}
