package canvas

import (
	"image"
	"image/color"
	"slices"
)

// zoneCrossing is where one zone's edge meets the current row: the first
// pixel past it, and which way the zone's ring runs there.
type zoneCrossing struct {
	at      int
	winding int
}

// FillPolygonZoned fills the area enclosed by rings exactly as FillPolygon
// does, and colours each pixel by how many of zones enclose it: inks[0] where
// none do, inks[1] inside one, and so on, a count past the end of inks taking
// the last of them.
//
// The zones are meant to be nested rings wound the same way round, such as
// the circles that bound a run of concentric bands. The count is the zones'
// winding number at the pixel without its sign, which for rings like that is
// how many of them enclose it; rings that cross or wind against each other
// get whatever that number comes to. A zone decides a pixel's colour and
// never whether it is painted, and it counts wherever it runs, inside clip or
// out.
//
// It exists to fade a set of shapes across bands in a single pass. The
// even-odd rule can only exclusive-or two sets of rings, never intersect
// them, so filling the shapes once per band would mean cutting the band out
// of them by hand and sweeping every row of them once per band.
//
// Nothing is painted when inks is empty. Like FillPolygon it allocates
// nothing once the canvas's scratch has grown.
func (c *Canvas) FillPolygonZoned(rings, zones [][]image.Point, clip image.Rectangle, inks []color.RGBA) {
	area := clip.Intersect(c.img.Rect)
	if area.Empty() || len(inks) == 0 {
		return
	}

	c.edges = buildEdges(c.edges[:0], rings, area)
	if len(c.edges) == 0 {
		return
	}

	c.zoneEdges = buildEdges(c.zoneEdges[:0], zones, area)

	slices.SortFunc(c.edges, byTopRow)
	slices.SortFunc(c.zoneEdges, byTopRow)
	c.sweepZoned(area, inks)
}

// sweepZoned is sweep with a second active list for the zones beside the
// rings'. The zones are stepped on every row, painted or not, because an edge
// that is not advanced on a row would sit in the wrong place on the next.
func (c *Canvas) sweepZoned(area image.Rectangle, inks []color.RGBA) {
	c.active, c.zoneActive = c.active[:0], c.zoneActive[:0]
	next, zoneNext := 0, 0

	//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
	for y := area.Min.Y; y < area.Max.Y; y++ {
		c.active = retire(c.active, y)
		c.active, next = admit(c.active, c.edges, next, y)
		c.zoneActive = retire(c.zoneActive, y)
		c.zoneActive, zoneNext = admit(c.zoneActive, c.zoneEdges, zoneNext, y)

		// Nothing on this row and nothing left to admit means every
		// remaining row is empty too, whatever the zones are doing.
		if len(c.active) == 0 && next == len(c.edges) {
			return
		}

		if len(c.active) > 0 {
			c.fillRowZoned(y, area, inks)
		}

		advance(c.active)
		advance(c.zoneActive)
	}
}

// fillRowZoned paints one row's even-odd spans, each cut where a zone's edge
// crosses it into pieces of one colour.
//
// The zone crossings are sorted once and walked once across the whole row,
// because the spans arrive left to right: the count a span starts on is the
// count the one before it ended on plus whatever lies between them.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
func (c *Canvas) fillRowZoned(y int, area image.Rectangle, inks []color.RGBA) {
	c.sortCrossings()

	// The active zones are kept in the order the last row sorted them into,
	// which a row later is almost always still the order, so an insertion
	// sort over the list itself is one pass. A fresh sort of the crossings
	// was most of the frame at thirty-odd bands. pixelAt keeps the order.
	insertByX(c.zoneActive)

	c.zoneCrossings = c.zoneCrossings[:0]
	for _, edge := range c.zoneActive {
		c.zoneCrossings = append(c.zoneCrossings, zoneCrossing{at: pixelAt(edge.x), winding: edge.winding})
	}

	walk := zoneWalk{crossings: c.zoneCrossings}

	for index := 0; index+1 < len(c.crossings); index += 2 {
		from := max(pixelAt(c.crossings[index]), area.Min.X)
		to := min(pixelAt(c.crossings[index+1])-1, area.Max.X-1)

		for from <= to {
			end := walk.reach(from, to)
			c.hline(from, end, y, inks[min(walk.depth(), len(inks)-1)])
			from = end + 1
		}
	}
}

// insertByX sorts edges by where they sit on the current row, in place.
func insertByX(edges []polyEdge) {
	for index := 1; index < len(edges); index++ {
		edge := edges[index]

		slot := index
		for slot > 0 && edges[slot-1].x > edge.x {
			edges[slot] = edges[slot-1]
			slot--
		}

		edges[slot] = edge
	}
}

// zoneWalk is the running count across one row of zone crossings.
type zoneWalk struct {
	crossings []zoneCrossing
	next      int
	winding   int
}

// reach takes in every crossing at or before pixel from and reports the last
// pixel, no further than to, that shares from's count.
//
//nolint:varnamelen // from, to bound a span, as they do in fillRow.
func (w *zoneWalk) reach(from, to int) int {
	for w.next < len(w.crossings) && w.crossings[w.next].at <= from {
		w.winding += w.crossings[w.next].winding
		w.next++
	}

	if w.next < len(w.crossings) {
		return min(to, w.crossings[w.next].at-1)
	}

	return to
}

// depth is how many zones enclose the pixels reach last answered for.
func (w *zoneWalk) depth() int { return abs(w.winding) }
