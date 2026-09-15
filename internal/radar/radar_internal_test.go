package radar

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
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

func TestCallsignOf(t *testing.T) {
	t.Parallel()

	const callsignTestICAO = "ABC123"

	for _, testCase := range []struct {
		name  string
		plane airplane.Snapshot
		want  string
	}{
		{
			name: "a decoded callsign wins", want: "KLM123",
			plane: airplane.Snapshot{ICAO: callsignTestICAO, Callsign: "KLM123"},
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
