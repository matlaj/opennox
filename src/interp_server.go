//go:build server

package opennox

import (
	"image"
	"time"

	"github.com/opennox/opennox/v1/client"
)

// Frame interpolation is a client-side rendering concern; see interp.go. The dedicated
// server has no renderer, so these are stubs.

func interpEnabled() bool { return false }

func interpTrackCamera(pos image.Point) {}

func (c *Client) renderInterpUntil(budget time.Duration) {
	c.srv.LoopSleep(budget)
}

func interpReset()                             {}
func interpBeginTick()                         {}
func interpTrackDrawable(dr *client.Drawable)  {}
func interpForgetDrawable(dr *client.Drawable) {}

func (c *Client) interpBeginLoop()                   {}
func (c *Client) interpRateRemaining() time.Duration { return c.srv.RateRemaining() }
