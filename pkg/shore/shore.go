// Package shore carries the coastlines, lake shores and land uScope draws
// under the scope, so the surroundings are recognisable without a map service.
//
// The data is Natural Earth's 1:10m coastline, lakes and land, converted once
// by internal/tools/shoregen and compiled into the binary as two packed files.
// See README.md in this directory for the provenance, the licence and how to
// regenerate them. uScope runs on a handheld with no network, anywhere in the
// world, so querying a tile server or an Overpass endpoint at run time was
// never an option: the data ships or it does not exist.
//
// There are two halves because the scope draws two things. Within gives the
// open polylines the coastline is drawn as. LandWithin gives the closed rings
// that say which side of it is sea, which a line cannot: an open curve has no
// inside, so the fill has to come from polygons.
//
// The world is cut into five degree cells and every shape is filed under the
// cell it lies in. A scope covering a few hundred nautical miles touches a
// handful of those, so a query walks a few thousand points instead of the half
// million in the set. Five degrees is the compromise: smaller cells mean a
// longer index and more shapes cut in half, larger ones mean more points
// visited that are nowhere near the scope.
//
// The two halves are cut into those cells differently. A polyline is split
// where it leaves a cell, with the crossing segment kept on both sides so the
// line shows no gap. A ring is clipped against the cell rectangle with
// Sutherland-Hodgman, so each piece is a closed ring that runs along the
// boundary where the original ran off it; splitting it the way a line is split
// would leave two open curves, and an open curve cannot be filled. See land.go.
//
// A Set is read-only once Decode has built it, so any number of goroutines
// may call Within or LandWithin on the same one.
package shore

import (
	"bytes"
	_ "embed"
	"math"
	"sync"
)

// Point is one position on a shoreline, in degrees.
type Point struct {
	Lat float64
	Lon float64
}

// Polyline is a run of points, and what it means depends on which half of the
// set it came from.
//
// From Within it is an open line: a stretch of coastline, or a lake shore,
// which arrives as a ring whose last point repeats its first because the
// source data has it that way. From LandWithin it is a closed ring, and the
// edge from its last point back to its first is part of it whether or not that
// point is repeated. It is not, because repeating it would cost bytes in the
// file and say nothing the shape does not already say.
type Polyline []Point

// Set is the whole world's shorelines, indexed by cell.
//
// Every Polyline in it is a window on one shared slice of points, which is
// what keeps a set of half a million points down to a handful of allocations
// rather than one per line.
type Set struct {
	// cells maps a packed row and column onto the polylines filed there. A
	// cell with nothing in it is absent rather than empty, so open ocean
	// costs nothing.
	cells map[int][]Polyline

	// land is the same index over the closed rings that separate land from
	// water: the outer ring of every landmass, the holes in it, and the lake
	// shores cut out of it. It is empty in a Set that came from Decode rather
	// than DecodeWithLand, and LandWithin then visits nothing.
	land map[int][]Polyline
}

// The packed data, built by internal/tools/shoregen from the three Natural
// Earth files. Regenerate both with `make shore-data`.
//
//nolint:gochecknoglobals // go:embed needs a package-level variable.
//go:embed shore.bin.gz
var packed []byte

//nolint:gochecknoglobals // go:embed needs a package-level variable.
//go:embed land.bin.gz
var packedLand []byte

// loaded is the decode that happens at most once. It is a pointer so the
// sync.Once inside is never copied.
//
//nolint:gochecknoglobals // the cache has to outlive the call that fills it.
var loaded = &cache{data: packed, land: packedLand}

// cache holds the decoded set and replays the outcome, error included, to
// every caller after the first. The bytes are fields rather than read straight
// off the embeds, the way pkg/fonts does it, so a test can put a damaged file
// through the same path the real one takes.
type cache struct {
	once sync.Once
	data []byte
	land []byte
	set  *Set
	err  error
}

// Load decodes the embedded data, once, and hands the same Set to every
// later caller.
//
// It is the only thing uScope itself calls. Decoding costs a few million
// point conversions, so a program that never draws a shoreline should not pay
// for it at init time, which is why there is no eager package variable here.
// The returned Set is read-only and safe to share.
func Load() (*Set, error) { return loaded.load() }

// load runs the decode under the Once.
func (c *cache) load() (*Set, error) {
	c.once.Do(func() {
		c.set, c.err = DecodeWithLand(bytes.NewReader(c.data), bytes.NewReader(c.land))
	})

	return c.set, c.err
}

// Within calls visit for every polyline whose cell meets the box, which is
// given in degrees.
//
// The test is on cells, not on points: a polyline in a cell that touches the
// box is visited whole, even when every point of it sits outside. The caller
// is projecting and clipping each segment anyway, so a per-point test here
// would cost more than the segments it saved.
//
// Nothing is allocated, which is what makes this safe on a render path. The
// Polyline handed to visit aliases the set and must not be written to or kept
// after visit returns.
//
// A nil Set visits nothing, so a caller with no shore data does not have to
// nil-check. Longitudes outside the usual range are wrapped, because a scope
// near the dateline is centred on a box that runs off both ends of it.
func (s *Set) Within(latMin, latMax, lonMin, lonMax float64, visit func(Polyline)) {
	if s == nil {
		return
	}

	visitCells(s.cells, latMin, latMax, lonMin, lonMax, visit)
}

// LandWithin calls visit for every land ring whose cell meets the box, which
// is given in degrees.
//
// A ring is a closed piece of the boundary between land and water, cut to the
// cell it is filed under: the outline of a landmass, a hole in one, or a lake
// shore. Which side of it is land is not recorded and does not need to be.
// Fill every ring the call visits in one even-odd pass and the answer falls
// out: a point on land is enclosed an odd number of times, a point in a lake
// inside that land an even number, and a point at sea none at all.
//
// The pieces two neighbouring cells hold meet on exactly the same
// coordinates, because both were cut from the same ring by the same
// arithmetic, so one even-odd pass over the rings of several cells fills them
// as one shape with no seam between them.
//
// Everything Within's own documentation says about allocation, aliasing, nil
// receivers and the dateline holds here word for word: the two walk the same
// index in the same way.
func (s *Set) LandWithin(latMin, latMax, lonMin, lonMax float64, visit func(Polyline)) {
	if s == nil {
		return
	}

	visitCells(s.land, latMin, latMax, lonMin, lonMax, visit)
}

// visitCells hands every polyline in the cells the box meets to visit. It is
// the walk Within and LandWithin share; only the index differs.
func visitCells(
	cells map[int][]Polyline, latMin, latMax, lonMin, lonMax float64, visit func(Polyline),
) {
	if visit == nil || badBox(latMin, latMax, lonMin, lonMax) {
		return
	}

	rowLow, rowHigh := cellRow(latMin), cellRow(latMax)
	colLow, colHigh := columnIndex(lonMin), columnIndex(lonMax)

	// A box wider than the world is every column once rather than the same
	// columns several times over.
	if colHigh-colLow >= cellCols {
		colLow, colHigh = 0, cellCols-1
	}

	for row := rowLow; row <= rowHigh; row++ {
		visitRow(cells, row, colLow, colHigh, visit)
	}
}

// visitRow hands over one row's share of the box.
func visitRow(cells map[int][]Polyline, row, colLow, colHigh int, visit func(Polyline)) {
	for col := colLow; col <= colHigh; col++ {
		for _, line := range cells[row*cellCols+wrapColumn(col)] {
			visit(line)
		}
	}
}

// badBox rejects a box that cannot be turned into cells.
//
// NaN is named rather than left to the comparisons, because every ordering
// test against a NaN is false: it would slip past a minimum-past-maximum
// check written the obvious way, reach math.Floor, and end up in an integer
// conversion the spec does not define.
func badBox(latMin, latMax, lonMin, lonMax float64) bool {
	return math.IsNaN(latMin) || math.IsNaN(latMax) ||
		math.IsNaN(lonMin) || math.IsNaN(lonMax) ||
		latMin > latMax || lonMin > lonMax
}
