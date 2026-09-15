package shore

import "math"

// The cell grid. It is the index Within walks and the order Encode writes in,
// so the two sides of the file agree on it by sharing this file.
const (
	// cellDegrees is the side of one cell. Five degrees is about 300 nautical
	// miles north to south, so uScope's widest range touches four cells and
	// its narrowest one.
	cellDegrees = 5

	// cellRows and cellCols are how many cells cover the world.
	cellRows = 180 / cellDegrees
	cellCols = 360 / cellDegrees

	// maxCells is every cell once, which is the most a well-formed file can
	// declare.
	maxCells = cellRows * cellCols

	// quarterTurn and halfTurn move latitude and longitude into a range that
	// starts at zero, so a cell index is a division rather than a division
	// and a branch.
	quarterTurn = 90
	halfTurn    = 180
)

// cellRow is the five degree band a latitude falls in, counting north from
// the south pole.
//
// Exactly 90 north belongs to the last band rather than to a 37th one a
// single line of latitude wide, which is what the clamp is for.
func cellRow(lat float64) int {
	band := int(math.Floor((clampLat(lat) + quarterTurn) / cellDegrees))

	return min(band, cellRows-1)
}

// columnIndex is the five degree column a longitude falls in, counting east
// from 180 west.
//
// The result is not wrapped. Within needs to know how many columns a box
// spans, and a box that runs past the dateline spans columns numbered past
// the last one; wrapColumn turns each of those into a real column when it is
// used as an index.
func columnIndex(lon float64) int {
	return int(math.Floor((clampLon(lon) + halfTurn) / cellDegrees))
}

// wrapColumn folds a column index back onto the globe, so 72 is column 0
// again and -1 is column 71.
func wrapColumn(col int) int {
	return ((col % cellCols) + cellCols) % cellCols
}

// cellOf is the packed cell a point belongs to, which is what a cell is keyed
// by in a Set and what Encode groups polylines under.
func cellOf(point Point) int {
	return cellRow(point.Lat)*cellCols + wrapColumn(columnIndex(point.Lon))
}

// clampLat pins a latitude to the poles, folding a NaN onto the equator.
//
// NaN is handled first because min and max propagate it rather than pinning
// it, and converting a NaN to an int is undefined in the spec: one bad
// coordinate would otherwise index the cell map with whatever the compiler
// felt like that day.
func clampLat(lat float64) float64 {
	if math.IsNaN(lat) {
		return 0
	}

	return min(max(lat, -quarterTurn), quarterTurn)
}

// clampLon pins a longitude to a box three worlds wide, folding a NaN onto
// the prime meridian.
//
// Three worlds rather than one: Within is handed a box that may run off both
// ends of the dateline, and the columns past the end are what tell it how far
// the box reaches. Anything past that is a caller mistake or an infinity, and
// both are better pinned than converted to an int.
func clampLon(lon float64) float64 {
	if math.IsNaN(lon) {
		return 0
	}

	return min(max(lon, -3*halfTurn), 3*halfTurn)
}
