package theme

import (
	"image/color"
	"math"
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

// luminanceEpsilon bounds the tolerance for comparing linearise's and
// relativeLuminance's output against a known value: the gamma branch runs
// through math.Pow, and floating point arithmetic does not round-trip to an
// exact decimal.
const luminanceEpsilon = 0.000001

// Channel values for TestLinearise, chosen to land on both sides of the sRGB
// linear/gamma split: channel 10 is the last one still on the linear
// segment, channel 11 the first on the gamma curve.
const (
	channelLinearEdge = 10
	channelGammaEdge  = 11
	channelMid        = 128
)

// Expected linearise() outputs for the channel values above, computed once
// from the same WCAG formula the function implements, so the test is
// checking the arithmetic rather than re-deriving it inline.
const (
	linearEdgeWant = 0.0030352698
	gammaEdgeWant  = 0.0033465358
	midWant        = 0.2158605001
)

// TestLinearise checks both branches of the sRGB gamma undo: the flat linear
// segment near black and the power curve above it. Getting the threshold
// wrong would shift every dark colour's measured brightness, which is what
// Light and the contrast tests above build on to tell a torch-in-the-face
// field from a readable one.
func TestLinearise(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		channel uint8
		want    float64
	}{
		{name: "black is zero, on the linear segment", channel: 0, want: 0},
		{name: "the last channel on the linear segment", channel: channelLinearEdge, want: linearEdgeWant},
		{name: "the first channel on the gamma curve", channel: channelGammaEdge, want: gammaEdgeWant},
		{name: "a mid value uses the gamma curve", channel: channelMid, want: midWant},
		{name: "white is one, at the top of the gamma curve", channel: byteMask, want: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := linearise(testCase.channel); math.Abs(got-testCase.want) > luminanceEpsilon {
				t.Errorf("linearise(%d) = %.10f, want %.10f", testCase.channel, got, testCase.want)
			}
		})
	}
}

// TestRelativeLuminance checks the three-channel weighting against values that
// can be worked out by hand: a pure channel's relative luminance is exactly
// that channel's WCAG coefficient, because the other two contribute nothing.
// A weight mistyped or transposed here would shift where Light draws its
// line without ever showing up in a test that only feeds it black and white.
func TestRelativeLuminance(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value uint32
		want  float64
	}{
		{name: "black is zero", value: packedBlack, want: 0},
		{name: "white is one", value: packedWhite, want: 1},
		{name: "pure red weighs in at the red coefficient", value: packedRed, want: wcagRedWeight},
		{name: "pure green weighs in at the green coefficient", value: packedGreen, want: wcagGreenWeight},
		{name: "pure blue weighs in at the blue coefficient", value: packedBlue, want: wcagBlueWeight},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := relativeLuminance(rgb(testCase.value))
			if math.Abs(got-testCase.want) > luminanceEpsilon {
				t.Errorf("relativeLuminance(%#x) = %.6f, want %.6f", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestLookPalettes checks the night/day pair each look resolves to, including
// the default branch: an unrecognised look reads as glass, which is what
// keeps a Look built from unvalidated input safe to resolve instead of a
// panic waiting for a string Parse never saw.
func TestLookPalettes(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		look      Look
		wantNight Palette
		wantDay   Palette
	}{
		{name: "glass", look: LookGlass, wantNight: Night, wantDay: Day},
		{name: "phosphor", look: LookPhosphor, wantNight: PhosphorNight, wantDay: PhosphorDay},
		{name: "mono", look: LookMono, wantNight: MonoNight, wantDay: MonoDay},
		{name: "the zero value reads as glass", look: Look(""), wantNight: Night, wantDay: Day},
		{name: "an unrecognised look falls back to glass", look: Look("sepia"), wantNight: Night, wantDay: Day},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			night, day := testCase.look.palettes()

			if night != testCase.wantNight {
				t.Errorf("%v.palettes() night = %v, want %v", testCase.look, night, testCase.wantNight)
			}

			if day != testCase.wantDay {
				t.Errorf("%v.palettes() day = %v, want %v", testCase.look, day, testCase.wantDay)
			}
		})
	}
}
