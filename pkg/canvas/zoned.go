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
	c.FillLayersZoned([]ZonedLayer{{Rings: rings, Inks: inks}}, zones, clip)
}

// ZonedLayer is one set of rings for FillLayersZoned, with the inks its
// pixels take by zone count.
type ZonedLayer struct {
	Rings [][]image.Point
	Inks  []color.RGBA
}

// FillLayersZoned is FillPolygonZoned for each layer in turn, each over the
// ones before it, all against the same zones: the picture is exactly the one
// those calls in that order would leave.
//
// It is one sweep rather than one per layer, and each pixel is written once,
// by the topmost layer that covers it. Ground flooded with water and land
// painted back over it was two sweeps that walked the same zones twice and
// wrote every pixel of the land twice.
//
// A layer with no inks paints nothing and hides nothing. Like FillPolygon it
// allocates nothing once the canvas's scratch has grown to the most layers
// and the largest of each it has been handed.
func (c *Canvas) FillLayersZoned(layers []ZonedLayer, zones [][]image.Point, clip image.Rectangle) {
	area := clip.Intersect(c.img.Rect)
	if area.Empty() {
		return
	}

	c.sweeps = c.sweeps[:0]

	for index, layer := range layers {
		if len(layer.Inks) > 0 {
			c.sweeps = c.prepareLayer(c.sweeps, index, layer.Rings, area)
		}
	}

	if len(c.sweeps) == 0 {
		return
	}

	c.zoneEdges = buildEdges(c.zoneEdges[:0], zones, area)
	c.zoneEdges, c.spare = sortByTop(c.zoneEdges, c.spare, &c.tops)
	c.sweepLayers(layers, area)
}

// layerSweep is one layer's share of the sweep: which layer it is, its edges
// sorted by top row, the ones crossing the current row, how far through the
// edges admission has got, and the row's crossings and the spans they make.
//
// It holds the layer's index rather than its inks. Anything taken from the
// caller's layers and kept on the canvas would make the compiler move the
// caller's inks to the heap, which on a per-frame path is an allocation a
// frame.
type layerSweep struct {
	layer     int
	edges     []polyEdge
	active    []polyEdge
	next      int
	crossings []float64
	spans     []span
}

// span is a run of pixels on one row, both ends included.
type span struct{ from, to int }

// prepareLayer appends the sweep for one layer, reusing whatever scratch the
// slot it lands in held last time.
func (c *Canvas) prepareLayer(
	sweeps []layerSweep, layer int, rings [][]image.Point, area image.Rectangle,
) []layerSweep {
	if len(sweeps) < cap(sweeps) {
		sweeps = sweeps[:len(sweeps)+1]
	} else {
		sweeps = append(sweeps, layerSweep{})
	}

	sweep := &sweeps[len(sweeps)-1]

	sweep.layer, sweep.next = layer, 0
	sweep.active = sweep.active[:0]
	sweep.edges = buildEdges(sweep.edges[:0], rings, area)
	sweep.edges, c.spare = sortByTop(sweep.edges, c.spare, &c.tops)

	return sweeps
}

// sweepLayers walks the rows of area with an active list per layer and one
// for the zones. The zones are stepped on every row, painted or not, because
// an edge that is not advanced on a row would sit in the wrong place on the
// next.
func (c *Canvas) sweepLayers(layers []ZonedLayer, area image.Rectangle) {
	c.zoneActive = c.zoneActive[:0]
	zoneNext := 0

	//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
	for y := area.Min.Y; y < area.Max.Y; y++ {
		live, pending := false, false

		for index := range c.sweeps {
			sweep := &c.sweeps[index]
			sweep.active = retire(sweep.active, y)
			sweep.active, sweep.next = admit(sweep.active, sweep.edges, sweep.next, y)
			live = live || len(sweep.active) > 0
			pending = pending || sweep.next < len(sweep.edges)
		}

		c.zoneActive = retire(c.zoneActive, y)
		c.zoneActive, zoneNext = admit(c.zoneActive, c.zoneEdges, zoneNext, y)

		// Nothing on this row and nothing left to admit means every
		// remaining row is empty too, whatever the zones are doing.
		if !live && !pending {
			return
		}

		if live {
			c.fillRowLayers(layers, y, area)
		}

		for index := range c.sweeps {
			advance(c.sweeps[index].active)
		}

		advance(c.zoneActive)
	}
}

// fillRowLayers paints one row: each layer's even-odd spans, less whatever
// the layers over it cover, cut where a zone's edge crosses them into pieces
// of one colour.
//
// The layers are taken from the top down, so covered holds every pixel some
// layer above has already claimed and each pixel is written by the first
// layer to reach it.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
func (c *Canvas) fillRowLayers(layers []ZonedLayer, y int, area image.Rectangle) {
	// The active zones are kept in the order the last row sorted them into,
	// which a row later is almost always still the order, so an insertion
	// sort over the list itself is one pass. A fresh sort of the crossings
	// was most of the frame at thirty-odd bands. pixelAt keeps the order.
	insertByX(c.zoneActive)

	c.zoneCrossings = c.zoneCrossings[:0]
	for _, edge := range c.zoneActive {
		c.zoneCrossings = append(c.zoneCrossings, zoneCrossing{at: pixelAt(edge.x), winding: edge.winding})
	}

	c.covered = c.covered[:0]

	for index := len(c.sweeps) - 1; index >= 0; index-- {
		sweep := &c.sweeps[index]
		sweep.rowSpans(area)

		walk := zoneWalk{crossings: c.zoneCrossings}
		for _, piece := range sweep.spans {
			c.paintUncovered(&walk, piece, y, layers[sweep.layer].Inks)
		}

		if index > 0 {
			c.covered, c.merged = mergeSpans(c.merged[:0], c.covered, sweep.spans), c.covered
		}
	}
}

// rowSpans lists the pixels this layer covers on the current row, left to
// right, which is fillRow's even-odd pairing clipped to area.
func (l *layerSweep) rowSpans(area image.Rectangle) {
	l.crossings = l.crossings[:0]
	for _, edge := range l.active {
		l.crossings = append(l.crossings, edge.x)
	}

	slices.Sort(l.crossings)

	l.spans = l.spans[:0]

	for index := 0; index+1 < len(l.crossings); index += 2 {
		from := max(pixelAt(l.crossings[index]), area.Min.X)
		to := min(pixelAt(l.crossings[index+1])-1, area.Max.X-1)

		if from <= to {
			l.spans = append(l.spans, span{from: from, to: to})
		}
	}
}

// paintUncovered paints the parts of piece that covered leaves open, each
// in the ink of its zone count. Both piece and covered run left to right, so
// the walk through the zones only ever moves forward.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
func (c *Canvas) paintUncovered(walk *zoneWalk, piece span, y int, inks []color.RGBA) {
	from := piece.from

	for _, hidden := range c.covered {
		if hidden.to < from {
			continue
		}

		if hidden.from > piece.to {
			break
		}

		c.paintZoned(walk, from, hidden.from-1, y, inks)
		from = hidden.to + 1
	}

	c.paintZoned(walk, from, piece.to, y, inks)
}

// paintZoned paints the pixels from x0 to x1 on row y, cut where the zone
// count changes.
//
//nolint:varnamelen // from, to bound a span, as they do in fillRow.
func (c *Canvas) paintZoned(walk *zoneWalk, x0, x1, y int, inks []color.RGBA) {
	from, to := x0, x1

	for from <= to {
		end := walk.reach(from, to)
		c.hline(from, end, y, inks[min(walk.depth(), len(inks)-1)])
		from = end + 1
	}
}

// mergeSpans is the union of two lists of disjoint spans, each left to right,
// appended to dst as one such list. Spans that touch or overlap come out as
// one.
func mergeSpans(dst, left, right []span) []span {
	for len(left) > 0 || len(right) > 0 {
		var next span

		if len(right) == 0 || (len(left) > 0 && left[0].from <= right[0].from) {
			next, left = left[0], left[1:]
		} else {
			next, right = right[0], right[1:]
		}

		if last := len(dst) - 1; last >= 0 && next.from <= dst[last].to+1 {
			dst[last].to = max(dst[last].to, next.to)

			continue
		}

		dst = append(dst, next)
	}

	return dst
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
