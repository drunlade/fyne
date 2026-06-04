package canvas

import "image"

// DirtyRegionReporter may be implemented by the object associated with a
// canvas.Raster to report the pixel-space bounding rectangle of what
// actually changed in the most recent Generator call. Fyne's GL renderer
// uses this to narrow the scissor rect so only the changed region is
// repainted, skipping draw calls for widgets outside it.
type DirtyRegionReporter interface {
	DirtyPixelBounds() image.Rectangle
}
