package noxrender

import (
	"image"
	"image/color"
	"log/slog"
	"slices"
	"testing"

	"github.com/opennox/libs/noximage"
)

func TestFadeExtraFrames(t *testing.T) {
	for _, kind := range []string{"screen in", "screen out", "cinema in", "cinema out"} {
		t.Run(kind, func(t *testing.T) {
			newRenderer := func(done *int) *NoxRender {
				r := NewRender(slog.Default(), nil)
				r.SetData(newRenderData(32, 32))
				r.SetPixBuffer(noximage.NewImage16(image.Rect(0, 0, 32, 32)))
				callback := func() { *done++ }
				switch kind {
				case "screen in":
					r.FadeInScreen(10, false, callback)
				case "screen out":
					r.FadeOutScreen(10, false, callback)
				case "cinema in":
					r.FadeInCinema(0.25, 10, color.Black)
				case "cinema out":
					r.FadeOutCinema(0.25, 10, color.Black)
				}
				return r
			}
			var baseDone, extraDone int
			base, extra := newRenderer(&baseDone), newRenderer(&extraDone)
			paint := func(r *NoxRender) []uint16 {
				r.ClearScreen(color.White)
				r.DrawFade(false)
				return r.PixBuffer().Pix
			}
			for tick := 0; tick < 15; tick++ {
				want := paint(base)
				if got := paint(extra); !slices.Equal(want, got) {
					t.Fatalf("tick %d changed with extra frames", tick)
				}
				extra.SetFadeFrozen(true)
				for i := 0; i < 4; i++ {
					if !slices.Equal(want, paint(extra)) {
						t.Fatalf("tick %d, frozen repaint %d changed pixels", tick, i)
					}
				}
				extra.SetFadeFrozen(false)
				if baseDone != extraDone {
					t.Fatalf("completion timing changed at tick %d", tick)
				}
			}
			if (kind == "screen in" || kind == "screen out") && extraDone != 1 {
				t.Fatalf("completion count: %d", extraDone)
			}
		})
	}
}
