package shore

// Cutting land polygons into the cell grid.
//
// The shoreline file and this one solve the same problem in opposite ways. A
// coastline is a line, so split may cut it anywhere and hand the two halves a
// shared segment to hide the seam. A land polygon is an area, and an area cut
// in half is not two areas unless the cut itself is closed up: the piece has
// to run along the cell boundary where the original ran off it, or the fill
// would leak out of the cell and across the rest of the scope.
//
// Sutherland-Hodgman is what closes it. Clipping a ring against the four
// half-planes a cell is the intersection of leaves a ring again, walking the
// boundary wherever the original left the cell. Two neighbouring cells compute
// the same crossing from the same pair of points with the same arithmetic, so
// the two pieces meet on exactly the same coordinates and the even-odd fill
// sees one continuous span rather than a seam.
//
// Concave rings come out of the algorithm with zero-width bridges along the
// boundary, which is the textbook complaint about it. For an even-odd fill
// they cost nothing: a bridge is two coincident edges, the scanline counts
// both, and the parity is where it started.

// cellBox is one cell's rectangle, in degrees.
type cellBox struct {
	latMin float64
	latMax float64
	lonMin float64
	lonMax float64
}

// boxOf is the rectangle a row and a column cover.
func boxOf(row, col int) cellBox {
	latMin := float64(row*cellDegrees - quarterTurn)
	lonMin := float64(col*cellDegrees - halfTurn)

	return cellBox{
		latMin: latMin,
		latMax: latMin + cellDegrees,
		lonMin: lonMin,
		lonMax: lonMin + cellDegrees,
	}
}

// halfPlane is one of the four sides a cellBox is the intersection of.
//
// It is a pair of numbers and two flags rather than a function value, because
// the clipper runs this over every ring against every cell its bounding box
// touches: on Natural Earth's land that is a few hundred thousand passes, and
// an indirect call per point is not what the generator should spend them on.
type halfPlane struct {
	// limit is the degree the plane cuts at, and lat says which coordinate is
	// measured against it.
	limit float64
	lat   bool

	// upper says the kept side is the one below limit. A cell keeps what is
	// above its minimum and below its maximum, so two of the four are upper.
	upper bool
}

// sides is the cell's four half-planes, in the order the clipper applies them.
// The order does not change the result; any order leaves the intersection.
func (b cellBox) sides() [4]halfPlane {
	return [4]halfPlane{
		{limit: b.lonMin, lat: false, upper: false},
		{limit: b.lonMax, lat: false, upper: true},
		{limit: b.latMin, lat: true, upper: false},
		{limit: b.latMax, lat: true, upper: true},
	}
}

// value is the coordinate this plane measures.
func (h halfPlane) value(point Point) float64 {
	if h.lat {
		return point.Lat
	}

	return point.Lon
}

// inside reports whether a point is on the side the cell keeps.
//
// The test is inclusive, so a point sitting exactly on a boundary is inside
// both of the cells that share it. That is what makes the two pieces meet
// rather than leave a hairline of water between them.
func (h halfPlane) inside(point Point) bool {
	if h.upper {
		return h.value(point) <= h.limit
	}

	return h.value(point) >= h.limit
}

// cross is where the segment from one point to the other meets this plane.
//
// It is only ever called for a segment with one end on each side, so the
// denominator cannot be zero: a segment whose two ends measure the same value
// has both ends inside or both outside, and the caller does not ask.
func (h halfPlane) cross(from, to Point) Point {
	fraction := (h.limit - h.value(from)) / (h.value(to) - h.value(from))

	return Point{
		Lat: from.Lat + fraction*(to.Lat-from.Lat),
		Lon: from.Lon + fraction*(to.Lon-from.Lon),
	}
}

// clipper cuts rings to cells, holding the two buffers the passes alternate
// between so a run over the whole world allocates a handful of times rather
// than once per cell per ring.
type clipper struct {
	from Polyline
	to   Polyline
}

// clip returns the part of ring inside box, or nothing when none of it is.
//
// The result is a fresh slice. The buffers are reused by the next call, so
// handing one of them back would leave every piece pointing at the last ring
// the clipper happened to see.
func (c *clipper) clip(ring Polyline, box cellBox) Polyline {
	c.from = append(c.from[:0], ring...)

	for _, side := range box.sides() {
		c.to = clipHalf(c.to[:0], c.from, side)
		c.from, c.to = c.to, c.from

		if len(c.from) == 0 {
			return nil
		}
	}

	if len(c.from) < minRingPoints {
		return nil
	}

	return append(Polyline(nil), c.from...)
}

// clipHalf is one Sutherland-Hodgman pass: everything on the kept side of the
// plane, with a crossing point written wherever the ring changes sides.
//
// The ring is walked as a closed loop, starting from the edge that runs from
// the last point back to the first, so the piece comes out closed however the
// source ring happened to start.
func clipHalf(dst, src Polyline, side halfPlane) Polyline {
	if len(src) == 0 {
		return dst
	}

	previous := src[len(src)-1]
	was := side.inside(previous)

	for _, current := range src {
		now := side.inside(current)

		if now != was {
			dst = append(dst, side.cross(previous, current))
		}

		if now {
			dst = append(dst, current)
		}

		previous, was = current, now
	}

	return dst
}

// bucketRings files every ring under each cell it covers, clipped to that
// cell.
//
// A ring is offered only to the cells its bounding box reaches, so an island
// costs one clip and a continent costs one per cell it spans. Cells inside the
// bounding box that the ring misses come back empty from the clipper and are
// never filed.
func bucketRings(rings []Polyline) map[int][]Polyline {
	buckets := make(map[int][]Polyline)
	cut := &clipper{}

	for _, ring := range rings {
		fileRing(buckets, cut, ring)
	}

	return buckets
}

// fileRing clips one ring into every cell it reaches.
func fileRing(buckets map[int][]Polyline, cut *clipper, ring Polyline) {
	if len(ring) < minRingPoints {
		return
	}

	span := ringCells(ring)

	for row := span.rowLow; row <= span.rowHigh; row++ {
		for col := span.colLow; col <= span.colHigh; col++ {
			piece := cut.clip(ring, boxOf(row, col))
			if piece == nil {
				continue
			}

			key := row*cellCols + col
			buckets[key] = append(buckets[key], piece)
		}
	}
}

// cellRange is the block of cells a ring's bounding box covers, as a pair of
// inclusive bounds on each axis.
type cellRange struct {
	rowLow  int
	rowHigh int
	colLow  int
	colHigh int
}

// ringCells is the block of cells a ring's bounding box covers.
//
// The columns are not wrapped, because Natural Earth splits its land at the
// dateline rather than letting a polygon run across it: every ring in the
// input stays inside one hemisphere's worth of columns. A ring that did cross
// would be filed into the columns between its two edges instead, the long way
// round the world, which is wrong but bounded and would show up as a filled
// band rather than as a crash.
func ringCells(ring Polyline) cellRange {
	latLow, latHigh := ring[0].Lat, ring[0].Lat
	lonLow, lonHigh := ring[0].Lon, ring[0].Lon

	for _, point := range ring[1:] {
		latLow, latHigh = min(latLow, point.Lat), max(latHigh, point.Lat)
		lonLow, lonHigh = min(lonLow, point.Lon), max(lonHigh, point.Lon)
	}

	return cellRange{
		rowLow:  cellRow(latLow),
		rowHigh: cellRow(latHigh),
		colLow:  boundedColumn(lonLow),
		colHigh: boundedColumn(lonHigh),
	}
}

// boundedColumn is columnIndex pinned to the grid.
//
// columnIndex leaves its result unwrapped on purpose, so Within can measure
// how many columns a box spans. Here the answer is used as an index straight
// away, and a longitude of exactly 180 would otherwise name a 73rd column.
func boundedColumn(lon float64) int {
	return min(max(columnIndex(lon), 0), cellCols-1)
}
