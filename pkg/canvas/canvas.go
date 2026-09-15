// Package canvas is the drawing surface uScope renders into.
//
// It is a thin wrapper over image.RGBA with the primitives a radar scope
// needs and nothing else. Keeping it independent of the framebuffer is what
// lets the whole scene be rendered to a PNG on a Mac, which is how the
// layout gets checked without a uConsole on the desk.
//
// A Canvas is not safe for concurrent use. One goroutine draws, then hands
// the frame to the blitter.
package canvas

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

// bytesPerPixel is the stride of image.RGBA: R, G, B, A, one byte each.
const bytesPerPixel = 4

// opaque is the alpha every palette colour carries. The framebuffer has no
// alpha channel, so anything translucent would be silently flattened anyway.
const opaque uint8 = 0xFF

// ErrSize is returned by New for a zero or negative dimension. image.Rect
// would happily normalise those into an empty rectangle, and an empty canvas
// only fails much later, in the blitter.
var ErrSize = errors.New("canvas: size must be positive")

// The palette. Eight colours is all slice 1 needs, and naming them keeps the
// scene code readable.
//
//nolint:gochecknoglobals // color.RGBA is a struct, so these cannot be const.
var (
	Black   = color.RGBA{R: 0, G: 0, B: 0, A: opaque}
	White   = color.RGBA{R: opaque, G: opaque, B: opaque, A: opaque}
	Red     = color.RGBA{R: opaque, G: 0, B: 0, A: opaque}
	Green   = color.RGBA{R: 0, G: opaque, B: 0, A: opaque}
	Blue    = color.RGBA{R: 0, G: 0, B: opaque, A: opaque}
	Yellow  = color.RGBA{R: opaque, G: opaque, B: 0, A: opaque}
	Magenta = color.RGBA{R: opaque, G: 0, B: opaque, A: opaque}
	Cyan    = color.RGBA{R: 0, G: opaque, B: opaque, A: opaque}
)

// Canvas is a fixed-size RGBA drawing surface anchored at the origin.
type Canvas struct {
	img *image.RGBA
}

// New allocates a canvas of width by height pixels.
func New(width, height int) (*Canvas, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: got %dx%d", ErrSize, width, height)
	}

	return &Canvas{img: image.NewRGBA(image.Rect(0, 0, width, height))}, nil
}

// Bounds returns the drawable rectangle, always anchored at (0, 0).
func (c *Canvas) Bounds() image.Rectangle {
	return c.img.Rect
}

// Image returns the backing image. It aliases the canvas rather than copying
// it, because the blitter reads this every frame and a copy per frame is the
// one allocation the render path cannot afford.
func (c *Canvas) Image() *image.RGBA {
	return c.img
}

// Clear paints the whole canvas one colour.
//
// It fills the first row and then copies that row down, which turns most of
// the work into memmove. A per-pixel loop shows up in a profile at 1280x720.
func (c *Canvas) Clear(col color.RGBA) {
	pix := c.img.Pix
	if len(pix) == 0 {
		return
	}

	stride := c.img.Stride
	row := pix[:stride]

	for x := 0; x < stride; x += bytesPerPixel {
		row[x], row[x+1], row[x+2], row[x+3] = col.R, col.G, col.B, col.A
	}

	for y := stride; y < len(pix); y += stride {
		copy(pix[y:y+stride], row)
	}
}

// Set paints one pixel. Coordinates outside the canvas are dropped, so
// callers can draw shapes that run off the edge without clipping first.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom; this is the hot path.
func (c *Canvas) Set(x, y int, col color.RGBA) {
	rect := c.img.Rect
	if x < rect.Min.X || x >= rect.Max.X || y < rect.Min.Y || y >= rect.Max.Y {
		return
	}

	off := c.img.PixOffset(x, y)
	pix := c.img.Pix[off : off+bytesPerPixel : off+bytesPerPixel]
	pix[0], pix[1], pix[2], pix[3] = col.R, col.G, col.B, col.A
}

// FillRect fills a rectangle, clipped to the canvas.
func (c *Canvas) FillRect(rect image.Rectangle, col color.RGBA) {
	area := rect.Intersect(c.img.Rect)
	if area.Empty() {
		return
	}

	for y := area.Min.Y; y < area.Max.Y; y++ {
		c.hline(area.Min.X, area.Max.X-1, y, col)
	}
}

// Line draws a straight line with Bresenham's integer algorithm. Clipping
// happens in Set, so lines may start or end off-canvas.
//
//nolint:varnamelen // x0, y0, x1, y1 is how Bresenham is written everywhere.
func (c *Canvas) Line(x0, y0, x1, y1 int, col color.RGBA) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := step(x0, x1), step(y0, y1)
	err := dx + dy
	x, y := x0, y0

	for {
		c.Set(x, y, col)

		if x == x1 && y == y1 {
			return
		}

		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x += sx
		}

		if e2 <= dx {
			err += dx
			y += sy
		}
	}
}

// Circle draws a one-pixel outline with the midpoint algorithm. A negative
// radius draws nothing; a zero radius draws the centre pixel.
func (c *Canvas) Circle(centerX, centerY, radius int, col color.RGBA) {
	if radius < 0 {
		return
	}

	//nolint:varnamelen // x, y walk the octant; the midpoint algorithm names them so.
	x, y := radius, 0
	err := 1 - radius

	for x >= y {
		c.octants(centerX, centerY, x, y, col)
		y++

		if err < 0 {
			err += 2*y + 1

			continue
		}

		x--
		err += 2*(y-x) + 1
	}
}

// FillCircle draws a filled disc, one horizontal span per row.
//
// The half-width comes from math.Sqrt rather than an incremental integer
// test: the operand is an exact integer well under 2^53, so the result is
// exact, and it costs one sqrt per row instead of one compare per pixel.
func (c *Canvas) FillCircle(centerX, centerY, radius int, col color.RGBA) {
	if radius < 0 {
		return
	}

	for dy := -radius; dy <= radius; dy++ {
		half := int(math.Sqrt(float64(radius*radius - dy*dy)))
		c.hline(centerX-half, centerX+half, centerY+dy, col)
	}
}

// hline fills the inclusive span from x0 to x1 on row y.
func (c *Canvas) hline(x0, x1, y int, col color.RGBA) {
	for x := x0; x <= x1; x++ {
		c.Set(x, y, col)
	}
}

// octants mirrors one midpoint-circle point into all eight octants.
//
//nolint:varnamelen // dx, dy are offsets from the centre, spelled as usual.
func (c *Canvas) octants(centerX, centerY, dx, dy int, col color.RGBA) {
	c.Set(centerX+dx, centerY+dy, col)
	c.Set(centerX-dx, centerY+dy, col)
	c.Set(centerX+dx, centerY-dy, col)
	c.Set(centerX-dx, centerY-dy, col)
	c.Set(centerX+dy, centerY+dx, col)
	c.Set(centerX-dy, centerY+dx, col)
	c.Set(centerX+dy, centerY-dx, col)
	c.Set(centerX-dy, centerY-dx, col)
}

// abs is the integer absolute value; math.Abs would round-trip through
// float64 for no reason.
func abs(v int) int {
	if v < 0 {
		return -v
	}

	return v
}

// step returns the direction to walk from a towards b.
func step(from, to int) int {
	if from > to {
		return -1
	}

	return 1
}
