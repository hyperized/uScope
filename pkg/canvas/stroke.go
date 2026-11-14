package canvas

import (
	"image"
	"image/color"
	"math"
)

// The Stroke methods are the outline primitives with a line weight.
//
// Each one takes its width as an explicit argument rather than through an
// option, because they are called on the render path, a frame's worth of
// times, and pkg/text already shows what an option list there costs to keep
// off the heap. A width of one or less draws exactly what the one-pixel method
// of the same shape draws, pixel for pixel, which is what lets a scene pass
// its scale straight through without a branch of its own: at the size the
// scene was designed at nothing moves.
//
// The one-pixel methods keep their own names and signatures. internal/pattern
// draws with them and has no width to pass, and a width argument it always
// set to one would be noise at every call.

// StrokeLineAA draws an anti-aliased line width pixels thick.
//
// It is LineAA's coverage rule widened. Wu's algorithm gives each column of
// the line a band one pixel tall along the minor axis and splits it between
// the two pixels it straddles; this gives the band width pixels and splits it
// between however many it straddles. The band is measured along the minor
// axis rather than square to the line, the same as Wu's, so a diagonal comes
// out a little lighter than a horizontal of the same width, which is what the
// one-pixel lines beside it already do.
//
// The band's top sits (width-1)/2 above where LineAA's would, which keeps a
// thick line centred on the thin one it replaces and puts width one exactly on
// top of it.
//
//nolint:varnamelen // x0, y0, x1, y1 is how Line spells the same four values.
func (c *Canvas) StrokeLineAA(x0, y0, x1, y1 float64, width int, col color.RGBA) {
	if width <= 1 {
		c.LineAA(x0, y0, x1, y1, col)

		return
	}

	if invalidCoordinate(x0) || invalidCoordinate(y0) || invalidCoordinate(x1) || invalidCoordinate(y1) {
		return
	}

	run := orientRun(x0, y0, x1, y1)
	band := float64(width)
	lift := (band - 1) / 2
	interY := run.startMinor + run.gradient*(run.firstCol-run.startMajor)

	for pixelX := run.firstCol; pixelX <= run.lastCol; pixelX++ {
		c.coverBand(int(pixelX), interY-lift, band, run.steep, col)
		interY += run.gradient
	}
}

// lineRun is one line turned so it walks along its longer axis from left to
// right, which is the shape both anti-aliased line methods step through.
type lineRun struct {
	startMajor float64
	startMinor float64
	gradient   float64
	firstCol   float64
	lastCol    float64
	steep      bool
}

// orientRun swaps a steep line into (y, x) space and puts its ends in order,
// the same two steps LineAA takes and for the same reasons: the loop always
// advances one pixel along the axis with more ground to cover, and the two
// orders of the same endpoints draw the same picture.
//
//nolint:varnamelen // x0, y0, x1, y1 is how Line spells the same four values.
func orientRun(x0, y0, x1, y1 float64) lineRun {
	startX, startY, endX, endY := x0, y0, x1, y1

	steep := math.Abs(endY-startY) > math.Abs(endX-startX)
	if steep {
		startX, startY = startY, startX
		endX, endY = endY, endX
	}

	if startX > endX {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	var gradient float64
	if deltaX := endX - startX; deltaX > 0 {
		gradient = (endY - startY) / deltaX
	}

	return lineRun{
		startMajor: startX,
		startMinor: startY,
		gradient:   gradient,
		firstCol:   math.Round(startX),
		lastCol:    math.Round(endX),
		steep:      steep,
	}
}

// coverBand blends one column of a thick line: the band from top to
// top+height along the minor axis, each pixel weighted by how much of it the
// band covers.
//
// The first and last pixels get a fraction and the ones between get all of
// it, which for a band one pixel tall is exactly the pair Wu's algorithm
// writes.
//
//nolint:revive // flag-parameter: steep says which axis major is on, the same swap LineAA makes inline.
func (c *Canvas) coverBand(major int, top, height float64, steep bool, col color.RGBA) {
	bottom := top + height

	for row := math.Floor(top); row < bottom; row++ {
		cover := min(row+1, bottom) - max(row, top)

		if steep {
			c.Blend(int(row), major, col, cover)
		} else {
			c.Blend(major, int(row), col, cover)
		}
	}
}

// StrokeDashedCircle draws DashedCircle's broken ring width pixels thick.
//
// The dash and the gap are counted along the ring's own radius, so a thick
// ring has the same number of dashes in the same places as a thin one of that
// radius. What changes is that each sample becomes width pixels laid along the
// radius, from width/2 inside the ring outward, and that the ring is sampled
// at its outer edge: sampled at its own radius, the outermost pixels would be
// further apart than the samples are and the dashes would come out speckled.
func (c *Canvas) StrokeDashedCircle(centerX, centerY, radius, dash, gap, width int, col color.RGBA) {
	if width <= 1 {
		c.DashedCircle(centerX, centerY, radius, dash, gap, col)

		return
	}

	if radius < 0 || dash <= 0 {
		return
	}

	if gap <= 0 {
		c.StrokeCircle(centerX, centerY, radius, width, col)

		return
	}

	inner := max(radius-width/2, 0)
	outer := inner + width - 1
	along := max(minCircleSamples, int(2*math.Pi*float64(radius)))
	samples := max(along, int(2*math.Pi*float64(outer)))
	period := dash + gap

	for i := range samples {
		if (i*along/samples)%period >= dash {
			continue
		}

		angle := 2 * math.Pi * float64(i) / float64(samples)

		for ring := inner; ring <= outer; ring++ {
			px, py := pointOnCircle(centerX, centerY, ring, angle)
			c.Set(px, py, col)
		}
	}
}

// StrokeCircle draws a ring width pixels thick around (centerX, centerY).
//
// Width one is Circle, the midpoint ring. Anything thicker is filled as an
// annulus instead, a span or two per row: every pixel whose centre is within
// half a pixel of a radius from radius-width/2 to that plus width-1 is set.
// Stacking midpoint rings of neighbouring radii would be the obvious way to
// get a thick one, and it leaves a moiré of unset pixels between them on the
// diagonals.
//
// A negative radius draws nothing, as it does for Circle.
func (c *Canvas) StrokeCircle(centerX, centerY, radius, width int, col color.RGBA) {
	if width <= 1 {
		c.Circle(centerX, centerY, radius, col)

		return
	}

	if radius < 0 {
		return
	}

	inner := float64(max(radius-width/2, 0)) - halfPixel
	outer := float64(max(radius-width/2, 0)+width-1) + halfPixel
	reach := int(math.Ceil(outer))

	for dy := -reach; dy <= reach; dy++ {
		c.annulusRow(centerX, centerY+dy, float64(dy), inner, outer, col)
	}
}

// annulusRow fills one row of StrokeCircle's annulus: the pixels whose
// centres are at least inner and less than outer from the centre.
//
// The row is the outer disc's span with the inner disc's span taken out of
// the middle, which is one span where the row passes outside the inner disc
// and two where it cuts through it.
//
//nolint:varnamelen // y and dy are the row and its offset from the centre, the idiom used throughout this package.
func (c *Canvas) annulusRow(centerX, y int, dy, inner, outer float64, col color.RGBA) {
	far := outer*outer - dy*dy
	if far <= 0 {
		return
	}

	// The largest offset strictly inside the outer edge, and the smallest one
	// on or past the inner edge.
	last := int(math.Ceil(math.Sqrt(far))) - 1

	// An inner edge at or inside the centre leaves no hole at all. Squaring it
	// regardless would turn -0.5 into a hole a pixel wide around the centre.
	first := 0
	if near := inner*inner - dy*dy; inner > 0 && near > 0 {
		first = int(math.Ceil(math.Sqrt(near)))
	}

	if first > last {
		return
	}

	if first == 0 {
		c.hline(centerX-last, centerX+last, y, col)

		return
	}

	c.hline(centerX-last, centerX-first, y, col)
	c.hline(centerX+first, centerX+last, y, col)
}

// StrokeRect draws the outline of rect width pixels thick, growing inward so
// the box keeps the bounds it was given the way Rect's one-pixel outline
// does. A width that would meet in the middle fills the box.
//
// It is four filled bands rather than four thick lines, because the bands are
// exactly the pixels wanted and FillRect writes them a row at a time.
func (c *Canvas) StrokeRect(rect image.Rectangle, width int, col color.RGBA) {
	if width <= 1 {
		c.Rect(rect, col)

		return
	}

	if rect.Empty() {
		return
	}

	thick := width

	if 2*thick >= rect.Dx() || 2*thick >= rect.Dy() {
		c.FillRect(rect, col)

		return
	}

	c.FillRect(image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+thick), col)
	c.FillRect(image.Rect(rect.Min.X, rect.Max.Y-thick, rect.Max.X, rect.Max.Y), col)
	c.FillRect(image.Rect(rect.Min.X, rect.Min.Y+thick, rect.Min.X+thick, rect.Max.Y-thick), col)
	c.FillRect(image.Rect(rect.Max.X-thick, rect.Min.Y+thick, rect.Max.X, rect.Max.Y-thick), col)
}

// StrokeLine draws Line's Bresenham path with each pixel stamped as a width
// by width block, the block's top-left corner on the pixel.
//
// That is a nearest-neighbour enlargement of the thin line rather than a
// thick line in its own right, which is the point: it is for marks small
// enough that they were drawn pixel by pixel at the design size, a charging
// bolt or a leader line, and doubling them should look like the same mark
// doubled.
//
//nolint:varnamelen // x0, y0, x1, y1 is how Bresenham is written everywhere.
func (c *Canvas) StrokeLine(x0, y0, x1, y1, width int, col color.RGBA) {
	if width <= 1 {
		c.Line(x0, y0, x1, y1, col)

		return
	}

	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := step(x0, x1), step(y0, y1)
	err := dx + dy
	x, y := x0, y0

	for {
		c.FillRect(image.Rect(x, y, x+width, y+width), col)

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
