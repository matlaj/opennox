//go:build !server

package opennox

import (
	"image"
	"testing"

	"github.com/opennox/opennox/v1/client"
	"github.com/opennox/opennox/v1/client/noxrender"
	"github.com/opennox/opennox/v1/internal/frameinterp"
	"github.com/opennox/opennox/v1/legacy"
	"github.com/opennox/opennox/v1/server"
	"github.com/spf13/viper"
)

func TestInterpPreservesTickPositions(t *testing.T) {
	oldServer, oldCamera, oldRepaint := noxServer, interpCam, client.DrawRepaint
	oldEnabled := interpEnabled()
	t.Cleanup(func() {
		noxServer, interpCam = oldServer, oldCamera
		viper.Set(configVideoInterpolation, oldEnabled)
		legacy.SetDrawRepaint(oldRepaint)
	})
	viper.Set(configVideoInterpolation, true)
	s := &Server{Server: new(server.Server)}
	s.SetFrame(2)
	s.SetTickRate(30)
	noxServer = s
	interpCam = frameinterp.Track{}
	c := &Client{Client: &client.Client{Server: s.Server}, srv: s}
	dr := &client.Drawable{Field_8: 100, Field_9: 100, Field_5: 2, Field_10: 1}
	dr.SetPos(image.Pt(120, 110))
	c.Objs.List1 = dr
	var track frameinterp.Track
	track.Observe(1, image.Pt(100, 100))
	track.Observe(2, dr.Pos())
	interpObjects[dr] = track
	t.Cleanup(func() {
		delete(interpObjects, dr)
		dr.RestoreDrawPosition()
		interpSaved = interpSaved[:0]
	})
	vp := &noxrender.Viewport{Size: image.Pt(640, 480), Jiggle12: 3}
	interpCam.Observe(1, image.Pt(100, 100))
	interpCam.Observe(2, image.Pt(120, 110))
	for _, repaint := range []bool{false, true} {
		for _, alpha := range []float64{0, 0.25, 0.5, 1} {
			legacy.SetDrawRepaint(repaint)
			saved, savedVP := *dr, *vp
			restore := c.interpApplyAt(vp, alpha)
			if dr.Pos() != interpLerp(image.Pt(100, 100), saved.Pos(), alpha) {
				t.Fatal("frame did not interpolate")
			}
			if dr.AuthoritativePos() != saved.Pos() {
				t.Fatal("tick effect received interpolated coordinates")
			}
			if repaint {
				pos := dr.Pos()
				c.Nox_xxx_updateSpritePosition_49AA90(dr, 900, 900)
				if dr.Pos() != pos {
					t.Fatal("repaint moved a sprite")
				}
			}
			dr.Field_121 = 42 // visibility cache
			vp.Jiggle12 = 99
			restore()
			restore() // deferred and explicit restoration are safe together
			if !repaint {
				saved.Field_121 = 42
				savedVP.Jiggle12 = 99
			}
			if *dr != saved || *vp != savedVP {
				t.Fatal("restoration lost tick state or kept repaint state")
			}
			p := &particleFx{flags: 4, drawable8: dr, ticksLeft: 10}
			if !partfxUpdateDef(p) || p.x16 != 20<<16 || p.y16 != 10<<16 {
				t.Fatal("attached particle did not receive full owner displacement")
			}
		}
	}
	legacy.SetDrawRepaint(false)
	restore := c.interpApplyAt(vp, 0.5)
	// A real position update (or deletion) during drawing must end the temporary
	// displacement, so the outer restore cannot resurrect the old position.
	dr.RestoreDrawPosition()
	dr.SetPos(image.Pt(130, 115))
	restore()
	if dr.Pos() != image.Pt(130, 115) {
		t.Fatal("tick movement was undone")
	}
}

func TestInterpRepaintSkipsEffectsAndLifecycle(t *testing.T) {
	oldRepaint := client.DrawRepaint
	legacy.SetDrawRepaint(true)
	t.Cleanup(func() { legacy.SetDrawRepaint(oldRepaint) })
	// A repaint must not reach the uninitialized effect allocators or RNGs.
	c := &Client{Client: new(client.Client)}
	dr := &client.Drawable{Buffs: 1<<server.ENCHANT_HASTED | 1<<server.ENCHANT_RUN |
		1<<server.ENCHANT_SHOCK | 1<<server.ENCHANT_SLOWED | 1<<server.ENCHANT_INFRAVISION}
	saved := *dr
	vp := new(noxrender.Viewport)
	c.drawCreatureBackEffects(vp, dr)
	c.drawCreatureFrontEffects(vp, dr)
	if c.Nox_new_drawable_for_thing(1) != nil {
		t.Fatal("repaint allocated a drawable")
	}
	c.Nox_xxx_spriteDeleteStatic_45A4E0_drawable(dr)
	c.Nox_xxx_spriteDelete_45A4B0(dr)
	c.Objs.List34Add(dr)
	c.Objs.MinimapAdd(dr, 1)
	c.Objs.TransparentDecay(dr, 10)
	if *dr != saved || c.Objs.Count != 0 {
		t.Fatal("repaint changed drawable lifecycle state")
	}
}
