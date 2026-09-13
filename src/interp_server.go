//go:build server

package opennox

import (
	"image"
	"time"
)

// Frame interpolation is a client-side rendering concern; see interp.go. The dedicated
// server has no renderer, so these are stubs.

func interpEnabled() bool { return false }

func interpTrackCamera(pos image.Point) {}

func (c *Client) renderInterpUntil(budget time.Duration) {
	c.srv.LoopSleep(budget)
}
