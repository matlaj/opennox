package client

import "image"

// DrawRepaint is true while repainting between client ticks. Drawing may rebuild
// rendering caches, but must not spawn, move, or destroy simulation objects.
// The client loop owns this flag; legacy.SetDrawRepaint also sets its C counterpart.
var DrawRepaint bool

// DrawPositions retains authoritative positions while the renderer temporarily
// displaces sprites. Tick callbacks use these for motion, emission and history.
var DrawPositions = make(map[*Drawable]image.Point)

func (dr *Drawable) AuthoritativePos() image.Point {
	if pos, ok := DrawPositions[dr]; ok {
		return pos
	}
	return dr.Pos()
}

// RestoreDrawPosition ends a temporary displacement before moving or deleting a
// sprite. Removing the entry prevents the end-of-frame restore from undoing a
// legitimate tick update, or touching a freed/reused sprite.
func (dr *Drawable) RestoreDrawPosition() {
	if pos, ok := DrawPositions[dr]; ok {
		dr.SetPos(pos)
		delete(DrawPositions, dr)
	}
}
