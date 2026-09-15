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

//export nox_drawable_authoritative_pos
func nox_drawable_authoritative_pos(dr *C.nox_drawable, out *C.int2) {
	pos := asDrawable(dr).AuthoritativePos()
	out.field_0, out.field_4 = C.int(pos.X), C.int(pos.Y)
}
