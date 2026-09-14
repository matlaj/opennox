//go:build !server

package opennox

import (
	"image"
	"time"

	"github.com/spf13/viper"

	"github.com/opennox/libs/platform"

	"github.com/opennox/opennox/v1/client"
	"github.com/opennox/opennox/v1/client/noxrender"
	noxflags "github.com/opennox/opennox/v1/common/flags"
	"github.com/opennox/opennox/v1/internal/frameinterp"
	"github.com/opennox/opennox/v1/legacy"
)

// Frame interpolation.
//
// The game simulation is locked to 30 ticks per second, and originally the client
// rendered exactly one frame per tick. Object and camera positions travel over the
// wire as truncated 16 bit integers (see nox_xxx_netSendComplexObject_518960), so at
// 30 Hz the camera advances by an uneven whole number of pixels per frame. That, plus
// 30 Hz frames landing on a display that refreshes at some unrelated rate, is what
// makes movement judder and smear.
//
// The simulation is left exactly as it was. Instead, the idle time that the main loop
// used to spend sleeping in mainloopFrameLimit is now spent drawing extra frames, with
// every drawable and the camera placed between their previous and current tick
// positions. Positions stay integers, so the legacy C draw code and its fixed struct
// layouts are untouched; they simply receive a different integer each frame.
//
// Ordinary tick draws run callbacks at authoritative positions, then repaint at
// interpolated positions before presenting. This costs an additional draw per tick
// while enabled, but keeps effect updates independent of the presentation clock.
// Repaint callbacks must use DrawRepaint (nox_draw_repaint in C) to skip persistent
// updates. Drawable snapshots protect render caches; they do not undo changes to
// external lists, allocations, or effect histories.
//
// Note that interpolating rather than extrapolating means the picture trails the
// simulation by one tick (~33ms). That is the standard trade and it is why this is
// behind a flag.

const (
	configVideoInterpolation = "video.interpolation"
	configVideoMaxFPS        = "video.max_fps"
)

func init() {
	viper.SetDefault(configVideoInterpolation, false)
	viper.SetDefault(configVideoMaxFPS, 60)
}

func interpEnabled() bool {
	return viper.GetBool(configVideoInterpolation)
}

// interpFrameInterval returns the minimum spacing between rendered frames, or 0 if
// extra frames are disabled. A cap at or below the tick rate means there is nothing
// to gain, so no extra frames are drawn.
func interpFrameInterval() time.Duration {
	fps := viper.GetInt(configVideoMaxFPS)
	if rate := int(noxServer.TickRate()); fps <= rate || fps <= 0 {
		return 0
	}
	if fps > 1000 {
		fps = 1000
	}
	return time.Second / time.Duration(fps)
}

// interpTickDur is the duration of one simulation step.
func interpTickDur() time.Duration {
	rate := noxServer.TickRate()
	if rate == 0 {
		return time.Second / 30
	}
	return time.Second / time.Duration(rate)
}

// Remote timestamps need a local clock: the host's simulation sleep baseline
// does not describe packet arrival times on a joined client.
var interpTimeline frameinterp.Timeline
var interpCam frameinterp.Track
var interpObjects = make(map[*client.Drawable]frameinterp.Track)

func interpReset() {
	interpTimeline = frameinterp.Timeline{}
	interpCam = frameinterp.Track{}
	clear(interpObjects)
	interpSchedule.Reset()
	interpSkipFrame = false
}

func interpBeginTick() {
	if !interpEnabled() {
		interpReset()
		return
	}
	interpTimeline.Observe(noxServer.Frame(), platform.Ticks())
}

func interpTrackDrawable(dr *client.Drawable) {
	if !interpEnabled() {
		return
	}
	t := interpObjects[dr]
	t.Observe(noxServer.Frame(), dr.Pos())
	interpObjects[dr] = t
}

func interpForgetDrawable(dr *client.Drawable) { delete(interpObjects, dr) }

func interpTrackCamera(pos image.Point) {
	interpCam.Observe(noxServer.Frame(), pos)
}

func interpAlpha() float64 {
	if !noxflags.HasGame(noxflags.GameHost) {
		return interpTimeline.Alpha(platform.Ticks(), interpTickDur())
	}
	return min(1, max(0, 1-float64(nox_ticks_getNext())/float64(interpTickDur())))
}

func interpLerp(prev, cur image.Point, alpha float64) image.Point {
	return frameinterp.Lerp(prev, cur, alpha)
}

func interpCameraAt(alpha float64) (image.Point, bool) {
	return interpCam.At(noxServer.Frame(), alpha, client.Nox_drawable_2d_div)
}

type interpSavedDrawable struct {
	dr    *client.Drawable
	state client.Drawable
}

// interpSaved holds authoritative drawable state displaced by the current draw. It is reused between frames to keep the draw path allocation
// free.
var interpSaved []interpSavedDrawable

// interpApply moves the camera and every eligible drawable to where they were part way
// through the current tick, and returns a function that puts them all back. It is a
// no-op when interpolation is off, so the normal draw path is unchanged.
//
// A drawable is eligible only if the server updated it on this tick and the update
// before that was on the immediately preceding tick. Anything updated less often has
// no usable pair of endpoints and is left alone.
func (c *Client) interpApply(vp *noxrender.Viewport) func() {
	return c.interpApplyAt(vp, interpAlpha())
}

func (c *Client) interpApplyAt(vp *noxrender.Viewport, alpha float64) func() {
	if !interpEnabled() || !client.DrawRepaint {
		return func() {}
	}

	savedViewport := *vp
	if pos, ok := interpCameraAt(alpha); ok {
		setCameraPos(vp, pos.X, pos.Y)
	}

	interpSaved = interpSaved[:0]
	frame := c.srv.Frame()
	for dr := c.Objs.FirstList1(); dr != nil; dr = dr.NextPtr {
		// Legacy drawing fills visibility/light caches in the drawable. Keep
		// those writes local to this repaint as well as the displaced position.
		interpSaved = append(interpSaved, interpSavedDrawable{dr: dr, state: *dr})
		if pos, ok := interpObjects[dr].At(frame, alpha, client.Nox_drawable_2d_div); ok {
			dr.SetPos(pos)
		}
	}

	restored := false
	return func() {
		if restored {
			return
		}
		restored = true
		for _, it := range interpSaved {
			*it.dr = it.state
		}
		interpSaved = interpSaved[:0]
		*vp = savedViewport
	}
}

var interpSchedule frameinterp.Scheduler

// Keep one-shot clears visible until the next ordinary world draw.
var interpSkipFrame bool

func (c *Client) renderInterpUntil(budget time.Duration) {
	interpSchedule.Run(platform.Ticks()+budget, interpFrameInterval(), platform.Ticks,
		c.srv.LoopSleep, c.drawInterpFrame, c.copyPixBuffer)
}

func (c *Client) drawInterpFrame() bool {
	if interpSkipFrame || interpFrameInterval() <= 0 {
		return false
	}
	if noxflags.HasEngine(noxflags.EnginePause) || noxflags.HasEngine(noxflags.EngineNoRendering) {
		return false
	}
	if nox_client_gui_flag_815132 != 0 || nox_xxx_checkGameFlagPause_413A50() {
		return false
	}
	if c.ClientPlayerUnit() == nil || !nox_client_isConnected() {
		return false
	}
	start := platform.Ticks()
	defer func() { interpSchedule.ObserveDraw(start, platform.Ticks()) }()
	legacy.SetDrawRepaint(true)
	c.r.SetFadeFrozen(true)
	// Procedural visuals use the legacy RNGs too. Repaints must not change
	// the sequence consumed by the next simulation tick.
	logic, other := c.srv.Rand.Logic.Index(), c.srv.Rand.Other.Index()
	defer func() {
		legacy.SetDrawRepaint(false)
		c.r.SetFadeFrozen(false)
		c.srv.Rand.Logic.Reset(logic)
		c.srv.Rand.Other.Reset(other)
	}()

	c.drawClientFrame()

	sub_437180()
	if legacy.Get_nox_client_gui_flag_1556112() == 0 {
		c.GUI.Draw()
	}
	c.DrawSparks()
	c.nox_client_drawCursorAndTooltips_477830()
	c.r.DrawFade(true)
	return true
}
