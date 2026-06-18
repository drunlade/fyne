package glfw

import (
	"image"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/internal"
	"fyne.io/fyne/v2/internal/app"
	"fyne.io/fyne/v2/internal/build"
	"fyne.io/fyne/v2/internal/driver"
	"fyne.io/fyne/v2/internal/driver/common"
	"fyne.io/fyne/v2/theme"
)

// Declare conformity with Canvas interface
var _ fyne.Canvas = (*glCanvas)(nil)

type glCanvas struct {
	common.Canvas

	content fyne.CanvasObject
	menu    fyne.CanvasObject
	padded  bool
	size    fyne.Size

	onTypedRune func(rune)
	onTypedKey  func(*fyne.KeyEvent)
	onKeyDown   func(*fyne.KeyEvent)
	onKeyUp     func(*fyne.KeyEvent)
	// shortcut    fyne.ShortcutHandler

	scale, detectedScale, texScale float32

	context         driver.WithContext
	webExtraWindows *container.MultipleWindows

	// objPosMu guards objPosCache which caches each object's absolute canvas
	// position as recorded during the previous frame's WalkTrees. Used to
	// compute the dirty pixel rect before the current frame's paint walk.
	objPosMu    sync.RWMutex
	objPosCache map[fyne.CanvasObject]objPosEntry
}

// objPosEntry caches both the absolute canvas position (pos passed by WalkTrees)
// and the object's own local Position() at the time of caching. The local
// position is used to compute a delta in computeDirtyRect so that objects that
// moved between frames (e.g. the cursor) are correctly covered by the dirty rect.
type objPosEntry struct {
	absPos   fyne.Position
	localPos fyne.Position
}

func (c *glCanvas) Capture() image.Image {
	var img image.Image
	c.context.(*window).RunWithContext(func() {
		img = c.Painter().Capture(c)
	})
	return img
}

func (c *glCanvas) Content() fyne.CanvasObject {
	return c.content
}

func (c *glCanvas) DismissMenu() bool {
	if c.menu != nil && c.menu.(*MenuBar).IsActive() {
		c.menu.(*MenuBar).Toggle()
		return true
	}
	return false
}

func (c *glCanvas) InteractiveArea() (fyne.Position, fyne.Size) {
	return fyne.Position{}, c.Size()
}

func (c *glCanvas) MinSize() fyne.Size {
	return c.canvasSize(c.content.MinSize())
}

func (c *glCanvas) OnKeyDown() func(*fyne.KeyEvent) {
	return c.onKeyDown
}

func (c *glCanvas) OnKeyUp() func(*fyne.KeyEvent) {
	return c.onKeyUp
}

func (c *glCanvas) OnTypedKey() func(*fyne.KeyEvent) {
	return c.onTypedKey
}

func (c *glCanvas) OnTypedRune() func(rune) {
	return c.onTypedRune
}

func (c *glCanvas) Padded() bool {
	return c.padded
}

func (c *glCanvas) PixelCoordinateForPosition(pos fyne.Position) (int, int) {
	multiple := c.scale * c.texScale
	scaleInt := func(x float32) int {
		return int(math.Round(float64(x * multiple)))
	}

	return scaleInt(pos.X), scaleInt(pos.Y)
}

func (c *glCanvas) Resize(size fyne.Size) {
	// This might not be the ideal solution, but it effectively avoid the first frame to be blurry due to the
	// rounding of the size to the loower integer when scale == 1. It does not affect the other cases as far as we tested.
	// This can easily be seen with fyne/cmd/hello and a scale == 1 as the text will happear blurry without the following line.
	var nearestSize fyne.Size
	if c.scale == 1 {
		nearestSize = fyne.NewSize(float32(math.Ceil(float64(size.Width))), float32(math.Ceil(float64(size.Height))))
	} else {
		nearestSize = size
	}

	c.size = nearestSize

	if c.webExtraWindows != nil {
		c.webExtraWindows.Resize(size)
	}
	for _, overlay := range c.Overlays().List() {
		overlay.Resize(nearestSize)
	}

	content := c.content
	contentSize := c.contentSize(nearestSize)
	contentPos := c.contentPos()
	menu := c.menu
	menuHeight := c.menuHeight()

	content.Resize(contentSize)
	content.Move(contentPos)

	if menu != nil {
		menu.Refresh()
		menu.Resize(fyne.NewSize(nearestSize.Width, menuHeight))
	}
}

func (c *glCanvas) Scale() float32 {
	return c.scale
}

func (c *glCanvas) SetContent(content fyne.CanvasObject) {
	newSize := c.size.Max(c.canvasSize(content.MinSize()))

	c.setContent(content)

	c.Resize(newSize)
	c.SetFullDirty()
}

func (c *glCanvas) SetOnKeyDown(typed func(*fyne.KeyEvent)) {
	c.onKeyDown = typed
}

func (c *glCanvas) SetOnKeyUp(typed func(*fyne.KeyEvent)) {
	c.onKeyUp = typed
}

func (c *glCanvas) SetOnTypedKey(typed func(*fyne.KeyEvent)) {
	c.onTypedKey = typed
}

func (c *glCanvas) SetOnTypedRune(typed func(rune)) {
	c.onTypedRune = typed
}

func (c *glCanvas) SetPadded(padded bool) {
	c.padded = padded

	c.content.Move(c.contentPos())
}

func (c *glCanvas) reloadScale() {
	w := c.context.(*window)
	windowVisible := w.visible
	if !windowVisible {
		return
	}

	c.scale = w.calculatedScale()
	c.SetFullDirty()

	c.context.RescaleContext()
}

func (c *glCanvas) Size() fyne.Size {
	return c.size
}

func (c *glCanvas) ToggleMenu() {
	if c.menu != nil {
		c.menu.(*MenuBar).Toggle()
	}
}

func (c *glCanvas) buildMenu(w *window, m *fyne.MainMenu) {
	c.setMenuOverlay(nil)
	if m == nil {
		return
	}
	if build.HasNativeMenu {
		setupNativeMenu(w, m)
	} else {
		c.setMenuOverlay(buildMenuOverlay(m, w))
	}
}

// canvasSize computes the needed canvas size for the given content size
func (c *glCanvas) canvasSize(contentSize fyne.Size) fyne.Size {
	canvasSize := contentSize.Add(fyne.NewSize(0, c.menuHeight()))
	if c.Padded() {
		return canvasSize.Add(fyne.NewSquareSize(theme.Padding() * 2))
	}
	return canvasSize
}

func (c *glCanvas) contentPos() fyne.Position {
	contentPos := fyne.NewPos(0, c.menuHeight())
	if c.Padded() {
		return contentPos.Add(fyne.NewSquareOffsetPos(theme.Padding()))
	}
	return contentPos
}

func (c *glCanvas) contentSize(canvasSize fyne.Size) fyne.Size {
	contentSize := fyne.NewSize(canvasSize.Width, canvasSize.Height-c.menuHeight())
	if c.Padded() {
		return contentSize.Subtract(fyne.NewSquareSize(theme.Padding() * 2))
	}
	return contentSize
}

func (c *glCanvas) menuHeight() float32 {
	if c.menu == nil {
		return 0 // no menu or native menu -> does not consume space on the canvas
	}

	return c.menu.MinSize().Height
}

func (c *glCanvas) overlayChanged() {
	c.SetFullDirty()
}

func (c *glCanvas) paint(size fyne.Size) {
	c.paintWithDirty(size, image.Rectangle{})
}

// paintWithDirty paints the canvas with optional dirty-region optimisation.
// When dirtyPx is non-zero (non-empty), only objects whose pixel bounds
// overlap it are drawn; the clear is scissored to dirtyPx only (FBO keeps
// unchanged content outside it). When dirtyPx is zero the full canvas is
// cleared and all objects are drawn (same as the original paint()).
func (c *glCanvas) paintWithDirty(size fyne.Size, dirtyPx image.Rectangle) {
	clips := &internal.ClipStack{}
	if c.Content() == nil {
		return
	}

	useDirty := !dirtyPx.Empty()
	pixScale := c.scale * c.texScale
	fbH := int(math.Round(float64(size.Height * pixScale)))

	if useDirty {
		c.Painter().ClearRegion(dirtyPx, fbH)
		// Scissor the entire walk to the dirty rect so that full-extent
		// widgets (e.g. the terminal raster) cannot overwrite FBO pixels
		// belonging to UI objects that are within the raster's bounds but
		// outside the changed cells. Those objects are culled from Paint
		// calls and their FBO pixels must be preserved.
		dirtyLogPos := fyne.NewPos(float32(dirtyPx.Min.X)/pixScale, float32(dirtyPx.Min.Y)/pixScale)
		dirtyLogSize := fyne.NewSize(float32(dirtyPx.Dx())/pixScale, float32(dirtyPx.Dy())/pixScale)
		baseClip := clips.Push(dirtyLogPos, dirtyLogSize)
		c.Painter().StartClipping(baseClip.Rect())
		defer func() {
			clips.Pop()
			c.Painter().StopClipping()
		}()
	} else {
		c.Painter().Clear()
	}

	paint := func(node *common.RenderCacheNode, pos fyne.Position) {
		obj := node.Obj()
		if driver.IsClip(obj) {
			inner := clips.Push(pos, obj.Size())
			c.Painter().StartClipping(inner.Rect())
		}
		if size.Width <= 0 || size.Height <= 0 { // iconifying on Windows can do bad things
			return
		}
		// Dirty-region culling: skip objects entirely outside the dirty rect.
		if useDirty && !c.objOverlapsDirty(pos, obj.Size(), dirtyPx, pixScale) {
			return
		}
		c.Painter().Paint(obj, pos, size, clips.Top())
		// Cache absolute position and local position for next-frame dirty-rect
		// computation. Both are needed to detect movement between frames.
		c.objPosMu.Lock()
		if c.objPosCache == nil {
			c.objPosCache = make(map[fyne.CanvasObject]objPosEntry)
		}
		c.objPosCache[obj] = objPosEntry{absPos: pos, localPos: obj.Position()}
		c.objPosMu.Unlock()
	}
	afterPaint := func(node *common.RenderCacheNode, pos fyne.Position) {
		if driver.IsClip(node.Obj()) {
			clips.Pop()
			if top := clips.Top(); top != nil {
				c.Painter().StartClipping(top.Rect())
			} else {
				c.Painter().StopClipping()
			}
		}

		if build.Mode == fyne.BuildDebug {
			c.DrawDebugOverlay(node.Obj(), pos, size, clips.Top())
		}
	}
	c.WalkTrees(paint, afterPaint)
}

// objOverlapsDirty reports whether an object at canvas position pos with the
// given size has any pixel-space overlap with the dirty rect dirtyPx.
// pos and sz are in Fyne logical units; pixScale converts to pixels.
func (c *glCanvas) objOverlapsDirty(pos fyne.Position, sz fyne.Size, dirtyPx image.Rectangle, pixScale float32) bool {
	if sz.Width <= 0 || sz.Height <= 0 {
		return false
	}
	x0 := int(pos.X * pixScale)
	y0 := int(pos.Y * pixScale)
	x1 := int(math.Ceil(float64((pos.X + sz.Width) * pixScale)))
	y1 := int(math.Ceil(float64((pos.Y + sz.Height) * pixScale)))
	objPx := image.Rect(x0, y0, x1, y1)
	return objPx.Overlaps(dirtyPx)
}

// computeDirtyRect estimates the pixel dirty rect from the given dirty object
// set using cached canvas positions from the previous frame. Returns a zero
// rectangle if the dirty rect cannot be computed (unknown positions) and a
// full-repaint should be done instead.
func (c *glCanvas) computeDirtyRect(dirtyObjs map[fyne.CanvasObject]struct{}, pixScale float32) image.Rectangle {
	if len(dirtyObjs) == 0 {
		return image.Rectangle{}
	}

	c.objPosMu.RLock()
	defer c.objPosMu.RUnlock()

	var dirty image.Rectangle
	for obj := range dirtyObjs {
		// Rasters with a DirtyReporter give us tight pixel bounds.
		if rast, ok := obj.(*canvas.Raster); ok && rast.DirtyReporter != nil {
			bounds := rast.DirtyReporter.DirtyPixelBounds()
			if bounds.Empty() {
				continue
			}
			// Offset by the raster's last-known canvas position.
			if entry, found := c.objPosCache[obj]; found {
				rasterOff := image.Pt(int(entry.absPos.X*pixScale), int(entry.absPos.Y*pixScale))
				bounds = bounds.Add(rasterOff)
				dirty = dirty.Union(bounds)
				continue
			}
			// Position unknown → can't use tight bounds, fall back.
			return image.Rectangle{}
		}
		// Other objects: use their full size at both their previous absolute
		// position (so the old area gets cleared) and their current absolute
		// position (so objects that moved, like the cursor, are not culled by
		// objOverlapsDirty during the paint walk).
		entry, found := c.objPosCache[obj]
		if !found {
			return image.Rectangle{} // position unknown → full repaint
		}
		sz := obj.Size()

		// Old bounds — where the object was in the previous frame.
		x0 := int(entry.absPos.X * pixScale)
		y0 := int(entry.absPos.Y * pixScale)
		x1 := int(math.Ceil(float64((entry.absPos.X + sz.Width) * pixScale)))
		y1 := int(math.Ceil(float64((entry.absPos.Y + sz.Height) * pixScale)))
		dirty = dirty.Union(image.Rect(x0, y0, x1, y1))

		// New bounds — adjusted for any movement since last frame. An object's
		// local Position() reflects the current state (set by Move() calls),
		// while entry.localPos is what Position() returned at paint time. The
		// delta maps correctly to an absolute canvas displacement as long as the
		// parent container hasn't also moved (true in practice for the cursor).
		delta := obj.Position().Subtract(entry.localPos)
		curAbs := entry.absPos.Add(delta)
		x0 = int(curAbs.X * pixScale)
		y0 = int(curAbs.Y * pixScale)
		x1 = int(math.Ceil(float64((curAbs.X + sz.Width) * pixScale)))
		y1 = int(math.Ceil(float64((curAbs.Y + sz.Height) * pixScale)))
		dirty = dirty.Union(image.Rect(x0, y0, x1, y1))
	}
	return dirty
}

// healDirtyRasterSpill guards against a scissor race on dirty-region rasters (the
// terminal grid). The FBO dirty rect is built from a DirtyPixelBounds scan, but a
// raster's content can be mutated (PTY output) between that scan and the actual
// render in the paint walk, so the render may touch pixels outside the scissor.
// Those pixels are clipped out of the FBO yet marked clean by the raster, so they
// would stay stale until something forces a full repaint. When a refreshed raster
// reports it rendered outside the scissor this frame, mark the canvas fully dirty
// so the next frame repaints the spilled region (the texture already holds the
// correct pixels — only the FBO copy was clipped). Costs one full repaint, and
// only when the race actually fires.
func (c *glCanvas) healDirtyRasterSpill(dirtyObjs map[fyne.CanvasObject]struct{}, dirtyPx image.Rectangle, pixScale float32) {
	if len(dirtyObjs) == 0 || dirtyPx.Empty() {
		return
	}
	c.objPosMu.RLock()
	defer c.objPosMu.RUnlock()
	for obj := range dirtyObjs {
		rast, ok := obj.(*canvas.Raster)
		if !ok || rast.DirtyReporter == nil {
			continue
		}
		reporter, ok := rast.DirtyReporter.(interface{ LastDirtyBounds() image.Rectangle })
		if !ok {
			continue
		}
		rendered := reporter.LastDirtyBounds()
		if rendered.Empty() {
			continue
		}
		entry, found := c.objPosCache[obj]
		if !found {
			continue
		}
		// Mirror computeDirtyRect's raster offset so the spaces match exactly.
		off := image.Pt(int(entry.absPos.X*pixScale), int(entry.absPos.Y*pixScale))
		if !rendered.Add(off).In(dirtyPx) {
			c.SetFullDirty()
			return
		}
	}
}

func (c *glCanvas) setContent(content fyne.CanvasObject) {
	c.content = content
	c.SetContentTreeAndFocusMgr(content)
}

func (c *glCanvas) setMenuOverlay(b fyne.CanvasObject) {
	c.menu = b
	c.SetMenuTreeAndFocusMgr(b)

	if c.menu != nil && !c.size.IsZero() {
		c.content.Resize(c.contentSize(c.size))
		c.content.Move(c.contentPos())

		c.menu.Refresh()
		c.menu.Resize(fyne.NewSize(c.size.Width, c.menu.MinSize().Height))
	}
}

func (c *glCanvas) applyThemeOutOfTreeObjects() {
	if c.menu != nil {
		app.ApplyThemeTo(c.menu, c) // Ensure our menu gets the theme change message as it's out-of-tree
	}

	c.SetPadded(c.padded) // refresh the padding for potential theme differences
}

func newCanvas() *glCanvas {
	c := &glCanvas{scale: 1.0, texScale: 1.0, padded: true}
	connectKeyboard(c)
	c.Initialize(c, c.overlayChanged)
	c.setContent(&canvas.Rectangle{FillColor: theme.Color(theme.ColorNameBackground)})
	return c
}
