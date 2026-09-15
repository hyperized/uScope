package blocks

import (
	"bytes"
	"image"
	"testing"
)

// TestPixelAtIgnoresAlpha exercises pixelAt directly, since Frame never
// hands the alpha byte to a caller and this is the only path that reads it.
// The pixel is poked straight into img.Pix, bypassing color.Color
// conversion, so the alpha byte is exactly 40 rather than whatever a
// premultiplying convert would make of it.
func TestPixelAtIgnoresAlpha(t *testing.T) {
	t.Parallel()

	const width, height = 2, 2

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	offset := img.PixOffset(1, 1)
	img.Pix[offset] = 10
	img.Pix[offset+1] = 20
	img.Pix[offset+2] = 30
	img.Pix[offset+3] = 40

	got := pixelAt(img, 1, 1)
	want := [3]uint8{10, 20, 30}

	if got != want {
		t.Fatalf("pixelAt() = %v, want %v", got, want)
	}
}

// TestWriteUintReusesCapacity proves writeUint appends through the buffer's
// spare capacity instead of discarding it, which is the property the whole
// package leans on to avoid per-frame allocation.
func TestWriteUintReusesCapacity(t *testing.T) {
	t.Parallel()

	renderer := New()
	renderer.buf.Grow(64)

	before := renderer.buf.Cap()

	renderer.writeUint(255)

	if got := renderer.buf.String(); got != "255" {
		t.Fatalf("writeUint(255) wrote %q, want %q", got, "255")
	}

	if renderer.buf.Cap() != before {
		t.Fatalf("writeUint grew capacity from %d to %d, want unchanged", before, renderer.buf.Cap())
	}
}

// TestWriteColorChangeNeitherWritesNothing reaches the early return in
// writeColorChange, the one branch Frame's own run-length behaviour never
// takes on the first cell of a row.
func TestWriteColorChangeNeitherWritesNothing(t *testing.T) {
	t.Parallel()

	r := New()
	r.writeColorChange(false, false, [3]uint8{1, 2, 3}, [3]uint8{4, 5, 6})

	if r.buf.Len() != 0 {
		t.Fatalf("writeColorChange(false, false, ...) wrote %d bytes, want 0", r.buf.Len())
	}
}

// TestFrameBufferReset proves Reset, not append, governs reuse: seeding the
// buffer with stale bytes before Frame must not leak them into the output.
func TestFrameBufferReset(t *testing.T) {
	t.Parallel()

	const cols, rows = 1, 1

	img := image.NewRGBA(image.Rect(0, 0, cols, rows*2))

	r := New()
	r.buf.WriteString("stale data that must not survive")

	var out bytes.Buffer
	if err := r.Frame(&out, img, cols, rows); err != nil {
		t.Fatalf("Frame() unexpected error: %v", err)
	}

	if bytes.Contains(out.Bytes(), []byte("stale")) {
		t.Fatalf("Frame() output contains stale buffer contents: %q", out.Bytes())
	}
}
