package theme

import (
	"image/color"
	"testing"
)

// Packed 0xRRGGBB inputs for rgb, one per channel plus black, white and a
// mixed value. Named so the mnd linter sees identifiers rather than bare
// literals.
const (
	packedRed   = 0xFF0000
	packedGreen = 0x00FF00
	packedBlue  = 0x0000FF
	packedBlack = 0x000000
	packedWhite = 0xFFFFFF
	packedMixed = 0x123456
)

// Expected channel bytes for packedMixed (0x12, 0x34, 0x56), named for the
// same reason.
const (
	mixedR = 0x12
	mixedG = 0x34
	mixedB = 0x56
)

func TestRGB(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value uint32
		want  color.RGBA
	}{
		{name: "pure red", value: packedRed, want: color.RGBA{R: byteMask, G: 0, B: 0, A: opaque}},
		{name: "pure green", value: packedGreen, want: color.RGBA{R: 0, G: byteMask, B: 0, A: opaque}},
		{name: "pure blue", value: packedBlue, want: color.RGBA{R: 0, G: 0, B: byteMask, A: opaque}},
		{name: "black", value: packedBlack, want: color.RGBA{R: 0, G: 0, B: 0, A: opaque}},
		{name: "white", value: packedWhite, want: color.RGBA{R: byteMask, G: byteMask, B: byteMask, A: opaque}},
		{name: "mixed value", value: packedMixed, want: color.RGBA{R: mixedR, G: mixedG, B: mixedB, A: opaque}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := rgb(testCase.value); got != testCase.want {
				t.Errorf("rgb(%#x) = %v, want %v", testCase.value, got, testCase.want)
			}
		})
	}
}
