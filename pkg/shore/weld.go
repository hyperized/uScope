package shore

import (
	"math"
	"slices"
)

// Welding the pieces of one ring along the cell boundaries they share.
//
// Sutherland-Hodgman gives two neighbouring pieces the same crossing points,
// but not the same edges between them. Where a ring leaves a cell, the piece
// inside walks the boundary from one crossing to the next one the clipper
// happens to reach, and that is often a cell corner several crossings further
// on. The neighbour walks the same stretch in other steps. In exact arithmetic
// the two walks still cancel under even-odd, edge for edge, and on a flat map
// they cancel in pixels as well: a boundary there is a pixel column or a pixel
// row, and every edge along it rounds onto that column whatever its ends are.
//
// In perspective a boundary is a slanted line, and an edge along it is drawn
// between its own two rounded ends. Two edges covering the same stretch from
// different ends are two slightly different lines, and on every scanline where
// they round to different columns the fill leaves a pixel open between them.
// That was the dashed line down the five degree meridian in both tilted views.
//
// So every edge that runs along a boundary is cut at every point any piece of
// the same ring has on that boundary. The two walks then consist of the same
// edges between the same points, which round identically however they are
// projected, and cancel exactly. The points added are collinear with the edge
// they split, so on the flat map, where the boundary is a pixel column or row
// already, nothing moves.

// onLineTolerance is how far, in degrees, a coordinate may sit from a
// multiple of the cell size and still count as on that boundary.
//
// A crossing is computed as from plus a fraction of the step to the plane, so
// it can land a rounding error off the limit it was aimed at. A billionth of
// a degree is ten thousand times smaller than the file's fixed-point unit and
// millions of times larger than that rounding error.
const onLineTolerance = 1e-9

// gridLine is one cell boundary: a meridian when lat is false, a parallel when
// it is true, at index times cellDegrees.
type gridLine struct {
	lat   bool
	index int
}

// lineStops is, for every boundary any piece of one ring touches, the points
// along it that some piece has a vertex at. A meridian is measured in
// latitude and a parallel in longitude.
type lineStops map[gridLine][]float64

// gridIndex reports which multiple of the cell size a coordinate sits on, if
// it sits on one at all.
func gridIndex(value float64) (int, bool) {
	index := math.Round(value / cellDegrees)

	return int(index), math.Abs(value-index*cellDegrees) <= onLineTolerance
}

// meridianOf and parallelOf are the boundary a point is on in each direction,
// if any.
func meridianOf(point Point) (gridLine, bool) {
	index, ok := gridIndex(point.Lon)

	return gridLine{lat: false, index: index}, ok
}

func parallelOf(point Point) (gridLine, bool) {
	index, ok := gridIndex(point.Lat)

	return gridLine{lat: true, index: index}, ok
}

// along is the coordinate that runs along a boundary.
func (l gridLine) along(point Point) float64 {
	if l.lat {
		return point.Lon
	}

	return point.Lat
}

// add records a point as a stop on whichever boundaries it lies on.
func (s lineStops) add(point Point) {
	if line, ok := meridianOf(point); ok {
		s[line] = append(s[line], line.along(point))
	}

	if line, ok := parallelOf(point); ok {
		s[line] = append(s[line], line.along(point))
	}
}

// sharedLine is the boundary an edge runs along, if it runs along one.
//
// A degenerate edge from a point to itself is not along anything: it has no
// interior to cut.
func sharedLine(from, to Point) (gridLine, bool) {
	for _, onLine := range [...]func(Point) (gridLine, bool){meridianOf, parallelOf} {
		line, fromOK := onLine(from)
		other, toOK := onLine(to)

		if fromOK && toOK && line == other && line.along(from) != line.along(to) {
			return line, true
		}
	}

	return gridLine{}, false
}

// weld cuts every boundary edge of every piece at the stops the ring's pieces
// share, in place.
//
// Pieces with no edge along a boundary come back untouched, which is nearly
// all of them: only a ring that crosses the grid has any.
func weld(pieces []Polyline) {
	stops := make(lineStops)

	for _, piece := range pieces {
		for _, point := range piece {
			stops.add(point)
		}
	}

	if len(stops) == 0 {
		return
	}

	for line, values := range stops {
		slices.Sort(values)
		stops[line] = slices.Compact(values)
	}

	for index, piece := range pieces {
		pieces[index] = stops.cut(piece)
	}
}

// cut returns the piece with every boundary edge split at the stops strictly
// inside it.
func (s lineStops) cut(piece Polyline) Polyline {
	out := make(Polyline, 0, len(piece))

	for index, from := range piece {
		out = append(out, from)

		to := piece[(index+1)%len(piece)]
		if line, ok := sharedLine(from, to); ok {
			out = s.between(out, line, from, to)
		}
	}

	return out
}

// between appends the stops strictly between two points on one boundary, in
// the order a walk from the first to the second meets them.
//
// The new points take the boundary coordinate from the edge's own first end
// rather than from whichever piece the stop came from, so a split edge stays
// exactly on the line its ends were on.
func (s lineStops) between(dst Polyline, line gridLine, from, to Point) Polyline {
	values := s[line]
	low, high := line.along(from), line.along(to)
	descending := low > high

	if descending {
		low, high = high, low
	}

	first, _ := slices.BinarySearch(values, low)
	last, _ := slices.BinarySearch(values, high)

	inner := values[first:last]
	if len(inner) > 0 && inner[0] == low {
		inner = inner[1:]
	}

	for step := range inner {
		value := inner[step]
		if descending {
			value = inner[len(inner)-1-step]
		}

		dst = append(dst, line.at(from, value))
	}

	return dst
}

// at is the point on the boundary through base at the given distance along it.
func (l gridLine) at(base Point, value float64) Point {
	if l.lat {
		return Point{Lat: base.Lat, Lon: value}
	}

	return Point{Lat: value, Lon: base.Lon}
}
