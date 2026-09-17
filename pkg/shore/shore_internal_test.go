package shore

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
)

// errCompressWriteFailed is the sentinel a failing io.Writer in these tests
// returns, declared once at package level so err113 has a static error to
// wrap instead of a fresh errors.New at the point of use.
var errCompressWriteFailed = errors.New("shore internal test: write failed")

// failingWriter lets the gzip header through, since gzip.Writer sends that
// on every call to Write regardless of body size, and fails every write
// after that with errCompressWriteFailed. A small body only reaches the
// underlying writer a second time when Close flushes flate's buffered data,
// so it fails there; a body past flate's internal buffer (around 64 KB)
// flushes mid-Write instead, so it fails there. One fake writer exercises
// both of compress's error branches this way.
type failingWriter struct {
	calls int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		return len(p), nil
	}

	return 0, errCompressWriteFailed
}

// uvarints appends each value to dst as an unsigned varint, in order. Tests
// below use it to hand-build a decodeBody input, since the point of most of
// these cases is bytes the real encoder would never produce.
func uvarints(dst []byte, values ...uint64) []byte {
	for _, value := range values {
		dst = binary.AppendUvarint(dst, value)
	}

	return dst
}

func TestCellRow(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		lat  float64
		want int
	}{
		{name: "equator", lat: 0, want: 18},
		{name: "south pole", lat: -90, want: 0},
		{name: "north pole", lat: 90, want: cellRows - 1},
		{name: "just south of the north pole", lat: 89, want: cellRows - 1},
		{name: "past the north pole", lat: 200, want: cellRows - 1},
		{name: "past the south pole", lat: -200, want: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := cellRow(testCase.lat); got != testCase.want {
				t.Errorf("cellRow(%v) = %v, want %v", testCase.lat, got, testCase.want)
			}
		})
	}
}

func TestColumnIndex(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		lon  float64
		want int
	}{
		{name: "prime meridian", lon: 0, want: 36},
		{name: "west edge of the grid", lon: -180, want: 0},
		{name: "east edge of the grid", lon: 175, want: cellCols - 1},
		{name: "exactly at the dateline", lon: 180, want: cellCols},
		{name: "past three worlds east, clamped", lon: 10000, want: 144},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := columnIndex(testCase.lon); got != testCase.want {
				t.Errorf("columnIndex(%v) = %v, want %v", testCase.lon, got, testCase.want)
			}
		})
	}
}

func TestWrapColumn(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		col  int
		want int
	}{
		{name: "already in range", col: 0, want: 0},
		{name: "one past the last column", col: cellCols, want: 0},
		{name: "one before the first column", col: -1, want: cellCols - 1},
		{name: "two worlds past the last column", col: cellCols*2 + 3, want: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := wrapColumn(testCase.col); got != testCase.want {
				t.Errorf("wrapColumn(%v) = %v, want %v", testCase.col, got, testCase.want)
			}
		})
	}
}

func TestCellOf(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		point Point
		want  int
	}{
		{name: "origin", point: Point{Lat: 0, Lon: 0}, want: 18*cellCols + 36},
		{
			name:  "a longitude past the dateline wraps back onto the grid",
			point: Point{Lat: 0, Lon: 182},
			want:  18*cellCols + 0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := cellOf(testCase.point); got != testCase.want {
				t.Errorf("cellOf(%v) = %v, want %v", testCase.point, got, testCase.want)
			}
		})
	}
}

func TestClampLat(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		lat  float64
		want float64
	}{
		{name: "within range", lat: 12.5, want: 12.5},
		{name: "past the north pole", lat: 500, want: 90},
		{name: "past the south pole", lat: -500, want: -90},
		{name: "NaN folds onto the equator", lat: math.NaN(), want: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := clampLat(testCase.lat); got != testCase.want {
				t.Errorf("clampLat(%v) = %v, want %v", testCase.lat, got, testCase.want)
			}
		})
	}
}

func TestClampLon(t *testing.T) {
	t.Parallel()

	const threeWorlds = 540

	for _, testCase := range []struct {
		name string
		lon  float64
		want float64
	}{
		{name: "within range", lon: -12.5, want: -12.5},
		{name: "past three worlds east", lon: 10000, want: threeWorlds},
		{name: "past three worlds west", lon: -10000, want: -threeWorlds},
		{name: "NaN folds onto the prime meridian", lon: math.NaN(), want: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := clampLon(testCase.lon); got != testCase.want {
				t.Errorf("clampLon(%v) = %v, want %v", testCase.lon, got, testCase.want)
			}
		})
	}
}

func TestScale(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		point   Point
		wantLat int64
		wantLon int64
	}{
		{name: "origin", point: Point{Lat: 0, Lon: 0}, wantLat: 0, wantLon: 0},
		{
			name:    "rounds to the nearest unit",
			point:   Point{Lat: 1.234567, Lon: -2.345678},
			wantLat: 123457,
			wantLon: -234568,
		},
		{
			name:    "an out of range latitude is clamped before scaling",
			point:   Point{Lat: 1000, Lon: 0},
			wantLat: 90 * unit,
			wantLon: 0,
		},
		{
			name:    "a NaN longitude folds onto the prime meridian before scaling",
			point:   Point{Lat: 0, Lon: math.NaN()},
			wantLat: 0,
			wantLon: 0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotLat, gotLon := scale(testCase.point)
			if gotLat != testCase.wantLat || gotLon != testCase.wantLon {
				t.Errorf("scale(%v) = (%v, %v), want (%v, %v)",
					testCase.point, gotLat, gotLon, testCase.wantLat, testCase.wantLon)
			}
		})
	}
}

// TestSplitTooShort checks that a line with fewer than two points leaves the
// bucket map untouched, since there is nothing that could be called a line.
func TestSplitTooShort(t *testing.T) {
	t.Parallel()

	buckets := make(map[int][]Polyline)
	split(buckets, Polyline{{Lat: 1, Lon: 1}})

	if len(buckets) != 0 {
		t.Errorf("split() on a one-point line touched %d buckets, want 0", len(buckets))
	}
}

// TestSplitSingleCell checks that a line whose points never leave one cell
// comes back as a single, unmodified piece.
func TestSplitSingleCell(t *testing.T) {
	t.Parallel()

	line := Polyline{{Lat: 20, Lon: 20}, {Lat: 20, Lon: 21}, {Lat: 20, Lon: 22}}
	buckets := make(map[int][]Polyline)
	split(buckets, line)

	key := cellOf(line[0])

	if len(buckets) != 1 {
		t.Fatalf("split() produced %d buckets, want 1", len(buckets))
	}

	pieces, ok := buckets[key]
	if !ok || len(pieces) != 1 {
		t.Fatalf("split() buckets[%v] = %v, want one piece", key, buckets[key])
	}

	if !slicesEqualPoints(pieces[0], line) {
		t.Errorf("split() piece = %v, want %v unchanged", pieces[0], line)
	}
}

// TestSplitCrossesBoundary checks the cell-splitting rule the doc comment on
// split promises: the piece left behind ends with the first point of the
// next cell, and the piece that opens the next cell starts with the last
// point of the one before it, so the crossing segment is never a gap.
func TestSplitCrossesBoundary(t *testing.T) {
	t.Parallel()

	first := Point{Lat: 0, Lon: 1}
	second := Point{Lat: 0, Lon: 4}
	third := Point{Lat: 0, Lon: 6}

	buckets := make(map[int][]Polyline)
	split(buckets, Polyline{first, second, third})

	oldKey := cellOf(first)
	newKey := cellOf(third)

	if oldKey == newKey {
		t.Fatalf("fixture does not cross a cell boundary: both ends are in cell %v", oldKey)
	}

	wantOld := Polyline{first, second, third}
	wantNew := Polyline{second, third}

	if !slicesEqualPoints(buckets[oldKey][0], wantOld) {
		t.Errorf("split() buckets[%v][0] = %v, want %v", oldKey, buckets[oldKey], wantOld)
	}

	if !slicesEqualPoints(buckets[newKey][0], wantNew) {
		t.Errorf("split() buckets[%v][0] = %v, want %v", newKey, buckets[newKey], wantNew)
	}
}

// TestBucket checks that every line handed to bucket is split independently
// and that two lines landing in the same cell are both kept, in order,
// rather than one overwriting the other.
func TestBucket(t *testing.T) {
	t.Parallel()

	lineA := Polyline{{Lat: 20, Lon: 20}, {Lat: 20, Lon: 21}}
	lineB := Polyline{{Lat: 20, Lon: 20.5}, {Lat: 20, Lon: 20.8}}
	lineC := Polyline{{Lat: -40, Lon: -39.5}, {Lat: -40, Lon: -39.8}}

	buckets := bucket([]Polyline{lineA, lineB, lineC})

	sharedKey := cellOf(lineA[0])
	otherKey := cellOf(lineC[0])

	if got := len(buckets[sharedKey]); got != 2 {
		t.Errorf("bucket() buckets[%v] has %d pieces, want 2", sharedKey, got)
	}

	if got := len(buckets[otherKey]); got != 1 {
		t.Errorf("bucket() buckets[%v] has %d pieces, want 1", otherKey, got)
	}
}

// slicesEqualPoints reports whether two polylines hold exactly the same
// points in the same order.
func slicesEqualPoints(got, want Polyline) bool {
	if len(got) != len(want) {
		return false
	}

	for index, point := range got {
		if point != want[index] {
			return false
		}
	}

	return true
}

// TestCompressWriteError drives a body past flate's internal buffer through
// failingWriter, which is what makes the flush happen mid-Write and so makes
// compress's own Write branch, rather than Close's, see the failure. The body
// has to be incompressible for its size to be what forces the flush: a run of
// zeros this size still fits in flate's buffer with room to spare.
func TestCompressWriteError(t *testing.T) {
	t.Parallel()

	const pastFlateBuffer = 150000

	body := make([]byte, pastFlateBuffer)
	if _, err := rand.Read(body); err != nil {
		t.Fatalf("rand.Read() error = %v, want nil", err)
	}

	err := compress(&failingWriter{}, body)
	if !errors.Is(err, errCompressWriteFailed) {
		t.Errorf("compress() error = %v, want it to wrap %v", err, errCompressWriteFailed)
	}

	const want = "compressing"

	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("compress() error = %v, want it to contain %q", err, want)
	}
}

// TestCompressCloseError drives a body small enough that flate buffers all
// of it internally through failingWriter, so the failure only surfaces when
// Close flushes the buffered bytes.
func TestCompressCloseError(t *testing.T) {
	t.Parallel()

	err := compress(&failingWriter{}, []byte("a few bytes"))
	if !errors.Is(err, errCompressWriteFailed) {
		t.Errorf("compress() error = %v, want it to wrap %v", err, errCompressWriteFailed)
	}

	const want = "finishing gzip"

	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("compress() error = %v, want it to contain %q", err, want)
	}
}

// TestDecodeBodyErrors drives decodeBody directly with hand-built bytes,
// bypassing gzip entirely, since every one of these is a format error decode
// itself never gets far enough to see through a real file.
func TestDecodeBodyErrors(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		body    []byte
		wantErr error
	}{
		{
			name:    "wrong magic",
			body:    []byte("XXXX"),
			wantErr: ErrMagic,
		},
		{
			name:    "body shorter than the magic",
			body:    []byte("US"),
			wantErr: ErrMagic,
		},
		{
			name:    "unknown version",
			body:    uvarints([]byte(magic), 2),
			wantErr: ErrVersion,
		},
		{
			name:    "cell count larger than the bytes left",
			body:    uvarints([]byte(magic), version, 10),
			wantErr: ErrCount,
		},
		{
			name:    "cell count past maxCells",
			body:    uvarints([]byte(magic), version, maxCells+1),
			wantErr: ErrCount,
		},
		{
			// row 99, column 0, plus one filler byte so the cell count's own
			// byte-floor check passes before cellKey ever runs.
			name:    "row outside the grid",
			body:    uvarints([]byte(magic), version, 1, 99, 0, 0),
			wantErr: ErrCell,
		},
		{
			name:    "column outside the grid",
			body:    uvarints([]byte(magic), version, 1, 0, 99, 0),
			wantErr: ErrCell,
		},
		{
			name:    "polyline count larger than the bytes left",
			body:    uvarints([]byte(magic), version, 1, 0, 0, 5),
			wantErr: ErrCount,
		},
		{
			name:    "point count larger than the bytes left",
			body:    uvarints([]byte(magic), version, 1, 0, 0, 1, 5, 0, 0),
			wantErr: ErrCount,
		},
		{
			name:    "polyline declaring fewer than two points",
			body:    uvarints([]byte(magic), version, 1, 0, 0, 1, 1, 0, 0),
			wantErr: ErrCount,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeBody(testCase.body)
			if !errors.Is(err, testCase.wantErr) {
				t.Errorf("decodeBody(%v) error = %v, want %v", testCase.body, err, testCase.wantErr)
			}
		})
	}
}

// TestDecodeBodyTruncated covers the ways a varint itself can be malformed,
// split out from TestDecodeBodyErrors's count and range checks so neither
// function runs long.
func TestDecodeBodyTruncated(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		body []byte
	}{
		{
			// Two points declared, but the second point's latitude delta
			// never terminates before the buffer runs out.
			name: "a varint runs off the end",
			body: append(
				uvarints([]byte(magic), version, 1, 0, 0, 1, 2),
				0x00, 0x00, 0x80, 0x80,
			),
		},
		{
			// Eleven continuation bytes: no varint for a uint64 can be more
			// than ten bytes wide, so this overflows rather than merely
			// running out of buffer.
			name: "a varint wider than 64 bits",
			body: append([]byte(magic), bytes.Repeat([]byte{0x80}, 11)...),
		},
		{
			// The cell count's own varint never terminates, which is a
			// different line from a count declaring more than the bytes
			// left: here there is no count to compare against yet.
			name: "the count itself runs off the end",
			body: append(uvarints([]byte(magic), version), 0x80),
		},
		{
			// Three bytes satisfy the outer cell count's byte-floor check,
			// but the row varint they hold never terminates.
			name: "a cell's row runs off the end",
			body: append(uvarints([]byte(magic), version, 1), 0x80, 0x80, 0x80),
		},
		{
			// The row reads fine; the column that follows it does not.
			name: "a cell's column runs off the end",
			body: append(uvarints([]byte(magic), version, 1), 0x00, 0x80, 0x80),
		},
		{
			// The first point's latitude delta reads fine; its longitude
			// delta runs out of buffer instead.
			name: "a point's longitude delta runs off the end",
			body: append(
				uvarints([]byte(magic), version, 1, 0, 0, 1, 2),
				0x00, 0x80, 0x80, 0x80,
			),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeBody(testCase.body)
			if !errors.Is(err, ErrTruncated) {
				t.Errorf("decodeBody(%v) error = %v, want ErrTruncated", testCase.body, err)
			}
		})
	}
}

// TestDecodeBodySuccess builds one small, valid body by hand and checks that
// decodeBody resolves it into the Set the bytes describe, which is what
// proves finish's span bookkeeping lines up with what cell and polyline
// wrote.
func TestDecodeBodySuccess(t *testing.T) {
	t.Parallel()

	const (
		row          = 5
		column       = 10
		firstLatStep = 500000
		firstLonStep = 1000000
		nextLatStep  = 100000
		nextLonStep  = -50000
	)

	body := []byte(magic)
	body = binary.AppendUvarint(body, version)
	body = binary.AppendUvarint(body, 1)
	body = binary.AppendUvarint(body, row)
	body = binary.AppendUvarint(body, column)
	body = binary.AppendUvarint(body, 1)
	body = binary.AppendUvarint(body, 2)
	body = binary.AppendVarint(body, firstLatStep)
	body = binary.AppendVarint(body, firstLonStep)
	body = binary.AppendVarint(body, nextLatStep)
	body = binary.AppendVarint(body, nextLonStep)

	cells, err := decodeBody(body)
	if err != nil {
		t.Fatalf("decodeBody() error = %v, want nil", err)
	}

	key := row*cellCols + column

	lines, ok := cells[key]
	if !ok || len(lines) != 1 || len(lines[0]) != 2 {
		t.Fatalf("decodeBody() cells[%v] = %v, want one polyline of two points", key, cells[key])
	}

	want := Polyline{{Lat: 5, Lon: 10}, {Lat: 6, Lon: 9.5}}
	if !slicesEqualPoints(lines[0], want) {
		t.Errorf("decodeBody() cells[%v][0] = %v, want %v", key, lines[0], want)
	}
}

func TestBadBox(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name                           string
		latMin, latMax, lonMin, lonMax float64
		want                           bool
	}{
		{name: "a normal box", latMin: -10, latMax: 10, lonMin: -10, lonMax: 10, want: false},
		{name: "minimum past maximum latitude", latMin: 10, latMax: -10, lonMin: -10, lonMax: 10, want: true},
		{name: "minimum past maximum longitude", latMin: -10, latMax: 10, lonMin: 10, lonMax: -10, want: true},
		{name: "a NaN latitude", latMin: math.NaN(), latMax: 10, lonMin: -10, lonMax: 10, want: true},
		{name: "a NaN longitude", latMin: -10, latMax: 10, lonMin: -10, lonMax: math.NaN(), want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := badBox(testCase.latMin, testCase.latMax, testCase.lonMin, testCase.lonMax)
			if got != testCase.want {
				t.Errorf("badBox(%v, %v, %v, %v) = %v, want %v",
					testCase.latMin, testCase.latMax, testCase.lonMin, testCase.lonMax, got, testCase.want)
			}
		})
	}
}

// TestCacheLoad checks the replay path: sync.Once only ever runs Decode
// once, so a second call on a cache seeded with damaged data has to hand
// back the exact error the first call already stored, the same way
// pkg/fonts's face.load does for its own cache.
func TestCacheLoad(t *testing.T) {
	t.Parallel()

	badCache := &cache{data: []byte("not a gzip stream")}

	firstSet, firstErr := badCache.load()
	if firstErr == nil {
		t.Fatal("load() error = nil, want non-nil")
	}

	if firstSet != nil {
		t.Errorf("load() set = %v, want nil", firstSet)
	}

	secondSet, secondErr := badCache.load()
	if secondSet != nil {
		t.Errorf("second load() set = %v, want nil", secondSet)
	}

	if !errors.Is(secondErr, firstErr) {
		t.Errorf("second load() error = %v, want the same error as the first call (%v)", secondErr, firstErr)
	}
}

func TestAreaKm2(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		ring Polyline
		want float64
	}{
		{
			name: "too few points to enclose anything",
			ring: Polyline{{Lat: 0, Lon: 0}, {Lat: 0, Lon: 1}},
			want: 0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := areaKm2(testCase.ring); got != testCase.want {
				t.Errorf("areaKm2(%v) = %v, want %v", testCase.ring, got, testCase.want)
			}
		})
	}

	t.Run("a square near the equator is about its planar area", func(t *testing.T) {
		t.Parallel()

		// A tenth of a degree on a side, near the equator where the cosine
		// squeeze is close to one: about 111.32 km to a degree of latitude
		// gives a square with sides near 11.1 km, so the area should land
		// close to 124 km2.
		ring := Polyline{
			{Lat: 0, Lon: 0}, {Lat: 0, Lon: 0.1}, {Lat: 0.1, Lon: 0.1}, {Lat: 0.1, Lon: 0}, {Lat: 0, Lon: 0},
		}

		const (
			wantLow  = 120.0
			wantHigh = 128.0
		)

		got := areaKm2(ring)
		if got < wantLow || got > wantHigh {
			t.Errorf("areaKm2(%v) = %v, want between %v and %v", ring, got, wantLow, wantHigh)
		}
	})
}

func TestSimplify(t *testing.T) {
	t.Parallel()

	straight := Polyline{{Lat: 0, Lon: 0}, {Lat: 0, Lon: 1}, {Lat: 0, Lon: 2}, {Lat: 0, Lon: 3}}
	corner := Polyline{{Lat: 0, Lon: 0}, {Lat: 5, Lon: 0}, {Lat: 5, Lon: 5}}
	short := Polyline{{Lat: 0, Lon: 0}, {Lat: 1, Lon: 1}}

	const tolerance = 100.0

	for _, testCase := range []struct {
		name      string
		line      Polyline
		tolerance float64
		want      int
	}{
		{name: "a straight line loses its middle points", line: straight, tolerance: tolerance, want: 2},
		{name: "a real corner is kept", line: corner, tolerance: tolerance, want: 3},
		{name: "zero tolerance keeps everything", line: straight, tolerance: 0, want: len(straight)},
		{name: "negative tolerance keeps everything", line: straight, tolerance: -1, want: len(straight)},
		{name: "fewer than three points is returned untouched", line: short, tolerance: tolerance, want: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := len(simplify(testCase.line, testCase.tolerance)); got != testCase.want {
				t.Errorf("len(simplify(%v, %v)) = %v, want %v", testCase.line, testCase.tolerance, got, testCase.want)
			}
		})
	}
}

func TestFarthest(t *testing.T) {
	t.Parallel()

	t.Run("a zero-length chord falls back to plain distance", func(t *testing.T) {
		t.Parallel()

		// First and last share a point, so the chord has no length and
		// farthest must fall back to Hypot instead of dividing by span.
		line := Polyline{{Lat: 0, Lon: 0}, {Lat: 3, Lon: 4}, {Lat: 0, Lon: 0}}

		const wantIndex = 1

		wantDistance := 5.0

		gotIndex, gotDistance := farthest(line, 0, 2)
		if gotIndex != wantIndex || gotDistance != wantDistance {
			t.Errorf("farthest(%v, 0, 2) = (%v, %v), want (%v, %v)",
				line, gotIndex, gotDistance, wantIndex, wantDistance)
		}
	})

	t.Run("the farthest of several interior points wins", func(t *testing.T) {
		t.Parallel()

		line := Polyline{{Lat: 0, Lon: 0}, {Lat: 1, Lon: 5}, {Lat: 2, Lon: 5}, {Lat: 0, Lon: 10}}

		const wantIndex = 2

		gotIndex, gotDistance := farthest(line, 0, 3)
		if gotIndex != wantIndex {
			t.Errorf("farthest(%v, 0, 3) index = %v, want %v", line, gotIndex, wantIndex)
		}

		if gotDistance <= 0 {
			t.Errorf("farthest(%v, 0, 3) distance = %v, want > 0", line, gotDistance)
		}
	})
}

func TestKept(t *testing.T) {
	t.Parallel()

	line := Polyline{{Lat: 0, Lon: 0}, {Lat: 1, Lon: 1}, {Lat: 2, Lon: 2}}
	mask := []bool{true, false, true}

	want := Polyline{{Lat: 0, Lon: 0}, {Lat: 2, Lon: 2}}

	if got := kept(line, mask); !slicesEqualPoints(got, want) {
		t.Errorf("kept(%v, %v) = %v, want %v", line, mask, got, want)
	}
}

// TestPolyline checks the coordinate filter GeoJSON cannot exercise on its
// own: JSON has no way to spell NaN or an infinity that still unmarshals
// successfully, so the only way to reach this branch is to call polyline
// directly with values built in Go.
func TestPolyline(t *testing.T) {
	t.Parallel()

	coords := [][]float64{
		{4, 52},
		{1},
		{math.NaN(), 52},
		{4, math.Inf(1)},
		{5, 53},
	}

	want := Polyline{{Lat: 52, Lon: 4}, {Lat: 53, Lon: 5}}

	if got := polyline(coords); !slicesEqualPoints(got, want) {
		t.Errorf("polyline(%v) = %v, want %v", coords, got, want)
	}
}

func TestFinite(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value float64
		want  bool
	}{
		{name: "zero", value: 0, want: true},
		{name: "an ordinary number", value: 12.5, want: true},
		{name: "NaN", value: math.NaN(), want: false},
		{name: "positive infinity", value: math.Inf(1), want: false},
		{name: "negative infinity", value: math.Inf(-1), want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := finite(testCase.value); got != testCase.want {
				t.Errorf("finite(%v) = %v, want %v", testCase.value, got, testCase.want)
			}
		})
	}
}

// relativeAreaTolerance is how far a sum of clipped areas may drift from the
// original ring's area before a test calls it wrong. The arithmetic is plain
// float64 shoelace on both sides, so anything closer than this is rounding.
const relativeAreaTolerance = 1e-9

// shoelaceArea is the planar shoelace formula over a closed ring, in degrees
// squared. It is deliberately independent of areaKm2: that function squeezes
// longitude by a latitude cosine for a real-world estimate, and the land
// tests below want the plain planar figure clip's arithmetic itself works in.
func shoelaceArea(ring Polyline) float64 {
	var twice float64

	for index, point := range ring {
		next := ring[(index+1)%len(ring)]
		twice += point.Lon*next.Lat - next.Lon*point.Lat
	}

	return math.Abs(twice) / 2
}

// areaClose reports whether got is within relativeAreaTolerance of want.
func areaClose(got, want float64) bool {
	return math.Abs(got-want) <= math.Abs(want)*relativeAreaTolerance
}

func TestBoxOf(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		row, col int
		want     cellBox
	}{
		{
			name: "the cell straddling the equator and the prime meridian",
			row:  18, col: 36,
			want: cellBox{latMin: 0, latMax: 5, lonMin: 0, lonMax: 5},
		},
		{
			name: "the south-west corner of the grid",
			row:  0, col: 0,
			want: cellBox{latMin: -90, latMax: -85, lonMin: -180, lonMax: -175},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := boxOf(testCase.row, testCase.col); got != testCase.want {
				t.Errorf("boxOf(%v, %v) = %v, want %v", testCase.row, testCase.col, got, testCase.want)
			}
		})
	}
}

// TestCellBoxSides checks the four half-planes come out in the fixed order
// clip relies on and each carries the right limit, axis and side.
func TestCellBoxSides(t *testing.T) {
	t.Parallel()

	box := cellBox{latMin: 10, latMax: 15, lonMin: 20, lonMax: 25}

	want := [4]halfPlane{
		{limit: 20, lat: false, upper: false},
		{limit: 25, lat: false, upper: true},
		{limit: 10, lat: true, upper: false},
		{limit: 15, lat: true, upper: true},
	}

	if got := box.sides(); got != want {
		t.Errorf("cellBox.sides() = %v, want %v", got, want)
	}
}

func TestHalfPlaneValue(t *testing.T) {
	t.Parallel()

	point := Point{Lat: 12, Lon: 34}

	for _, testCase := range []struct {
		name string
		lat  bool
		want float64
	}{
		{name: "a latitude plane measures latitude", lat: true, want: 12},
		{name: "a longitude plane measures longitude", lat: false, want: 34},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plane := halfPlane{lat: testCase.lat}
			if got := plane.value(point); got != testCase.want {
				t.Errorf("value(%v) = %v, want %v", point, got, testCase.want)
			}
		})
	}
}

func TestHalfPlaneInside(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		plane halfPlane
		point Point
		want  bool
	}{
		{
			name:  "an upper plane keeps a point at the limit",
			plane: halfPlane{limit: 10, upper: true},
			point: Point{Lon: 10}, want: true,
		},
		{
			name:  "an upper plane rejects a point past the limit",
			plane: halfPlane{limit: 10, upper: true},
			point: Point{Lon: 11}, want: false,
		},
		{
			name:  "a lower plane keeps a point at the limit",
			plane: halfPlane{limit: 10, upper: false},
			point: Point{Lon: 10}, want: true,
		},
		{
			name:  "a lower plane rejects a point short of the limit",
			plane: halfPlane{limit: 10, upper: false},
			point: Point{Lon: 9}, want: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.plane.inside(testCase.point); got != testCase.want {
				t.Errorf("inside(%v) = %v, want %v", testCase.point, got, testCase.want)
			}
		})
	}
}

// TestHalfPlaneCross covers cross on both axes it measures and both sides a
// cell keeps, which between them are the only shapes of call clip ever makes:
// a segment with one end above the limit and one below it.
func TestHalfPlaneCross(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		plane    halfPlane
		from, to Point
		want     Point
	}{
		{
			name:  "crossing a lower latitude plane",
			plane: halfPlane{limit: 0, lat: true, upper: false},
			from:  Point{Lat: -2, Lon: 0}, to: Point{Lat: 2, Lon: 4},
			want: Point{Lat: 0, Lon: 2},
		},
		{
			name:  "crossing an upper latitude plane",
			plane: halfPlane{limit: 0, lat: true, upper: true},
			from:  Point{Lat: 2, Lon: 4}, to: Point{Lat: -2, Lon: 0},
			want: Point{Lat: 0, Lon: 2},
		},
		{
			name:  "crossing a lower longitude plane",
			plane: halfPlane{limit: 0, lat: false, upper: false},
			from:  Point{Lat: 0, Lon: -2}, to: Point{Lat: 4, Lon: 2},
			want: Point{Lat: 2, Lon: 0},
		},
		{
			name:  "crossing an upper longitude plane",
			plane: halfPlane{limit: 0, lat: false, upper: true},
			from:  Point{Lat: 4, Lon: 2}, to: Point{Lat: 0, Lon: -2},
			want: Point{Lat: 2, Lon: 0},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.plane.cross(testCase.from, testCase.to); got != testCase.want {
				t.Errorf("cross(%v, %v) = %v, want %v", testCase.from, testCase.to, got, testCase.want)
			}
		})
	}
}

// TestClipHalf checks one Sutherland-Hodgman pass directly: a rectangle that
// crosses the plane comes back with the far corners replaced by the two
// crossing points, in the same walk order the ring was given in.
func TestClipHalf(t *testing.T) {
	t.Parallel()

	side := halfPlane{limit: 5, lat: false, upper: true}
	ring := Polyline{{Lat: 0, Lon: 3}, {Lat: 0, Lon: 7}, {Lat: 2, Lon: 7}, {Lat: 2, Lon: 3}}
	want := Polyline{{Lat: 0, Lon: 3}, {Lat: 0, Lon: 5}, {Lat: 2, Lon: 5}, {Lat: 2, Lon: 3}}

	if got := clipHalf(nil, ring, side); !slicesEqualPoints(got, want) {
		t.Errorf("clipHalf(%v, %v) = %v, want %v", ring, side, got, want)
	}
}

// TestClipHalfEmptySource checks the one case clip itself never reaches: clip
// always returns as soon as a pass leaves c.from empty, so the only way to
// hand clipHalf zero points is to call it directly.
func TestClipHalfEmptySource(t *testing.T) {
	t.Parallel()

	if got := clipHalf(nil, nil, halfPlane{}); got != nil {
		t.Errorf("clipHalf(nil, nil, ...) = %v, want nil", got)
	}
}

// TestClipperClip checks the properties Sutherland-Hodgman promises for a
// single cell: a ring wholly inside comes back untouched, a ring wholly
// outside comes back as nothing, and a ring that contains the whole cell
// comes back as exactly the cell's rectangle.
func TestClipperClip(t *testing.T) {
	t.Parallel()

	box := boxOf(18, 36)

	t.Run("wholly inside the cell comes back unchanged", func(t *testing.T) {
		t.Parallel()

		ring := Polyline{{Lat: 1, Lon: 1}, {Lat: 1, Lon: 2}, {Lat: 2, Lon: 2}, {Lat: 2, Lon: 1}, {Lat: 1, Lon: 1}}

		got := (&clipper{}).clip(ring, box)
		if !slicesEqualPoints(got, ring) {
			t.Errorf("clip() = %v, want %v unchanged", got, ring)
		}
	})

	t.Run("wholly outside the cell comes back as nothing", func(t *testing.T) {
		t.Parallel()

		ring := Polyline{{Lat: 20, Lon: 20}, {Lat: 20, Lon: 21}, {Lat: 21, Lon: 21}, {Lat: 20, Lon: 20}}

		if got := (&clipper{}).clip(ring, box); got != nil {
			t.Errorf("clip() = %v, want nil", got)
		}
	})

	t.Run("surviving points below minRingPoints come back as nothing", func(t *testing.T) {
		t.Parallel()

		// Both points sit inside the box on every one of the four planes, so
		// none of the four passes empties c.from early: it is the length
		// check after the loop, not the one inside it, that has to catch a
		// ring too short to enclose anything.
		ring := Polyline{{Lat: 1, Lon: 1}, {Lat: 1, Lon: 2}}

		if got := (&clipper{}).clip(ring, box); got != nil {
			t.Errorf("clip() = %v, want nil for a ring left with only %d points", got, len(ring))
		}
	})

	t.Run("containing the whole cell comes back as its rectangle", func(t *testing.T) {
		t.Parallel()

		ring := Polyline{
			{Lat: -10, Lon: -10}, {Lat: -10, Lon: 20}, {Lat: 20, Lon: 20}, {Lat: 20, Lon: -10}, {Lat: -10, Lon: -10},
		}

		const wantArea = 25.0

		got := (&clipper{}).clip(ring, box)
		if len(got) != 4 {
			t.Fatalf("clip() = %v, want 4 points", got)
		}

		if area := shoelaceArea(got); !areaClose(area, wantArea) {
			t.Errorf("clip() area = %v, want %v", area, wantArea)
		}
	})
}

// TestClipperClipConcaveRing checks that a concave ring straddling a cell
// boundary still encloses the right area once clipped, even though the
// algorithm may hand back a zero-width bridge along the boundary rather than
// a tidy rectangle: see the note at the top of land.go on why that costs an
// even-odd fill nothing. The ring is an L-shape whose notch sits entirely
// inside the cell and whose outer arm runs past its eastern edge, so only
// that arm is cut.
func TestClipperClipConcaveRing(t *testing.T) {
	t.Parallel()

	ring := Polyline{
		{Lat: 0, Lon: 3}, {Lat: 0, Lon: 7}, {Lat: 2, Lon: 7},
		{Lat: 2, Lon: 5}, {Lat: 3, Lon: 5}, {Lat: 3, Lon: 3},
		{Lat: 0, Lon: 3},
	}

	const wantArea = 6.0

	got := (&clipper{}).clip(ring, boxOf(18, 36))
	if len(got) < minRingPoints {
		t.Fatalf("clip() = %v, want at least %d points", got, minRingPoints)
	}

	if area := shoelaceArea(got); !areaClose(area, wantArea) {
		t.Errorf("clip() area = %v, want %v", area, wantArea)
	}
}

// TestBucketRingsAcrossCellBoundaries covers the three ways a ring can
// straddle the grid: across a longitude boundary, across a latitude one, and
// across the corner where four cells meet. In every case the ring is filed
// under every cell it touches, each piece is a closed ring of at least three
// points, and the pieces' areas sum back to the original ring's area: the two
// neighbouring cells compute the same crossing from the same arithmetic, so
// nothing is lost and nothing is doubled at the seam.
func TestBucketRingsAcrossCellBoundaries(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		ring      Polyline
		wantCells int
	}{
		{
			name: "straddling a longitude boundary",
			ring: Polyline{
				{Lat: 1, Lon: 3}, {Lat: 1, Lon: 7}, {Lat: 3, Lon: 7}, {Lat: 3, Lon: 3}, {Lat: 1, Lon: 3},
			},
			wantCells: 2,
		},
		{
			name: "straddling a latitude boundary",
			ring: Polyline{
				{Lat: 3, Lon: 1}, {Lat: 7, Lon: 1}, {Lat: 7, Lon: 3}, {Lat: 3, Lon: 3}, {Lat: 3, Lon: 1},
			},
			wantCells: 2,
		},
		{
			name: "straddling the corner where four cells meet",
			ring: Polyline{
				{Lat: 3, Lon: 3}, {Lat: 3, Lon: 7}, {Lat: 7, Lon: 7}, {Lat: 7, Lon: 3}, {Lat: 3, Lon: 3},
			},
			wantCells: 4,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			buckets := bucketRings([]Polyline{testCase.ring})
			if len(buckets) != testCase.wantCells {
				t.Fatalf("bucketRings() filed under %d cells, want %d", len(buckets), testCase.wantCells)
			}

			want := shoelaceArea(testCase.ring)

			var sum float64

			for _, pieces := range buckets {
				for _, piece := range pieces {
					if len(piece) < minRingPoints {
						t.Errorf("piece %v has %d points, want at least %d", piece, len(piece), minRingPoints)
					}

					sum += shoelaceArea(piece)
				}
			}

			if !areaClose(sum, want) {
				t.Errorf("sum of piece areas = %v, want %v", sum, want)
			}
		})
	}
}

// TestFileRingTooShort mirrors TestSplitTooShort for the ring side of the
// codec: a ring with fewer than three points cannot enclose anything, so it
// is dropped before it ever reaches the clipper.
func TestFileRingTooShort(t *testing.T) {
	t.Parallel()

	buckets := make(map[int][]Polyline)
	fileRing(buckets, &clipper{}, Polyline{{Lat: 1, Lon: 1}, {Lat: 2, Lon: 2}})

	if len(buckets) != 0 {
		t.Errorf("fileRing() touched %d buckets for a two-point ring, want 0", len(buckets))
	}
}

// TestFileRingSkipsCellsTheRingMisses checks the other half of bucketRings's
// doc comment: a cell inside the ring's bounding box that the ring itself
// never reaches comes back empty from the clipper and is never filed. A
// right triangle is the simplest shape whose bounding box has a corner its
// own hypotenuse cuts away, here three of the nine cells the 3x3 box covers.
func TestFileRingSkipsCellsTheRingMisses(t *testing.T) {
	t.Parallel()

	ring := Polyline{{Lat: 0, Lon: 0}, {Lat: 0, Lon: 10}, {Lat: 10, Lon: 0}, {Lat: 0, Lon: 0}}

	const wantCells = 6

	buckets := bucketRings([]Polyline{ring})
	if len(buckets) != wantCells {
		t.Errorf("bucketRings() filed under %d cells, want %d", len(buckets), wantCells)
	}
}

func TestRingCells(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		ring Polyline
		want cellRange
	}{
		{
			name: "a ring that never leaves one cell",
			ring: Polyline{{Lat: 1, Lon: 1}, {Lat: 2, Lon: 2}, {Lat: 1, Lon: 2}},
			want: cellRange{rowLow: 18, rowHigh: 18, colLow: 36, colHigh: 36},
		},
		{
			name: "a ring spanning several rows and columns",
			ring: Polyline{{Lat: -3, Lon: -3}, {Lat: 8, Lon: 8}},
			want: cellRange{rowLow: 17, rowHigh: 19, colLow: 35, colHigh: 37},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := ringCells(testCase.ring); got != testCase.want {
				t.Errorf("ringCells(%v) = %v, want %v", testCase.ring, got, testCase.want)
			}
		})
	}
}

func TestBoundedColumn(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		lon  float64
		want int
	}{
		{name: "an ordinary longitude", lon: 10, want: 38},
		{name: "exactly the dateline pins to the last column", lon: 180, want: cellCols - 1},
		{name: "exactly the west edge pins to the first column", lon: -180, want: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := boundedColumn(testCase.lon); got != testCase.want {
				t.Errorf("boundedColumn(%v) = %v, want %v", testCase.lon, got, testCase.want)
			}
		})
	}
}
