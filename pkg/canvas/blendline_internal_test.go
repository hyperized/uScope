package canvas

import (
	"image/color"
	"testing"
)

// TestBlendBand checks one column of a blended band: a band 2.5 pixels tall
// from row 1.5 covers row 1 by half, rows 2 and 3 fully and row 4 not at
// all, each scaled by alpha, and a band starting above the canvas is cut at
// its first row rather than written before it.
func TestBlendBand(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		top  float64
		want [6]uint8
	}{
		{name: "inside", top: 1.5, want: [6]uint8{0, 50, 100, 100, 0, 0}},
		{name: "above the canvas", top: -1.5, want: [6]uint8{100, 0, 0, 0, 0, 0}},
		{name: "past the bottom", top: 4.5, want: [6]uint8{0, 0, 0, 0, 50, 100}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := New(1, 6)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			lanes := canv.lanes(false)
			ink := levelsOf(color.RGBA{R: 200, A: opaque})
			canv.blendBand(&lanes, 0, testCase.top, 2.5, &ink, 0.5)

			for row, want := range testCase.want {
				if got := canv.img.RGBAAt(0, row).R; got != want {
					t.Errorf("row %d red = %d, want %d", row, got, want)
				}
			}
		})
	}
}
