package blocks_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperized/uScope/pkg/blocks"
)

// esc is the byte that starts every control sequence the decoder below
// looks for, matching the one the package under test emits.
const esc = 0x1b

// glyph is the exact byte sequence blocks prints for U+2580, upper half
// block: e2 96 80 in UTF-8.
const glyph = "▀"

// errWriteFailed is the sentinel a failing io.Writer in these tests returns,
// declared once at package level so err113 has a static error to wrap
// instead of a fresh errors.New at the point of use.
var errWriteFailed = errors.New("blocks_test: write failed")

// cellColor is the decoded (foreground, background) pair for one terminal
// cell, each an RGB triplet: the wire format never carries alpha, matching
// how the package under test reads pixels.
type cellColor struct {
	foreground [3]uint8
	background [3]uint8
}

// rgbOf drops the alpha channel, the same thing the renderer itself does
// when reading a source pixel, so a test can compare like with like.
func rgbOf(c color.RGBA) [3]uint8 {
	return [3]uint8{c.R, c.G, c.B}
}

// buildImage allocates a cols by 2*rows image.RGBA and fills it pixel by
// pixel through colorAt, using RGBA's own SetRGBA so the stored bytes are
// exactly the ones passed in rather than something a colour-model
// conversion decided to make of them.
func buildImage(t *testing.T, cols, rows int, colorAt func(x, y int) color.RGBA) *image.RGBA {
	t.Helper()

	const pixelsPerCell = 2

	img := image.NewRGBA(image.Rect(0, 0, cols, rows*pixelsPerCell))

	for y := range rows * pixelsPerCell {
		for x := range cols {
			img.SetRGBA(x, y, colorAt(x, y))
		}
	}

	return img
}

// decodeFrame parses a rendered frame back into a grid of cells by running
// the same SGR state machine a real terminal would, so a test can compare
// what actually got drawn against the source image instead of trusting the
// byte layout on faith.
func decodeFrame(t *testing.T, data []byte, cols, rows int) [][]cellColor {
	t.Helper()

	const homeLen = 3 // ESC [ H

	if len(data) < homeLen || !bytes.Equal(data[:homeLen], []byte{esc, '[', 'H'}) {
		t.Fatalf("decodeFrame: missing cursor-home prefix in %q", data)
	}

	pos := homeLen
	grid := make([][]cellColor, rows)

	for row := range rows {
		var gridRow []cellColor

		gridRow, pos = decodeRow(t, data, pos, cols)
		grid[row] = gridRow

		if row < rows-1 {
			const sepLen = 2 // CR LF

			if pos+sepLen > len(data) || !bytes.Equal(data[pos:pos+sepLen], []byte("\r\n")) {
				t.Fatalf("decodeFrame: expected row separator at offset %d in %q", pos, data)
			}

			pos += sepLen
		}
	}

	if pos != len(data) {
		t.Fatalf("decodeFrame: %d trailing bytes after last row: %q", len(data)-pos, data[pos:])
	}

	return grid
}

// decodeRow decodes exactly cols cells starting at pos, followed by the row
// reset, and returns the decoded cells plus the position just past the
// reset.
func decodeRow(t *testing.T, data []byte, pos, cols int) ([]cellColor, int) {
	t.Helper()

	row := make([]cellColor, cols)

	var current cellColor

	for col := range cols {
		if pos < len(data) && data[pos] == esc {
			var changed sgrChange

			current, pos, changed = decodeSGR(t, data, pos, current)

			if col == 0 && (!changed.foreground || !changed.background) {
				t.Fatalf("decodeRow: first cell of a row must set both colours, got %+v", changed)
			}
		}

		row[col] = current

		glyphBytes := []byte(glyph)
		if pos+len(glyphBytes) > len(data) || !bytes.Equal(data[pos:pos+len(glyphBytes)], glyphBytes) {
			t.Fatalf("decodeRow: expected glyph at offset %d in %q", pos, data)
		}

		pos += len(glyphBytes)
	}

	const resetLen = 4 // ESC [ 0 m

	if pos+resetLen > len(data) || !bytes.Equal(data[pos:pos+resetLen], []byte{esc, '[', '0', 'm'}) {
		t.Fatalf("decodeRow: expected row reset at offset %d in %q", pos, data)
	}

	return row, pos + resetLen
}

// sgrChange records which of a cell's two colours one decoded SGR sequence
// actually set, bundled into one value so decodeSGR stays within the
// package's return-count limit.
type sgrChange struct {
	foreground bool
	background bool
}

// decodeSGR reads one "ESC [ ... m" sequence starting at pos and applies it
// to current, reporting which of the two colours the sequence actually set.
func decodeSGR(t *testing.T, data []byte, pos int, current cellColor) (cellColor, int, sgrChange) {
	t.Helper()

	if pos+2 > len(data) || data[pos] != esc || data[pos+1] != '[' {
		t.Fatalf("decodeSGR: expected CSI at offset %d in %q", pos, data)
	}

	start := pos + 2

	end := bytes.IndexByte(data[start:], 'm')
	if end < 0 {
		t.Fatalf("decodeSGR: unterminated SGR sequence at offset %d in %q", pos, data)
	}

	end += start

	values := decodeSGRParams(t, data[start:end], pos)

	const (
		fgCode        = 38
		bgCode        = 48
		trueColorMode = 2
		groupLen      = 5
	)

	var changed sgrChange

	for groupStart := 0; groupStart < len(values); groupStart += groupLen {
		if groupStart+groupLen > len(values) {
			t.Fatalf("decodeSGR: truncated colour group at offset %d", pos)
		}

		if values[groupStart+1] != trueColorMode {
			t.Fatalf("decodeSGR: unexpected colour mode %d at offset %d", values[groupStart+1], pos)
		}

		col := [3]uint8{
			uint8(values[groupStart+2]), //nolint:gosec // decodeSGRParams already bounds these to 0-255.
			uint8(values[groupStart+3]), //nolint:gosec // decodeSGRParams already bounds these to 0-255.
			uint8(values[groupStart+4]), //nolint:gosec // decodeSGRParams already bounds these to 0-255.
		}

		switch values[groupStart] {
		case fgCode:
			current.foreground = col
			changed.foreground = true
		case bgCode:
			current.background = col
			changed.background = true
		default:
			t.Fatalf("decodeSGR: unexpected SGR code %d at offset %d", values[groupStart], pos)
		}
	}

	return current, end + 1, changed
}

// decodeSGRParams splits the parameter list of one SGR sequence into ints,
// failing the test on anything that is not a valid 0-255 channel or mode
// value.
func decodeSGRParams(t *testing.T, params []byte, pos int) []int {
	t.Helper()

	const maxChannel = 255

	parts := bytes.Split(params, []byte(";"))
	values := make([]int, len(parts))

	for partIndex, part := range parts {
		v, err := strconv.Atoi(string(part))
		if err != nil || v < 0 || v > maxChannel {
			t.Fatalf("decodeSGRParams: bad SGR parameter %q at offset %d", part, pos)
		}

		values[partIndex] = v
	}

	return values
}

// extractColorSequences walks the raw frame and returns the parameter text
// of every colour-setting SGR sequence, in order, skipping the cursor-home
// prefix and every row's trailing reset. It is how the run-length tests
// check the byte stream directly instead of through the cell decoder.
func extractColorSequences(t *testing.T, data []byte) []string {
	t.Helper()

	var sequences []string

	pos := 0
	for pos < len(data) {
		if data[pos] != esc {
			pos++

			continue
		}

		if pos+1 >= len(data) || data[pos+1] != '[' {
			t.Fatalf("extractColorSequences: bare ESC not followed by '[' at offset %d", pos)
		}

		if pos+2 < len(data) && data[pos+2] == 'H' {
			const homeLen = 3

			pos += homeLen

			continue
		}

		end := bytes.IndexByte(data[pos+2:], 'm')
		if end < 0 {
			t.Fatalf("extractColorSequences: unterminated sequence at offset %d", pos)
		}

		end += pos + 2

		params := string(data[pos+2 : end])

		const resetParams = "0"
		if params != resetParams {
			sequences = append(sequences, params)
		}

		pos = end + 1
	}

	return sequences
}

// checkFrameMatchesSource renders a cols by rows image built from colorAt
// and asserts the decoded grid matches the source pixel for pixel.
func checkFrameMatchesSource(t *testing.T, cols, rows int, colorAt func(x, y int) color.RGBA) {
	t.Helper()

	const pixelsPerCell = 2

	img := buildImage(t, cols, rows, colorAt)

	r := blocks.New()

	var out bytes.Buffer
	if err := r.Frame(&out, img, cols, rows); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	grid := decodeFrame(t, out.Bytes(), cols, rows)

	for row := range rows {
		for col := range cols {
			wantFG := rgbOf(colorAt(col, row*pixelsPerCell))
			wantBG := rgbOf(colorAt(col, row*pixelsPerCell+1))
			got := grid[row][col]

			if got.foreground != wantFG {
				t.Fatalf("cell (%d,%d) foreground = %v, want %v", col, row, got.foreground, wantFG)
			}

			if got.background != wantBG {
				t.Fatalf("cell (%d,%d) background = %v, want %v", col, row, got.background, wantBG)
			}
		}
	}
}

// TestFrameMatchesSourcePixels decodes several shapes of frame and checks
// every cell against the image that produced it.
func TestFrameMatchesSourcePixels(t *testing.T) {
	t.Parallel()

	const colorStep = 41

	tests := []struct {
		name    string
		cols    int
		rows    int
		colorAt func(x, y int) color.RGBA
	}{
		{
			name: "single cell",
			cols: 1,
			rows: 1,
			colorAt: func(_, y int) color.RGBA {
				if y == 0 {
					return color.RGBA{R: colorStep, G: colorStep * 2, B: colorStep * 3, A: colorStep}
				}

				return color.RGBA{R: colorStep * 3, G: colorStep * 2, B: colorStep, A: colorStep}
			},
		},
		{
			name: "solid fill",
			cols: 4,
			rows: 3,
			colorAt: func(_, _ int) color.RGBA {
				return color.RGBA{R: colorStep, G: colorStep * 2, B: colorStep * 3, A: colorStep}
			},
		},
		{
			name: "per-cell gradient",
			cols: 5,
			rows: 4,
			colorAt: func(x, y int) color.RGBA {
				return color.RGBA{
					R: uint8(x * colorStep),       //nolint:gosec // bounded by the small test dimensions above.
					G: uint8(y * colorStep),       //nolint:gosec // bounded by the small test dimensions above.
					B: uint8((x + y) * colorStep), //nolint:gosec // bounded by the small test dimensions above.
					A: colorStep,
				}
			},
		},
		{
			name: "wide short",
			cols: 10,
			rows: 1,
			colorAt: func(x, y int) color.RGBA {
				return color.RGBA{
					R: uint8(x * colorStep),       //nolint:gosec // bounded by the small test dimensions above.
					G: uint8(y * colorStep),       //nolint:gosec // bounded by the small test dimensions above.
					B: uint8((x + y) * colorStep), //nolint:gosec // bounded by the small test dimensions above.
					A: colorStep,
				}
			},
		},
		{
			name: "narrow tall",
			cols: 1,
			rows: 10,
			colorAt: func(x, y int) color.RGBA {
				return color.RGBA{
					R: uint8(y * colorStep),       //nolint:gosec // bounded by the small test dimensions above.
					G: uint8(x * colorStep),       //nolint:gosec // bounded by the small test dimensions above.
					B: uint8((x + y) * colorStep), //nolint:gosec // bounded by the small test dimensions above.
					A: colorStep,
				}
			},
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			checkFrameMatchesSource(t, tcase.cols, tcase.rows, tcase.colorAt)
		})
	}
}

// TestFrameSolidRowSingleEscape asserts a solid-colour row costs exactly one
// SGR sequence, on the raw byte stream rather than through the decoder,
// which is the run-length behaviour the whole package exists for.
func TestFrameSolidRowSingleEscape(t *testing.T) {
	t.Parallel()

	const (
		cols, rows = 6, 1
		colorStep  = 50
	)

	img := buildImage(t, cols, rows, func(_, _ int) color.RGBA {
		return color.RGBA{R: colorStep, G: colorStep, B: colorStep, A: colorStep}
	})

	r := blocks.New()

	var out bytes.Buffer
	if err := r.Frame(&out, img, cols, rows); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	sequences := extractColorSequences(t, out.Bytes())
	if len(sequences) != 1 {
		t.Fatalf("solid row produced %d colour sequences, want 1: %v", len(sequences), sequences)
	}
}

// TestFrameAlternatingRowEscapePerCell asserts a row that alternates two
// colours costs one SGR sequence per cell, since every cell then differs
// from the one before it.
func TestFrameAlternatingRowEscapePerCell(t *testing.T) {
	t.Parallel()

	const (
		cols, rows = 6, 1
		colorStep  = 50
	)

	colorA := color.RGBA{R: colorStep, G: colorStep, B: colorStep, A: colorStep}
	colorB := color.RGBA{R: colorStep * 3, G: colorStep * 3, B: colorStep * 3, A: colorStep}

	img := buildImage(t, cols, rows, func(x, _ int) color.RGBA {
		if x%2 == 0 {
			return colorA
		}

		return colorB
	})

	r := blocks.New()

	var out bytes.Buffer
	if err := r.Frame(&out, img, cols, rows); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	sequences := extractColorSequences(t, out.Bytes())
	if len(sequences) != cols {
		t.Fatalf("alternating row produced %d colour sequences, want %d: %v", len(sequences), cols, sequences)
	}
}

// TestFrameBackgroundOnlyChange builds a row whose top pixel never changes
// and whose bottom pixel changes on some cells, and asserts the resulting
// sequences carry a background parameter without ever carrying a foreground
// one, except on the first cell which always sets both.
func TestFrameBackgroundOnlyChange(t *testing.T) {
	t.Parallel()

	const (
		cols, rows = 4, 1
		colorStep  = 50
	)

	top := color.RGBA{R: colorStep, G: colorStep, B: colorStep, A: colorStep}
	bottomA := color.RGBA{R: colorStep * 2, G: colorStep, B: colorStep, A: colorStep}
	bottomB := color.RGBA{R: colorStep * 3, G: colorStep, B: colorStep, A: colorStep}

	img := buildImage(t, cols, rows, func(x, y int) color.RGBA {
		if y == 0 {
			return top
		}

		if x == 0 {
			return bottomA
		}

		return bottomB
	})

	r := blocks.New()

	var out bytes.Buffer
	if err := r.Frame(&out, img, cols, rows); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	sequences := extractColorSequences(t, out.Bytes())

	const wantSequences = 2 // cell 0 (forced, both) and cell 1 (bg-only); cells 2-3 repeat cell 1's colours.
	if len(sequences) != wantSequences {
		t.Fatalf("background-only row produced %d colour sequences, want %d: %v",
			len(sequences), wantSequences, sequences)
	}

	if !containsBoth(sequences[0]) {
		t.Fatalf("first cell sequence %q does not set both colours", sequences[0])
	}

	if containsForeground(sequences[1]) {
		t.Fatalf("background-only sequence %q unexpectedly contains a foreground code", sequences[1])
	}

	if !containsBackground(sequences[1]) {
		t.Fatalf("background-only sequence %q is missing its background code", sequences[1])
	}
}

func containsBoth(seq string) bool {
	return containsForeground(seq) && containsBackground(seq)
}

func containsForeground(seq string) bool {
	return strings.Contains(seq, "38;2;")
}

func containsBackground(seq string) bool {
	return strings.Contains(seq, "48;2;")
}

// TestFrameStructure checks the parts of the wire format that are not about
// colour at all: the cursor-home prefix, the reset at the end of every row,
// and that rows are CR LF separated with no trailing separator.
func TestFrameStructure(t *testing.T) {
	t.Parallel()

	const (
		cols, rows = 3, 4
		colorStep  = 30
	)

	img := buildImage(t, cols, rows, func(x, y int) color.RGBA {
		return color.RGBA{
			R: uint8(x * colorStep), //nolint:gosec // bounded by the small test dimensions above.
			G: uint8(y * colorStep), //nolint:gosec // bounded by the small test dimensions above.
			B: colorStep,
			A: colorStep,
		}
	})

	r := blocks.New()

	var out bytes.Buffer
	if err := r.Frame(&out, img, cols, rows); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	data := out.Bytes()

	if !bytes.HasPrefix(data, []byte{esc, '[', 'H'}) {
		t.Fatalf("frame does not start with the cursor-home sequence: %q", data)
	}

	if bytes.HasSuffix(data, []byte("\r\n")) {
		t.Fatalf("frame ends with a trailing row separator: %q", data)
	}

	parts := bytes.Split(data, []byte("\r\n"))
	if len(parts) != rows {
		t.Fatalf("frame has %d rows separated by CR LF, want %d", len(parts), rows)
	}

	for i, part := range parts {
		if !bytes.HasSuffix(part, []byte{esc, '[', '0', 'm'}) {
			t.Fatalf("row %d does not end with the reset sequence: %q", i, part)
		}
	}
}

// TestFrameValidation covers every way Frame can be asked to draw something
// that does not make sense, and checks it is reported through a sentinel
// rather than panicking or drawing nonsense.
func TestFrameValidation(t *testing.T) {
	t.Parallel()

	const cols, rows = 2, 2

	validImage := image.NewRGBA(image.Rect(0, 0, cols, rows*2))

	tests := []struct {
		name    string
		img     *image.RGBA
		cols    int
		rows    int
		wantErr error
	}{
		{name: "nil image", img: nil, cols: cols, rows: rows, wantErr: blocks.ErrSize},
		{name: "zero cols", img: nil, cols: 0, rows: rows, wantErr: blocks.ErrCells},
		{name: "negative cols", img: nil, cols: -1, rows: rows, wantErr: blocks.ErrCells},
		{name: "zero rows", img: nil, cols: cols, rows: 0, wantErr: blocks.ErrCells},
		{name: "negative rows", img: nil, cols: cols, rows: -1, wantErr: blocks.ErrCells},
		{name: "zero cols and rows with nil image", img: nil, cols: 0, rows: 0, wantErr: blocks.ErrCells},
		{
			name: "image one pixel too wide", cols: cols, rows: rows, wantErr: blocks.ErrSize,
			img: image.NewRGBA(image.Rect(0, 0, cols+1, rows*2)),
		},
		{
			name: "image one pixel too narrow", cols: cols, rows: rows, wantErr: blocks.ErrSize,
			img: image.NewRGBA(image.Rect(0, 0, cols-1, rows*2)),
		},
		{
			name: "image one pixel too short", cols: cols, rows: rows, wantErr: blocks.ErrSize,
			img: image.NewRGBA(image.Rect(0, 0, cols, rows*2-1)),
		},
		{
			name: "image one pixel too tall", cols: cols, rows: rows, wantErr: blocks.ErrSize,
			img: image.NewRGBA(image.Rect(0, 0, cols, rows*2+1)),
		},
		{name: "valid image is not itself an error", img: validImage, cols: cols, rows: rows, wantErr: nil},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			r := blocks.New()

			var out bytes.Buffer

			err := r.Frame(&out, tcase.img, tcase.cols, tcase.rows)
			if tcase.wantErr == nil {
				if err != nil {
					t.Fatalf("Frame() unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, tcase.wantErr) {
				t.Fatalf("Frame() error = %v, want %v", err, tcase.wantErr)
			}
		})
	}
}

// errWriter is an io.Writer that always fails, so Frame's own error-wrapping
// can be exercised without a real broken pipe.
type errWriter struct {
	err error
}

func (w errWriter) Write([]byte) (int, error) {
	return 0, w.err
}

// TestFrameWriteError checks that a failing writer's error comes back
// wrapped, so callers can still errors.Is against their own sentinel.
func TestFrameWriteError(t *testing.T) {
	t.Parallel()

	const cols, rows = 1, 1

	img := image.NewRGBA(image.Rect(0, 0, cols, rows*2))

	r := blocks.New()

	err := r.Frame(errWriter{err: errWriteFailed}, img, cols, rows)
	if !errors.Is(err, errWriteFailed) {
		t.Fatalf("Frame() error = %v, want it to wrap %v", err, errWriteFailed)
	}
}

// TestFrameDeterministicAcrossCalls renders the same image twice on the same
// Renderer and checks the output is byte-identical, which is only true if
// the reused buffer is reset rather than appended to on every call.
func TestFrameDeterministicAcrossCalls(t *testing.T) {
	t.Parallel()

	const (
		cols, rows = 3, 2
		colorStep  = 37
	)

	img := buildImage(t, cols, rows, func(x, y int) color.RGBA {
		return color.RGBA{
			R: uint8(x * colorStep), //nolint:gosec // bounded by the small test dimensions above.
			G: uint8(y * colorStep), //nolint:gosec // bounded by the small test dimensions above.
			B: colorStep,
			A: colorStep,
		}
	})

	renderer := blocks.New()

	var first, second bytes.Buffer
	if err := renderer.Frame(&first, img, cols, rows); err != nil {
		t.Fatalf("Frame() first call error: %v", err)
	}

	if err := renderer.Frame(&second, img, cols, rows); err != nil {
		t.Fatalf("Frame() second call error: %v", err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("Frame() output differs between calls:\nfirst:  %q\nsecond: %q", first.Bytes(), second.Bytes())
	}
}
