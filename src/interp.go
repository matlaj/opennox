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
// Tick frames and extra frames each draw the world once. Tick callbacks use
// authoritative positions for effects and history, and extra frames skip updates.
// Repaint snapshots protect render caches; they do not undo changes to external
// lists, allocations, or effect histories.
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
	for dr := range client.DrawPositions {
		dr.RestoreDrawPosition()
	}
	interpSchedule.Reset()
	interpRemoteDeadline = 0
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

func interpForgetDrawable(dr *client.Drawable) {
	dr.RestoreDrawPosition()
	delete(interpObjects, dr)
}

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
	if !interpEnabled() || interpFrameInterval() == 0 {
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
		if client.DrawRepaint {
			interpSaved = append(interpSaved, interpSavedDrawable{dr: dr, state: *dr})
		}
		if pos, ok := interpObjects[dr].At(frame, alpha, client.Nox_drawable_2d_div); ok {
			client.DrawPositions[dr] = dr.Pos()
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
		for dr := range client.DrawPositions {
			dr.RestoreDrawPosition()
		}
		if client.DrawRepaint {
			*vp = savedViewport
		} else {
			// Keep tick updates such as camera shake, restoring only its origin.
			vp.World = savedViewport.World
		}
	}
}

var interpSchedule frameinterp.Scheduler

// Keep one-shot clears visible until the next ordinary world draw.
var interpSkipFrame bool

// Ordinary frames share the repaint cadence and refresh its cost estimate even
// when a previous slow frame prevented any extra draws.
var interpDrawStart time.Duration

func (c *Client) interpStartFrame() {
	if interpEnabled() {
		interpSchedule.SetVSync(viper.GetBool(configVideoVSync))
		interpSchedule.WaitFrame(interpFrameInterval(), platform.Ticks, c.srv.LoopSleep)
	}
	interpDrawStart = platform.Ticks()
}

// A joined client's old relative limiter would add every VSync overrun to the
// next tick. Keep its 30 Hz polling deadline across loop iterations instead.
var interpRemoteDeadline time.Duration

func (c *Client) interpBeginLoop() {
	if !useFrameLimit || !interpEnabled() || !noxflags.HasGame(noxflags.GameClient) || noxflags.HasGame(noxflags.GameHost) {
		interpRemoteDeadline = 0
		return
	}
	interpRemoteDeadline = frameinterp.NextTick(interpRemoteDeadline, platform.Ticks(), time.Second/30)
}

func (c *Client) interpRateRemaining() time.Duration {
	if interpRemoteDeadline == 0 {
		return c.srv.RateRemaining()
	}
	return max(0, interpRemoteDeadline-platform.Ticks())
}

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
