package legacy

/*
#include "defs.h"
*/
import "C"

import "github.com/opennox/opennox/v1/client"

// SetDrawRepaint selects the same rendering phase in Go and C callbacks.
func SetDrawRepaint(v bool) {
	client.DrawRepaint = v
	C.nox_draw_repaint = 0
	if v {
		C.nox_draw_repaint = 1
	}
}
