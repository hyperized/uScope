package canvas

import (
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

	// winding is one when the ring runs down the screen along this edge and
	// minus one when it runs up. The even-odd fill never reads it; the zones
	// of FillPolygonZoned are counted by it.
	winding int
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
	c.fill(rings, clip, col, 1)
}

// BlendPolygon is FillPolygon with col composited over what is already there
// at alpha, instead of replacing it.
//
// The rings, the clip and the even-odd rule mean exactly what they do for
// FillPolygon, and every covered pixel is blended once per call however many
// rings overlap it, which is what a translucent shape needs: drawing the same
// area as several strokes would darken the seams. An alpha of one or more is
// FillPolygon, byte for byte. An alpha that is NaN, zero or negative draws
// nothing.
//
// The mix is done in fixed point on the pixel bytes, so a channel can land one
// step from what Blend would give for the same alpha. That is the price of
// not calling Blend, with its clipping and float rounding, for every pixel of
// a large shape. Like FillPolygon it allocates nothing once the scratch
// buffers have grown.
func (c *Canvas) BlendPolygon(rings [][]image.Point, clip image.Rectangle, col color.RGBA, alpha float64) {
	if math.IsNaN(alpha) || alpha <= 0 {
		return
	}

	c.fill(rings, clip, col, alpha)
}

// fill is the body of both polygon fills: build the edges inside the clip,
// sort them by the row they start on, and sweep.
func (c *Canvas) fill(rings [][]image.Point, clip image.Rectangle, col color.RGBA, alpha float64) {
	area := clip.Intersect(c.img.Rect)
	if area.Empty() {
		return
	}

	c.edges = buildEdges(c.edges[:0], rings, area)
	if len(c.edges) == 0 {
		return
	}

	c.edges, c.spare = sortByTop(c.edges, c.spare, &c.tops)
	c.sweep(area, col, alpha)
}

// sortByTop orders edges by the first row they appear on, which is the order
// the sweep admits them in, and returns the sorted list and the buffer that
// held it before, for the next call to sort into.
//
// A coastline hands over thousands of edges and a corridor tube's strip a
// handful, a few thousand times a frame, and neither sort suits both. The
// thousands go through a counting sort on the row, over only the rows the
// edges start on: a comparison sort was a tenth of the land fill, and a
// histogram as tall as the clip cost every small strip a pass over the whole
// clip's height. The handful go through an insertion sort in place. Edges
// that start on the same row keep the order they came in, which nothing
// relies on, since every row's crossings are sorted by where they sit before
// they are used. counts is the histogram's scratch, grown and kept.
func sortByTop(edges, spare []polyEdge, counts *[]int) ([]polyEdge, []polyEdge) {
	if len(edges) <= shortEdgeList {
		insertByTop(edges)

		return edges, spare
	}

	first, last := edges[0].top, edges[0].top
	for index := range edges {
		first, last = min(first, edges[index].top), max(last, edges[index].top)
	}

	rows := last - first + 2
	if cap(*counts) < rows {
		*counts = make([]int, rows)
	}

	tally := (*counts)[:rows]
	clear(tally)

	for index := range edges {
		tally[edges[index].top-first+1]++
	}

	for row := 1; row < rows; row++ {
		tally[row] += tally[row-1]
	}

	spare = slices.Grow(spare[:0], len(edges))[:len(edges)]

	for index := range edges {
		slot := edges[index].top - first
		spare[tally[slot]] = edges[index]
		tally[slot]++
	}

	return spare, edges
}

// shortEdgeList is the longest edge list sortByTop sorts by insertion. A
// strip of a tube is four edges; past a few dozen the counting sort wins.
const shortEdgeList = 32

// insertByTop sorts edges by their first row, in place, keeping ties in the
// order they came in.
func insertByTop(edges []polyEdge) {
	for index := 1; index < len(edges); index++ {
		edge := edges[index]

		slot := index
		for slot > 0 && edges[slot-1].top > edge.top {
			edges[slot] = edges[slot-1]
			slot--
		}

		edges[slot] = edge
	}
}

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

	top, bottom, winding := from, to, 1
	if top.Y > bottom.Y {
		top, bottom, winding = bottom, top, -1
	}

	first := max(top.Y, area.Min.Y)
	last := min(bottom.Y, area.Max.Y)

	if first >= last {
		return dst
	}

	slope := float64(bottom.X-top.X) / float64(bottom.Y-top.Y)

	return append(dst, polyEdge{
		top:     first,
		bottom:  last,
		x:       float64(top.X) + slope*float64(first-top.Y),
		slope:   slope,
		winding: winding,
	})
}

// sweep walks the rows of area, keeping an active list of the edges that cross
// the current one.
//
// The alternative is to ask every edge about every row, which for a coastline
// is tens of thousands of edges against hundreds of rows. The active list
// holds only the edges that actually cross the row being filled, which for a
// map is a couple of dozen however much geometry is in the picture.
//
// The walk starts at the first edge's row and jumps any run of empty rows to
// the next edge's, rather than stepping through them. A small polygon on a
// tall clip, which is what every strip of a corridor tube is, would otherwise
// pay for the whole height of the picture to fill a few rows of it. The
// caller hands over at least one edge, sorted by top row.
func (c *Canvas) sweep(area image.Rectangle, col color.RGBA, alpha float64) {
	c.active = c.active[:0]
	next := 0

	//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
	for y := c.edges[0].top; y < area.Max.Y; y++ {
		c.active = retire(c.active, y)
		c.active, next = admit(c.active, c.edges, next, y)

		if len(c.active) == 0 {
			// Nothing on this row and nothing left to admit means every
			// remaining row is empty too.
			if next == len(c.edges) {
				return
			}

			y = c.edges[next].top - 1

			continue
		}

		c.fillRow(y, area, col, alpha)
		advance(c.active)
	}
}

// admit moves the edges that start on this row onto the active list, and
// returns the list and how far through the sorted edges that got.
func admit(active, edges []polyEdge, next, y int) ([]polyEdge, int) {
	for next < len(edges) && edges[next].top == y {
		active = append(active, edges[next])
		next++
	}

	return active, next
}

// retire drops the edges that ended above this row, compacting the active list
// in place so it never grows past the widest row of the polygon.
func retire(active []polyEdge, y int) []polyEdge {
	kept := active[:0]

	for _, edge := range active {
		if edge.bottom > y {
			kept = append(kept, edge)
		}
	}

	return kept
}

// advance steps every active edge onto the next row.
func advance(active []polyEdge) {
	for index := range active {
		active[index].x += active[index].slope
	}
}

// fillRow paints the spans between pairs of crossings on one row, opaquely
// when alpha is one or more and blended otherwise.
//
// The crossings are sorted and taken two at a time, which is the even-odd rule
// written out: the first crossing enters the shape, the second leaves it, the
// third enters again. Two edges meeting the row at the same place, which is
// what happens where two cells of the map share a boundary, produce two
// crossings there and cancel out, so the fill runs straight across the join
// instead of stopping at it.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
func (c *Canvas) fillRow(y int, area image.Rectangle, col color.RGBA, alpha float64) {
	c.sortCrossings()

	for index := 0; index+1 < len(c.crossings); index += 2 {
		from := max(pixelAt(c.crossings[index]), area.Min.X)
		to := min(pixelAt(c.crossings[index+1])-1, area.Max.X-1)

		if alpha >= 1 {
			c.hline(from, to, y, col)

			continue
		}

		c.blendSpan(from, to, y, col, alpha)
	}
}

// sortCrossings lists where every active edge meets the current row, left to
// right.
func (c *Canvas) sortCrossings() {
	c.crossings = c.crossings[:0]
	for _, edge := range c.active {
		c.crossings = append(c.crossings, edge.x)
	}

	slices.Sort(c.crossings)
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
