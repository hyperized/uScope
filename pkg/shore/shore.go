// Package shore carries the coastlines and lake shores uScope draws under the
// scope, so the surroundings are recognisable without a map service.
//
// The data is Natural Earth's 1:10m coastline and lakes, converted once by
// internal/tools/shoregen and compiled into the binary. See README.md in this
// directory for the provenance, the licence and how to regenerate it. uScope
// runs on a handheld with no network, anywhere in the world, so querying a
// tile server or an Overpass endpoint at run time was never an option: the
// data ships or it does not exist.
//
// The world is cut into five degree cells and every polyline is filed under
// the cell it lies in. A scope covering a few hundred nautical miles touches
// a handful of those, so Within walks a few thousand points instead of the
// half million in the set. Five degrees is the compromise: smaller cells mean
// a longer index and more polylines cut in half, larger ones mean more points
// visited that are nowhere near the scope.
//
// A Set is read-only once Decode has built it, so any number of goroutines
// may call Within on the same one.
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

// Polyline is a run of shoreline. It is a line rather than a ring even when
// it came from a lake: the scope draws outlines, and a closed ring is a line
// whose last point repeats its first.
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
}

// The packed data, built by internal/tools/shoregen from the two Natural
// Earth files. Regenerate it with `make shore-data`.
//
//nolint:gochecknoglobals // go:embed needs a package-level variable.
//go:embed shore.bin.gz
var packed []byte

// loaded is the decode that happens at most once. It is a pointer so the
// sync.Once inside is never copied.
//
//nolint:gochecknoglobals // the cache has to outlive the call that fills it.
var loaded = &cache{data: packed}

// cache holds the decoded set and replays the outcome, error included, to
// every caller after the first. The bytes are a field rather than read
// straight off the embed, the way pkg/fonts does it, so a test can put a
// damaged file through the same path the real one takes.
type cache struct {
	once sync.Once
	data []byte
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
		c.set, c.err = Decode(bytes.NewReader(c.data))
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
	if s == nil || visit == nil || badBox(latMin, latMax, lonMin, lonMax) {
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
		s.visitRow(row, colLow, colHigh, visit)
	}
}

// visitRow hands over one row's share of the box.
func (s *Set) visitRow(row, colLow, colHigh int, visit func(Polyline)) {
	for col := colLow; col <= colHigh; col++ {
		for _, line := range s.cells[row*cellCols+wrapColumn(col)] {
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
