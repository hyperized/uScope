package shore_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hyperized/uScope/pkg/shore"
)

// coordTolerance is how far a round-tripped coordinate may drift from the
// original value. The format is fixed point at 1e-5 degrees, so anything
// closer than that is the encoding doing its job rather than a bug.
const coordTolerance = 1e-5

// emptyCollection is a FeatureCollection with no features, standing in for
// whichever half of Build's input a test case does not care about.
const emptyCollection = `{"type":"FeatureCollection","features":[]}`

// gzipBytes compresses data and fails the test if gzip itself errors, which
// would mean the fixture is broken rather than the code under test.
func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer

	writer := gzip.NewWriter(&compressed)

	if _, err := writer.Write(data); err != nil {
		t.Fatalf("gzip.Write: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}

	return compressed.Bytes()
}

// buildSet encodes lines through Encode and decodes them back, which is the
// only way an external test can get hold of a *shore.Set: its field is
// unexported, so even a test always goes through the file format.
func buildSet(t *testing.T, lines []shore.Polyline) *shore.Set {
	t.Helper()

	var buf bytes.Buffer

	if err := shore.Encode(&buf, lines); err != nil {
		t.Fatalf("Encode() error = %v, want nil", err)
	}

	set, err := shore.Decode(&buf)
	if err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}

	return set
}

// collectAll drains every polyline Within can see for the whole world, in the
// deterministic row-then-column order Within itself walks. That order is
// what lets a round-trip test compare the result against a hand-worked-out
// expectation instead of a set.
func collectAll(set *shore.Set) []shore.Polyline {
	var got []shore.Polyline

	set.Within(-90, 90, -180, 180, func(line shore.Polyline) {
		got = append(got, slices.Clone(line))
	})

	return got
}

// pointsClose reports whether two points match to within the packed format's
// fixed-point precision.
func pointsClose(got, want shore.Point) bool {
	return math.Abs(got.Lat-want.Lat) <= coordTolerance && math.Abs(got.Lon-want.Lon) <= coordTolerance
}

// polylinesClose reports whether two polylines have the same length and
// every point matches to within coordTolerance, which is the round trip's
// only fidelity promise.
func polylinesClose(got, want shore.Polyline) bool {
	if len(got) != len(want) {
		return false
	}

	for index := range got {
		if !pointsClose(got[index], want[index]) {
			return false
		}
	}

	return true
}

// openFixture opens a testdata fixture by name and closes it when the test
// ends, which keeps every Build test a one-liner for the reader it needs.
func openFixture(t *testing.T, name string) *os.File {
	t.Helper()

	//nolint:gosec // name is a fixture name this test file wrote, not user input.
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("os.Open(%q) error = %v, want nil", name, err)
	}

	t.Cleanup(func() { _ = file.Close() })

	return file
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		lines []shore.Polyline
		want  []shore.Polyline
	}{
		{
			name:  "a single two-point line",
			lines: []shore.Polyline{{{Lat: 52, Lon: 4}, {Lat: 52, Lon: 4.5}}},
			want:  []shore.Polyline{{{Lat: 52, Lon: 4}, {Lat: 52, Lon: 4.5}}},
		},
		{
			name:  "a line whose points are all inside one cell",
			lines: []shore.Polyline{{{Lat: 20, Lon: 20}, {Lat: 20, Lon: 21}, {Lat: 20, Lon: 22}}},
			want:  []shore.Polyline{{{Lat: 20, Lon: 20}, {Lat: 20, Lon: 21}, {Lat: 20, Lon: 22}}},
		},
		{
			// The doc comment on split promises the crossing segment appears
			// on both sides of the cut: the old cell's piece carries the
			// first point of the new cell, and the new cell's piece opens
			// with the last point of the old one.
			name:  "a line crossing a cell boundary",
			lines: []shore.Polyline{{{Lat: 0, Lon: 1}, {Lat: 0, Lon: 4}, {Lat: 0, Lon: 6}}},
			want: []shore.Polyline{
				{{Lat: 0, Lon: 1}, {Lat: 0, Lon: 4}, {Lat: 0, Lon: 6}},
				{{Lat: 0, Lon: 4}, {Lat: 0, Lon: 6}},
			},
		},
		{
			name: "negative latitudes and longitudes",
			lines: []shore.Polyline{
				{{Lat: -12, Lon: -9}, {Lat: -12.5, Lon: -8}, {Lat: -13, Lon: -7}},
			},
			want: []shore.Polyline{
				{{Lat: -12, Lon: -9}, {Lat: -12.5, Lon: -8}, {Lat: -13, Lon: -7}},
			},
		},
		{
			// A two-point line crossing both axes at once: with only two
			// points, the whole segment is duplicated into both of the
			// cells it touches.
			name:  "a line crossing the equator and the prime meridian",
			lines: []shore.Polyline{{{Lat: -1, Lon: -1}, {Lat: 1, Lon: 1}}},
			want: []shore.Polyline{
				{{Lat: -1, Lon: -1}, {Lat: 1, Lon: 1}},
				{{Lat: -1, Lon: -1}, {Lat: 1, Lon: 1}},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			set := buildSet(t, testCase.lines)
			got := collectAll(set)

			if len(got) != len(testCase.want) {
				t.Fatalf("collectAll() = %d polylines, want %d", len(got), len(testCase.want))
			}

			for index, line := range got {
				if !polylinesClose(line, testCase.want[index]) {
					t.Errorf("collectAll()[%d] = %v, want %v", index, line, testCase.want[index])
				}
			}
		})
	}
}

// TestEncodeDeterministic checks that the same input encodes to the same
// bytes every time, which only holds if cells are written in a fixed order
// rather than whatever order a map iteration hands them out in.
func TestEncodeDeterministic(t *testing.T) {
	t.Parallel()

	lines := []shore.Polyline{
		{{Lat: 10, Lon: 10}, {Lat: 10, Lon: 12}},
		{{Lat: -20, Lon: 30}, {Lat: -20, Lon: 32}, {Lat: -20, Lon: 34}},
	}

	var first, second bytes.Buffer

	if err := shore.Encode(&first, lines); err != nil {
		t.Fatalf("Encode() first call error = %v, want nil", err)
	}

	if err := shore.Encode(&second, lines); err != nil {
		t.Fatalf("Encode() second call error = %v, want nil", err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Error("Encode() produced different bytes for the same input across two calls, want identical")
	}
}

func TestDecodeNotGzip(t *testing.T) {
	t.Parallel()

	_, err := shore.Decode(strings.NewReader("this is plainly not a gzip stream"))
	if err == nil {
		t.Fatal("Decode() error = nil, want non-nil")
	}

	const want = "reading gzip header"

	if !strings.Contains(err.Error(), want) {
		t.Errorf("Decode() error = %q, want to contain %q", err.Error(), want)
	}
}

func TestDecodeTruncatedGzip(t *testing.T) {
	t.Parallel()

	compressed := gzipBytes(t, []byte("enough plain bytes that truncating the trailer still leaves a valid header"))

	const trailerCut = 4

	truncated := compressed[:len(compressed)-trailerCut]

	_, err := shore.Decode(bytes.NewReader(truncated))
	if err == nil {
		t.Fatal("Decode() error = nil, want non-nil")
	}

	const want = "decompressing"

	if !strings.Contains(err.Error(), want) {
		t.Errorf("Decode() error = %q, want to contain %q", err.Error(), want)
	}
}

// oversizedBodyLen is one byte past shore's 64 MiB decompressed body cap.
const oversizedBodyLen = 67108865

// TestDecodeBodyTooLarge gzips more than the 64 MiB cap's worth of zeros.
// That is deliberate: a run of zeros this size is what a gzip bomb looks
// like, compressing down to a few kilobytes and decompressing in well under
// a second, and the cap is exactly what is supposed to stop it turning into
// an out-of-memory kill before a single cell has been read.
func TestDecodeBodyTooLarge(t *testing.T) {
	t.Parallel()

	compressed := gzipBytes(t, make([]byte, oversizedBodyLen))

	_, err := shore.Decode(bytes.NewReader(compressed))
	if !errors.Is(err, shore.ErrCount) {
		t.Errorf("Decode() error = %v, want it to wrap ErrCount", err)
	}
}

// boxCase is one shape of box Within and LandWithin are both tested against:
// a hit, a miss, minimum past maximum, a NaN edge, an infinite edge clamped
// rather than rejected, a box wider than the world, and one that wraps the
// dateline.
type boxCase struct {
	name                           string
	latMin, latMax, lonMin, lonMax float64
	wantFound                      bool
}

// boxCases is the table TestWithin and TestLandWithin share. The two methods
// walk the same cell index the same way, so the box shapes that matter to
// one matter equally to the other. It is a function rather than a package
// variable so nothing but checkBoxCases ever holds a reference to it.
func boxCases() []boxCase {
	return []boxCase{
		{name: "a box that finds it", latMin: 51, latMax: 53, lonMin: 3, lonMax: 6, wantFound: true},
		{name: "an empty part of the world", latMin: -10, latMax: -5, lonMin: -10, lonMax: -5, wantFound: false},
		{name: "minimum past maximum", latMin: 53, latMax: 51, lonMin: 3, lonMax: 6, wantFound: false},
		{name: "a NaN edge", latMin: 51, latMax: math.NaN(), lonMin: 3, lonMax: 6, wantFound: false},
		{
			name:   "an infinite edge is clamped rather than rejected",
			latMin: 51, latMax: math.Inf(1), lonMin: 3, lonMax: 6,
			wantFound: true,
		},
		{
			name:   "a box spanning more than 360 degrees of longitude",
			latMin: 51, latMax: 53, lonMin: -400, lonMax: 400,
			wantFound: true,
		},
		{
			name:   "a box past the dateline that has to wrap",
			latMin: 9, latMax: 11, lonMin: 170, lonMax: 190,
			wantFound: true,
		},
	}
}

// checkBoxCases drives call, a Within or a LandWithin bound to some Set,
// over boxCases and checks each one only ever reports whether it found
// something, which is all either method's own tests need to tell apart.
func checkBoxCases(t *testing.T, call func(latMin, latMax, lonMin, lonMax float64, visit func(shore.Polyline))) {
	t.Helper()

	for _, testCase := range boxCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var found bool

			call(testCase.latMin, testCase.latMax, testCase.lonMin, testCase.lonMax, func(shore.Polyline) {
				found = true
			})

			if found != testCase.wantFound {
				t.Errorf("(%v, %v, %v, %v) found = %v, want %v",
					testCase.latMin, testCase.latMax, testCase.lonMin, testCase.lonMax, found, testCase.wantFound)
			}
		})
	}
}

func TestWithin(t *testing.T) {
	t.Parallel()

	lineA := shore.Polyline{{Lat: 52, Lon: 4}, {Lat: 52, Lon: 4.5}, {Lat: 52, Lon: 5}}
	lineB := shore.Polyline{{Lat: 10, Lon: -175}, {Lat: 10, Lon: -174.9}}

	set := buildSet(t, []shore.Polyline{lineA, lineB})

	checkBoxCases(t, set.Within)
}

// TestWithinNilSet checks that a nil *Set visits nothing rather than
// panicking, so a caller with no shore data does not have to nil-check.
func TestWithinNilSet(t *testing.T) {
	t.Parallel()

	var set *shore.Set

	visited := false

	set.Within(-1, 1, -1, 1, func(shore.Polyline) { visited = true })

	if visited {
		t.Error("Within() on a nil Set called visit, want it to visit nothing")
	}
}

// TestWithinNilVisit checks that a nil visit function is a no-op rather than
// a panic.
func TestWithinNilVisit(t *testing.T) {
	t.Parallel()

	set := buildSet(t, []shore.Polyline{{{Lat: 0, Lon: 0}, {Lat: 0, Lon: 1}}})

	set.Within(-1, 1, -1, 1, nil)
}

// TestWithinAllocations checks that a render-path call allocates nothing.
// AllocsPerRun panics when called from a parallel test, which is why this
// test, unlike its neighbours, does not call t.Parallel: see
// internal/radar/radar_external_test.go for the same restriction.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestWithinAllocations(t *testing.T) {
	set := buildSet(t, []shore.Polyline{{{Lat: 52, Lon: 4}, {Lat: 52, Lon: 4.5}, {Lat: 52, Lon: 5}}})
	visit := func(shore.Polyline) {}

	got := testing.AllocsPerRun(50, func() { set.Within(51, 53, 3, 6, visit) })
	if got != 0 {
		t.Errorf("Within() allocated %.1f times per call, want 0", got)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	first, err := shore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if first == nil {
		t.Fatal("Load() set = nil, want the decoded world")
	}

	second, err := shore.Load()
	if err != nil {
		t.Fatalf("Load() second call error = %v, want nil", err)
	}

	if second != first {
		t.Error("Load() returned different pointers across calls, want the cached one")
	}

	var polylineCount, pointCount int

	// A degree or so around the Dutch coast, which Natural Earth's 1:10m
	// coastline certainly crosses.
	first.Within(51.5, 52.5, 3.5, 4.5, func(line shore.Polyline) {
		polylineCount++
		pointCount += len(line)
	})

	const (
		wantMinPolylines = 5
		wantMinPoints    = 200
	)

	if polylineCount < wantMinPolylines {
		t.Errorf("Within() around the Dutch coast found %d polylines, want more than %d",
			polylineCount, wantMinPolylines)
	}

	if pointCount < wantMinPoints {
		t.Errorf("Within() around the Dutch coast found %d points, want more than %d", pointCount, wantMinPoints)
	}
}

// wantPoint is one expected Point, compared with pointsClose so a fixture's
// decimal coordinates are not sensitive to float64 rounding.
type wantPoint struct {
	lat, lon float64
}

// checkLines asserts that got matches want line for line and point for
// point, which every Build test below uses so a failure names exactly which
// line and point disagreed.
func checkLines(t *testing.T, got []shore.Polyline, want [][]wantPoint) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("Build() returned %d lines, want %d", len(got), len(want))
	}

	for lineIndex, line := range got {
		if len(line) != len(want[lineIndex]) {
			t.Fatalf("Build() line %d has %d points, want %d", lineIndex, len(line), len(want[lineIndex]))
		}

		for pointIndex, point := range line {
			wantPt := shore.Point{Lat: want[lineIndex][pointIndex].lat, Lon: want[lineIndex][pointIndex].lon}
			if !pointsClose(point, wantPt) {
				t.Errorf("Build() line %d point %d = %v, want %v", lineIndex, pointIndex, point, wantPt)
			}
		}
	}
}

// TestBuildCoastline checks a LineString and a MultiLineString parse in
// input order, and that a third coordinate element (elevation) is ignored.
func TestBuildCoastline(t *testing.T) {
	t.Parallel()

	lines, err := shore.Build(openFixture(t, "coast_basic.geojson"), strings.NewReader(emptyCollection))
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	checkLines(t, lines, [][]wantPoint{
		{{52, 4}, {52.05, 4.1}, {52.1, 4.2}},
		{{53, 5}, {53.05, 5.1}},
		{{54, 6}, {54.05, 6.1}, {54.1, 6.2}},
	})
}

// TestBuildDroppedCoastline checks that a geometry type Build does not know,
// a line too short to survive, and coordinates that fail to unmarshal all
// vanish from the result without failing the run.
func TestBuildDroppedCoastline(t *testing.T) {
	t.Parallel()

	lines, err := shore.Build(openFixture(t, "coast_dropped.geojson"), strings.NewReader(emptyCollection))
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	if len(lines) != 0 {
		t.Errorf("Build() = %v, want no lines", lines)
	}
}

// TestBuildSimplify checks that WithSimplify changes the result: a straight
// run of points loses its interior once a tolerance is set, and keeps them
// all without one.
func TestBuildSimplify(t *testing.T) {
	t.Parallel()

	coast := openFixture(t, "coast_simplify.geojson")

	plain, err := shore.Build(coast, strings.NewReader(emptyCollection))
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	if len(plain) != 1 || len(plain[0]) != 6 {
		t.Fatalf("Build() without WithSimplify = %v, want one 6-point line", plain)
	}

	const toleranceM = 1

	simplified, err := shore.Build(
		openFixture(t, "coast_simplify.geojson"), strings.NewReader(emptyCollection), shore.WithSimplify(toleranceM),
	)
	if err != nil {
		t.Fatalf("Build() with WithSimplify error = %v, want nil", err)
	}

	if len(simplified) != 1 || len(simplified[0]) != 2 {
		t.Errorf("Build() with WithSimplify(%v) = %v, want one 2-point line", toleranceM, simplified)
	}
}

// TestBuildLakes checks a Polygon and a MultiPolygon lake both above the
// area threshold parse in input order.
func TestBuildLakes(t *testing.T) {
	t.Parallel()

	lines, err := shore.Build(strings.NewReader(emptyCollection), openFixture(t, "lakes_basic.geojson"))
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	checkLines(t, lines, [][]wantPoint{
		{{0, 0}, {0, 0.1}, {0.1, 0.1}, {0.1, 0}, {0, 0}},
		{{10, 10}, {10, 10.1}, {10.1, 10.1}, {10.1, 10}, {10, 10}},
	})
}

// TestBuildLakeAreaThreshold checks that a lake below the default minimum
// area is dropped, and that WithMinLakeArea changes that outcome.
func TestBuildLakeAreaThreshold(t *testing.T) {
	t.Parallel()

	dropped, err := shore.Build(strings.NewReader(emptyCollection), openFixture(t, "lakes_small.geojson"))
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	if len(dropped) != 0 {
		t.Errorf("Build() with the default area threshold = %v, want it dropped", dropped)
	}

	kept, err := shore.Build(
		strings.NewReader(emptyCollection), openFixture(t, "lakes_small.geojson"), shore.WithMinLakeArea(0),
	)
	if err != nil {
		t.Fatalf("Build() with WithMinLakeArea(0) error = %v, want nil", err)
	}

	if len(kept) != 1 || len(kept[0]) != 5 {
		t.Errorf("Build() with WithMinLakeArea(0) = %v, want one 5-point ring kept", kept)
	}
}

// TestBuildDroppedLakes checks that a geometry type Build does not know, a
// polygon with no rings, and coordinates that fail to unmarshal all vanish
// from the result without failing the run.
func TestBuildDroppedLakes(t *testing.T) {
	t.Parallel()

	lines, err := shore.Build(strings.NewReader(emptyCollection), openFixture(t, "lakes_dropped.geojson"))
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	if len(lines) != 0 {
		t.Errorf("Build() = %v, want no lines", lines)
	}
}

// TestBuildInvalidJSON checks that a coastline or a lakes reader returning
// invalid JSON fails the whole run with ErrGeoJSON, rather than being
// silently dropped the way one malformed feature among many good ones is.
func TestBuildInvalidJSON(t *testing.T) {
	t.Parallel()

	t.Run("invalid coastline", func(t *testing.T) {
		t.Parallel()

		_, err := shore.Build(strings.NewReader("not valid json"), strings.NewReader(emptyCollection))
		if !errors.Is(err, shore.ErrGeoJSON) {
			t.Errorf("Build() error = %v, want it to wrap ErrGeoJSON", err)
		}
	})

	t.Run("invalid lakes", func(t *testing.T) {
		t.Parallel()

		_, err := shore.Build(strings.NewReader(emptyCollection), strings.NewReader("not valid json"))
		if !errors.Is(err, shore.ErrGeoJSON) {
			t.Errorf("Build() error = %v, want it to wrap ErrGeoJSON", err)
		}
	})
}

// TestBuildLandRings checks that BuildLand keeps every ring of a Polygon,
// outer boundary and hole alike, and every ring of a MultiPolygon, all in
// input order.
func TestBuildLandRings(t *testing.T) {
	t.Parallel()

	rings, err := shore.BuildLand(openFixture(t, "land_basic.geojson"), strings.NewReader(emptyCollection))
	if err != nil {
		t.Fatalf("BuildLand() error = %v, want nil", err)
	}

	checkLines(t, rings, [][]wantPoint{
		{{0, 0}, {0, 1}, {1, 1}, {1, 0}, {0, 0}},
		{{0.2, 0.2}, {0.8, 0.2}, {0.8, 0.8}, {0.2, 0.8}, {0.2, 0.2}},
		{{10, 10}, {10, 11}, {11, 11}, {11, 10}, {10, 10}},
		{{20, 20}, {20, 21}, {21, 21}, {21, 20}, {20, 20}},
	})
}

// TestBuildLandDropped checks that a geometry type BuildLand does not know
// and a ring with fewer than three points both vanish from the result
// without failing the run.
func TestBuildLandDropped(t *testing.T) {
	t.Parallel()

	rings, err := shore.BuildLand(openFixture(t, "land_dropped.geojson"), strings.NewReader(emptyCollection))
	if err != nil {
		t.Fatalf("BuildLand() error = %v, want nil", err)
	}

	if len(rings) != 0 {
		t.Errorf("BuildLand() = %v, want no rings", rings)
	}
}

// TestBuildLandSimplify checks that WithSimplify changes BuildLand's result.
// Unlike Build, BuildLand simplifies by default, so the direction here runs
// the other way from TestBuildSimplify: passing WithSimplify(0) is what turns
// simplification off and keeps every point of the fixture's straight run.
func TestBuildLandSimplify(t *testing.T) {
	t.Parallel()

	plain, err := shore.BuildLand(openFixture(t, "land_simplify.geojson"), strings.NewReader(emptyCollection))
	if err != nil {
		t.Fatalf("BuildLand() error = %v, want nil", err)
	}

	if len(plain) != 1 || len(plain[0]) != 2 {
		t.Fatalf("BuildLand() with the default tolerance = %v, want one 2-point ring", plain)
	}

	unsimplified, err := shore.BuildLand(
		openFixture(t, "land_simplify.geojson"), strings.NewReader(emptyCollection), shore.WithSimplify(0),
	)
	if err != nil {
		t.Fatalf("BuildLand() with WithSimplify(0) error = %v, want nil", err)
	}

	if len(unsimplified) != 1 || len(unsimplified[0]) != 6 {
		t.Errorf("BuildLand() with WithSimplify(0) = %v, want one 6-point ring", unsimplified)
	}
}

// TestBuildLandLakes checks that BuildLand adds the same lake rings Build
// does, at the same default area threshold.
func TestBuildLandLakes(t *testing.T) {
	t.Parallel()

	rings, err := shore.BuildLand(strings.NewReader(emptyCollection), openFixture(t, "lakes_basic.geojson"))
	if err != nil {
		t.Fatalf("BuildLand() error = %v, want nil", err)
	}

	checkLines(t, rings, [][]wantPoint{
		{{0, 0}, {0, 0.1}, {0.1, 0.1}, {0.1, 0}, {0, 0}},
		{{10, 10}, {10, 10.1}, {10.1, 10.1}, {10.1, 10}, {10, 10}},
	})
}

// TestBuildLandLakeAreaThreshold checks that a lake below the default area
// survives in BuildLand's result only once WithMinLakeArea lowers the bar.
func TestBuildLandLakeAreaThreshold(t *testing.T) {
	t.Parallel()

	dropped, err := shore.BuildLand(strings.NewReader(emptyCollection), openFixture(t, "lakes_small.geojson"))
	if err != nil {
		t.Fatalf("BuildLand() error = %v, want nil", err)
	}

	if len(dropped) != 0 {
		t.Errorf("BuildLand() with the default area threshold = %v, want it dropped", dropped)
	}

	kept, err := shore.BuildLand(
		strings.NewReader(emptyCollection), openFixture(t, "lakes_small.geojson"), shore.WithMinLakeArea(0),
	)
	if err != nil {
		t.Fatalf("BuildLand() with WithMinLakeArea(0) error = %v, want nil", err)
	}

	if len(kept) != 1 || len(kept[0]) != 5 {
		t.Errorf("BuildLand() with WithMinLakeArea(0) = %v, want one 5-point ring kept", kept)
	}
}

// TestBuildLandInvalidJSON checks that a land or a lakes reader returning
// invalid JSON fails the whole run with ErrGeoJSON.
func TestBuildLandInvalidJSON(t *testing.T) {
	t.Parallel()

	t.Run("invalid land", func(t *testing.T) {
		t.Parallel()

		_, err := shore.BuildLand(strings.NewReader("not valid json"), strings.NewReader(emptyCollection))
		if !errors.Is(err, shore.ErrGeoJSON) {
			t.Errorf("BuildLand() error = %v, want it to wrap ErrGeoJSON", err)
		}
	})

	t.Run("invalid lakes", func(t *testing.T) {
		t.Parallel()

		_, err := shore.BuildLand(strings.NewReader(emptyCollection), strings.NewReader("not valid json"))
		if !errors.Is(err, shore.ErrGeoJSON) {
			t.Errorf("BuildLand() error = %v, want it to wrap ErrGeoJSON", err)
		}
	})
}

// buildLandSet builds a Set whose outline half is empty and whose land half
// is the given rings, going through Encode, EncodeLand and DecodeWithLand the
// same way a real caller would: it is the only way an external test can get
// hold of a *shore.Set with land in it, since the field is unexported.
func buildLandSet(t *testing.T, rings []shore.Polyline) *shore.Set {
	t.Helper()

	var outlineBuf, landBuf bytes.Buffer

	if err := shore.Encode(&outlineBuf, nil); err != nil {
		t.Fatalf("Encode() error = %v, want nil", err)
	}

	if err := shore.EncodeLand(&landBuf, rings); err != nil {
		t.Fatalf("EncodeLand() error = %v, want nil", err)
	}

	set, err := shore.DecodeWithLand(&outlineBuf, &landBuf)
	if err != nil {
		t.Fatalf("DecodeWithLand() error = %v, want nil", err)
	}

	return set
}

// collectAllLand drains every ring LandWithin can see for the whole world, in
// the row-then-column order visitCells walks, the same way collectAll does
// for the outline half.
func collectAllLand(set *shore.Set) []shore.Polyline {
	var got []shore.Polyline

	set.LandWithin(-90, 90, -180, 180, func(line shore.Polyline) {
		got = append(got, slices.Clone(line))
	})

	return got
}

// TestEncodeLandDecodeWithLandRoundTrip checks that a ring survives Encode,
// EncodeLand, and DecodeWithLand to the file's fixed-point precision, and
// that Within on the resulting Set still returns the outline half's
// polylines rather than the rings LandWithin sees.
func TestEncodeLandDecodeWithLandRoundTrip(t *testing.T) {
	t.Parallel()

	outline := []shore.Polyline{{{Lat: 0, Lon: 0}, {Lat: 0, Lon: 1}}}
	ring := shore.Polyline{
		{Lat: 10, Lon: 10}, {Lat: 10, Lon: 11}, {Lat: 11, Lon: 11}, {Lat: 11, Lon: 10}, {Lat: 10, Lon: 10},
	}

	var outlineBuf, landBuf bytes.Buffer

	if err := shore.Encode(&outlineBuf, outline); err != nil {
		t.Fatalf("Encode() error = %v, want nil", err)
	}

	if err := shore.EncodeLand(&landBuf, []shore.Polyline{ring}); err != nil {
		t.Fatalf("EncodeLand() error = %v, want nil", err)
	}

	set, err := shore.DecodeWithLand(&outlineBuf, &landBuf)
	if err != nil {
		t.Fatalf("DecodeWithLand() error = %v, want nil", err)
	}

	gotLand := collectAllLand(set)
	if len(gotLand) != 1 || !polylinesClose(gotLand[0], ring) {
		t.Errorf("LandWithin(...) = %v, want one ring close to %v", gotLand, ring)
	}

	gotOutline := collectAll(set)
	if len(gotOutline) != 1 || !polylinesClose(gotOutline[0], outline[0]) {
		t.Errorf("Within(...) = %v, want the outline polyline %v, not the land ring", gotOutline, outline[0])
	}
}

// TestLandWithin mirrors TestWithin: the same shapes of box, against a Set
// whose land half holds the rings instead of the outline half holding lines.
// Both rings need a third point that TestWithin's matching lines do not,
// since fileRing drops anything under minRingPoints before it ever reaches a
// cell.
func TestLandWithin(t *testing.T) {
	t.Parallel()

	ringA := shore.Polyline{{Lat: 52, Lon: 4}, {Lat: 52, Lon: 4.5}, {Lat: 52, Lon: 5}}
	ringB := shore.Polyline{{Lat: 10, Lon: -175}, {Lat: 10, Lon: -174.9}, {Lat: 10.05, Lon: -174.95}}

	set := buildLandSet(t, []shore.Polyline{ringA, ringB})

	checkBoxCases(t, set.LandWithin)
}

// TestLandWithinNilSet checks that a nil *Set visits nothing rather than
// panicking, the same contract Within has.
func TestLandWithinNilSet(t *testing.T) {
	t.Parallel()

	var set *shore.Set

	visited := false

	set.LandWithin(-1, 1, -1, 1, func(shore.Polyline) { visited = true })

	if visited {
		t.Error("LandWithin() on a nil Set called visit, want it to visit nothing")
	}
}

// TestLandWithinNilVisit checks that a nil visit function is a no-op rather
// than a panic.
func TestLandWithinNilVisit(t *testing.T) {
	t.Parallel()

	set := buildLandSet(t, []shore.Polyline{{{Lat: 0, Lon: 0}, {Lat: 0, Lon: 1}}})

	set.LandWithin(-1, 1, -1, 1, nil)
}

// TestLandWithinNoLandHalf checks that a Set built by Decode, which never
// reads a land file, visits nothing when asked for land: it has no land
// index to walk, not an empty one it walks and finds nothing in.
func TestLandWithinNoLandHalf(t *testing.T) {
	t.Parallel()

	set := buildSet(t, []shore.Polyline{{{Lat: 52, Lon: 4}, {Lat: 52, Lon: 4.5}}})

	visited := false

	set.LandWithin(-90, 90, -180, 180, func(shore.Polyline) { visited = true })

	if visited {
		t.Error("LandWithin() on a Set from Decode called visit, want it to visit nothing")
	}
}

// TestDecodeWithLandOutlinesError checks that a bad outlines reader is
// reported without DecodeWithLand ever touching the land reader.
func TestDecodeWithLandOutlinesError(t *testing.T) {
	t.Parallel()

	_, err := shore.DecodeWithLand(strings.NewReader("not a gzip stream"), strings.NewReader("does not matter"))
	if err == nil {
		t.Fatal("DecodeWithLand() error = nil, want non-nil")
	}

	const want = "reading gzip header"

	if !strings.Contains(err.Error(), want) {
		t.Errorf("DecodeWithLand() error = %q, want to contain %q", err.Error(), want)
	}
}

// TestDecodeWithLandLandErrors checks that a bad land reader, behind a good
// outlines reader, is reported with "land" in the message and still
// satisfies errors.Is against the right sentinel: one case that never gets
// past gzip, one that does and fails on the packed format itself.
func TestDecodeWithLandLandErrors(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		land      io.Reader
		wantErrIs error
	}{
		{name: "not a gzip stream at all", land: strings.NewReader("not a gzip stream")},
		{
			name:      "a gzip stream with the wrong magic",
			land:      bytes.NewReader(gzipBytes(t, []byte("XXXX"))),
			wantErrIs: shore.ErrMagic,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var outlineBuf bytes.Buffer
			if err := shore.Encode(&outlineBuf, nil); err != nil {
				t.Fatalf("Encode() error = %v, want nil", err)
			}

			_, err := shore.DecodeWithLand(&outlineBuf, testCase.land)
			if err == nil {
				t.Fatal("DecodeWithLand() error = nil, want non-nil")
			}

			if testCase.wantErrIs != nil && !errors.Is(err, testCase.wantErrIs) {
				t.Errorf("DecodeWithLand() error = %v, want it to wrap %v", err, testCase.wantErrIs)
			}

			const want = "land"

			if !strings.Contains(err.Error(), want) {
				t.Errorf("DecodeWithLand() error = %q, want to contain %q", err.Error(), want)
			}
		})
	}
}

// TestLoadHasLandNearTheNetherlands checks the real, embedded land file
// against a spot that has to be dry land: the cell around 52N 4E, well
// inland of the Dutch coast. Load is cached, so this costs nothing beyond
// the first shore test that also calls it.
func TestLoadHasLandNearTheNetherlands(t *testing.T) {
	t.Parallel()

	set, err := shore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	visited := 0

	set.LandWithin(51.9, 52.1, 3.9, 4.1, func(line shore.Polyline) {
		visited++

		if len(line) < 3 {
			t.Errorf("LandWithin() visited a ring of %d points, want at least 3", len(line))
		}
	})

	if visited == 0 {
		t.Error("LandWithin() around 52N 4E visited nothing, want at least one land ring")
	}
}
