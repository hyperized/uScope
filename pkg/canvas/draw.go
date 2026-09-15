package canvas

import (
	"image"
	"image/color"
	"math"
)

// minCircleSamples is the fewest angle steps DashedCircle takes around the
// ring. Below this, a small radius would sample so coarsely that the dash
// pattern stops looking like dashes at all.
const minCircleSamples = 8

// Blend alpha-composites col over the pixel already at (x, y), weighted by
// alpha. Coordinates outside the canvas are dropped, matching Set.
//
// alpha is clamped to [0, 1], and a NaN is folded into that same clamp by
// math.IsNaN rather than compared with < or > directly: any ordering
// comparison against NaN is false, so a NaN would otherwise slip straight
// past a bounds check written the naive way. At alpha 0 (NaN included) the
// function returns before touching the pixel at all, so the destination is
// left completely untouched rather than merely unchanged in colour. At
// alpha 1 the write is byte-identical to Set. The destination alpha channel
// is always forced to opaque: the framebuffer this canvas eventually
// reaches has none of its own, so anything else would just be discarded
// downstream anyway.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom; this is the hot path.
func (c *Canvas) Blend(x, y int, col color.RGBA, alpha float64) {
	rect := c.img.Rect
	if x < rect.Min.X || x >= rect.Max.X || y < rect.Min.Y || y >= rect.Max.Y {
		return
	}

	if math.IsNaN(alpha) || alpha <= 0 {
		return
	}

	if alpha > 1 {
		alpha = 1
	}

	off := c.img.PixOffset(x, y)
	pix := c.img.Pix[off : off+bytesPerPixel : off+bytesPerPixel]

	pix[0] = blendChannel(pix[0], col.R, alpha)
	pix[1] = blendChannel(pix[1], col.G, alpha)
	pix[2] = blendChannel(pix[2], col.B, alpha)
	pix[3] = opaque
}

// blendChannel interpolates one 8-bit channel from prev towards next by
// alpha. It rounds with math.Round instead of truncating, so a 50% blend
// lands on a deterministic byte rather than always rounding down.
func blendChannel(prev, next uint8, alpha float64) uint8 {
	return uint8(math.Round(float64(prev) + (float64(next)-float64(prev))*alpha))
}

// invalidCoordinate reports whether v cannot be plotted: NaN or either
// infinity. LineAA rejects such a coordinate outright, since feeding it into
// the interpolation would spread garbage across the whole row or column
// instead of failing where the bad value was introduced.
func invalidCoordinate(v float64) bool {
	return math.IsNaN(v) || math.IsInf(v, 0)
}

// LineAA draws an anti-aliased line with Xiaolin Wu's algorithm, plotting
// through Blend so each pixel picks up fractional coverage instead of the
// all-or-nothing pixels Line leaves behind.
//
// Coordinates are float64, unlike Line's integers, because sub-pixel
// endpoints are the reason to reach for this over Line in the first place.
// The endpoints are folded into the same loop as every other sample instead
// of being special-cased with their own edge-coverage weighting: the caps
// this leaves are a little softer than the textbook version, but nothing
// here depends on their exact shape.
//
//nolint:varnamelen // x0, y0, x1, y1 is how Line spells the same four values.
func (c *Canvas) LineAA(x0, y0, x1, y1 float64, col color.RGBA) {
	if invalidCoordinate(x0) || invalidCoordinate(y0) || invalidCoordinate(x1) || invalidCoordinate(y1) {
		return
	}

	startX, startY, endX, endY := x0, y0, x1, y1

	// Steep lines are walked column-by-column in (y, x) space instead of
	// (x, y): swapping the roles here keeps the stepping loop below always
	// advancing along the axis with more ground to cover, one pixel per
	// step, however the line is oriented.
	steep := math.Abs(endY-startY) > math.Abs(endX-startX)
	if steep {
		startX, startY = startY, startX
		endX, endY = endY, endX
	}

	// Walking left-to-right after this point, regardless of which endpoint
	// the caller gave first, is what makes LineAA(a, b, c, d) and
	// LineAA(c, d, a, b) produce the same image.
	if startX > endX {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	deltaX := endX - startX
	deltaY := endY - startY

	var gradient float64
	if deltaX > 0 {
		gradient = deltaY / deltaX
	}

	firstCol, lastCol := math.Round(startX), math.Round(endX)
	interY := startY + gradient*(firstCol-startX)

	for pixelX := firstCol; pixelX <= lastCol; pixelX++ {
		rowFloor := math.Floor(interY)
		frac := interY - rowFloor

		// steep means the loop is walking what is conceptually the y-axis,
		// so the coordinates it produces have to be swapped back before
		// they reach Blend.
		if steep {
			c.Blend(int(rowFloor), int(pixelX), col, 1-frac)
			c.Blend(int(rowFloor)+1, int(pixelX), col, frac)
		} else {
			c.Blend(int(pixelX), int(rowFloor), col, 1-frac)
			c.Blend(int(pixelX), int(rowFloor)+1, col, frac)
		}

		interY += gradient
	}
}

// DashedCircle draws a circle outline broken into dashes, sampling points by
// angle rather than stepping the midpoint algorithm: a dash pattern needs to
// know how far around the ring each point sits, which is exactly what
// midpoint's octant symmetry throws away.
//
// A negative radius, or a dash of zero or less, draws nothing. A gap of zero
// or less draws a solid circle instead, since there is no gap left to leave.
func (c *Canvas) DashedCircle(centerX, centerY, radius, dash, gap int, col color.RGBA) {
	if radius < 0 || dash <= 0 {
		return
	}

	if gap <= 0 {
		c.Circle(centerX, centerY, radius, col)

		return
	}

	samples := max(minCircleSamples, int(2*math.Pi*float64(radius)))
	period := dash + gap

	for i := range samples {
		if i%period >= dash {
			continue
		}

		angle := 2 * math.Pi * float64(i) / float64(samples)
		px, py := pointOnCircle(centerX, centerY, radius, angle)
		c.Set(px, py, col)
	}
}

// pointOnCircle returns the pixel at the given angle around a circle of
// radius centred on (centerX, centerY), rounding to the nearest pixel so
// DashedCircle and Circle can agree on where the ring sits.
func pointOnCircle(centerX, centerY, radius int, angle float64) (int, int) {
	x := centerX + int(math.Round(float64(radius)*math.Cos(angle)))
	y := centerY + int(math.Round(float64(radius)*math.Sin(angle)))

	return x, y
}

// FillTriangle fills the triangle (x0, y0)-(x1, y1)-(x2, y2).
//
// It tests the bounding box against each edge function rather than
// interpolating scanlines: an edge function is a multiply and a subtract,
// never a division, so a degenerate triangle (three collinear points, or
// three identical ones) cannot divide by zero. It just fills whichever
// pixels happen to satisfy the test, which may be none.
//
// The triangle's signed area is computed once. Its sign records the winding
// order, and every pixel's three edge values are compared against that same
// sign, which is what makes clockwise and counter-clockwise vertex order
// produce the identical filled triangle.
//
//nolint:varnamelen // x0, y0, x1, y1, x2, y2 name the three vertices; matches Line's convention.
func (c *Canvas) FillTriangle(x0, y0, x1, y1, x2, y2 int, col color.RGBA) {
	box := image.Rect(
		min(x0, x1, x2), min(y0, y1, y2),
		max(x0, x1, x2)+1, max(y0, y1, y2)+1,
	).Intersect(c.img.Rect)
	if box.Empty() {
		return
	}

	area := edgeFunction(x0, y0, x1, y1, x2, y2)

	for py := box.Min.Y; py < box.Max.Y; py++ {
		for px := box.Min.X; px < box.Max.X; px++ {
			w0 := edgeFunction(x1, y1, x2, y2, px, py)
			w1 := edgeFunction(x2, y2, x0, y0, px, py)
			w2 := edgeFunction(x0, y0, x1, y1, px, py)

			if sameSign(area, w0, w1, w2) {
				c.Set(px, py, col)
			}
		}
	}
}

// edgeFunction returns twice the signed area of the triangle (ax, ay),
// (bx, by), (px, py). Its sign says which side of the directed line from a
// to b the point p falls on, which is the whole test FillTriangle needs.
//
//nolint:varnamelen // ax, ay, bx, by, px, py name two segment endpoints and a test point.
func edgeFunction(ax, ay, bx, by, px, py int) int {
	return (bx-ax)*(py-ay) - (by-ay)*(px-ax)
}

// sameSign reports whether w0, w1 and w2 all sit on the same side as area:
// all non-negative when area is non-negative, all non-positive otherwise.
// Branching on area's own sign, rather than assuming positive is always
// "inside", is what lets FillTriangle accept either winding order.
func sameSign(area, w0, w1, w2 int) bool {
	if area >= 0 {
		return w0 >= 0 && w1 >= 0 && w2 >= 0
	}

	return w0 <= 0 && w1 <= 0 && w2 <= 0
}

// Rect draws a one-pixel outline of rect, following image.Rectangle's
// half-open convention: image.Rect(2, 3, 8, 9) outlines the box whose
// top-left pixel is (2, 3) and whose bottom-right pixel is (7, 8). An empty
// rectangle draws nothing. Corners get drawn twice, once by each of the two
// Line calls that meet there, which costs nothing since Set is idempotent.
func (c *Canvas) Rect(rect image.Rectangle, col color.RGBA) {
	if rect.Empty() {
		return
	}

	top, bottom := rect.Min.Y, rect.Max.Y-1
	left, right := rect.Min.X, rect.Max.X-1

	c.Line(left, top, right, top, col)
	c.Line(left, bottom, right, bottom, col)
	c.Line(left, top, left, bottom, col)
	c.Line(right, top, right, bottom, col)
}
