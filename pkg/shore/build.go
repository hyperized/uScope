package shore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

// The conversion's defaults. They live here rather than on the generator's
// command line because they are the shape of the shipped file, not a knob an
// operator turns: changing one changes what every uScope draws, so it is a
// code change with a number in the commit message.
const (
	// defaultMinLakeKm2 is the smallest lake that keeps its shoreline. Below
	// it a lake is a few pixels of noise at any range the scope draws, and
	// Natural Earth has thousands of them.
	defaultMinLakeKm2 = 5

	// defaultToleranceM leaves Douglas-Peucker off, because the file does not
	// need it: the full coastline packs to 2.1 MB, inside the budget the
	// binary has for it. WithSimplify is still there and still tested, since
	// the next Natural Earth release or a smaller budget would want it, and a
	// hundred metres is the figure to reach for. That is a fifth of a pixel at
	// the widest range uScope draws, and it takes about 11% off the file.
	defaultToleranceM = 0

	// The two geometry types a coastline arrives as, and the two a lake does.
	typeLineString      = "LineString"
	typeMultiLineString = "MultiLineString"
	typePolygon         = "Polygon"
	typeMultiPolygon    = "MultiPolygon"

	// kmPerDegree is one degree of latitude in kilometres, which is the only
	// scale the area approximation needs.
	kmPerDegree = 111.32

	// metresPerDegree is the same figure in metres, used to turn the
	// simplification tolerance into the degree units the geometry is in.
	metresPerDegree = kmPerDegree * 1000

	// degToRad converts degrees to radians for the one cosine in here.
	degToRad = math.Pi / halfTurn

	// minRingPoints is the fewest points that enclose an area.
	minRingPoints = 3
)

// ErrGeoJSON is returned for input that is not the GeoJSON the generator
// expects. It is a sentinel so the generator can say which file was wrong
// without matching on a message.
var ErrGeoJSON = errors.New("shore: cannot read geojson")

// build is the settled conversion.
type build struct {
	minLakeKm2 float64
	toleranceM float64
}

// BuildOption adjusts the conversion.
type BuildOption func(*build)

// WithMinLakeArea sets the smallest lake, in square kilometres, whose
// shoreline survives. Zero or less keeps every lake.
func WithMinLakeArea(km2 float64) BuildOption {
	return func(b *build) { b.minLakeKm2 = km2 }
}

// WithSimplify sets the Douglas-Peucker tolerance in metres. Zero or less
// keeps every point.
func WithSimplify(metres float64) BuildOption {
	return func(b *build) { b.toleranceM = metres }
}

// Build turns Natural Earth's coastline and lakes files into the polylines to
// pack.
//
// It keeps coastline LineStrings and MultiLineStrings whole, and takes the
// outer ring of every lake Polygon and MultiPolygon that is big enough to see.
// Inner rings, the islands inside a lake, are dropped: at the ranges uScope
// draws they are a handful of pixels inside an outline that already reads as
// water.
//
// The order of the result follows the order of the input, so the same two
// files always produce the same polylines in the same order, and Encode turns
// that into the same bytes.
func Build(coastline, lakes io.Reader, opts ...BuildOption) ([]Polyline, error) {
	cfg := build{minLakeKm2: defaultMinLakeKm2, toleranceM: defaultToleranceM}
	for _, opt := range opts {
		opt(&cfg)
	}

	coastFeatures, err := features(coastline)
	if err != nil {
		return nil, fmt.Errorf("coastline: %w", err)
	}

	lakeFeatures, err := features(lakes)
	if err != nil {
		return nil, fmt.Errorf("lakes: %w", err)
	}

	lines := make([]Polyline, 0, len(coastFeatures)+len(lakeFeatures))

	for _, item := range coastFeatures {
		lines = cfg.appendCoast(lines, item)
	}

	for _, item := range lakeFeatures {
		lines = cfg.appendLake(lines, item)
	}

	return lines, nil
}

// collection is as much of a GeoJSON FeatureCollection as the conversion
// reads. Everything else in the file, and Natural Earth ships a lot of it, is
// names in thirty languages and Wikidata identifiers.
type collection struct {
	Features []feature `json:"features"`
}

// feature is one geometry. The properties are not read: the coastline file is
// all coastline, and a lake is kept or dropped on its area rather than on
// what it is called.
type feature struct {
	Geometry geometry `json:"geometry"`
}

// geometry is a shape and its coordinates, left raw because the coordinates
// nest one level deeper for every "Multi" in the type name.
type geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// features reads a FeatureCollection.
func features(r io.Reader) ([]feature, error) {
	var set collection

	if err := json.NewDecoder(r).Decode(&set); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGeoJSON, err)
	}

	return set.Features, nil
}

// appendCoast adds whatever lines one coastline feature holds.
func (b build) appendCoast(dst []Polyline, item feature) []Polyline {
	switch item.Geometry.Type {
	case typeLineString:
		return b.appendLine(dst, points(item.Geometry.Coordinates))
	case typeMultiLineString:
		for _, part := range groups(item.Geometry.Coordinates) {
			dst = b.appendLine(dst, part)
		}

		return dst
	default:
		return dst
	}
}

// appendLake adds the outer ring of one lake feature, if it is large enough.
func (b build) appendLake(dst []Polyline, item feature) []Polyline {
	switch item.Geometry.Type {
	case typePolygon:
		return b.appendRing(dst, first(groups(item.Geometry.Coordinates)))
	case typeMultiPolygon:
		for _, part := range nests(item.Geometry.Coordinates) {
			dst = b.appendRing(dst, first(part))
		}

		return dst
	default:
		return dst
	}
}

// appendLine simplifies a line and adds it, dropping anything that is no
// longer a line afterwards.
func (b build) appendLine(dst []Polyline, line Polyline) []Polyline {
	if len(line) < minPolylinePoints {
		return dst
	}

	return append(dst, simplify(line, b.toleranceM))
}

// appendRing adds a lake's outer ring when it encloses enough water.
func (b build) appendRing(dst []Polyline, ring Polyline) []Polyline {
	if len(ring) < minRingPoints || areaKm2(ring) < b.minLakeKm2 {
		return dst
	}

	return b.appendLine(dst, ring)
}

// first is the outer ring of a polygon, or nothing when it has no rings.
// GeoJSON puts the outer ring first and the holes after it.
func first(rings []Polyline) Polyline {
	if len(rings) == 0 {
		return nil
	}

	return rings[0]
}

// points reads a LineString's coordinates.
//
// A geometry whose coordinates will not parse is dropped rather than failing
// the run. Natural Earth is not the input this has to be strict about: it is
// a fixed file that either converts or is replaced, and half a coastline is
// more useful than none while somebody works out which feature is malformed.
func points(raw json.RawMessage) Polyline {
	var coords [][]float64
	if err := json.Unmarshal(raw, &coords); err != nil {
		return nil
	}

	return polyline(coords)
}

// groups reads the coordinates of a MultiLineString or a Polygon, which are
// the same shape: a list of point lists.
func groups(raw json.RawMessage) []Polyline {
	var coords [][][]float64
	if err := json.Unmarshal(raw, &coords); err != nil {
		return nil
	}

	lines := make([]Polyline, 0, len(coords))
	for _, part := range coords {
		lines = append(lines, polyline(part))
	}

	return lines
}

// nests reads a MultiPolygon's coordinates: a list of polygons, each a list
// of rings.
func nests(raw json.RawMessage) [][]Polyline {
	var coords [][][][]float64
	if err := json.Unmarshal(raw, &coords); err != nil {
		return nil
	}

	polygons := make([][]Polyline, 0, len(coords))

	for _, rings := range coords {
		lines := make([]Polyline, 0, len(rings))
		for _, ring := range rings {
			lines = append(lines, polyline(ring))
		}

		polygons = append(polygons, lines)
	}

	return polygons
}

// polyline turns GeoJSON coordinates into points. GeoJSON writes longitude
// first, and a position may carry a third element for elevation, which is
// ignored. A pair that is short or not finite is skipped: it cannot be
// projected, and a NaN would poison the cell index it lands in.
func polyline(coords [][]float64) Polyline {
	line := make(Polyline, 0, len(coords))

	for _, pair := range coords {
		if len(pair) < minPolylinePoints || !finite(pair[0]) || !finite(pair[1]) {
			continue
		}

		line = append(line, Point{Lat: pair[1], Lon: pair[0]})
	}

	return line
}

// finite reports whether a coordinate is a real number.
func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// areaKm2 approximates the area a ring encloses.
//
// It is the shoelace formula on a plane, with longitude squeezed by the
// cosine of the ring's mean latitude: the same cheap equirectangular
// projection the scope itself draws with. That is accurate enough for a
// threshold, because the error grows with the latitude span of the ring and a
// lake big enough to argue about spans a fraction of a degree. It would be
// wrong for a ring that straddles a pole or the dateline, and at this scale
// Natural Earth's lakes include neither.
func areaKm2(ring Polyline) float64 {
	if len(ring) < minRingPoints {
		return 0
	}

	var sumLat float64
	for _, point := range ring {
		sumLat += point.Lat
	}

	squeeze := math.Cos(sumLat / float64(len(ring)) * degToRad)

	var twice float64

	for index, point := range ring {
		next := ring[(index+1)%len(ring)]
		twice += point.Lon*next.Lat - next.Lon*point.Lat
	}

	return math.Abs(twice) / 2 * kmPerDegree * kmPerDegree * squeeze
}

// simplify runs Douglas-Peucker over a polyline with a tolerance in metres.
//
// The stack is explicit rather than recursive. Natural Earth's longest
// coastline feature runs to tens of thousands of points, and the worst case
// for this algorithm is a recursion as deep as the line is long.
func simplify(line Polyline, metres float64) Polyline {
	if metres <= 0 || len(line) < minRingPoints {
		return line
	}

	tolerance := metres / metresPerDegree
	keep := make([]bool, len(line))
	keep[0], keep[len(line)-1] = true, true

	stack := [][2]int{{0, len(line) - 1}}

	for len(stack) > 0 {
		segment := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		index, distance := farthest(line, segment[0], segment[1])
		if distance <= tolerance {
			continue
		}

		keep[index] = true
		stack = append(stack, [2]int{segment[0], index}, [2]int{index, segment[1]})
	}

	return kept(line, keep)
}

// kept collects the points the simplification decided to hold on to.
func kept(line Polyline, keep []bool) Polyline {
	out := make(Polyline, 0, len(line))

	for index, point := range line {
		if keep[index] {
			out = append(out, point)
		}
	}

	return out
}

// farthest finds the point between first and last that sits furthest off the
// straight line between them, and how far off it is in degrees of latitude.
//
// Longitude is squeezed by the cosine of the chord's mid latitude, so a
// tolerance means roughly the same distance on the ground wherever the line
// happens to be. Without it a hundred metres would be a hundred metres at the
// equator and four hundred in northern Norway.
func farthest(line Polyline, first, last int) (int, float64) {
	squeeze := math.Cos((line[first].Lat + line[last].Lat) / 2 * degToRad)
	baseX, baseY := line[first].Lon*squeeze, line[first].Lat
	runX, runY := line[last].Lon*squeeze-baseX, line[last].Lat-baseY
	span := math.Hypot(runX, runY)

	worst, worstAt := 0.0, first+1

	for index := first + 1; index < last; index++ {
		offX, offY := line[index].Lon*squeeze-baseX, line[index].Lat-baseY

		var distance float64
		if span == 0 {
			distance = math.Hypot(offX, offY)
		} else {
			distance = math.Abs(offX*runY-offY*runX) / span
		}

		if distance > worst {
			worst, worstAt = distance, index
		}
	}

	return worstAt, worst
}
