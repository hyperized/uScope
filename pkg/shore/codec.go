package shore

import (
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
)

// The packed format, version 1.
//
// The file is gzip over a body laid out like this, every number a varint:
//
//	"USHR"          four bytes of magic
//	version         uvarint
//	cell count      uvarint
//	per cell:
//	  row             uvarint, 0 to 35, counting north from the south pole
//	  column          uvarint, 0 to 71, counting east from 180 west
//	  polyline count  uvarint
//	  per polyline:
//	    point count   uvarint, two or more
//	    latitude      zigzag varint, in units of 1e-5 degrees
//	    longitude     zigzag varint, same units
//	    then one such pair per remaining point, each the step from the point
//	    before it
//
// Three choices are worth the words. Fixed point at 1e-5 degrees is about a
// metre, which is finer than a pixel at any range uScope draws and lets the
// whole file avoid floating point. Delta coding is what makes it small: two
// neighbouring points on a Natural Earth coastline are usually a few hundred
// units apart, so a step fits in two bytes where an absolute position needs
// five. And the first point of a polyline is written as a step from zero
// rather than given its own encoding, so the decoder has one loop instead of
// a special case.
//
// Cells are written in ascending key order and nothing else is sorted, so the
// same input produces byte-identical output.
const (
	// magic opens the body. It is checked before anything else, so a file
	// that is not ours fails with something better than a varint error.
	magic = "USHR"

	// version is what Encode writes. A reader that does not know a version
	// refuses the file rather than guessing at its layout.
	version = 1

	// unit is the fixed-point scale: one unit is 1e-5 degrees.
	unit = 1e5

	// minPolylinePoints is the fewest points that make a line.
	minPolylinePoints = 2
)

// The limits Decode enforces. They are not tuning knobs: they are what stops
// a corrupt or crafted file from making uScope allocate its way out of memory
// before a single byte of geometry has been read.
const (
	// maxBody is how much decompressed data Decode will hold. The real file
	// is a couple of megabytes; this sits above anything the counts below can
	// describe, so it never refuses data they would accept.
	maxBody = 64 << 20

	// maxPolylines and maxPoints are the totals across the whole set.
	maxPolylines = 1_000_000
	maxPoints    = 20_000_000

	// The fewest bytes one item of each kind can take, which is what turns a
	// declared count into a floor on the bytes that must follow it. A cell is
	// a row, a column and a polyline count; a polyline is a point count and
	// one pair of varints; a point is one pair.
	minCellBytes     = 3
	minPolylineBytes = 3
	minPointBytes    = 2
)

// Format errors. Each is a sentinel so a caller can tell a file that was
// never ours from one that is ours and damaged.
var (
	// ErrMagic means the first four bytes are not "USHR".
	ErrMagic = errors.New("shore: not a shore file")

	// ErrVersion means the file is ours and newer than this decoder.
	ErrVersion = errors.New("shore: unsupported format version")

	// ErrTruncated means a number ran off the end of the data, or ran past
	// the width of a 64-bit integer, which amounts to the same thing: the
	// bytes at that offset are not a number this decoder can read.
	ErrTruncated = errors.New("shore: data ends early")

	// ErrCount means a declared count is larger than what follows it could
	// possibly hold, or larger than the caps above allow.
	ErrCount = errors.New("shore: count is larger than the data")

	// ErrCell means a row or column outside the grid.
	ErrCell = errors.New("shore: cell outside the grid")
)

// Encode writes lines to w in the packed format, gzipped.
//
// Polylines are cut where they leave a cell, so a caller hands over whatever
// shape the source data had and the cell index is this function's problem.
//
// It is the generator's half of the format and is exported for that reason,
// and because it is the only way to build a Set by hand: Encode into a buffer
// and Decode back out is how a test makes one.
func Encode(out io.Writer, lines []Polyline) error {
	return encodeCells(out, bucket(lines))
}

// EncodeLand writes land rings to w in the same packed format, gzipped.
//
// The rings are clipped to the cells they cross rather than cut the way
// Encode cuts a polyline, because a piece of an area has to be an area: see
// the note at the top of land.go. What comes out is a file Decode reads with
// the same code and the same limits, holding closed rings instead of open
// lines.
func EncodeLand(out io.Writer, rings []Polyline) error {
	return encodeCells(out, bucketRings(rings))
}

// encodeCells writes an already-bucketed set, which is the half Encode and
// EncodeLand share. Only the bucketing differs between the two.
func encodeCells(out io.Writer, buckets map[int][]Polyline) error {
	keys := make([]int, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	body := make([]byte, 0, len(magic)+binary.MaxVarintLen64*2)
	body = append(body, magic...)
	body = binary.AppendUvarint(body, version)
	body = binary.AppendUvarint(body, uint64(len(keys)))

	for _, key := range keys {
		body = appendCell(body, key, buckets[key])
	}

	return compress(out, body)
}

// compress writes the body through gzip at the level that matters most here:
// the file is built once by a generator and decompressed on every start, so
// the encoder's time is free and the reader's size is not. Best beats the
// default by about 140 KB on the shipped file.
//
// NewWriterLevel's error is dropped rather than returned. It has exactly one
// cause, a level outside the range the package accepts, and the level here is
// a constant well inside it. Returning it would be a branch no test could
// ever reach, and if it somehow did fire the nil writer would panic on the
// next line rather than quietly write nothing.
func compress(out io.Writer, body []byte) error {
	zipped, _ := gzip.NewWriterLevel(out, gzip.BestCompression) //nolint:errcheck // see above.

	if _, err := zipped.Write(body); err != nil {
		_ = zipped.Close()

		return fmt.Errorf("shore: compressing: %w", err)
	}

	if err := zipped.Close(); err != nil {
		return fmt.Errorf("shore: finishing gzip: %w", err)
	}

	return nil
}

// appendCell writes one cell's header and its polylines.
//
//nolint:gosec // key comes from cellOf, which cannot produce a negative.
func appendCell(dst []byte, key int, lines []Polyline) []byte {
	dst = binary.AppendUvarint(dst, uint64(key/cellCols))
	dst = binary.AppendUvarint(dst, uint64(key%cellCols))
	dst = binary.AppendUvarint(dst, uint64(len(lines)))

	for _, line := range lines {
		dst = appendPolyline(dst, line)
	}

	return dst
}

// appendPolyline writes one polyline as a point count and a run of steps.
func appendPolyline(dst []byte, line Polyline) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(line)))

	var lat, lon int64

	for _, point := range line {
		scaledLat, scaledLon := scale(point)
		dst = binary.AppendVarint(dst, scaledLat-lat)
		dst = binary.AppendVarint(dst, scaledLon-lon)
		lat, lon = scaledLat, scaledLon
	}

	return dst
}

// scale turns a point into the fixed-point pair the file carries.
//
// The coordinates are clamped on the way through, so a source file with a
// stray infinity in it produces a point on the edge of the world rather than
// an integer conversion the spec does not define.
func scale(point Point) (int64, int64) {
	return int64(math.Round(clampLat(point.Lat) * unit)),
		int64(math.Round(clampLon(point.Lon) * unit))
}

// bucket cuts every polyline at the cell boundaries it crosses and files the
// pieces by cell.
func bucket(lines []Polyline) map[int][]Polyline {
	buckets := make(map[int][]Polyline)

	for _, line := range lines {
		split(buckets, line)
	}

	return buckets
}

// split cuts one polyline where it leaves a cell.
//
// A cut keeps the crossing segment on both sides of it: the piece that ends
// at the boundary carries the first point of the next cell, and the piece
// that starts there opens with the last point of the previous one. Without
// that overlap the coastline would show a gap one segment wide on every cell
// boundary it crosses, and at five degrees there are plenty of those in view
// at once.
func split(buckets map[int][]Polyline, line Polyline) {
	if len(line) < minPolylinePoints {
		return
	}

	cell := cellOf(line[0])
	piece := Polyline{line[0]}

	for _, point := range line[1:] {
		piece = append(piece, point)

		next := cellOf(point)
		if next == cell {
			continue
		}

		buckets[cell] = append(buckets[cell], piece)
		piece = Polyline{piece[len(piece)-2], point}
		cell = next
	}

	// The tail always has at least two points: piece starts with one, the
	// loop above runs at least once because the line has two or more, and a
	// cut restarts it with the pair that straddled the boundary.
	buckets[cell] = append(buckets[cell], piece)
}

// Decode reads a packed shoreline file and builds the Set it describes.
//
// Every count in the file is checked against the bytes that are left and
// against the caps above before anything is allocated for it, so a damaged or
// hostile file produces a sentinel error rather than a panic or an
// out-of-memory kill.
//
// The Set it returns has no land rings in it, so LandWithin visits nothing.
// That is the right answer for a caller that only wants outlines, and
// DecodeWithLand is the one that reads both files.
func Decode(r io.Reader) (*Set, error) {
	cells, err := decodeCells(r)
	if err != nil {
		return nil, err
	}

	return &Set{cells: cells}, nil
}

// DecodeWithLand reads the shoreline file and the land file together, which is
// what Load does with the two the binary carries.
//
// The two are separate files rather than two sections of one because they are
// read by different code paths for different reasons: a caller that draws
// outlines and no fill has no use for a couple of megabytes of polygons, and a
// format with an optional half in it is a format with a version problem
// waiting in it.
func DecodeWithLand(outlines, land io.Reader) (*Set, error) {
	set, err := Decode(outlines)
	if err != nil {
		return nil, err
	}

	cells, err := decodeCells(land)
	if err != nil {
		return nil, fmt.Errorf("land: %w", err)
	}

	set.land = cells

	return set, nil
}

// decodeCells reads one packed file into its cell index.
func decodeCells(r io.Reader) (map[int][]Polyline, error) {
	zipped, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("shore: reading gzip header: %w", err)
	}

	defer func() { _ = zipped.Close() }()

	body, err := io.ReadAll(io.LimitReader(zipped, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("shore: decompressing: %w", err)
	}

	if len(body) > maxBody {
		return nil, fmt.Errorf("%w: more than %d bytes of body", ErrCount, maxBody)
	}

	return decodeBody(body)
}

// decodeBody parses the decompressed body.
func decodeBody(body []byte) (map[int][]Polyline, error) {
	dec := decoder{buf: body}

	if err := dec.header(); err != nil {
		return nil, err
	}

	cells, err := dec.countOf(minCellBytes, maxCells)
	if err != nil {
		return nil, err
	}

	for range cells {
		if err := dec.cell(); err != nil {
			return nil, err
		}
	}

	return dec.finish(), nil
}

// decoder walks the decompressed body once.
//
// Points land in one slice for the whole set and every Polyline ends up as a
// window on it. The windows cannot be taken while the slice is still growing,
// because a growth moves the backing array out from under them, so the loop
// records spans and finish resolves them into slices afterwards.
type decoder struct {
	buf    []byte
	pos    int
	points []Point
	spans  []span
	runs   []run
}

// span is one polyline's window on the point slice.
type span struct {
	start  int
	length int
}

// run is one cell's window on the span slice.
type run struct {
	key   int
	first int
	count int
}

// header checks the magic and the version.
func (d *decoder) header() error {
	if len(d.buf) < len(magic) || string(d.buf[:len(magic)]) != magic {
		return ErrMagic
	}

	d.pos = len(magic)

	found, err := d.uvarint()
	if err != nil {
		return err
	}

	if found != version {
		return fmt.Errorf("%w: file says %d, this build reads %d", ErrVersion, found, version)
	}

	return nil
}

// cell reads one cell and everything filed under it.
func (d *decoder) cell() error {
	key, err := d.cellKey()
	if err != nil {
		return err
	}

	lines, err := d.countOf(minPolylineBytes, maxPolylines-uint64(len(d.spans)))
	if err != nil {
		return err
	}

	first := len(d.spans)

	for range lines {
		if err := d.polyline(); err != nil {
			return err
		}
	}

	d.runs = append(d.runs, run{key: key, first: first, count: len(d.spans) - first})

	return nil
}

// cellKey reads a row and a column and packs them the way a Set is keyed.
func (d *decoder) cellKey() (int, error) {
	row, err := d.uvarint()
	if err != nil {
		return 0, err
	}

	col, err := d.uvarint()
	if err != nil {
		return 0, err
	}

	if row >= cellRows || col >= cellCols {
		return 0, fmt.Errorf("%w: row %d column %d", ErrCell, row, col)
	}

	return int(row)*cellCols + int(col), nil //nolint:gosec // both are checked against the grid above.
}

// polyline reads one polyline's points onto the shared slice.
func (d *decoder) polyline() error {
	count, err := d.countOf(minPointBytes, maxPoints-uint64(len(d.points)))
	if err != nil {
		return err
	}

	if count < minPolylinePoints {
		return fmt.Errorf("%w: a polyline needs %d points, got %d", ErrCount, minPolylinePoints, count)
	}

	start := len(d.points)

	var lat, lon int64

	for range count {
		stepLat, stepLon, err := d.step()
		if err != nil {
			return err
		}

		lat, lon = lat+stepLat, lon+stepLon
		d.points = append(d.points, Point{Lat: float64(lat) / unit, Lon: float64(lon) / unit})
	}

	d.spans = append(d.spans, span{start: start, length: len(d.points) - start})

	return nil
}

// step reads one point's pair of deltas.
func (d *decoder) step() (int64, int64, error) {
	lat, err := d.varint()
	if err != nil {
		return 0, 0, err
	}

	lon, err := d.varint()
	if err != nil {
		return 0, 0, err
	}

	return lat, lon, nil
}

// finish turns the recorded spans into the polylines and the cell index they
// are filed under.
func (d *decoder) finish() map[int][]Polyline {
	lines := make([]Polyline, len(d.spans))

	for index, window := range d.spans {
		end := window.start + window.length
		lines[index] = Polyline(d.points[window.start:end:end])
	}

	// A file that names the same cell twice keeps the later one. It cannot
	// happen in anything Encode wrote, and merging the two would be work on
	// the startup path to tidy up a file that is already damaged.
	cells := make(map[int][]Polyline, len(d.runs))

	for _, cell := range d.runs {
		end := cell.first + cell.count
		cells[cell.key] = lines[cell.first:end:end]
	}

	return cells
}

// countOf reads a count and refuses one the rest of the file could not hold.
//
// perItem is the fewest bytes one item can take, so the count multiplied by
// it is a floor on the bytes that must follow. Without this check a corrupt
// varint could ask the decoder to make room for four billion polylines and
// find out there were ten bytes left only after the allocation.
func (d *decoder) countOf(perItem int, limit uint64) (uint64, error) {
	value, err := d.uvarint()
	if err != nil {
		return 0, err
	}

	if value > limit {
		return 0, fmt.Errorf("%w: %d is past the cap of %d", ErrCount, value, limit)
	}

	if value > uint64(d.remaining()/perItem) { //nolint:gosec // remaining is never negative.
		return 0, fmt.Errorf("%w: %d items in %d bytes", ErrCount, value, d.remaining())
	}

	return value, nil
}

// remaining is how many bytes are left to read.
func (d *decoder) remaining() int { return len(d.buf) - d.pos }

// uvarint reads one unsigned varint.
func (d *decoder) uvarint() (uint64, error) {
	value, width := binary.Uvarint(d.buf[d.pos:])
	if width <= 0 {
		return 0, ErrTruncated
	}

	d.pos += width

	return value, nil
}

// varint reads one zigzag-coded signed varint.
func (d *decoder) varint() (int64, error) {
	value, width := binary.Varint(d.buf[d.pos:])
	if width <= 0 {
		return 0, ErrTruncated
	}

	d.pos += width

	return value, nil
}
