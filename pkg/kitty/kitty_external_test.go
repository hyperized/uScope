package kitty_test

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"io"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperized/uScope/pkg/kitty"
)

// bytesPerPixel is the stride of image.RGBA: R, G, B, A, one byte each.
const bytesPerPixel = 4

// wireDefaultChunkSize mirrors the default WithChunkSize documents: 4096
// base64 characters per chunk when the option is not used. It is not
// exported by the package, so this test hardcodes the documented contract
// rather than reaching into kitty's internals.
const wireDefaultChunkSize = 4096

// errWrite is the sentinel a failing writer returns, so a test can prove
// Frame and Close wrap the real error instead of swallowing or replacing
// it.
var errWrite = errors.New("kitty_test: write failed")

// failingWriter is an io.Writer that always fails with errWrite, so Frame's
// and Close's own error-wrapping can be exercised without a real broken
// pipe.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWrite
}

// escBlock is one parsed Kitty escape sequence: its control data as an
// ordered list of keys plus a key-to-value lookup, and its raw base64
// payload (empty when the block carried none).
type escBlock struct {
	keys    []string
	values  map[string]string
	payload string
}

// splitBlocks parses data into the ESC _ G ... ESC \ blocks it is made of,
// failing the test on anything that does not fit that shape.
func splitBlocks(t *testing.T, data []byte) []escBlock {
	t.Helper()

	const (
		opener     = "\x1b_G"
		terminator = "\x1b\\"
	)

	text := string(data)

	var blocks []escBlock

	for len(text) > 0 {
		if !strings.HasPrefix(text, opener) {
			t.Fatalf("expected block opener at %q", text)
		}

		text = text[len(opener):]

		end := strings.Index(text, terminator)
		if end < 0 {
			t.Fatalf("unterminated block in %q", text)
		}

		blocks = append(blocks, parseBlockBody(t, text[:end]))
		text = text[end+len(terminator):]
	}

	return blocks
}

// parseBlockBody splits one block's body into its control data and
// payload, then parses the control data into ordered keys and a value
// lookup.
func parseBlockBody(t *testing.T, body string) escBlock {
	t.Helper()

	control, payload, _ := strings.Cut(body, ";")

	pairs := strings.Split(control, ",")
	keys := make([]string, 0, len(pairs))
	values := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			t.Fatalf("malformed control pair %q in %q", pair, control)
		}

		keys = append(keys, key)
		values[key] = value
	}

	return escBlock{keys: keys, values: values, payload: payload}
}

// buildTestImage allocates a width by height image.RGBA and fills it with a
// deterministic per-pixel pattern, so encoded output can be checked against
// known input instead of an opaque fixture.
func buildTestImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			img.SetRGBA(x, y, color.RGBA{
				R: byte(x),     //nolint:gosec // test images stay well under 256 pixels wide.
				G: byte(y),     //nolint:gosec // test images stay well under 256 pixels tall.
				B: byte(x + y), //nolint:gosec // sum stays well under 256 for these test sizes.
				A: 0xFF,
			})
		}
	}

	return img
}

// buildWideStrideSubImage returns a subimage of a larger backing image, so
// its Stride is wider than its own width, exercising the PixOffset path
// Frame and this test's own rawPixels both rely on.
func buildWideStrideSubImage(t *testing.T) *image.RGBA {
	t.Helper()

	const fullWidth, fullHeight = 10, 6

	full := buildTestImage(fullWidth, fullHeight)

	sub, ok := full.SubImage(image.Rect(2, 1, 7, 5)).(*image.RGBA)
	if !ok {
		t.Fatal("SubImage did not return *image.RGBA")
	}

	return sub
}

// rawPixels extracts img's own pixel bytes row by row using PixOffset, the
// same address arithmetic Frame relies on internally, so a subimage with a
// wider Stride than its own width is read identically here and there.
func rawPixels(img *image.RGBA) []byte {
	bounds := img.Bounds()
	rowLen := bounds.Dx() * bytesPerPixel

	out := make([]byte, 0, rowLen*bounds.Dy())

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		offset := img.PixOffset(bounds.Min.X, y)
		out = append(out, img.Pix[offset:offset+rowLen]...)
	}

	return out
}

// TestFrameFirstChunkControlData checks the first (and here, only) chunk
// of a small frame carries exactly the keys Frame documents, in that
// order, with the values the call actually asked for.
func TestFrameFirstChunkControlData(t *testing.T) {
	t.Parallel()

	const (
		imgWidth  = 3
		imgHeight = 2
		cols      = 40
		rows      = 20
		firstID   = 111
		secondID  = 222
	)

	enc := kitty.New(kitty.WithIDs(firstID, secondID))
	img := buildTestImage(imgWidth, imgHeight)

	var out bytes.Buffer
	if err := enc.Frame(&out, img, cols, rows); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	blocksList := splitBlocks(t, out.Bytes())
	if len(blocksList) != 1 {
		t.Fatalf("Frame() produced %d blocks, want 1 for a small single-chunk image", len(blocksList))
	}

	wantKeys := []string{"a", "f", "o", "q", "i", "s", "v", "c", "r", "m"}
	if !slices.Equal(blocksList[0].keys, wantKeys) {
		t.Fatalf("first chunk control keys = %v, want %v in that order", blocksList[0].keys, wantKeys)
	}

	wantValues := map[string]string{
		"a": "T",
		"f": "32",
		"o": "z",
		"q": "2",
		"i": strconv.Itoa(firstID),
		"s": strconv.Itoa(imgWidth),
		"v": strconv.Itoa(imgHeight),
		"c": strconv.Itoa(cols),
		"r": strconv.Itoa(rows),
		"m": "0",
	}

	for key, want := range wantValues {
		if got := blocksList[0].values[key]; got != want {
			t.Fatalf("control key %q = %q, want %q", key, got, want)
		}
	}

	if blocksList[0].payload == "" {
		t.Fatal("first chunk carried no payload")
	}
}

// checkChunking encodes an incompressible width by height image and checks
// every chunk is at most wantChunkSize base64 characters, that only the
// last chunk may be shorter, and that only the first chunk carries the
// full control data.
func checkChunking(t *testing.T, width, height, chunkSize int) {
	t.Helper()

	var opts []kitty.Option

	wantChunkSize := wireDefaultChunkSize

	if chunkSize != 0 {
		opts = append(opts, kitty.WithChunkSize(chunkSize))
		wantChunkSize = chunkSize
	}

	enc := kitty.New(opts...)

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	source := rand.New(rand.NewSource(1)) //nolint:gosec // a fixed seed only needs to be reproducible, not secure.
	_, _ = source.Read(img.Pix)           // Rand.Read on a fixed-size slice always fills it and never errors.

	const cols, rows = 10, 10

	var out bytes.Buffer
	if err := enc.Frame(&out, img, cols, rows); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	blocksList := splitBlocks(t, out.Bytes())
	if len(blocksList) < 2 {
		t.Fatalf("expected multiple chunks for a %dx%d incompressible image, got %d", width, height, len(blocksList))
	}

	wantFirstKeys := []string{"a", "f", "o", "q", "i", "s", "v", "c", "r", "m"}
	wantLaterKeys := []string{"m"}

	for chunkIndex, block := range blocksList {
		wantKeys := wantLaterKeys
		if chunkIndex == 0 {
			wantKeys = wantFirstKeys
		}

		if !slices.Equal(block.keys, wantKeys) {
			t.Fatalf("chunk %d control keys = %v, want %v", chunkIndex, block.keys, wantKeys)
		}

		last := chunkIndex == len(blocksList)-1

		switch {
		case !last && len(block.payload) != wantChunkSize:
			t.Fatalf("chunk %d payload length = %d, want exactly %d", chunkIndex, len(block.payload), wantChunkSize)
		case last && len(block.payload) > wantChunkSize:
			t.Fatalf("last chunk payload length = %d, want at most %d", len(block.payload), wantChunkSize)
		default:
			// The chunk is the size it should be.
		}

		wantMore := "1"
		if last {
			wantMore = "0"
		}

		if block.values["m"] != wantMore {
			t.Fatalf("chunk %d m = %q, want %q", chunkIndex, block.values["m"], wantMore)
		}
	}
}

// TestChunkingBoundaries covers both a tiny image forced into many chunks
// by a small WithChunkSize, and a large incompressible image that needs
// several chunks even at the default size.
func TestChunkingBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		width     int
		height    int
		chunkSize int // 0 means "use the package default"
	}{
		{name: "tiny image, minimum chunk size", width: 4, height: 4, chunkSize: 4},
		{name: "large incompressible image, default chunk size", width: 200, height: 200, chunkSize: 0},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			checkChunking(t, tcase.width, tcase.height, tcase.chunkSize)
		})
	}
}

// TestIDAlternation drives three consecutive frames and checks the id
// each one carries alternates, and that a delete block for the previous id
// appears on every call after the first.
func TestIDAlternation(t *testing.T) {
	t.Parallel()

	const (
		firstID   = 401
		secondID  = 402
		imgSize   = 4
		cols      = 8
		rows      = 8
		noDelete  = -1
		callCount = 3
	)

	enc := kitty.New(kitty.WithIDs(firstID, secondID))
	img := buildTestImage(imgSize, imgSize)

	wantIDs := [callCount]int{firstID, secondID, firstID}
	wantDeletes := [callCount]int{noDelete, firstID, secondID}

	for call := range callCount {
		var out bytes.Buffer
		if err := enc.Frame(&out, img, cols, rows); err != nil {
			t.Fatalf("Frame() call %d error: %v", call+1, err)
		}

		blocksList := splitBlocks(t, out.Bytes())

		wantBlockCount := 1
		if wantDeletes[call] != noDelete {
			wantBlockCount = 2
		}

		if len(blocksList) != wantBlockCount {
			t.Fatalf("call %d produced %d blocks, want %d", call+1, len(blocksList), wantBlockCount)
		}

		if blocksList[0].values["i"] != strconv.Itoa(wantIDs[call]) {
			t.Fatalf("call %d transmit id = %q, want %d", call+1, blocksList[0].values["i"], wantIDs[call])
		}

		if wantDeletes[call] == noDelete {
			continue
		}

		del := blocksList[1]
		if del.values["a"] != "d" || del.values["i"] != strconv.Itoa(wantDeletes[call]) {
			t.Fatalf("call %d delete block = %+v, want a=d naming id %d", call+1, del, wantDeletes[call])
		}

		if del.payload != "" {
			t.Fatalf("call %d delete block carried a payload: %q", call+1, del.payload)
		}
	}
}

// TestClose checks Close deletes both ids in one write and that a Frame
// call afterwards no longer tries to delete anything, since Close cleared
// the "on screen" state.
func TestClose(t *testing.T) {
	t.Parallel()

	const (
		firstID  = 501
		secondID = 502
		imgSize  = 4
		cols     = 8
		rows     = 8
	)

	enc := kitty.New(kitty.WithIDs(firstID, secondID))
	img := buildTestImage(imgSize, imgSize)

	var displayed bytes.Buffer
	if err := enc.Frame(&displayed, img, cols, rows); err != nil {
		t.Fatalf("Frame() error: %v", err)
	}

	var closeOut bytes.Buffer
	if err := enc.Close(&closeOut); err != nil {
		t.Fatalf("Close() unexpected error: %v", err)
	}

	blocksList := splitBlocks(t, closeOut.Bytes())
	if len(blocksList) != 2 {
		t.Fatalf("Close() produced %d blocks, want 2", len(blocksList))
	}

	wantIDs := [2]int{firstID, secondID}
	for deleteIndex, block := range blocksList {
		if block.values["a"] != "d" || block.values["i"] != strconv.Itoa(wantIDs[deleteIndex]) {
			t.Fatalf("delete block %d = %+v, want a=d naming id %d", deleteIndex, block, wantIDs[deleteIndex])
		}

		if block.payload != "" {
			t.Fatalf("delete block %d carried a payload: %q", deleteIndex, block.payload)
		}
	}

	var afterClose bytes.Buffer
	if err := enc.Frame(&afterClose, img, cols, rows); err != nil {
		t.Fatalf("Frame() after Close error: %v", err)
	}

	afterBlocks := splitBlocks(t, afterClose.Bytes())
	if len(afterBlocks) != 1 {
		t.Fatalf("Frame() after Close produced %d blocks, want 1 (no delete block)", len(afterBlocks))
	}
}

// TestCloseWithoutPriorFrame checks Close still deletes both ids even when
// no frame was ever displayed, since a caller may Close defensively.
func TestCloseWithoutPriorFrame(t *testing.T) {
	t.Parallel()

	const firstID, secondID = 601, 602

	enc := kitty.New(kitty.WithIDs(firstID, secondID))

	var out bytes.Buffer
	if err := enc.Close(&out); err != nil {
		t.Fatalf("Close() unexpected error: %v", err)
	}

	blocksList := splitBlocks(t, out.Bytes())
	if len(blocksList) != 2 {
		t.Fatalf("Close() produced %d blocks, want 2", len(blocksList))
	}
}

// TestFrameValidation covers every way Frame can be asked to draw something
// that does not make sense, and checks it is reported through a sentinel
// rather than panicking or drawing nonsense.
func TestFrameValidation(t *testing.T) {
	t.Parallel()

	validImg := buildTestImage(2, 2)

	tests := []struct {
		name    string
		img     *image.RGBA
		cols    int
		rows    int
		wantErr error
	}{
		{name: "nil image", img: nil, cols: 2, rows: 2, wantErr: kitty.ErrImage},
		{name: "empty image", img: image.NewRGBA(image.Rectangle{}), cols: 2, rows: 2, wantErr: kitty.ErrImage},
		{name: "zero cols", img: validImg, cols: 0, rows: 2, wantErr: kitty.ErrCells},
		{name: "negative cols", img: validImg, cols: -1, rows: 2, wantErr: kitty.ErrCells},
		{name: "zero rows", img: validImg, cols: 2, rows: 0, wantErr: kitty.ErrCells},
		{name: "negative rows", img: validImg, cols: 2, rows: -1, wantErr: kitty.ErrCells},
		{name: "valid input is not itself an error", img: validImg, cols: 2, rows: 2, wantErr: nil},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			enc := kitty.New()

			var out bytes.Buffer

			err := enc.Frame(&out, tcase.img, tcase.cols, tcase.rows)
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

// TestFrameWriteError checks a failing writer's error comes back wrapped,
// so callers can still errors.Is against their own sentinel.
func TestFrameWriteError(t *testing.T) {
	t.Parallel()

	enc := kitty.New()
	img := buildTestImage(2, 2)

	err := enc.Frame(failingWriter{}, img, 2, 2)
	if !errors.Is(err, errWrite) {
		t.Fatalf("Frame() error = %v, want it to wrap %v", err, errWrite)
	}
}

// TestCloseWriteError is TestFrameWriteError's counterpart for Close.
func TestCloseWriteError(t *testing.T) {
	t.Parallel()

	enc := kitty.New()

	err := enc.Close(failingWriter{})
	if !errors.Is(err, errWrite) {
		t.Fatalf("Close() error = %v, want it to wrap %v", err, errWrite)
	}
}

// checkRoundTrip encodes img through a fresh Encoder, reassembles the
// payload of every chunk, and asserts the decompressed bytes match the
// image's own pixels exactly.
func checkRoundTrip(t *testing.T, img *image.RGBA) {
	t.Helper()

	enc := kitty.New()

	const cols, rows = 10, 10

	var out bytes.Buffer
	if err := enc.Frame(&out, img, cols, rows); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	blocksList := splitBlocks(t, out.Bytes())

	var encoded strings.Builder
	for _, block := range blocksList {
		encoded.WriteString(block.payload)
	}

	compressed, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	zlibReader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("zlib.NewReader failed: %v", err)
	}

	defer func() {
		_ = zlibReader.Close()
	}()

	got, err := io.ReadAll(zlibReader)
	if err != nil {
		t.Fatalf("zlib decompress failed: %v", err)
	}

	want := rawPixels(img)
	if !bytes.Equal(got, want) {
		t.Fatalf("round trip mismatch: got %d bytes, want %d bytes", len(got), len(want))
	}
}

// TestRoundTrip covers an image whose Stride equals 4*width and a subimage
// whose Stride is wider, since Frame reads pixels through PixOffset rather
// than assuming a packed buffer.
func TestRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		buildImage func(t *testing.T) *image.RGBA
	}{
		{
			name:       "stride equals width",
			buildImage: func(*testing.T) *image.RGBA { return buildTestImage(9, 5) },
		},
		{
			name:       "subimage with wider stride",
			buildImage: buildWideStrideSubImage,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			checkRoundTrip(t, tcase.buildImage(t))
		})
	}
}

// TestFrameReusesBuffersAcrossCalls calls Frame twice on the same Encoder
// with the same image and checks the second call's output matches the
// first apart from the alternated id and the trailing delete block, which
// is only true if the reused buffers are reset rather than appended to.
func TestFrameReusesBuffersAcrossCalls(t *testing.T) {
	t.Parallel()

	const (
		firstID  = 701
		secondID = 702
		imgSize  = 6
		cols     = 12
		rows     = 12
	)

	enc := kitty.New(kitty.WithIDs(firstID, secondID))
	img := buildTestImage(imgSize, imgSize)

	var first, second bytes.Buffer
	if err := enc.Frame(&first, img, cols, rows); err != nil {
		t.Fatalf("Frame() first call error: %v", err)
	}

	if err := enc.Frame(&second, img, cols, rows); err != nil {
		t.Fatalf("Frame() second call error: %v", err)
	}

	firstBlocks := splitBlocks(t, first.Bytes())
	secondBlocks := splitBlocks(t, second.Bytes())

	if len(secondBlocks) != len(firstBlocks)+1 {
		t.Fatalf("second call produced %d blocks, want %d (transmit blocks plus one delete)",
			len(secondBlocks), len(firstBlocks)+1)
	}

	for i, block := range firstBlocks {
		other := secondBlocks[i]
		if !slices.Equal(block.keys, other.keys) || block.payload != other.payload {
			t.Fatalf("transmit block %d differs beyond the id: first=%+v second=%+v", i, block, other)
		}

		for _, key := range block.keys {
			if key == "i" {
				continue
			}

			if block.values[key] != other.values[key] {
				t.Fatalf("control key %q differs between calls: first=%q second=%q",
					key, block.values[key], other.values[key])
			}
		}
	}

	if firstBlocks[0].values["i"] != strconv.Itoa(firstID) {
		t.Fatalf("first call id = %q, want %d", firstBlocks[0].values["i"], firstID)
	}

	if secondBlocks[0].values["i"] != strconv.Itoa(secondID) {
		t.Fatalf("second call id = %q, want %d", secondBlocks[0].values["i"], secondID)
	}

	deleteBlock := secondBlocks[len(secondBlocks)-1]
	if deleteBlock.values["a"] != "d" || deleteBlock.values["i"] != strconv.Itoa(firstID) {
		t.Fatalf("trailing delete block = %+v, want a=d naming id %d", deleteBlock, firstID)
	}
}

// TestInvalidOptionsFallBackSafely checks that options given values outside
// their accepted range do not break the Encoder: it still produces a valid
// stream using whatever configuration was already in effect.
func TestInvalidOptionsFallBackSafely(t *testing.T) {
	t.Parallel()

	const belowMinChunkSize = 1

	enc := kitty.New(kitty.WithIDs(0, 0), kitty.WithChunkSize(belowMinChunkSize))
	img := buildTestImage(2, 2)

	var out bytes.Buffer
	if err := enc.Frame(&out, img, 10, 10); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	blocksList := splitBlocks(t, out.Bytes())
	if len(blocksList) == 0 {
		t.Fatal("Frame() produced no blocks")
	}

	if blocksList[0].values["i"] == "" {
		t.Fatal("first block carries no image id")
	}
}
