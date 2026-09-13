//go:build !server

package opennox

import (
	"image"
	"math"
	"time"

	"github.com/spf13/viper"

	"github.com/opennox/libs/platform"

	"github.com/opennox/opennox/v1/client"
	"github.com/opennox/opennox/v1/client/noxrender"
	noxflags "github.com/opennox/opennox/v1/common/flags"
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
// Note that interpolating rather than extrapolating means the picture trails the
// simulation by one tick (~33ms). That is the standard trade and it is why this is
// behind a flag.

const (
	configVideoInterpolation = "video.interpolation"
	configVideoMaxFPS        = "video.max_fps"
)

// interpMaxStep2 is the squared distance beyond which a drawable is assumed to have
// teleported rather than moved, and is drawn at its current position instead of being
// smeared across the map. It matches one spatial index bucket (Nox_drawable_2d_div).
const interpMaxStep2 = client.Nox_drawable_2d_div * client.Nox_drawable_2d_div

func init() {
	viper.SetDefault(configVideoInterpolation, false)
	viper.SetDefault(configVideoMaxFPS, 60)
}

// interpNoAdvance is set while drawing an extra frame between ticks. Draw code that
// also steps state must check it and skip the state change; the drawing itself still
// has to happen or the affected element would flicker at the tick rate.
var interpNoAdvance bool

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

// interpAlpha reports how far the current real time has progressed through the
// current tick, as a fraction in [0,1]. nox_ticks_getNext reports the time still
// owed to this tick, measured against a baseline that only resets on a hitch.
func interpAlpha() float64 {
	a := 1 - float64(nox_ticks_getNext())/float64(interpTickDur())
	if a < 0 {
		return 0
	}
	if a > 1 {
		return 1
	}
	return a
}

func interpLerp(prev, cur image.Point, alpha float64) image.Point {
	d := cur.Sub(prev)
	return image.Point{
		X: prev.X + int(math.Round(float64(d.X)*alpha)),
		Y: prev.Y + int(math.Round(float64(d.Y)*alpha)),
	}
}

// interpCamTrack remembers the last two camera positions the server sent, along with
// the ticks they arrived on.
type interpCamTrack struct {
	prev, cur           image.Point
	prevFrame, curFrame uint32
	valid               bool
}

var interpCam interpCamTrack

// interpTrackCamera records a camera position pushed by the server. It is called from
// nox_xxx_cliUpdateCameraPos_435600, which may fire more than once per tick, so the
// previous position only rolls over when the tick actually changes.
func interpTrackCamera(pos image.Point) {
	fr := noxServer.Frame()
	if !interpCam.valid {
		interpCam = interpCamTrack{prev: pos, cur: pos, prevFrame: fr, curFrame: fr, valid: true}
		return
	}
	if fr != interpCam.curFrame {
		interpCam.prev, interpCam.prevFrame = interpCam.cur, interpCam.curFrame
	}
	interpCam.cur, interpCam.curFrame = pos, fr
}

// interpCameraAt returns the camera position for the given point within the tick.
// It reports false when the last two updates were not exactly one tick apart, or when
// the camera jumped far enough to be a teleport or a map change; in both cases the
// caller keeps the position the server gave it.
func interpCameraAt(alpha float64) (image.Point, bool) {
	t := interpCam
	if !t.valid || t.curFrame != noxServer.Frame() || t.curFrame != t.prevFrame+1 {
		return image.Point{}, false
	}
	d := t.cur.Sub(t.prev)
	if d.X == 0 && d.Y == 0 {
		return image.Point{}, false
	}
	if d.X*d.X+d.Y*d.Y > interpMaxStep2 {
		return image.Point{}, false
	}
	return interpLerp(t.prev, t.cur, alpha), true
}

type interpSavedPos struct {
	dr  *client.Drawable
	pos image.Point
}

// interpSaved holds the true positions displaced by the current draw, so they can be
// put back afterwards. It is reused between frames to keep the draw path allocation
// free.
var interpSaved []interpSavedPos

// interpApply moves the camera and every eligible drawable to where they were part way
// through the current tick, and returns a function that puts them all back. It is a
// no-op when interpolation is off, so the normal draw path is unchanged.
//
// A drawable is eligible only if the server updated it on this tick and the update
// before that was on the immediately preceding tick. Anything updated less often has
// no usable pair of endpoints and is left alone.
func (c *Client) interpApply(vp *noxrender.Viewport) func() {
	if !interpEnabled() {
		return func() {}
	}
	alpha := interpAlpha()

	savedWorld := vp.World
	camMoved := false
	if pos, ok := interpCameraAt(alpha); ok {
		setCameraPos(vp, pos.X, pos.Y)
		camMoved = true
	}

	interpSaved = interpSaved[:0]
	frame := c.srv.Frame()
	for dr := c.Objs.FirstList1(); dr != nil; dr = dr.NextPtr {
		if dr.Field_5 != frame || dr.Field_5 != dr.Field_10+1 {
			continue
		}
		cur, prev := dr.Pos(), dr.Point8()
		d := cur.Sub(prev)
		if d.X == 0 && d.Y == 0 {
			continue
		}
		if d.X*d.X+d.Y*d.Y > interpMaxStep2 {
			continue
		}
		interpSaved = append(interpSaved, interpSavedPos{dr: dr, pos: cur})
		dr.SetPos(interpLerp(prev, cur, alpha))
	}

	return func() {
		for _, it := range interpSaved {
			it.dr.SetPos(it.pos)
		}
		interpSaved = interpSaved[:0]
		if camMoved {
			vp.World = savedWorld
		}
	}
}

// renderInterpUntil fills the time the main loop would otherwise sleep away with extra
// interpolated frames, returning once the next tick is due. budget is the time left
// until then.
func (c *Client) renderInterpUntil(budget time.Duration) {
	step := interpFrameInterval()
	if step <= 0 {
		c.srv.LoopSleep(budget)
		return
	}
	deadline := platform.Ticks() + budget
	for {
		rem := deadline - platform.Ticks()
		if rem <= 0 {
			return
		}
		if step >= rem {
			// Not enough time left to place another frame before the tick.
			c.srv.LoopSleep(rem)
			return
		}
		c.srv.LoopSleep(step)
		c.renderInterpFrame()
	}
}

// renderInterpFrame draws and presents one extra frame between ticks. It deliberately
// skips the parts of the tick frame that step state rather than draw: screen particles
// advance and spawn inside their own draw callbacks, and a screenshot is a one-shot
// request that belongs to the tick that asked for it.
func (c *Client) renderInterpFrame() {
	if noxflags.HasEngine(noxflags.EnginePause) || noxflags.HasEngine(noxflags.EngineNoRendering) {
		return
	}
	if nox_client_gui_flag_815132 != 0 || nox_xxx_checkGameFlagPause_413A50() {
		return
	}
	if c.ClientPlayerUnit() == nil || !nox_client_isConnected() {
		return
	}
	interpNoAdvance = true
	c.r.SetFadeFrozen(true)
	defer func() {
		interpNoAdvance = false
		c.r.SetFadeFrozen(false)
	}()

	c.drawClientFrame()

	sub_437180()
	if legacy.Get_nox_client_gui_flag_1556112() == 0 {
		c.GUI.Draw()
	}
	c.nox_client_drawCursorAndTooltips_477830()
	c.r.DrawFade(true)
	c.copyPixBuffer()
}
