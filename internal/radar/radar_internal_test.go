package radar

import (
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/airlines"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
)

// Headings at and around the compass's eight point boundaries, plus the
// values that arrive off the air rather than off a protractor.
const (
	headingNorth           = 0.0
	headingBelowNEBoundary = 22.4
	headingOnNEBoundary    = 22.5
	headingNE              = 45.0
	headingEast            = 90.0
	headingSouth           = 180.0
	headingWest            = 270.0
	headingWrapBoundary    = 337.5
	headingBelowFullTurn   = 359.9
	headingFullTurn        = 360.0
	headingTwoFullTurns    = 720.0
	headingNegative        = -45.0
)

func TestCompass(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		heading float64
		want    string
	}{
		{name: "exactly north", heading: headingNorth, want: "N"},
		{name: "just under the NE boundary stays north", heading: headingBelowNEBoundary, want: "N"},
		{name: "exactly on the NE boundary", heading: headingOnNEBoundary, want: "NE"},
		{name: "exactly NE", heading: headingNE, want: "NE"},
		{name: "exactly east", heading: headingEast, want: "E"},
		{name: "exactly south", heading: headingSouth, want: "S"},
		{name: "exactly west", heading: headingWest, want: "W"},
		{name: "the wrap boundary rounds forward into north", heading: headingWrapBoundary, want: "N"},
		{name: "just under a full turn", heading: headingBelowFullTurn, want: "N"},
		{name: "exactly a full turn", heading: headingFullTurn, want: "N"},
		{name: "two full turns", heading: headingTwoFullTurns, want: "N"},
		{name: "a negative heading wraps backward", heading: headingNegative, want: "NW"},
		{name: "NaN reads as north rather than panicking", heading: math.NaN(), want: "N"},
		{name: "positive infinity reads as north", heading: math.Inf(1), want: "N"},
		{name: "negative infinity reads as north", heading: math.Inf(-1), want: "N"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := compass(testCase.heading); got != testCase.want {
				t.Errorf("compass(%v) = %q, want %q", testCase.heading, got, testCase.want)
			}
		})
	}
}

// Boundary values shared by the number formatters below: either side of a
// thousands separator, and the sentinel uAirwaves uses for an unknown value.
const (
	justUnderThousand = 999.0
	exactlyThousand   = 1000.0
	exactlyMillion    = 1000000.0
	negativeWhole     = -42.0
)

// Case names repeated across several of the formatter tables below, named
// once so goconst does not flag the repetition.
const (
	caseZero              = "zero"
	caseNegative          = "negative"
	caseNaNReadsAsDash    = "NaN reads as a dash"
	caseJustUnderThousand = "just under a thousand"
)

func TestSceneWhole(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: caseZero, value: 0, want: "0"},
		{name: caseNegative, value: negativeWhole, want: "-42"},
		{name: caseNaNReadsAsDash, value: math.NaN(), want: "-"},
		{name: "positive infinity reads as a dash", value: math.Inf(1), want: "-"},
		{name: "negative infinity reads as a dash", value: math.Inf(-1), want: "-"},
		{name: caseJustUnderThousand, value: justUnderThousand, want: "999"},
		{name: "exactly a thousand, no grouping", value: exactlyThousand, want: "1000"},
		{name: "a million, no grouping", value: exactlyMillion, want: "1000000"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.whole(testCase.value)); got != testCase.want {
				t.Errorf("whole(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// fixedDecimals is the decimal count every fixed() case in this file is
// written with, matching how the card calls it for a distance.
const fixedDecimals = 1

func TestSceneFixed(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: caseZero, value: 0, want: "0.0"},
		{name: caseNegative, value: -1.5, want: "-1.5"},
		{name: caseNaNReadsAsDash, value: math.NaN(), want: "-"},
		{name: "positive infinity reads as a dash", value: math.Inf(1), want: "-"},
		{name: "negative infinity reads as a dash", value: math.Inf(-1), want: "-"},
		{name: caseJustUnderThousand, value: justUnderThousand, want: "999.0"},
		{name: "exactly a thousand", value: exactlyThousand, want: "1000.0"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.fixed(testCase.value, fixedDecimals)); got != testCase.want {
				t.Errorf("fixed(%v, %d) = %q, want %q", testCase.value, fixedDecimals, got, testCase.want)
			}
		})
	}
}

func TestSceneCount(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value int
		want  string
	}{
		{name: caseZero, value: 0, want: "0"},
		{name: caseNegative, value: -5, want: "-5"},
		{name: caseJustUnderThousand, value: 999, want: "999"},
		{name: "a million", value: 1000000, want: "1000000"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.count(testCase.value)); got != testCase.want {
				t.Errorf("count(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

func TestSceneCounter(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value uint64
		want  string
	}{
		{name: caseZero, value: 0, want: "0"},
		{name: caseJustUnderThousand, value: 999, want: "999"},
		{name: "exactly a thousand", value: 1000, want: "1000"},
		{name: "a million", value: 1000000, want: "1000000"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.counter(testCase.value)); got != testCase.want {
				t.Errorf("counter(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestSceneIndex checks the leading-zero padding under ten. A negative
// position never arrives in production (the card clamps to notSelected
// before calling index), but the buffer arithmetic still has to not panic on
// one.
func TestSceneIndex(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value int
		want  string
	}{
		{name: "zero gets a leading zero", value: 0, want: "00"},
		{name: "single digit gets a leading zero", value: 5, want: "05"},
		{name: "ten needs no padding", value: 10, want: "10"},
		{name: "three digits", value: 100, want: "100"},
		{name: "a negative value is not padded", value: -3, want: "-3"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.index(testCase.value)); got != testCase.want {
				t.Errorf("index(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// Headings that exercise degrees' own leading-zero and wraparound logic,
// distinct from the compass boundary values above.
const (
	headingBelowHundred  = 41.0
	headingNegativeSmall = -30.0
)

func TestSceneDegrees(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: caseZero, value: headingNorth, want: "000"},
		{name: "under a hundred gets two leading zeros", value: headingBelowHundred, want: "041"},
		{name: "negative wraps into the positive range", value: headingNegativeSmall, want: "330"},
		{name: "NaN reads as due north", value: math.NaN(), want: "000"},
		{name: "positive infinity reads as due north", value: math.Inf(1), want: "000"},
		{name: "negative infinity reads as due north", value: math.Inf(-1), want: "000"},
		{name: "just under a thousand wraps around the circle", value: justUnderThousand, want: "279"},
		{name: "exactly a thousand wraps around the circle", value: exactlyThousand, want: "280"},
		{name: "a million wraps around the circle", value: exactlyMillion, want: "280"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.degrees(testCase.value)); got != testCase.want {
				t.Errorf("degrees(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

func TestSceneThousands(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: caseZero, value: 0, want: "0"},
		{name: "no grouping needed", value: 12, want: "12"},
		{name: "negative groups past the sign", value: -1234567, want: "-1,234,567"},
		{name: "just under a thousand, no comma yet", value: justUnderThousand, want: "999"},
		{name: "exactly a thousand gets one comma", value: exactlyThousand, want: "1,000"},
		{name: "a million gets two commas", value: exactlyMillion, want: "1,000,000"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.thousands(testCase.value)); got != testCase.want {
				t.Errorf("thousands(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// A latitude close enough to Schiphol to be a realistic fixture, used only
// to exercise coordinate's sign and formatting, not to mean anything
// geographically.
const coordinateSampleLat = 52.1234

func TestSceneCoordinate(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: "zero reads as the positive hemisphere", value: 0, want: "0.0000 N"},
		{name: "positive reads as the positive hemisphere", value: coordinateSampleLat, want: "52.1234 N"},
		{name: "negative reads as the negative hemisphere", value: -coordinateSampleLat, want: "52.1234 S"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.coordinate(testCase.value, 'N', 'S')); got != testCase.want {
				t.Errorf("coordinate(%v, 'N', 'S') = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestSceneDistance checks the one sentinel the card has to catch: uAirwaves
// reports math.MaxFloat64 for a position nobody has decoded yet, and that has
// to render as a dash rather than as a very large number of nautical miles.
func TestSceneDistance(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: caseZero, value: 0, want: "0.0"},
		{name: "an ordinary distance", value: justUnderThousand, want: "999.0"},
		{name: "the unknown-position sentinel reads as a dash", value: math.MaxFloat64, want: "-"},
		{name: caseNaNReadsAsDash, value: math.NaN(), want: "-"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.distance(testCase.value)); got != testCase.want {
				t.Errorf("distance(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

func TestClip(t *testing.T) {
	t.Parallel()

	const (
		clipRunes       = 4
		clipExactLength = "abcd"
	)

	for _, testCase := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty string", value: "", want: ""},
		{name: "shorter than the limit", value: "ab", want: "ab"},
		{name: "exactly the limit", value: clipExactLength, want: clipExactLength},
		{name: "longer than the limit", value: clipExactLength + "efgh", want: clipExactLength},
		{name: "multi-byte runes are not split mid-rune", value: "café" + "au lait", want: "café"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := clip(testCase.value, clipRunes); got != testCase.want {
				t.Errorf("clip(%q, %d) = %q, want %q", testCase.value, clipRunes, got, testCase.want)
			}
		})
	}
}

func TestMix(t *testing.T) {
	t.Parallel()

	const (
		mixField uint8 = 0
		mixInk   uint8 = 200
	)

	for _, testCase := range []struct {
		name  string
		alpha float64
		want  uint8
	}{
		{name: "alpha zero is the source colour", alpha: 0, want: mixField},
		{name: "alpha one is the target colour", alpha: 1, want: mixInk},
		{name: "alpha one half is the midpoint", alpha: 0.5, want: 100},
		{name: "alpha below zero clamps to the source colour", alpha: -1, want: mixField},
		{name: "alpha above one clamps to the target colour", alpha: 2, want: mixInk},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := mix(mixField, mixInk, testCase.alpha); got != testCase.want {
				t.Errorf("mix(%d, %d, %v) = %d, want %d", mixField, mixInk, testCase.alpha, got, testCase.want)
			}
		})
	}

	// A NaN alpha reads as fully transparent, which puts the source colour
	// through untouched. It is asserted rather than merely survived because
	// min and max propagate a NaN instead of pinning it, and converting one to
	// uint8 is undefined in the spec: without the guard in mix this would be
	// whatever the compiler produced that day.
	t.Run("alpha NaN leaves the source colour alone", func(t *testing.T) {
		t.Parallel()

		if got := mix(mixField, mixInk, math.NaN()); got != mixField {
			t.Errorf("mix(%d, %d, NaN) = %d, want %d", mixField, mixInk, got, mixField)
		}
	})
}

func TestSceneFade(t *testing.T) {
	t.Parallel()

	field := color.RGBA{R: 0, G: 0, B: 0, A: opaque}
	trail := color.RGBA{R: 200, G: 100, B: 50, A: opaque}
	pal := theme.Palette{Field: field}

	for _, testCase := range []struct {
		name  string
		alpha float64
		want  color.RGBA
	}{
		{name: "alpha zero is the field", alpha: 0, want: field},
		{name: "alpha one is the colour itself", alpha: 1, want: trail},
		{name: "alpha below zero clamps to the field", alpha: -1, want: field},
		{name: "alpha above one clamps to the colour itself", alpha: 2, want: trail},
		{name: "NaN clamps to the field", alpha: math.NaN(), want: field},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: pal}
			if got := scene.fade(trail, testCase.alpha); got != testCase.want {
				t.Errorf("fade(%v, %v) = %v, want %v", trail, testCase.alpha, got, testCase.want)
			}
		})
	}
}

// Altitudes either side of bandColour's two ceilings, plus one well clear of
// both.
const (
	negativeAltitude = -100.0
	justBelowLowBand = 9999.0
	justBelowMidBand = 24999.0
	wellAboveMidBand = 40000.0
)

func TestBandColour(t *testing.T) {
	t.Parallel()

	pal := theme.Palette{
		Muted:   color.RGBA{R: 1, A: opaque},
		AltLow:  color.RGBA{R: 2, A: opaque},
		AltMid:  color.RGBA{R: 3, A: opaque},
		AltHigh: color.RGBA{R: 4, A: opaque},
	}

	for _, testCase := range []struct {
		name     string
		altitude float64
		want     color.RGBA
	}{
		{name: "zero reads as unknown, not sea level", altitude: 0, want: pal.Muted},
		{name: "negative also reads as unknown", altitude: negativeAltitude, want: pal.Muted},
		{name: "just under the low ceiling", altitude: justBelowLowBand, want: pal.AltLow},
		{name: "exactly the low ceiling moves into the mid band", altitude: lowCeiling, want: pal.AltMid},
		{name: "just under the mid ceiling", altitude: justBelowMidBand, want: pal.AltMid},
		{name: "exactly the mid ceiling moves into the high band", altitude: midCeiling, want: pal.AltHigh},
		{name: "well above the mid ceiling", altitude: wellAboveMidBand, want: pal.AltHigh},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: pal}
			if got := scene.bandColour(testCase.altitude); got != testCase.want {
				t.Errorf("bandColour(%v) = %v, want %v", testCase.altitude, got, testCase.want)
			}
		})
	}
}

func TestLineHeight(t *testing.T) {
	t.Parallel()

	body, err := fonts.Body()
	if err != nil {
		t.Fatalf("fonts.Body: %v", err)
	}

	for _, testCase := range []struct {
		name string
		face *psf.Font
		want int
	}{
		{name: "a nil face has no height", face: nil, want: 0},
		{name: "a real face reports its own height", face: body, want: body.Height()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := lineHeight(testCase.face); got != testCase.want {
				t.Errorf("lineHeight(...) = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestGlyphWidth(t *testing.T) {
	t.Parallel()

	body, err := fonts.Body()
	if err != nil {
		t.Fatalf("fonts.Body: %v", err)
	}

	for _, testCase := range []struct {
		name string
		face *psf.Font
		want int
	}{
		{name: "a nil face has no width", face: nil, want: 0},
		{name: "a real face reports its own width", face: body, want: body.Width()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := glyphWidth(testCase.face); got != testCase.want {
				t.Errorf("glyphWidth(...) = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestIndexOf(t *testing.T) {
	t.Parallel()

	icaos := []string{"AAA111", "BBB222", "CCC333"}

	for _, testCase := range []struct {
		name      string
		list      []string
		search    string
		wantIndex int
	}{
		{name: "an empty search string reports a miss", list: icaos, search: "", wantIndex: -1},
		{name: "a hit reports its position", list: icaos, search: "BBB222", wantIndex: 1},
		{name: "a miss reports -1", list: icaos, search: "ZZZ999", wantIndex: -1},
		{name: "an empty list reports a miss", list: nil, search: "AAA111", wantIndex: -1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := indexOf(testCase.list, testCase.search); got != testCase.wantIndex {
				t.Errorf("indexOf(%v, %q) = %d, want %d", testCase.list, testCase.search, got, testCase.wantIndex)
			}
		})
	}
}

// trackWindowFit is the number of rows on screen every trackWindow case in
// this file uses, and trackWindowCount the size of the aircraft list, unless
// a case overrides one to make a particular branch fire.
const (
	trackWindowFit   = 5
	trackWindowCount = 20
)

func TestTrackWindow(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		selIndex     int
		initialStart int
		fit          int
		count        int
		wantRowStart int
	}{
		{
			name: "no selection resets the window to the top", selIndex: -1,
			initialStart: 7, fit: trackWindowFit, count: trackWindowCount, wantRowStart: 0,
		},
		{
			name: "a selection above the window pulls it up", selIndex: 2,
			initialStart: 5, fit: trackWindowFit, count: trackWindowCount, wantRowStart: 2,
		},
		{
			name: "a selection below the window pushes it down", selIndex: 12,
			initialStart: 0, fit: trackWindowFit, count: trackWindowCount, wantRowStart: 8,
		},
		{
			// A stale window from a previous, longer list is not itself moved
			// by either branch (the selection is already inside it), so only
			// the final clamp keeps it from going negative.
			name: "a stale negative window clamps to zero", selIndex: 2,
			initialStart: -5, fit: trackWindowCount, count: trackWindowCount, wantRowStart: 0,
		},
		{
			// A stale window left over from a much longer list is not moved by
			// either branch either, so only the final clamp pulls it back
			// under the shrunk list's own count.
			name: "a stale window past a shrunk list clamps to the end", selIndex: 52,
			initialStart: 50, fit: trackWindowFit, count: 10, wantRowStart: 5,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{selIndex: testCase.selIndex, rowStart: testCase.initialStart}
			scene.trackWindow(testCase.fit, testCase.count)

			if scene.rowStart != testCase.wantRowStart {
				t.Errorf("rowStart = %d, want %d", scene.rowStart, testCase.wantRowStart)
			}
		})
	}
}

// geometryBoxSide is the side of the square box every geometry case in this
// file measures unless it is specifically testing a degenerate box.
const geometryBoxSide = 200

func TestGeometry(t *testing.T) {
	t.Parallel()

	body, err := fonts.Body()
	if err != nil {
		t.Fatalf("fonts.Body: %v", err)
	}

	insetWithoutLabels := cardinalGap
	insetWithLabels := cardinalGap + body.Height()

	for _, testCase := range []struct {
		name   string
		box    image.Rectangle
		inset  int
		wantOK bool
	}{
		{
			name: "a degenerate zero-width box reports false",
			box:  image.Rect(0, 0, 0, geometryBoxSide), inset: insetWithoutLabels, wantOK: false,
		},
		{
			name: "a box too small for the inset it must leave reports false",
			box:  image.Rect(0, 0, insetWithLabels, insetWithLabels), inset: insetWithLabels, wantOK: false,
		},
		{
			name: "labels off leaves room for the rings",
			box:  image.Rect(0, 0, geometryBoxSide, geometryBoxSide), inset: insetWithoutLabels, wantOK: true,
		},
		{
			name: "labels on still leaves room for the rings, just less of it",
			box:  image.Rect(0, 0, geometryBoxSide, geometryBoxSide), inset: insetWithLabels, wantOK: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			geom, ok := geometry(testCase.box, testCase.inset)
			if ok != testCase.wantOK {
				t.Fatalf("geometry(...) ok = %v, want %v", ok, testCase.wantOK)
			}

			if ok && geom.rangeR <= 0 {
				t.Errorf("geometry(...) rangeR = %d, want > 0", geom.rangeR)
			}
		})
	}

	t.Run("labels take more room than no labels", func(t *testing.T) {
		t.Parallel()

		box := image.Rect(0, 0, geometryBoxSide, geometryBoxSide)

		withLabels, _ := geometry(box, insetWithLabels)
		withoutLabels, _ := geometry(box, insetWithoutLabels)

		if withLabels.rangeR >= withoutLabels.rangeR {
			t.Errorf("rangeR with labels = %d, want less than without labels (%d)",
				withLabels.rangeR, withoutLabels.rangeR)
		}
	})
}

// The projector fixture every newProjector/at case in this file builds
// around, standing in for a receiver a little south of Schiphol.
const (
	projCenterX = 100
	projCenterY = 100
	projRangeR  = 90
	projScopeNm = 60.0
	projLat0    = 52.0
	projLon0    = 4.0

	// nmPastEdge is how far past the current range a position must sit to be
	// dropped rather than clipped.
	nmPastEdge = 1.0
)

func validProjectorGeometry() scopeGeometry {
	return scopeGeometry{centerX: projCenterX, centerY: projCenterY, rangeR: projRangeR}
}

func TestNewProjector(t *testing.T) {
	t.Parallel()

	validReceiver := source.Receiver{Latitude: projLat0, Longitude: projLon0}

	for _, testCase := range []struct {
		name     string
		geom     scopeGeometry
		receiver source.Receiver
		scopeNm  float64
	}{
		{name: "a non-positive rangeR", geom: scopeGeometry{rangeR: 0}, receiver: validReceiver, scopeNm: projScopeNm},
		{name: "a non-positive scope range", geom: validProjectorGeometry(), receiver: validReceiver, scopeNm: 0},
		{
			name: "a NaN scope range", geom: validProjectorGeometry(),
			receiver: validReceiver, scopeNm: math.NaN(),
		},
		{
			name: "a receiver with no position yet", geom: validProjectorGeometry(),
			receiver: source.Receiver{}, scopeNm: projScopeNm,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, ok := newProjector(testCase.geom, testCase.receiver, testCase.scopeNm); ok {
				t.Error("newProjector(...) ok = true, want false")
			}
		})
	}

	t.Run("a valid receiver and range build a usable projector", func(t *testing.T) {
		t.Parallel()

		if _, ok := newProjector(validProjectorGeometry(), validReceiver, projScopeNm); !ok {
			t.Error("newProjector(...) ok = false, want true")
		}
	})
}

func TestProjectorAt(t *testing.T) {
	t.Parallel()

	proj, ok := newProjector(validProjectorGeometry(), source.Receiver{Latitude: projLat0, Longitude: projLon0},
		projScopeNm)
	if !ok {
		t.Fatal("newProjector(...) ok = false, want true")
	}

	t.Run("the centre projects onto the centre pixel", func(t *testing.T) {
		t.Parallel()

		x, y, inside := proj.at(projLat0, projLon0)
		if !inside || x != projCenterX || y != projCenterY {
			t.Errorf("at(centre) = (%d, %d, %v), want (%d, %d, true)", x, y, inside, projCenterX, projCenterY)
		}
	})

	t.Run("a position of exactly (0, 0) is skipped", func(t *testing.T) {
		t.Parallel()

		if _, _, inside := proj.at(0, 0); inside {
			t.Error("at(0, 0) reported inside, want it skipped")
		}
	})

	t.Run("a position exactly on the range boundary lands inside", func(t *testing.T) {
		t.Parallel()

		// One degree of latitude north is nmPerDegree nautical miles by the
		// projector's own definition, so this sits exactly on the range.
		latOnEdge := projLat0 + projScopeNm/nmPerDegree

		if _, _, inside := proj.at(latOnEdge, projLon0); !inside {
			t.Error("at(edge of range) reported outside, want it inside")
		}
	})

	t.Run("a position just outside the range is dropped", func(t *testing.T) {
		t.Parallel()

		latOutside := projLat0 + (projScopeNm+nmPastEdge)/nmPerDegree

		if _, _, inside := proj.at(latOutside, projLon0); inside {
			t.Error("at(just outside the range) reported inside, want it dropped")
		}
	})

	t.Run("a NaN position is dropped rather than propagated", func(t *testing.T) {
		t.Parallel()

		if _, _, inside := proj.at(math.NaN(), projLon0); inside {
			t.Error("at(NaN, lon) reported inside, want it dropped")
		}
	})
}

func TestLayoutFits(t *testing.T) {
	t.Parallel()

	const fitsTestHeight = 50

	for _, testCase := range []struct {
		name   string
		top    int
		bottom int
		want   bool
	}{
		{name: "comfortably fits", top: 0, bottom: fitsTestHeight * 2, want: true},
		{name: "fits exactly at the boundary", top: 0, bottom: fitsTestHeight, want: true},
		{name: "one pixel too tall does not fit", top: 0, bottom: fitsTestHeight - 1, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lay := layout{top: testCase.top, bottom: testCase.bottom}
			if got := lay.fits(fitsTestHeight); got != testCase.want {
				t.Errorf("fits(%d) = %v, want %v", fitsTestHeight, got, testCase.want)
			}
		})
	}
}

// Canvas and box sizes for the split() cases below. wideCanvasWidth sits
// above minColumnWidth so the wide branch runs; narrowCanvasWidth sits below
// it so the column is dropped. noGapBoxRight is worked out so the box is
// wide enough for the scope square but leaves less than blockGap beside it,
// which is what makes split() clear a column it had already started to lay
// out.
const (
	wideCanvasWidth  = 800
	wideCanvasHeight = 600

	narrowCanvasWidth = 500
	narrowBoxHeight   = 300

	noGapBoxRight = wideCanvasHeight + blockGap
)

func TestLayoutSplit(t *testing.T) {
	t.Parallel()

	t.Run("a non-positive width leaves scope and column empty", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(wideCanvasWidth, wideCanvasHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		lay := layout{dst: canv, left: wideCanvasWidth, right: 0, top: 0, bottom: wideCanvasHeight}
		lay.split()

		if !lay.scope.Empty() || !lay.column.Empty() {
			t.Errorf("split() = scope %v column %v, want both empty", lay.scope, lay.column)
		}
	})

	t.Run("a narrow canvas centres the scope and drops the column", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(narrowCanvasWidth, wideCanvasHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		lay := layout{dst: canv, left: 0, right: narrowCanvasWidth, top: 0, bottom: narrowBoxHeight}
		lay.split()

		side := narrowBoxHeight
		wantLeft := (narrowCanvasWidth - side) / 2
		want := image.Rect(wantLeft, 0, wantLeft+side, side)

		if lay.scope != want {
			t.Errorf("scope = %v, want %v", lay.scope, want)
		}

		if !lay.column.Empty() {
			t.Errorf("column = %v, want empty", lay.column)
		}
	})

	t.Run("a wide canvas keeps the column when there is room beside the scope", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(wideCanvasWidth, wideCanvasHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		lay := layout{dst: canv, left: 0, right: wideCanvasWidth, top: 0, bottom: wideCanvasHeight}
		lay.split()

		if lay.column.Empty() || lay.column.Dx() <= 0 {
			t.Errorf("column = %v, want a non-empty column", lay.column)
		}
	})

	t.Run("a wide canvas with no room for the gap drops the column anyway", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(wideCanvasWidth, wideCanvasHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		lay := layout{dst: canv, left: 0, right: noGapBoxRight, top: 0, bottom: wideCanvasHeight}
		lay.split()

		if !lay.column.Empty() {
			t.Errorf("column = %v, want empty when the side leaves no room for the gap", lay.column)
		}
	})
}

// sampleCallsign is a plain KLM callsign, reused wherever a test needs a
// realistic value and does not care which operator it names.
const sampleCallsign = "KLM123"

func TestCallsignOf(t *testing.T) {
	t.Parallel()

	const callsignTestICAO = "ABC123"

	for _, testCase := range []struct {
		name  string
		plane airplane.Snapshot
		want  string
	}{
		{
			name: "a decoded callsign wins", want: sampleCallsign,
			plane: airplane.Snapshot{ICAO: callsignTestICAO, Callsign: sampleCallsign},
		},
		{
			name: "an empty callsign falls back to the ICAO hex", want: callsignTestICAO,
			plane: airplane.Snapshot{ICAO: callsignTestICAO},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := callsignOf(testCase.plane); got != testCase.want {
				t.Errorf("callsignOf(%+v) = %q, want %q", testCase.plane, got, testCase.want)
			}
		})
	}
}

// TestParseColour checks the allow list --colour reads: both spellings, and
// everything else refused with an error errors.Is can match against ErrColour.
func TestParseColour(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		input   string
		want    ColourMode
		wantErr bool
	}{
		{name: "altitude", input: "altitude", want: ColourAltitude},
		{name: "airline", input: "airline", want: ColourAirline},
		{name: "empty string is rejected", input: "", want: ColourAltitude, wantErr: true},
		{name: "wrong case is rejected", input: "Airline", want: ColourAltitude, wantErr: true},
		{name: "a near-miss spelling is rejected", input: "altitudes", want: ColourAltitude, wantErr: true},
		{name: "an unrelated word is rejected", input: "rainbow", want: ColourAltitude, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseColour(testCase.input)
			if got != testCase.want {
				t.Errorf("ParseColour(%q) = %q, want %q", testCase.input, got, testCase.want)
			}

			if testCase.wantErr {
				if !errors.Is(err, ErrColour) {
					t.Errorf("ParseColour(%q) err = %v, want it to match ErrColour", testCase.input, err)
				}

				return
			}

			if err != nil {
				t.Errorf("ParseColour(%q) unexpected error: %v", testCase.input, err)
			}
		})
	}
}

// TestColourModeNext checks the c key's cycle, including the zero value a
// Scene never actually holds but that ColourMode as an exported type allows.
func TestColourModeNext(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		mode ColourMode
		want ColourMode
	}{
		{name: "altitude moves to airline", mode: ColourAltitude, want: ColourAirline},
		{name: "airline moves to altitude", mode: ColourAirline, want: ColourAltitude},
		{name: "the zero value moves to airline", mode: ColourMode(""), want: ColourAirline},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.mode.Next(); got != testCase.want {
				t.Errorf("Next() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestTallyCounting checks that several aircraft of the same operator collapse
// into one entry with the count they add up to.
func TestTallyCounting(t *testing.T) {
	t.Parallel()

	var counts tally

	counts.add("KLM101")
	counts.add("KLM202")
	counts.add("KLM303")

	if counts.count != 1 {
		t.Fatalf("count = %d, want 1 distinct operator", counts.count)
	}

	if got := counts.seen[0].count; got != 3 {
		t.Errorf("seen[0].count = %d, want 3", got)
	}

	if got := counts.seen[0].airline.ICAO; got != "KLM" {
		t.Errorf("seen[0].airline.ICAO = %q, want KLM", got)
	}
}

// TestTallyOther checks the two separate ways an aircraft ends up counted
// against OTHER: an unrecognised prefix, and no callsign at all.
func TestTallyOther(t *testing.T) {
	t.Parallel()

	t.Run("an unrecognised prefix sets other", func(t *testing.T) {
		t.Parallel()

		var counts tally
		counts.add("LFV21")

		if !counts.other {
			t.Error("other = false after an unrecognised prefix, want true")
		}

		if counts.count != 0 {
			t.Errorf("count = %d after an unrecognised prefix, want 0", counts.count)
		}
	})

	t.Run("an empty callsign sets other", func(t *testing.T) {
		t.Parallel()

		var counts tally
		counts.add("")

		if !counts.other {
			t.Error("other = false after an empty callsign, want true")
		}
	})
}

// TestTallyRank checks the ordering rank leaves the table in: busiest
// operator first, and the designator breaking a tie on count.
func TestTallyRank(t *testing.T) {
	t.Parallel()

	t.Run("orders by count descending", func(t *testing.T) {
		t.Parallel()

		var counts tally

		counts.add("EIN101")
		counts.add("KLM101")
		counts.add("KLM202")
		counts.add("KLM303")
		counts.add("DLH101")
		counts.add("DLH202")

		counts.rank()

		if counts.shown != 3 {
			t.Fatalf("shown = %d, want 3", counts.shown)
		}

		for index, want := range []string{"KLM", "DLH", "EIN"} {
			if got := counts.seen[index].airline.ICAO; got != want {
				t.Errorf("seen[%d].airline.ICAO = %q, want %q", index, got, want)
			}
		}
	})

	t.Run("the designator tie-break holds when counts are level", func(t *testing.T) {
		t.Parallel()

		var counts tally

		// Added in the order that would leave RYR first if rank did not
		// tie-break: it is the one added first, and both end up level at one.
		counts.add("RYR101")
		counts.add("BAW101")

		counts.rank()

		if got := counts.seen[0].airline.ICAO; got != "BAW" {
			t.Errorf("seen[0].airline.ICAO = %q, want BAW (alphabetically first on a tied count)", got)
		}
	})
}

// TestTallyReset checks that reset empties every field, not just the count.
func TestTallyReset(t *testing.T) {
	t.Parallel()

	var counts tally

	counts.add("KLM101")
	counts.add("LFV21")
	counts.rank()

	counts.reset()

	if counts.count != 0 || counts.shown != 0 || counts.other {
		t.Errorf("after reset count=%d shown=%d other=%v, want all zero/false",
			counts.count, counts.shown, counts.other)
	}
}

// TestTallyTableFull drives the branch a scope covering a few hundred
// nautical miles could plausibly reach: more distinct operators than the
// fixed table holds.
func TestTallyTableFull(t *testing.T) {
	t.Parallel()

	all := airlines.All()
	if len(all) <= maxOperators {
		t.Fatalf("need more than %d airlines to drive this branch, database has %d", maxOperators, len(all))
	}

	var counts tally
	for _, airline := range all {
		counts.add(airline.ICAO + "1")
	}

	if counts.count != maxOperators {
		t.Errorf("count = %d, want the table full at %d", counts.count, maxOperators)
	}

	if !counts.other {
		t.Error("other = false, want true once the table is full")
	}
}

// TestOutranks checks the legend's ordering rule on its own: count first,
// designator as the tie-break.
func TestOutranks(t *testing.T) {
	t.Parallel()

	// The two designators outranks compares, named once so goconst does not
	// flag the repetition across the table below.
	const (
		outranksFirst  = "AAA"
		outranksSecond = "ZZZ"
	)

	for _, testCase := range []struct {
		name        string
		left, right operatorCount
		want        bool
	}{
		{
			name:  "a higher count outranks a lower one",
			left:  operatorCount{count: 5, airline: airlines.Airline{ICAO: outranksSecond}},
			right: operatorCount{count: 3, airline: airlines.Airline{ICAO: outranksFirst}},
			want:  true,
		},
		{
			name:  "a lower count does not outrank a higher one",
			left:  operatorCount{count: 3, airline: airlines.Airline{ICAO: outranksFirst}},
			right: operatorCount{count: 5, airline: airlines.Airline{ICAO: outranksSecond}},
			want:  false,
		},
		{
			name:  "a level count falls to the earlier designator",
			left:  operatorCount{count: 2, airline: airlines.Airline{ICAO: outranksFirst}},
			right: operatorCount{count: 2, airline: airlines.Airline{ICAO: outranksSecond}},
			want:  true,
		},
		{
			name:  "a level count does not outrank the later designator",
			left:  operatorCount{count: 2, airline: airlines.Airline{ICAO: outranksSecond}},
			right: operatorCount{count: 2, airline: airlines.Airline{ICAO: outranksFirst}},
			want:  false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := outranks(testCase.left, testCase.right); got != testCase.want {
				t.Errorf("outranks(%+v, %+v) = %v, want %v", testCase.left, testCase.right, got, testCase.want)
			}
		})
	}
}

// TestSeenBucket names every boundary the SEEN figure reports, exactly at the
// second each bucket turns over.
func TestSeenBucket(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		age  time.Duration
		want string
	}{
		{name: "a negative age reads as just now", age: -time.Second, want: seenNowText},
		{name: caseZero, age: 0, want: seenNowText},
		{name: "four seconds is still just now", age: 4 * time.Second, want: seenNowText},
		{name: "five seconds moves to the quarter-minute bucket", age: 5 * time.Second, want: seenQuarterText},
		{name: "fourteen seconds is still the quarter-minute bucket", age: 14 * time.Second, want: seenQuarterText},
		{name: "fifteen seconds moves to the half-minute bucket", age: 15 * time.Second, want: seenHalfText},
		{name: "twenty-nine seconds is still the half-minute bucket", age: 29 * time.Second, want: seenHalfText},
		{name: "thirty seconds moves to the one-minute bucket", age: 30 * time.Second, want: seenMinuteText},
		{name: "fifty-nine seconds is still the one-minute bucket", age: 59 * time.Second, want: seenMinuteText},
		{name: "one minute moves to stale", age: time.Minute, want: seenStaleText},
		{name: "an hour is stale", age: time.Hour, want: seenStaleText},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := seenBucket(testCase.age); got != testCase.want {
				t.Errorf("seenBucket(%v) = %q, want %q", testCase.age, got, testCase.want)
			}
		})
	}
}

// TestLevel checks the one rule the details block and the compact rows both
// have to agree on: how small a vertical rate has to be to read as level.
func TestLevel(t *testing.T) {
	t.Parallel()

	const (
		levelJustUnder = 99.0
		climbFloor     = 100.0
		fastClimb      = 1800.0
	)

	for _, testCase := range []struct {
		name string
		rate float64
		want bool
	}{
		{name: "NaN reads as level", rate: math.NaN(), want: true},
		{name: "positive infinity reads as level", rate: math.Inf(1), want: true},
		{name: "negative infinity reads as level", rate: math.Inf(-1), want: true},
		{name: caseZero, rate: 0, want: true},
		{name: "just under the climb band is level", rate: levelJustUnder, want: true},
		{name: "just under the descent band is level", rate: -levelJustUnder, want: true},
		{name: "exactly the climb band is climbing", rate: climbFloor, want: false},
		{name: "exactly the descent band is descending", rate: -climbFloor, want: false},
		{name: "a fast climb is climbing", rate: fastClimb, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := level(testCase.rate); got != testCase.want {
				t.Errorf("level(%v) = %v, want %v", testCase.rate, got, testCase.want)
			}
		})
	}
}

// TestHasPosition checks the one sentinel every position-reading block in the
// scene defers to: exactly (0, 0) reads as undecoded, not as a real fix.
func TestHasPosition(t *testing.T) {
	t.Parallel()

	const positionSampleLon = 4.0

	for _, testCase := range []struct {
		name  string
		plane airplane.Snapshot
		want  bool
	}{
		{name: "exactly (0, 0) reads as undecoded", plane: airplane.Snapshot{}, want: false},
		{
			name: "a NaN latitude reads as undecoded", want: false,
			plane: airplane.Snapshot{Latitude: math.NaN(), Longitude: positionSampleLon},
		},
		{
			name: "a NaN longitude reads as undecoded", want: false,
			plane: airplane.Snapshot{Latitude: coordinateSampleLat, Longitude: math.NaN()},
		},
		{
			name: "a real position is decoded", want: true,
			plane: airplane.Snapshot{Latitude: coordinateSampleLat, Longitude: positionSampleLon},
		},
		{
			name: "only a longitude is still decoded", want: true,
			plane: airplane.Snapshot{Longitude: positionSampleLon},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := hasPosition(testCase.plane); got != testCase.want {
				t.Errorf("hasPosition(%+v) = %v, want %v", testCase.plane, got, testCase.want)
			}
		})
	}
}

// TestBearingTo checks the compass bearing the details block and the compact
// rows both read off a receiver and an aircraft, on the same flat-earth
// approximation the scope itself projects with.
func TestBearingTo(t *testing.T) {
	t.Parallel()

	const (
		bearingTestLat = 52.0
		bearingTestLon = 4.0
		bearingOffset  = 0.5

		// bearingTolerance allows for the flat-earth approximation bearingTo
		// itself documents as inexact away from the poles.
		bearingTolerance = 1.0
	)

	receiver := source.Receiver{Latitude: bearingTestLat, Longitude: bearingTestLon}

	t.Run("a receiver with no position reports false", func(t *testing.T) {
		t.Parallel()

		plane := airplane.Snapshot{Latitude: bearingTestLat + bearingOffset, Longitude: bearingTestLon}
		if _, known := bearingTo(source.Receiver{}, plane); known {
			t.Error("bearingTo(...) known = true, want false")
		}
	})

	t.Run("an aircraft with no position reports false", func(t *testing.T) {
		t.Parallel()

		if _, known := bearingTo(receiver, airplane.Snapshot{}); known {
			t.Error("bearingTo(...) known = true, want false")
		}
	})

	t.Run("an aircraft exactly on the receiver reports false", func(t *testing.T) {
		t.Parallel()

		plane := airplane.Snapshot{Latitude: bearingTestLat, Longitude: bearingTestLon}
		if _, known := bearingTo(receiver, plane); known {
			t.Error("bearingTo(...) known = true, want false")
		}
	})

	for _, testCase := range []struct {
		name  string
		plane airplane.Snapshot
		want  float64
	}{
		{
			name: "due north", want: headingNorth,
			plane: airplane.Snapshot{Latitude: bearingTestLat + bearingOffset, Longitude: bearingTestLon},
		},
		{
			name: "due east", want: headingEast,
			plane: airplane.Snapshot{Latitude: bearingTestLat, Longitude: bearingTestLon + bearingOffset},
		},
		{
			name: "due south", want: headingSouth,
			plane: airplane.Snapshot{Latitude: bearingTestLat - bearingOffset, Longitude: bearingTestLon},
		},
		{
			name: "due west", want: headingWest,
			plane: airplane.Snapshot{Latitude: bearingTestLat, Longitude: bearingTestLon - bearingOffset},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, known := bearingTo(receiver, testCase.plane)
			if !known {
				t.Fatal("bearingTo(...) known = false, want true")
			}

			if diff := math.Abs(got - testCase.want); diff > bearingTolerance {
				t.Errorf("bearingTo(...) = %v, want within %v of %v", got, bearingTolerance, testCase.want)
			}
		})
	}
}

// TestScenePlace checks the aircraft position formatter: two decimals and a
// hemisphere letter, one step coarser than the receiver's own coordinate.
func TestScenePlace(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: "zero reads as the positive hemisphere", value: 0, want: "0.00 N"},
		{name: "positive reads as the positive hemisphere", value: coordinateSampleLat, want: "52.12 N"},
		{name: "negative reads as the negative hemisphere", value: -coordinateSampleLat, want: "52.12 S"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.place(testCase.value, 'N', 'S')); got != testCase.want {
				t.Errorf("place(%v, 'N', 'S') = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestSceneBearing checks the one-field bearing the compact rows right-align
// as a unit: three digits, a separator, the compass point.
func TestSceneBearing(t *testing.T) {
	t.Parallel()

	const (
		bearingWestSample  = 264.0
		bearingNorthSample = 359.0
	)

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: caseZero, value: headingNorth, want: "000 / N"},
		{name: "west", value: bearingWestSample, want: "264 / W"},
		{name: "just under the wrap, still reads north", value: bearingNorthSample, want: "359 / N"},
		{name: "north-east", value: headingNE, want: "045 / NE"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.bearing(testCase.value)); got != testCase.want {
				t.Errorf("bearing(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// rowPlanFaces loads the one face planRows and rowPlan measure their columns
// against. A synthetic font would not exercise the real character widths the
// column arithmetic is built on.
func rowPlanFaces(tb testing.TB) *psf.Font {
	tb.Helper()

	body, err := fonts.Body()
	if err != nil {
		tb.Fatalf("fonts.Body: %v", err)
	}

	return body
}

// TestRowPlanGeometry drives rowPlan's own arithmetic directly: which columns
// count, how much room they need including the gap between them, and where
// place() puts each edge.
func TestRowPlanGeometry(t *testing.T) {
	t.Parallel()

	glyph := rowPlanFaces(t).Width()

	t.Run("count tallies only the enabled columns", func(t *testing.T) {
		t.Parallel()

		var plan rowPlan

		plan.on[colIndex] = true
		plan.on[colCallsign] = true

		if got := plan.count(); got != 2 {
			t.Errorf("count() = %d, want 2", got)
		}
	})

	t.Run("width sums the enabled columns plus the gap between them", func(t *testing.T) {
		t.Parallel()

		var plan rowPlan

		plan.on[colIndex] = true
		plan.on[colAltitude] = true

		want := rowColumns[colIndex].chars*glyph +
			rowColumns[colAltitude].chars*glyph + rowColumns[colAltitude].extra + columnGap

		if got := plan.width(glyph); got != want {
			t.Errorf("width(%d) = %d, want %d", glyph, got, want)
		}
	})

	t.Run("place with no slack puts the first edge at its own width and the last at the far edge", func(t *testing.T) {
		t.Parallel()

		var plan rowPlan

		plan.on[colIndex] = true
		plan.on[colCallsign] = true

		const left = 50

		available := plan.width(glyph)
		plan.place(left, available, glyph)

		if got, want := plan.edge[colIndex], left+rowColumns[colIndex].chars*glyph; got != want {
			t.Errorf("first edge = %d, want %d", got, want)
		}

		if got, want := plan.edge[colCallsign], left+available; got != want {
			t.Errorf("last edge = %d, want %d", got, want)
		}
	})

	t.Run("a single-column plan does not divide by zero", func(t *testing.T) {
		t.Parallel()

		var plan rowPlan

		plan.on[colSpeed] = true

		const left = 10

		available := plan.width(glyph)
		plan.place(left, available, glyph)

		if got, want := plan.edge[colSpeed], left+available; got != want {
			t.Errorf("edge = %d, want %d", got, want)
		}
	})
}

// rowPlanWithout builds a fully-enabled plan with the named columns switched
// off, mirroring how planRows drops columns one at a time.
func rowPlanWithout(exclude ...int) rowPlan {
	var plan rowPlan

	for index := range colCount {
		plan.on[index] = true
	}

	for _, drop := range exclude {
		plan.on[drop] = false
	}

	return plan
}

// TestPlanRows checks the column drop order as the available width narrows,
// and the two ways it refuses to draw at all: a width too narrow for even the
// identity, and a Scene with no body face to measure glyphs from.
func TestPlanRows(t *testing.T) {
	t.Parallel()

	body := rowPlanFaces(t)
	glyph := body.Width()
	scene := &Scene{faces: Faces{Body: body}}

	full := rowPlanWithout()
	noBearing := rowPlanWithout(colBearing)
	noSpeed := rowPlanWithout(colBearing, colSpeed)
	noICAO := rowPlanWithout(colBearing, colSpeed, colICAO)
	noAltitude := rowPlanWithout(colBearing, colSpeed, colICAO, colAltitude)
	identityOnly := rowPlanWithout(colBearing, colSpeed, colICAO, colAltitude, colDistance)

	const left = 0

	for _, testCase := range []struct {
		name   string
		right  int
		wantOn [colCount]bool
		wantOK bool
	}{
		{name: "a generous width keeps all seven columns", right: full.width(glyph), wantOn: full.on, wantOK: true},
		{name: "narrower drops bearing first", right: noBearing.width(glyph), wantOn: noBearing.on, wantOK: true},
		{name: "narrower still drops speed next", right: noSpeed.width(glyph), wantOn: noSpeed.on, wantOK: true},
		{name: "narrower still drops the ICAO hex", right: noICAO.width(glyph), wantOn: noICAO.on, wantOK: true},
		{name: "narrower still drops altitude", right: noAltitude.width(glyph), wantOn: noAltitude.on, wantOK: true},
		{
			name:  "narrower still drops distance, leaving the identity",
			right: identityOnly.width(glyph), wantOn: identityOnly.on, wantOK: true,
		},
		{name: "narrower than the identity draws nothing", right: identityOnly.width(glyph) - 1, wantOK: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plan, ok := scene.planRows(left, testCase.right)
			if ok != testCase.wantOK {
				t.Fatalf("planRows(...) ok = %v, want %v", ok, testCase.wantOK)
			}

			if ok && plan.on != testCase.wantOn {
				t.Errorf("planRows(...) on = %v, want %v", plan.on, testCase.wantOn)
			}
		})
	}

	t.Run("a Scene with no body face reports false", func(t *testing.T) {
		t.Parallel()

		bare := &Scene{}
		if _, ok := bare.planRows(0, 1000); ok {
			t.Error("planRows(...) ok = true, want false without a body face")
		}
	})
}

// TestFitRunes checks the glyph count the airline legend clips an operator's
// name to, including the two guards that keep it from dividing by zero.
func TestFitRunes(t *testing.T) {
	t.Parallel()

	body := rowPlanFaces(t)

	for _, testCase := range []struct {
		name     string
		face     *psf.Font
		width    int
		tracking int
		want     int
	}{
		// A nil face has no glyph width of its own, so it only fits nothing
		// when the tracking does not carry the step above zero either; the
		// one production call site always passes a positive tracking, so this
		// is reached by calling fitRunes directly rather than through Draw.
		{name: "a nil face with no tracking fits nothing", face: nil, width: 100, tracking: 0, want: 0},
		{name: caseZero, face: body, width: 0, tracking: labelTracking, want: 0},
		{name: caseNegative, face: body, width: -10, tracking: labelTracking, want: 0},
		{
			name: "a known width divides the way the tracking says", face: body, tracking: labelTracking,
			width: 3*body.Width() + 2*labelTracking, want: 3,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := fitRunes(testCase.face, testCase.width, testCase.tracking); got != testCase.want {
				t.Errorf("fitRunes(_, %d, %d) = %d, want %d", testCase.width, testCase.tracking, got, testCase.want)
			}
		})
	}
}

// TestBatteryInk checks the fill colour the battery glyph reuses from the
// altitude ramp: critical, low and healthy.
func TestBatteryInk(t *testing.T) {
	t.Parallel()

	pal := theme.Palette{
		AltHigh: color.RGBA{R: 1, A: opaque},
		AltMid:  color.RGBA{R: 2, A: opaque},
		BandInk: color.RGBA{R: 3, A: opaque},
	}

	for _, testCase := range []struct {
		name    string
		percent int
		want    color.RGBA
	}{
		{name: caseZero, percent: 0, want: pal.AltHigh},
		{name: "ten percent is still critical", percent: 10, want: pal.AltHigh},
		{name: "eleven percent moves to low", percent: 11, want: pal.AltMid},
		{name: "twenty percent is still low", percent: 20, want: pal.AltMid},
		{name: "twenty-one percent is a healthy charge", percent: 21, want: pal.BandInk},
		{name: "a hundred percent is a healthy charge", percent: 100, want: pal.BandInk},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: pal}
			if got := scene.batteryInk(testCase.percent); got != testCase.want {
				t.Errorf("batteryInk(%d) = %v, want %v", testCase.percent, got, testCase.want)
			}
		})
	}
}

// strictlyBetween reports whether mid sits strictly inside the open interval
// bounded by a and b, in whichever order they come.
func strictlyBetween(mid, a, b uint8) bool {
	if a == b {
		return false
	}

	low, high := a, b
	if low > high {
		low, high = high, low
	}

	return mid > low && mid < high
}

// TestBandMuted checks the header band's quiet ink on both themes: it has to
// sit between the band and the band's own ink rather than repeat either one.
func TestBandMuted(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		pal  theme.Palette
	}{
		{name: "night", pal: theme.Night},
		{name: "paper", pal: theme.Paper},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: testCase.pal}
			got := scene.bandMuted()

			straddles := strictlyBetween(got.R, testCase.pal.Band.R, testCase.pal.BandInk.R) ||
				strictlyBetween(got.G, testCase.pal.Band.G, testCase.pal.BandInk.G) ||
				strictlyBetween(got.B, testCase.pal.Band.B, testCase.pal.BandInk.B)

			if !straddles {
				t.Errorf("bandMuted() = %v, want a channel strictly between Band %v and BandInk %v",
					got, testCase.pal.Band, testCase.pal.BandInk)
			}
		})
	}
}

// TestDetailsShape checks the block's two layouts: one column of five lines
// when the width will not hold the longest pair twice over, two columns of
// three lines when it will, plus where detailAt places a pair in either shape.
func TestDetailsShape(t *testing.T) {
	t.Parallel()

	small, err := fonts.Small()
	if err != nil {
		t.Fatalf("fonts.Small: %v", err)
	}

	scene := &Scene{faces: Faces{Small: small, Body: rowPlanFaces(t)}}
	widest := scene.detailWidest()

	for _, testCase := range []struct {
		name        string
		width       int
		wantColumns int
		wantLines   int
	}{
		{
			name:  "exactly one pair takes one column of five lines",
			width: widest, wantColumns: 1, wantLines: detailsPairs,
		},
		{
			name:  "just short of two pairs still takes one column",
			width: 2*widest - 1, wantColumns: 1, wantLines: detailsPairs,
		},
		{
			name:  "exactly two pairs takes two columns of three lines",
			width: 2 * widest, wantColumns: detailsColumns, wantLines: detailsLines,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns, lines := scene.detailsShape(testCase.width)
			if columns != testCase.wantColumns || lines != testCase.wantLines {
				t.Errorf("detailsShape(%d) = (%d, %d), want (%d, %d)",
					testCase.width, columns, lines, testCase.wantColumns, testCase.wantLines)
			}
		})
	}

	t.Run("detailsHeight is zero just under the widest pair and real at it", func(t *testing.T) {
		t.Parallel()

		if got := scene.detailsHeight(widest - 1); got != 0 {
			t.Errorf("detailsHeight(widest-1) = %d, want 0", got)
		}

		if got := scene.detailsHeight(widest); got == 0 {
			t.Error("detailsHeight(widest) = 0, want a real height")
		}
	})

	t.Run("detailAt places pairs left to right then down", func(t *testing.T) {
		t.Parallel()

		box := image.Rect(0, 0, 200, 100)

		const step = 20

		vertX, vertY := detailAt(box, 2, step, pairVert)
		brgX, brgY := detailAt(box, 2, step, pairBrg)
		posX, posY := detailAt(box, 2, step, pairPos)

		if vertX != box.Min.X || vertY != box.Min.Y {
			t.Errorf("detailAt(pairVert) = (%d, %d), want the box origin", vertX, vertY)
		}

		if brgX == vertX || brgY != vertY {
			t.Errorf("detailAt(pairBrg) = (%d, %d), want the same row, a different column", brgX, brgY)
		}

		if posX != vertX || posY != vertY+step {
			t.Errorf("detailAt(pairPos) = (%d, %d), want the first column, one row down", posX, posY)
		}
	})
}

// TestDetailsHeightRowStepRowsHeightWithoutFaces checks the guard each of the
// three geometry helpers has for the face it cannot do without.
func TestDetailsHeightRowStepRowsHeightWithoutFaces(t *testing.T) {
	t.Parallel()

	const detailsProbeWidth = 1000

	t.Run("detailsHeight is zero without a small face", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{faces: Faces{Body: rowPlanFaces(t)}}
		if got := scene.detailsHeight(detailsProbeWidth); got != 0 {
			t.Errorf("detailsHeight(%d) = %d, want 0 without a small face", detailsProbeWidth, got)
		}
	})

	t.Run("detailsHeight is zero without a body face", func(t *testing.T) {
		t.Parallel()

		small, err := fonts.Small()
		if err != nil {
			t.Fatalf("fonts.Small: %v", err)
		}

		scene := &Scene{faces: Faces{Small: small}}
		if got := scene.detailsHeight(detailsProbeWidth); got != 0 {
			t.Errorf("detailsHeight(%d) = %d, want 0 without a body face", detailsProbeWidth, got)
		}
	})

	t.Run("rowStep is zero without a body face", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{}
		if got := scene.rowStep(); got != 0 {
			t.Errorf("rowStep() = %d, want 0", got)
		}
	})

	t.Run("rowsHeight is zero without a body face", func(t *testing.T) {
		t.Parallel()

		const probeRowCount = 5

		scene := &Scene{}
		if got := scene.rowsHeight(probeRowCount); got != 0 {
			t.Errorf("rowsHeight(%d) = %d, want 0", probeRowCount, got)
		}
	})
}

// TestFixColour checks how much the receiver's position is worth, in colour:
// every named mode plus a value outside the six the type defines, which a
// Receiver built by hand rather than by a Source could still hand the scene.
func TestFixColour(t *testing.T) {
	t.Parallel()

	pal := theme.Palette{
		Muted:   color.RGBA{R: 1, A: opaque},
		Accent:  color.RGBA{R: 2, A: opaque},
		AltHigh: color.RGBA{R: 3, A: opaque},
		AltMid:  color.RGBA{R: 4, A: opaque},
		AltLow:  color.RGBA{R: 5, A: opaque},
	}
	ink := color.RGBA{R: 6, A: opaque}

	for _, testCase := range []struct {
		name string
		mode source.FixMode
		want color.RGBA
	}{
		{name: "no fix reads as muted", mode: source.FixNone, want: pal.Muted},
		{name: "a manual position takes the ink handed in", mode: source.FixManual, want: ink},
		{name: "an estimate takes the accent", mode: source.FixEstimated, want: pal.Accent},
		{name: "a GPS still searching is critical", mode: source.FixGPSNoFix, want: pal.AltHigh},
		{name: "a 2D GPS fix is the mid band", mode: source.FixGPS2D, want: pal.AltMid},
		{name: "a full 3D GPS fix is the low band", mode: source.FixGPS3D, want: pal.AltLow},
		{name: "an out-of-range mode reads as muted", mode: source.FixMode(99), want: pal.Muted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: pal}
			if got := scene.fixColour(testCase.mode, ink); got != testCase.want {
				t.Errorf("fixColour(%v, %v) = %v, want %v", testCase.mode, ink, got, testCase.want)
			}
		})
	}
}

// TestModeWord checks the word the receiver line opens with for every mode
// that reaches it through Draw, plus the two that only reach it by calling
// modeWord directly: an estimate and no fix are never prefixed to coordinates
// they do not have, so their whole line is decided elsewhere.
func TestModeWord(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		mode source.FixMode
		want string
	}{
		{name: "manual", mode: source.FixManual, want: manualText},
		{name: "GPS 2D", mode: source.FixGPS2D, want: gps2DText},
		{name: "GPS 3D", mode: source.FixGPS3D, want: gps3DText},
		{name: "GPS still searching", mode: source.FixGPSNoFix, want: gpsNoFixText},
		{name: "no fix reads as unknown here", mode: source.FixNone, want: unknownFixText},
		{name: "an estimate reads as unknown here", mode: source.FixEstimated, want: unknownFixText},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := modeWord(testCase.mode); got != testCase.want {
				t.Errorf("modeWord(%v) = %q, want %q", testCase.mode, got, testCase.want)
			}
		})
	}
}

// colourCount counts the pixels in box that are exactly col, which is how the
// two tests below confirm a specific ink was used without pinning a whole
// picture.
func colourCount(canv *canvas.Canvas, box image.Rectangle, col color.RGBA) int {
	count := 0

	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) == col {
				count++
			}
		}
	}

	return count
}

// TestDrawRowAltitudeUsesTheBand checks that the compact row's ALT figure is
// set in the aircraft's altitude band in both colour modes: it is the one
// column where the number and a colour say the same thing, and that has to
// survive airline mode rather than being the price of turning it on.
func TestDrawRowAltitudeUsesTheBand(t *testing.T) {
	t.Parallel()

	const (
		rowCanvasWidth  = 200
		rowCanvasHeight = 30
		rowEdgeX        = 150
		rowTop          = 4

		lowAltitude  = 4000.0
		midAltitude  = 18000.0
		highAltitude = 39000.0
	)

	pal := theme.Palette{
		Muted:   color.RGBA{R: 1, A: opaque},
		AltLow:  color.RGBA{R: 2, A: opaque},
		AltMid:  color.RGBA{R: 3, A: opaque},
		AltHigh: color.RGBA{R: 4, A: opaque},
	}
	body := rowPlanFaces(t)

	for _, testCase := range []struct {
		name     string
		altitude float64
		mode     ColourMode
		want     color.RGBA
	}{
		{name: "under 10,000 feet is the low band", altitude: lowAltitude, mode: ColourAltitude, want: pal.AltLow},
		{
			name:     "between 10,000 and 25,000 is the mid band",
			altitude: midAltitude, mode: ColourAltitude, want: pal.AltMid,
		},
		{name: "above 25,000 is the high band", altitude: highAltitude, mode: ColourAltitude, want: pal.AltHigh},
		{name: "undecoded reads as muted", altitude: 0, mode: ColourAltitude, want: pal.Muted},
		{name: "the low band survives airline mode", altitude: lowAltitude, mode: ColourAirline, want: pal.AltLow},
		{name: "the mid band survives airline mode", altitude: midAltitude, mode: ColourAirline, want: pal.AltMid},
		{name: "the high band survives airline mode", altitude: highAltitude, mode: ColourAirline, want: pal.AltHigh},
		{name: "undecoded still reads as muted in airline mode", altitude: 0, mode: ColourAirline, want: pal.Muted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(rowCanvasWidth, rowCanvasHeight)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			scene := &Scene{faces: Faces{Body: body}, pal: pal, colour: testCase.mode}

			var plan rowPlan

			plan.on[colAltitude] = true
			plan.edge[colAltitude] = rowEdgeX

			pen := rowPen{dst: canv, face: body, plan: plan, glyph: body.Width(), top: rowTop}
			plane := airplane.Snapshot{Altitude: testCase.altitude, Callsign: sampleCallsign}

			scene.drawRowAltitude(pen, plane)

			if colourCount(canv, canv.Bounds(), testCase.want) == 0 {
				t.Errorf("no pixel painted in %v, want the ALT figure set in it", testCase.want)
			}
		})
	}
}

// TestDrawCardFiguresInk checks the one figure that changed colour: altitude
// takes the aircraft's band, distance and speed stay in the reading ink.
func TestDrawCardFiguresInk(t *testing.T) {
	t.Parallel()

	const (
		figuresWidth   = 300
		figuresHeight  = 40
		figuresMargin  = 10
		figureAltitude = 4000.0
		figureVelocity = 250.0
		figureLatStep  = 1.0

		cardTestLat = 52.0
		cardTestLon = 4.0
	)

	pal := theme.Palette{
		Ink:    color.RGBA{R: 1, A: opaque},
		Muted:  color.RGBA{R: 2, A: opaque},
		AltLow: color.RGBA{R: 3, A: opaque},
	}

	large, err := fonts.Large()
	if err != nil {
		t.Fatalf("fonts.Large: %v", err)
	}

	small, err := fonts.Small()
	if err != nil {
		t.Fatalf("fonts.Small: %v", err)
	}

	scene := &Scene{faces: Faces{Large: large, Small: small}, pal: pal}

	canv, err := canvas.New(figuresWidth, figuresHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	box := image.Rect(0, 0, figuresWidth-figuresMargin, figuresHeight)
	receiver := source.Receiver{Latitude: cardTestLat, Longitude: cardTestLon}
	plane := airplane.Snapshot{
		Altitude: figureAltitude, Velocity: figureVelocity,
		Latitude: cardTestLat + figureLatStep, Longitude: cardTestLon,
	}

	scene.drawCardFigures(canv, box, receiver, plane)

	column := box.Dx() / figureCount
	distanceCell := image.Rect(box.Min.X, box.Min.Y, box.Min.X+column, box.Max.Y)
	altitudeCell := image.Rect(box.Min.X+column, box.Min.Y, box.Min.X+2*column, box.Max.Y)
	speedCell := image.Rect(box.Min.X+2*column, box.Min.Y, box.Max.X, box.Max.Y)

	if colourCount(canv, distanceCell, pal.Ink) == 0 {
		t.Error("the distance figure did not use the reading ink")
	}

	if colourCount(canv, altitudeCell, pal.AltLow) == 0 {
		t.Error("the altitude figure did not use its altitude band")
	}

	if colourCount(canv, speedCell, pal.Ink) == 0 {
		t.Error("the speed figure did not use the reading ink")
	}
}

// TestParseToggle checks the allow list an on/off flag reads: both spellings,
// and everything else refused with an error errors.Is can match against
// ErrToggle.
func TestParseToggle(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		input   string
		want    Toggle
		wantErr bool
	}{
		{name: "on", input: "on", want: ToggleOn},
		{name: "off", input: "off", want: ToggleOff},
		{name: "empty string is rejected", input: "", want: ToggleOn, wantErr: true},
		{name: "wrong case is rejected", input: "On", want: ToggleOn, wantErr: true},
		{name: "an unrelated word is rejected", input: "maybe", want: ToggleOn, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseToggle(testCase.input)
			if got != testCase.want {
				t.Errorf("ParseToggle(%q) = %q, want %q", testCase.input, got, testCase.want)
			}

			if testCase.wantErr {
				if !errors.Is(err, ErrToggle) {
					t.Errorf("ParseToggle(%q) err = %v, want it to match ErrToggle", testCase.input, err)
				}

				return
			}

			if err != nil {
				t.Errorf("ParseToggle(%q) unexpected error: %v", testCase.input, err)
			}
		})
	}
}

// TestToggleOn checks the rule that makes the zero value usable: anything
// that is not ToggleOff is on.
func TestToggleOn(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		toggle Toggle
		want   bool
	}{
		{name: "on is on", toggle: ToggleOn, want: true},
		{name: "off is off", toggle: ToggleOff, want: false},
		{name: "the zero value is on", toggle: Toggle(""), want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.toggle.On(); got != testCase.want {
				t.Errorf("Toggle(%q).On() = %v, want %v", string(testCase.toggle), got, testCase.want)
			}
		})
	}
}

// TestApply checks that a settings block reaches both of the fields it
// carries, and that a Settings nobody filled in reads as the scene's own
// defaults: altitude colours and the airfield markers on.
func TestApply(t *testing.T) {
	t.Parallel()

	t.Run("sets the colour mode and the airports toggle", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{}
		scene.Apply(Settings{Colour: ColourAirline, Airports: ToggleOff})

		if scene.colour != ColourAirline {
			t.Errorf("colour = %q, want %q", scene.colour, ColourAirline)
		}

		if scene.airports {
			t.Error("airports = true, want false")
		}
	})

	t.Run("the zero-value Settings means altitude colours and airports on", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{}
		scene.Apply(Settings{})

		if scene.colour == ColourAirline {
			t.Error("colour = airline, want the zero value to read as altitude")
		}

		if !scene.airports {
			t.Error("airports = false, want the zero value to read as on")
		}
	})
}
