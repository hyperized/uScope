package canvas

import (
	"cmp"
	"image"
	"image/color"
	"math"
	"slices"
)

// polyEdge is one non-horizontal polygon edge, prepared for the sweep.
//
// It is stored as a position and a step rather than as its two endpoints,
// because the sweep asks the same question of it on every row it spans: where
// does this edge sit now. One addition per row answers that, where a pair of
// endpoints would need a subtraction, a multiply and a divide.
type polyEdge struct {
	// top is the first scanline the edge crosses and bottom is one past the
	// last, both already trimmed to the clip rectangle.
	top    int
	bottom int

	// x is where the edge sits on row top, and slope how far it moves per
	// row. The sweep advances x rather than recomputing it.
	x     float64
	slope float64
}

// FillPolygon fills the area enclosed by rings, clipped to clip, using the
// even-odd rule.
//
// All the rings are filled in one pass rather than one after another, and that
// is the whole point of the signature. Even-odd counts how many boundaries lie
// between a pixel and the edge of the world: odd means inside. A square with a
// hole in it is two rings handed over together, and the hole comes out empty
// because a pixel in it is enclosed twice. Filling the outer ring and then
// painting the hole in a second call is not the same thing at all, because the
// second call has no way of knowing what was underneath the first.
//
// Each ring is closed implicitly: the edge from its last point back to its
// first is walked whether or not the caller repeated the point. A ring may
// wind either way, may be concave, and may run outside clip; nothing is drawn
// outside clip, or outside the canvas.
//
// Nothing is allocated once the scratch buffers have grown to the largest
// polygon the canvas has been handed, which is what makes this usable on the
// background layer. Those buffers are why a Canvas is still single-threaded:
// two goroutines filling the same canvas would tread on each other's edge
// list, not just on each other's pixels.
func (c *Canvas) FillPolygon(rings [][]image.Point, clip image.Rectangle, col color.RGBA) {
	area := clip.Intersect(c.img.Rect)
	if area.Empty() {
		return
	}

	c.edges = buildEdges(c.edges[:0], rings, area)
	if len(c.edges) == 0 {
		return
	}

	slices.SortFunc(c.edges, byTopRow)
	c.sweep(area, col)
}

// byTopRow orders edges by the first row they appear on, which is the order
// the sweep admits them in.
func byTopRow(left, right polyEdge) int { return cmp.Compare(left.top, right.top) }

// buildEdges turns every ring into the edges that cross a scanline inside
// area.
func buildEdges(dst []polyEdge, rings [][]image.Point, area image.Rectangle) []polyEdge {
	for _, ring := range rings {
		for index, point := range ring {
			dst = appendEdge(dst, point, ring[(index+1)%len(ring)], area)
		}
	}

	return dst
}

// appendEdge adds one edge, trimmed to the rows inside area, and drops it if
// it crosses no scanline at all.
//
// A horizontal edge is dropped rather than recorded. The crossing rule a
// scanline fill needs is that an edge spanning rows covers the half-open range
// from its top row up to but not including its bottom one, which is what makes
// a shared vertex count once instead of twice; a horizontal edge spans no rows
// under that rule, so it has nothing to contribute and its two ends are
// already carried by the edges either side of it.
func appendEdge(dst []polyEdge, from, to image.Point, area image.Rectangle) []polyEdge {
	if from.Y == to.Y {
		return dst
	}

	top, bottom := from, to
	if top.Y > bottom.Y {
		top, bottom = bottom, top
	}

	first := max(top.Y, area.Min.Y)
	last := min(bottom.Y, area.Max.Y)

	if first >= last {
		return dst
	}

	slope := float64(bottom.X-top.X) / float64(bottom.Y-top.Y)

	return append(dst, polyEdge{
		top:    first,
		bottom: last,
		x:      float64(top.X) + slope*float64(first-top.Y),
		slope:  slope,
	})
}

// sweep walks the rows of area, keeping an active list of the edges that cross
// the current one.
//
// The alternative is to ask every edge about every row, which for a coastline
// is tens of thousands of edges against hundreds of rows. The active list
// holds only the edges that actually cross the row being filled, which for a
// map is a couple of dozen however much geometry is in the picture.
func (c *Canvas) sweep(area image.Rectangle, col color.RGBA) {
	c.active = c.active[:0]
	next := 0

	//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
	for y := area.Min.Y; y < area.Max.Y; y++ {
		c.retire(y)
		next = c.admit(next, y)

		if len(c.active) == 0 {
			// Nothing on this row and nothing left to admit means every
			// remaining row is empty too.
			if next == len(c.edges) {
				return
			}

			continue
		}

		c.fillRow(y, area, col)
		c.advance()
	}
}

// admit moves the edges that start on this row onto the active list, and
// returns how far through the sorted list that got.
func (c *Canvas) admit(next, y int) int {
	for next < len(c.edges) && c.edges[next].top == y {
		c.active = append(c.active, c.edges[next])
		next++
	}

	return next
}

// retire drops the edges that ended above this row, compacting the active list
// in place so it never grows past the widest row of the polygon.
func (c *Canvas) retire(y int) {
	kept := c.active[:0]

	for _, edge := range c.active {
		if edge.bottom > y {
			kept = append(kept, edge)
		}
	}

	c.active = kept
}

// advance steps every active edge onto the next row.
func (c *Canvas) advance() {
	for index := range c.active {
		c.active[index].x += c.active[index].slope
	}
}

// fillRow paints the spans between pairs of crossings on one row.
//
// The crossings are sorted and taken two at a time, which is the even-odd rule
// written out: the first crossing enters the shape, the second leaves it, the
// third enters again. Two edges meeting the row at the same place, which is
// what happens where two cells of the map share a boundary, produce two
// crossings there and cancel out, so the fill runs straight across the join
// instead of stopping at it.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
func (c *Canvas) fillRow(y int, area image.Rectangle, col color.RGBA) {
	c.crossings = c.crossings[:0]
	for _, edge := range c.active {
		c.crossings = append(c.crossings, edge.x)
	}

	slices.Sort(c.crossings)

	for index := 0; index+1 < len(c.crossings); index += 2 {
		from := max(pixelAt(c.crossings[index]), area.Min.X)
		to := min(pixelAt(c.crossings[index+1])-1, area.Max.X-1)

		c.hline(from, to, y, col)
	}
}

// halfPixel is a pixel's centre offset from its own coordinate; see pixelAt.
const halfPixel = 0.5

// pixelAt is the first pixel whose centre sits at or past x.
//
// A pixel is treated as covering the half-open span from its own coordinate to
// the next, with its centre half a pixel in, which is what makes a rectangle
// from (0, 0) to (4, 4) fill exactly the four columns image.Rect would: the
// crossings land on 0 and 4, and the last column filled is 3.
func pixelAt(x float64) int { return int(math.Ceil(x - halfPixel)) }
