package radar

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/airports"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/airlines"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/shore"
	"github.com/hyperized/uScope/pkg/text"
)

// The ICAOs in the small fleets these tests build. The first is the one an
// unpinned selection lands on.
const (
	icaoFirst  = "AAA111"
	icaoSecond = "BBB222"

	// icaoSampleReal is a real KLM 737's hex, reused wherever a test in this
	// package needs an aircraft that looks like a live contact rather than a
	// fixture with an obviously synthetic ICAO. Named once so goconst does
	// not flag the repetition.
	icaoSampleReal = "484AC1"
)

// The four cardinal headings. They are named rather than written out at each
// call site so a case reads as a direction instead of as a number.
const (
	headingNorth = 0.0
	headingEast  = 90.0
	headingSouth = 180.0
	headingWest  = 270.0
)

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

// TestSceneFlightLevel checks the four shapes a data block's level figure
// takes: two leading zeros under a thousand feet, one under ten thousand,
// none from ten thousand up, and dashes for an altitude nobody has decoded,
// which flightLevel's own comment draws a line under: zero is undecoded
// rather than sea level.
func TestSceneFlightLevel(t *testing.T) {
	t.Parallel()

	const (
		belowThousandFt  = 500.0
		belowTenThousand = 4000.0
		tenThousandAndUp = 24000.0
	)

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: "zero reads as undecoded, not sea level", value: 0, want: "---"},
		{name: caseNaNReadsAsDash, value: math.NaN(), want: "---"},
		{name: "under a thousand feet gets two leading zeros", value: belowThousandFt, want: "005"},
		{name: "under ten thousand feet gets one leading zero", value: belowTenThousand, want: "040"},
		{name: "ten thousand feet and up has no leading zero", value: tenThousandAndUp, want: "240"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.flightLevel(testCase.value)); got != testCase.want {
				t.Errorf("flightLevel(%v) = %q, want %q", testCase.value, got, testCase.want)
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

	icaos := []string{icaoFirst, icaoSecond, "CCC333"}

	for _, testCase := range []struct {
		name      string
		list      []string
		search    string
		wantIndex int
	}{
		{name: "an empty search string reports a miss", list: icaos, search: "", wantIndex: -1},
		{name: "a hit reports its position", list: icaos, search: icaoSecond, wantIndex: 1},
		{name: "a miss reports -1", list: icaos, search: "ZZZ999", wantIndex: -1},
		{name: "an empty list reports a miss", list: nil, search: icaoFirst, wantIndex: -1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := indexOf(testCase.list, testCase.search); got != testCase.wantIndex {
				t.Errorf("indexOf(%v, %q) = %d, want %d", testCase.list, testCase.search, got, testCase.wantIndex)
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

	validOrigin := geo{lat: projLat0, lon: projLon0}

	for _, testCase := range []struct {
		name    string
		geom    scopeGeometry
		origin  geo
		scopeNm float64
	}{
		{name: "a non-positive rangeR", geom: scopeGeometry{rangeR: 0}, origin: validOrigin, scopeNm: projScopeNm},
		{name: "a non-positive scope range", geom: validProjectorGeometry(), origin: validOrigin, scopeNm: 0},
		{
			name: "a NaN scope range", geom: validProjectorGeometry(),
			origin: validOrigin, scopeNm: math.NaN(),
		},
		{
			name: "an origin with no position yet", geom: validProjectorGeometry(),
			origin: geo{}, scopeNm: projScopeNm,
		},
		{
			name: "an origin with a NaN coordinate", geom: validProjectorGeometry(),
			origin: geo{lat: math.NaN(), lon: projLon0}, scopeNm: projScopeNm,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, ok := newProjector(testCase.geom, testCase.origin, testCase.scopeNm); ok {
				t.Error("newProjector(...) ok = true, want false")
			}
		})
	}

	t.Run("a valid origin and range build a usable projector", func(t *testing.T) {
		t.Parallel()

		if _, ok := newProjector(validProjectorGeometry(), validOrigin, projScopeNm); !ok {
			t.Error("newProjector(...) ok = false, want true")
		}
	})
}

func TestProjectorAt(t *testing.T) {
	t.Parallel()

	proj, ok := newProjector(validProjectorGeometry(), geo{lat: projLat0, lon: projLon0}, projScopeNm)
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

// The fixture TestDrawAirportsSkipsRangeLabelOverlap builds: a 200 nautical
// mile scope with round numbers, chosen so the outer range label lands well
// clear of every dashed ring and the solid boundary, and a canvas large
// enough to hold all of it with room to spare.
const (
	overlapCenter  = 300
	overlapRangeR  = 270
	overlapScopeNm = 200.0
	overlapLat0    = 52.0
	overlapLon0    = 4.0
	overlapCanvas  = 620

	// overlapElsewhereDY is how far above the centre the "elsewhere" airport
	// sits: far enough from every ring radius (90, 180 and 270 at this
	// fixture) that no ring pixel falls inside its check box either.
	overlapElsewhereDY = 50
	overlapCheckMargin = 5
)

// invertProjection is projector.at run backwards: the position that would
// project onto pixel (x, y), rather than the pixel a position projects onto.
// It only exists here, where a synthetic airport has to be placed at a known
// pixel instead of a known position.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func invertProjection(proj projector, x, y int) (float64, float64) {
	eastNm := float64(x-proj.centerX) / proj.scale
	northNm := float64(proj.centerY-y) / proj.scale

	return proj.lat0 + northNm/nmPerDegree, proj.lon0 + eastNm/(nmPerDegree*proj.cosLat0)
}

// TestDrawAirportsSkipsRangeLabelOverlap checks the fix for the range label
// colliding with an airport marker, seen live as "200EDDV" on the outer ring.
// An airport placed exactly under the outer range label is skipped, and one
// well clear of every range label is drawn as usual.
func TestDrawAirportsSkipsRangeLabelOverlap(t *testing.T) {
	t.Parallel()

	small, err := fonts.Small()
	if err != nil {
		t.Fatalf("fonts.Small: %v", err)
	}

	scene := &Scene{faces: Faces{Small: small}, pal: theme.Night}

	canv, err := canvas.New(overlapCanvas, overlapCanvas)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	lay := layout{dst: canv, labels: true}
	geom := scopeGeometry{
		centerX: overlapCenter, centerY: overlapCenter, outer: overlapRangeR + ringInset, rangeR: overlapRangeR,
	}

	scene.drawRings(&lay, geom, overlapScopeNm)

	if scene.rangeLabelCount == 0 {
		t.Fatal("drawRings recorded no range labels; the fixture needs adjusting")
	}

	outerLabel := scene.rangeLabelRects[scene.rangeLabelCount-1]

	proj, ok := newProjector(geom, geo{lat: overlapLat0, lon: overlapLon0}, overlapScopeNm)
	if !ok {
		t.Fatal("newProjector(...) ok = false, want true")
	}

	underLat, underLon := invertProjection(proj, outerLabel.Min.X+outerLabel.Dx()/2, outerLabel.Min.Y+outerLabel.Dy()/2)
	elsewhereLat, elsewhereLon := invertProjection(proj, overlapCenter, overlapCenter-overlapElsewhereDY)

	fields := []airports.Airport{
		{ICAO: "UNDR", Latitude: underLat, Longitude: underLon},
		{ICAO: "ELSE", Latitude: elsewhereLat, Longitude: elsewhereLon},
	}

	scene.drawAirports(&lay, proj, fields)

	if got := colourCount(canv, outerLabel, scene.pal.Rule); got != 0 {
		t.Errorf("airport marker pixels under the outer range label = %d, want 0", got)
	}

	elsewhereBox := image.Rect(
		overlapCenter-overlapCheckMargin, overlapCenter-overlapElsewhereDY-overlapCheckMargin,
		overlapCenter+overlapCheckMargin, overlapCenter-overlapElsewhereDY+overlapCheckMargin,
	)

	if got := colourCount(canv, elsewhereBox, scene.pal.Rule); got == 0 {
		t.Error("an airport clear of every range label was not drawn")
	}
}

// TestRecordRangeLabelCapsAtThree checks the safety net past the fixed three
// slots. drawRings never calls this a fourth time, since ringCount is three,
// but recordRangeLabel has to hold that limit on its own rather than assume
// its only caller keeps it.
func TestRecordRangeLabelCapsAtThree(t *testing.T) {
	t.Parallel()

	var scene Scene

	for index := range ringCount + 1 {
		scene.recordRangeLabel(image.Rect(index, index, index+1, index+1))
	}

	if scene.rangeLabelCount != ringCount {
		t.Errorf("rangeLabelCount = %d, want %d", scene.rangeLabelCount, ringCount)
	}

	if want := (image.Rect(0, 0, 1, 1)); scene.rangeLabelRects[0] != want {
		t.Errorf("rangeLabelRects[0] = %v, want %v kept rather than overwritten", scene.rangeLabelRects[0], want)
	}
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
// realistic value and does not care which operator it names, and sampleICAO is
// the operator designator it looks up to.
const (
	sampleCallsign = "KLM123"
	sampleICAO     = "KLM"
)

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

	if got := counts.seen[0].airline.ICAO; got != sampleICAO {
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

		for index, want := range []string{sampleICAO, "DLH", "EIN"} {
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

// rowPlanFaces loads the body face several of the layout tests in this file
// measure against. A synthetic font would not exercise the real character
// widths the arithmetic is built on.
func rowPlanFaces(tb testing.TB) *psf.Font {
	tb.Helper()

	body, err := fonts.Body()
	if err != nil {
		tb.Fatalf("fonts.Body: %v", err)
	}

	return body
}

// rowPlanSmall loads the small face the header's labels and the key bar's
// caps are set in, for the same reason rowPlanFaces loads the body one.
func rowPlanSmall(tb testing.TB) *psf.Font {
	tb.Helper()

	small, err := fonts.Small()
	if err != nil {
		tb.Fatalf("fonts.Small: %v", err)
	}

	return small
}

// TestStripPlanGeometry drives stripPlan's own arithmetic directly: which
// fields count, how much room they need including the gap between them, and
// where place() puts each one.
func TestStripPlanGeometry(t *testing.T) {
	t.Parallel()

	t.Run("count tallies only the enabled fields", func(t *testing.T) {
		t.Parallel()

		var plan stripPlan

		plan.on[fieldIdent] = true
		plan.on[fieldLevel] = true

		if got := plan.count(); got != 2 {
			t.Errorf("count() = %d, want 2", got)
		}
	})

	t.Run("total sums the enabled fields plus the gap between them", func(t *testing.T) {
		t.Parallel()

		const identWidth, levelWidth = 40, 30

		var plan stripPlan

		plan.on[fieldIdent] = true
		plan.width[fieldIdent] = identWidth
		plan.on[fieldLevel] = true
		plan.width[fieldLevel] = levelWidth

		if got, want := plan.total(), identWidth+levelWidth+stripGap; got != want {
			t.Errorf("total() = %d, want %d", got, want)
		}
	})

	t.Run("place with no slack puts fields end to end, the gap included", func(t *testing.T) {
		t.Parallel()

		const identWidth, levelWidth, left = 40, 30, 50

		var plan stripPlan

		plan.on[fieldIdent] = true
		plan.width[fieldIdent] = identWidth
		plan.on[fieldLevel] = true
		plan.width[fieldLevel] = levelWidth

		available := plan.total()
		plan.place(left, available)

		if got, want := plan.left[fieldIdent], left; got != want {
			t.Errorf("first field's left edge = %d, want %d", got, want)
		}

		if got, want := plan.left[fieldLevel], left+identWidth+stripGap; got != want {
			t.Errorf("second field's left edge = %d, want %d", got, want)
		}

		if got, want := plan.left[fieldLevel]+levelWidth, left+available; got != want {
			t.Errorf("last field's right edge = %d, want %d", got, want)
		}
	})

	t.Run("a single-field plan does not divide by zero", func(t *testing.T) {
		t.Parallel()

		const speedWidth, left = 20, 10

		var plan stripPlan

		plan.on[fieldSpeed] = true
		plan.width[fieldSpeed] = speedWidth

		available := plan.total()
		plan.place(left, available)

		if got, want := plan.left[fieldSpeed], left; got != want {
			t.Errorf("left edge = %d, want %d", got, want)
		}
	})
}

// stripPlanWithout builds a fully-enabled plan and applies the first drops
// entries of stripDropOrder to it, mirroring how planStrips narrows the board
// one entry at a time.
func stripPlanWithout(widths [fieldCount]int, drops int) stripPlan {
	var plan stripPlan

	for index := range fieldCount {
		plan.on[index] = true
	}

	plan.att = true
	plan.size(widths)

	for _, entry := range stripDropOrder[:drops] {
		plan.give(entry, widths)
	}

	return plan
}

// TestPlanStrips checks the field drop order as the available width narrows,
// and the two ways it refuses to draw a board at all: a width too narrow even
// for the identity, the level and the range, and a Scene missing one of the
// four faces it measures with (see TestPlanStripsWithoutFaces for the latter).
func TestPlanStrips(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t)}
	widths := scene.stripWidths()

	full := stripPlanWithout(widths, 0)
	noPos := stripPlanWithout(widths, 1)
	noSeen := stripPlanWithout(widths, 2)
	noTrack := stripPlanWithout(widths, 3)
	noAtt := stripPlanWithout(widths, 4)
	noSpeed := stripPlanWithout(widths, len(stripDropOrder))

	const left = 0

	for _, testCase := range []struct {
		name    string
		right   int
		wantOn  [fieldCount]bool
		wantAtt bool
		wantOK  bool
	}{
		{name: "a generous width keeps every field", right: full.total(), wantOn: full.on, wantAtt: true, wantOK: true},
		{
			name:  "narrower drops position first",
			right: noPos.total(), wantOn: noPos.on, wantAtt: true, wantOK: true,
		},
		{
			name:  "narrower still drops seen",
			right: noSeen.total(), wantOn: noSeen.on, wantAtt: true, wantOK: true,
		},
		{
			name:  "narrower still drops track",
			right: noTrack.total(), wantOn: noTrack.on, wantAtt: true, wantOK: true,
		},
		{
			name:  "narrower still drops the little aeroplane",
			right: noAtt.total(), wantOn: noAtt.on, wantAtt: false, wantOK: true,
		},
		{
			name:  "narrower still drops speed, leaving identity, level and range",
			right: noSpeed.total(), wantOn: noSpeed.on, wantAtt: false, wantOK: true,
		},
		{
			name:  "narrower than identity, level and range draws nothing",
			right: noSpeed.total() - 1, wantOK: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plan, ok := scene.planStrips(left, testCase.right)
			if ok != testCase.wantOK {
				t.Fatalf("planStrips(...) ok = %v, want %v", ok, testCase.wantOK)
			}

			if !ok {
				return
			}

			if plan.on != testCase.wantOn {
				t.Errorf("planStrips(...) on = %v, want %v", plan.on, testCase.wantOn)
			}

			if plan.att != testCase.wantAtt {
				t.Errorf("planStrips(...) att = %v, want %v", plan.att, testCase.wantAtt)
			}
		})
	}
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

// TestBatteryInk checks the fill colour the battery glyph takes: critical,
// low and healthy.
//
// It runs the three levels over two real palettes rather than over a synthetic
// one, because the glyph sits on the header band and the colour goes through
// Palette.OnBand on the way there. On glass night all three read against the
// band and come back untouched. On mono day the band is filled with the same
// ink the data is set in, so a healthy battery and a low one both come back as
// the band's ink instead: a percentage nobody can see would be worse than one
// drawn in the wrong colour, and mono has no colour to spare for it anyway.
func TestBatteryInk(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		pal     theme.Palette
		percent int
		want    color.RGBA
	}{
		{name: caseZero, pal: theme.Night, percent: 0, want: theme.Night.Warn},
		{name: "ten percent is still critical", pal: theme.Night, percent: 10, want: theme.Night.Warn},
		{name: "eleven percent moves to low", pal: theme.Night, percent: 11, want: theme.Night.Caution},
		{name: "twenty percent is still low", pal: theme.Night, percent: 20, want: theme.Night.Caution},
		{name: "twenty-one percent is a healthy charge", pal: theme.Night, percent: 21, want: theme.Night.Data},
		{name: "a hundred percent is a healthy charge", pal: theme.Night, percent: 100, want: theme.Night.Data},
		{
			// Mono day's warning red is the one of the three that still reads
			// against its band, so it is the one that survives the trip.
			name: "mono day keeps the warning red on a critical battery",
			pal:  theme.MonoDay, percent: 0, want: theme.MonoDay.Warn,
		},
		{
			name: "mono day lifts a low battery onto the band's ink",
			pal:  theme.MonoDay, percent: 11, want: theme.MonoDay.BandInk,
		},
		{
			name: "mono day lifts a healthy battery onto the band's ink",
			pal:  theme.MonoDay, percent: 100, want: theme.MonoDay.BandInk,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: testCase.pal}
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
		{name: caseNight, pal: theme.Night},
		{name: caseDay, pal: theme.Day},
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

// TestPlanStripsWithoutFaces checks that planStrips refuses to draw a board
// unless all four faces it measures with are present: the selected strip sets
// its callsign in Large and its codes in Small, and a half strip sets its
// figures in Body or BodyBold depending on whether it is the selected one.
func TestPlanStripsWithoutFaces(t *testing.T) {
	t.Parallel()

	const stripProbeWidth = 1000

	for _, testCase := range []struct {
		name  string
		blank func(*Faces)
	}{
		{name: "planStrips refuses without a small face", blank: func(f *Faces) { f.Small = nil }},
		{name: "planStrips refuses without a body face", blank: func(f *Faces) { f.Body = nil }},
		{name: "planStrips refuses without a bold face", blank: func(f *Faces) { f.BodyBold = nil }},
		{name: "planStrips refuses without a large face", blank: func(f *Faces) { f.Large = nil }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			faces := layerTestFaces(t)
			testCase.blank(&faces)

			scene := &Scene{faces: faces}
			if _, ok := scene.planStrips(0, stripProbeWidth); ok {
				t.Error("planStrips reported a plan without every face")
			}
		})
	}
}

// TestFixColour checks how much the receiver's position is worth, in colour:
// every named mode plus a value outside the six the type defines, which a
// Receiver built by hand rather than by a Source could still hand the scene.
//
// GPS 2D and GPS 3D come out the same colour, and so do an estimate and a
// GPS fix that has gone: fixColour only says whether a position was sensed,
// derived or absent, not how confident the sensor was. A test that wanted to
// tell 2D from 3D would have to read the mode word instead of the colour.
func TestFixColour(t *testing.T) {
	t.Parallel()

	pal := theme.Palette{
		Muted:   color.RGBA{R: 1, A: opaque},
		Caution: color.RGBA{R: 2, A: opaque},
		OK:      color.RGBA{R: 3, A: opaque},
	}
	ink := color.RGBA{R: 4, A: opaque}

	for _, testCase := range []struct {
		name string
		mode source.FixMode
		want color.RGBA
	}{
		{name: "no fix reads as muted", mode: source.FixNone, want: pal.Muted},
		{name: "a manual position takes the ink handed in", mode: source.FixManual, want: ink},
		{name: "an estimate is a caution", mode: source.FixEstimated, want: pal.Caution},
		{name: "a GPS fix that has gone is a caution too", mode: source.FixGPSNoFix, want: pal.Caution},
		{name: "a 2D GPS fix is OK", mode: source.FixGPS2D, want: pal.OK},
		{name: "a full 3D GPS fix is OK too", mode: source.FixGPS3D, want: pal.OK},
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
		{name: "GPS lost, holding its last position", mode: source.FixGPSNoFix, want: gpsNoFixText},
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

// TestDrawStripLevelUsesTheBand checks that a strip's LEVEL figure is set in
// the aircraft's altitude band in both colour modes: it is the one field
// where the number and a colour say the same thing, and that has to survive
// airline mode rather than being the price of turning it on.
func TestDrawStripLevelUsesTheBand(t *testing.T) {
	t.Parallel()

	const (
		levelCanvasWidth  = 200
		levelCanvasHeight = 30
		levelFieldX       = 150
		levelValueY       = 4

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

			canv, err := canvas.New(levelCanvasWidth, levelCanvasHeight)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			scene := &Scene{faces: Faces{Body: body}, pal: pal, colour: testCase.mode}

			var plan stripPlan

			plan.on[fieldLevel] = true
			plan.left[fieldLevel] = levelFieldX

			pen := stripPen{dst: canv, plan: plan, value: levelValueY}
			plane := airplane.Snapshot{Altitude: testCase.altitude, Callsign: sampleCallsign}

			scene.drawStripLevel(pen, plane)

			if colourCount(canv, canv.Bounds(), testCase.want) == 0 {
				t.Errorf("no pixel painted in %v, want the level figure set in it", testCase.want)
			}
		})
	}
}

// TestDrawStripsRefusesAColumnTooShortForOneFullStrip checks the second of
// drawStrips's two early returns (TestPlanStrips already covers the other, a
// column too narrow for even the identity, the level and the range): a column
// with all the width it needs but not enough height for the row of field names
// plus one strip draws nothing, rather than clipping a strip half-drawn.
//
// The width is set deliberately generous so only the height guard is under
// test; a column that width would draw every field on the board given the
// room.
func TestDrawStripsRefusesAColumnTooShortForTheBoard(t *testing.T) {
	t.Parallel()

	const (
		stripsColumnWidth = 2000
		stripsMargin      = 20
		stripsAltitudeFt  = 4000
		stripsLon         = 4.5
	)

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}
	frame := source.Frame{
		Planes:   []airplane.Snapshot{{ICAO: icaoFirst, Callsign: sampleCallsign, Altitude: stripsAltitudeFt}},
		Receiver: source.Receiver{Latitude: coordinateSampleLat, Longitude: stripsLon},
	}

	need := scene.boardHeadHeight() + scene.halfStripHeight()

	draw := func(bottom int) int {
		canv, err := canvas.New(stripsColumnWidth, bottom+stripsMargin)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)
		col := &layout{dst: canv, left: 0, right: stripsColumnWidth, top: 0, bottom: bottom}
		scene.drawStrips(col, frame)

		return filterPainted(canv)
	}

	if got := draw(need - 1); got != 0 {
		t.Errorf("a column one pixel short of the field names plus one strip painted %d pixels, want none", got)
	}

	if got := draw(need); got == 0 {
		t.Error("a column exactly tall enough for the field names plus one strip painted nothing")
	}
}

// TestDrawStripStatusRefusesWhenTooNarrow checks the status line's own early
// return: a column narrower than the words it has to carry draws nothing
// rather than running the count past the column's own right edge.
func TestDrawStripStatusRefusesWhenTooNarrow(t *testing.T) {
	t.Parallel()

	const (
		countShown        = 1
		countTotal        = 2
		countCanvasHeight = 20
	)

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}
	widest := measureTracked(scene.faces.Small, widestStatus)

	draw := func(right int) int {
		canv, err := canvas.New(right+1, countCanvasHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)
		col := &layout{dst: canv, left: 0, right: right, top: 0, bottom: countCanvasHeight}
		scene.drawStripStatus(col, selection{}, countShown, countTotal)

		return filterPainted(canv)
	}

	if got := draw(widest - 1); got != 0 {
		t.Errorf("a column one pixel narrower than the status line painted %d pixels, want none", got)
	}

	if got := draw(widest); got == 0 {
		t.Error("a column exactly as wide as the status line painted nothing")
	}
}

// TestStripFieldDrawersSkipWhenDroppedFromThePlan checks the guard every
// per-field drawer opens with. strips.go drops POS, SEEN, TRK, the little
// aeroplane and GS whole as the column narrows rather than squeezing them (see
// stripDropOrder); LEVEL and the little aeroplane's own drawer carry the same
// on/off gate for symmetry with the rest of the row. If any one of these
// guards were lost, a field the plan had dropped would still land whatever the
// aircraft's own reading is on top of whatever the plan put in its place.
//
// Every plan and pen field here is left at its zero value, which is exactly
// the state a dropped field is in: off, with no room reserved. Nothing else
// about the aircraft or the scene should matter to any of these guards, which
// is why the same empty plane and empty scene stand in for all seven.
func TestStripFieldDrawersSkipWhenDroppedFromThePlan(t *testing.T) {
	t.Parallel()

	const stripGuardSide = 20

	scene := &Scene{}
	plane := airplane.Snapshot{}
	receiver := source.Receiver{}

	for _, testCase := range []struct {
		name string
		draw func(pen stripPen)
	}{
		{name: "LEVEL", draw: func(pen stripPen) { scene.drawStripLevel(pen, plane) }},
		{name: "GS", draw: func(pen stripPen) { scene.drawStripSpeed(pen, plane) }},
		{name: "TRK", draw: func(pen stripPen) { scene.drawStripTrack(pen, plane) }},
		{name: "DIST / BRG", draw: func(pen stripPen) { scene.drawStripRange(pen, receiver, plane) }},
		{name: "POS", draw: func(pen stripPen) { scene.drawStripPos(pen, plane) }},
		{name: "SEEN", draw: func(pen stripPen) { scene.drawStripSeen(pen, time.Time{}, plane) }},
		{name: "the little aeroplane", draw: func(pen stripPen) { scene.drawStripAttitude(pen, plane) }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(stripGuardSide, stripGuardSide)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			canv.Clear(theme.Night.Field)
			testCase.draw(stripPen{dst: canv})

			if got := filterPainted(canv); got != 0 {
				t.Errorf("%s drawer painted %d pixels with its field off the plan, want none", testCase.name, got)
			}
		})
	}
}

// TestDrawHalfCodesShowsTheWarningBoxOnAnEmergency checks the one thing a half
// strip's ident line does differently from a full one: the warning box takes
// the hex's own place rather than following it, because a half strip has no
// room for both. The existing drawStripCodes tests only ever put the
// emergency on the selected aircraft, which takes the full-strip path
// instead of this one, so the half strip's own version of the same rule had
// nothing exercising it.
func TestDrawHalfCodesShowsTheWarningBoxOnAnEmergency(t *testing.T) {
	t.Parallel()

	const (
		halfCodesCanvasWidth  = 200
		halfCodesCanvasHeight = 20
		halfCodesRowY         = 8
	)

	scene := &Scene{faces: Faces{Small: rowPlanSmall(t)}, pal: theme.Night}

	draw := func(emergency bool) *canvas.Canvas {
		canv, err := canvas.New(halfCodesCanvasWidth, halfCodesCanvasHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)

		plane := airplane.Snapshot{ICAO: icaoFirst, Emergency: emergency}
		scene.drawHalfCodes(stripPen{dst: canv, small: halfCodesRowY}, 0, plane)

		return canv
	}

	quiet := draw(false)
	if got := colourCount(quiet, quiet.Bounds(), theme.Night.Warn); got != 0 {
		t.Errorf("a half strip with nothing wrong painted %d pixels of the warning colour, want none", got)
	}

	squawking := draw(true)
	if colourCount(squawking, squawking.Bounds(), theme.Night.Warn) == 0 {
		t.Error("a half strip squawking an emergency painted no warning colour, want the box drawn in the hex's place")
	}
}

// buildBareFace parses a minimal, valid PSF2 font with two blank glyphs and no
// unicode table, so indexOf falls back to treating a code point as a glyph
// index directly: any rune at or past the glyph count, the trend arrows
// included, reports no glyph. It exists for TestDrawTagTrend's fallback case,
// which needs a face that genuinely does not carry U+2191/U+2193. Both faces
// this package's other tests load do carry them (fonts.Body among them), so
// none of those can stand in for one.
func buildBareFace(tb testing.TB) *psf.Font {
	tb.Helper()

	const (
		bareGlyphs     = 2
		bareGlyphSide  = 8
		bareHeaderSize = 32
	)

	data := make([]byte, bareHeaderSize+bareGlyphs*bareGlyphSide)
	copy(data, []byte{0x72, 0xb5, 0x4a, 0x86})
	binary.LittleEndian.PutUint32(data[8:], bareHeaderSize)
	binary.LittleEndian.PutUint32(data[16:], bareGlyphs)
	binary.LittleEndian.PutUint32(data[20:], bareGlyphSide)
	binary.LittleEndian.PutUint32(data[24:], bareGlyphSide)
	binary.LittleEndian.PutUint32(data[28:], bareGlyphSide)

	font, err := psf.Parse(data)
	if err != nil {
		tb.Fatalf("psf.Parse of the bare test font: %v", err)
	}

	return font
}

// TestDrawTagTrend checks the tag's climb/descend marker: no arrow when
// level, the glyph when the face carries U+2191/U+2193, and the flight
// strips' own drawn triangle as a fallback when it does not. uAirwaves gives
// no guarantee a console font carries those two code points, and a shape
// drawn on the canvas always renders where a missing glyph would not.
func TestDrawTagTrend(t *testing.T) {
	t.Parallel()

	const (
		trendCanvasWidth  = 60
		trendCanvasHeight = 40
		trendPenX         = 10
		trendTopY         = 4
		trendClimbRate    = 2000.0
		trendDescendRate  = -2000.0
	)

	newCanvas := func(tb testing.TB) *canvas.Canvas {
		tb.Helper()

		canv, err := canvas.New(trendCanvasWidth, trendCanvasHeight)
		if err != nil {
			tb.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)

		return canv
	}

	// trend is one call to the shared mark drawer at the pen and top every case
	// here uses, in the scene's own body face and in the accent, so a case says
	// which rate it is about and nothing else.
	trend := func(scene *Scene, canv *canvas.Canvas, rate float64) int {
		return scene.drawTrend(canv, scene.faces.Body, trendPenX, trendTopY, rate, theme.Night.Accent)
	}

	t.Run("level draws no arrow and returns the pen unchanged", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{faces: Faces{Body: rowPlanFaces(t)}, pal: theme.Night}

		if got := trend(scene, newCanvas(t), 0); got != trendPenX {
			t.Errorf("drawTrend(level) pen = %d, want %d unchanged", got, trendPenX)
		}
	})

	t.Run("climbing draws the glyph when the face carries it", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{faces: Faces{Body: rowPlanFaces(t)}, pal: theme.Night}
		canv := newCanvas(t)

		if got := trend(scene, canv, trendClimbRate); got <= trendPenX {
			t.Errorf("drawTrend(climbing) pen = %d, want more than %d", got, trendPenX)
		}

		if colourCount(canv, canv.Bounds(), theme.Night.Accent) == 0 {
			t.Error("drawTrend(climbing) painted nothing in the accent colour")
		}
	})

	t.Run("descending draws the glyph when the face carries it", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{faces: Faces{Body: rowPlanFaces(t)}, pal: theme.Night}
		canv := newCanvas(t)

		if got := trend(scene, canv, trendDescendRate); got <= trendPenX {
			t.Errorf("drawTrend(descending) pen = %d, want more than %d", got, trendPenX)
		}

		if colourCount(canv, canv.Bounds(), theme.Night.Accent) == 0 {
			t.Error("drawTrend(descending) painted nothing in the accent colour")
		}
	})

	t.Run("a face with no arrow glyph falls back to the drawn triangle", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{faces: Faces{Body: buildBareFace(t)}, pal: theme.Night}
		canv := newCanvas(t)

		want := trendPenX + vertMarker + vertGap
		if got := trend(scene, canv, trendClimbRate); got != want {
			t.Errorf("drawTrend(no glyph) pen = %d, want %d", got, want)
		}

		if colourCount(canv, canv.Bounds(), theme.Night.Accent) == 0 {
			t.Error("drawTrend(no glyph) painted nothing where the fallback triangle should be")
		}
	})
}

// TestDrawTagWhere checks the tag's bottom line: DIST/BRG when the bearing is
// known, and the dash placeholder when bearingTo cannot work one out. The
// receiver sitting at exactly (0, 0) is one of the two ways that happens (the
// other, an aircraft with no position, is bearingTo's own TestBearingTo case),
// and it is the one nothing already drawing a tag exercises: every fixture
// elsewhere in this package gives the receiver a real position.
func TestDrawTagWhere(t *testing.T) {
	t.Parallel()

	const (
		whereCanvasWidth  = 120
		whereCanvasHeight = 20
		wherePenX         = 4
		whereTopY         = 4
		whereLon          = 4.5
	)

	scene := &Scene{faces: Faces{Body: rowPlanFaces(t)}, pal: theme.Night}
	plane := airplane.Snapshot{Latitude: coordinateSampleLat + 1, Longitude: whereLon}

	draw := func(receiver source.Receiver) int {
		canv, err := canvas.New(whereCanvasWidth, whereCanvasHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)
		scene.drawTagWhere(canv, wherePenX, whereTopY, plane, receiver)

		return filterPainted(canv)
	}

	known := draw(source.Receiver{Latitude: coordinateSampleLat, Longitude: whereLon})
	unknown := draw(source.Receiver{})

	if known == 0 {
		t.Fatal("a known bearing painted nothing")
	}

	if unknown == 0 {
		t.Fatal("an unknown bearing painted nothing, want the dash placeholder drawn")
	}

	if known == unknown {
		t.Error("a known bearing and the unknown-bearing dashes painted the same number of pixels")
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

// TestParseRange checks the allow list --range reads: "auto", every value
// inside the scope's own limits, and everything outside them refused with an
// error errors.Is can match against ErrRange. The limits come from scope.New
// rather than being retyped here, so a change to that package's own defaults
// cannot leave this test checking numbers ParseRange no longer enforces.
func TestParseRange(t *testing.T) {
	t.Parallel()

	limits := scope.New()
	low, high := limits.GetMin(), limits.GetMax()

	for _, testCase := range []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{name: "auto reads as zero", input: RangeAuto, want: 0},
		{name: "the minimum round-trips", input: strconv.FormatFloat(low, 'f', -1, 64), want: low},
		{name: "the maximum round-trips", input: strconv.FormatFloat(high, 'f', -1, 64), want: high},
		{name: "just under the minimum is rejected", input: strconv.FormatFloat(low-1, 'f', -1, 64), wantErr: true},
		{name: "just over the maximum is rejected", input: strconv.FormatFloat(high+1, 'f', -1, 64), wantErr: true},
		{name: "NaN parses as a number but fails the guard", input: "NaN", wantErr: true},
		{name: "an empty string is rejected", input: "", wantErr: true},
		{name: "a non-number is rejected", input: "forty", wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseRange(testCase.input)
			if got != testCase.want {
				t.Errorf("ParseRange(%q) = %v, want %v", testCase.input, got, testCase.want)
			}

			if testCase.wantErr {
				if !errors.Is(err, ErrRange) {
					t.Errorf("ParseRange(%q) err = %v, want it to match ErrRange", testCase.input, err)
				}

				return
			}

			if err != nil {
				t.Errorf("ParseRange(%q) unexpected error: %v", testCase.input, err)
			}
		})
	}
}

// stubSource hands back whatever frame it currently holds. It is this
// package's own analogue of radar_test's fakeSource: a package-radar test
// file cannot reach into radar_test to reuse it, and a test below wants to
// change the receiver's position between two draws by mutating the frame
// field directly rather than rebuilding the scene around it.
type stubSource struct {
	frame source.Frame
}

func (s *stubSource) Frame() source.Frame { return s.frame }

func (*stubSource) Close() error { return nil }

// BiasTee reads the same frame the scene does, so a test that sets the frame's
// bias-tee state gets a source that agrees with it.
//
//nolint:nonamedreturns // mirrors the interface it satisfies.
func (s *stubSource) BiasTee() (supported, enabled bool) {
	return s.frame.BiasTee.Supported, s.frame.BiasTee.Enabled
}

// SetBiasTee is never reached: the scene hands the flip to a Toggler and never
// calls the source itself, which is the rule this stub helps pin.
func (*stubSource) SetBiasTee(bool) error { return errStubBiasTee }

// errStubBiasTee is what stubSource.SetBiasTee reports if anything ever calls
// it, which nothing in the scene is allowed to.
//
//nolint:gochecknoglobals // error sentinel, not state.
var errStubBiasTee = errors.New("stub source: SetBiasTee must not be called from the scene")

// layerTestFaces loads all four embedded faces, for the tests below that
// draw a whole frame rather than measuring one glyph.
func layerTestFaces(tb testing.TB) Faces {
	tb.Helper()

	small, err := fonts.Small()
	if err != nil {
		tb.Fatalf("fonts.Small: %v", err)
	}

	body, err := fonts.Body()
	if err != nil {
		tb.Fatalf("fonts.Body: %v", err)
	}

	bold, err := fonts.BodyBold()
	if err != nil {
		tb.Fatalf("fonts.BodyBold: %v", err)
	}

	large, err := fonts.Large()
	if err != nil {
		tb.Fatalf("fonts.Large: %v", err)
	}

	return Faces{Small: small, Body: body, BodyBold: bold, Large: large}
}

// The background-layer tests' fixed geometry and receiver position.
const (
	layerCanvasWidth  = 1280
	layerCanvasHeight = 720

	// layerBaseLat and layerBaseLon sit exactly on the 1e-4 degree grid the
	// layer key snaps coordinates to, so a perturbation lands a known
	// distance from the nearest grid line rather than depending on where in
	// a cell the base position happens to fall.
	layerBaseLat = 52.3100
	layerBaseLon = 4.7700

	// layerSubGridMove is comfortably under that grid; layerOverGridMove is
	// comfortably over it.
	layerSubGridMove  = 0.00003
	layerOverGridMove = 0.0002

	// layerRangeNm is the range the redraw tests run at, picked only to be a
	// fixed, valid value so a change away from it is a real change.
	layerRangeNm = 60.0
)

// layerFrame is a frame with an empty sky at a fixed receiver longitude and
// the given latitude. An empty sky keeps fitRange from moving the range
// underneath a test that is reading layerRuns rather than the picture; the
// latitude is the only coordinate any caller below varies.
func layerFrame(lat float64) source.Frame {
	return source.Frame{
		Receiver: source.Receiver{Latitude: lat, Longitude: layerBaseLon, HasFix: true, Mode: source.FixManual},
	}
}

// layerScene builds a Scene and canvas for the redraw-counting tests below,
// plus the source itself so a test can swap its frame between two draws.
func layerScene(tb testing.TB) (*Scene, *canvas.Canvas, *stubSource) {
	tb.Helper()

	canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	src := &stubSource{frame: layerFrame(layerBaseLat)}
	scene := New(layerTestFaces(tb), src, scope.New(scope.WithCurrent(layerRangeNm)))

	return scene, canv, src
}

// TestBackgroundLayerSkipsUnchangedDraws checks the whole point of the
// background layer: two frames whose key has not moved copy the same layer
// rather than drawing the rings, the labels and the shore twice.
func TestBackgroundLayerSkipsUnchangedDraws(t *testing.T) {
	t.Parallel()

	scene, canv, _ := layerScene(t)

	scene.Draw(canv, 0)
	scene.Draw(canv, 0)

	if scene.layerRuns != 1 {
		t.Errorf("layerRuns after two identical draws = %d, want 1", scene.layerRuns)
	}
}

// TestBackgroundLayerRedrawsOnKeyChanges checks that everything the layer key
// carries actually triggers a redraw when it changes: the range, the
// palette, the airfield markers and the shore, plus the canvas size, which is
// part of the key but not one of the run-time toggles.
func TestBackgroundLayerRedrawsOnKeyChanges(t *testing.T) {
	t.Parallel()

	t.Run("a range change redraws", func(t *testing.T) {
		t.Parallel()

		scene, canv, _ := layerScene(t)
		scene.Draw(canv, 0)

		scene.Apply(Settings{RangeNm: layerRangeNm * 2})
		scene.Draw(canv, 0)

		if scene.layerRuns != 2 {
			t.Errorf("layerRuns after a range change = %d, want 2", scene.layerRuns)
		}
	})

	t.Run("a new palette redraws", func(t *testing.T) {
		t.Parallel()

		scene, canv, _ := layerScene(t)
		scene.Draw(canv, 0)

		scene.SetPalette(theme.Day)
		scene.Draw(canv, 0)

		if scene.layerRuns != 2 {
			t.Errorf("layerRuns after SetPalette = %d, want 2", scene.layerRuns)
		}
	})

	t.Run("toggling the airports redraws", func(t *testing.T) {
		t.Parallel()

		scene, canv, _ := layerScene(t)
		scene.Draw(canv, 0)

		if !scene.Handle(input.Key{Kind: input.Rune, Rune: 'a'}) {
			t.Fatal("Handle('a') = false, want the scene to take it")
		}

		scene.Draw(canv, 0)

		if scene.layerRuns != 2 {
			t.Errorf("layerRuns after toggling the airports = %d, want 2", scene.layerRuns)
		}
	})

	t.Run("toggling the shore redraws", func(t *testing.T) {
		t.Parallel()

		scene, canv, _ := layerScene(t)
		scene.Draw(canv, 0)

		if !scene.Handle(input.Key{Kind: input.Rune, Rune: 'm'}) {
			t.Fatal("Handle('m') = false, want the scene to take it")
		}

		scene.Draw(canv, 0)

		if scene.layerRuns != 2 {
			t.Errorf("layerRuns after toggling the shore = %d, want 2", scene.layerRuns)
		}
	})

	t.Run("a different canvas size redraws", func(t *testing.T) {
		t.Parallel()

		scene, canv, _ := layerScene(t)
		scene.Draw(canv, 0)

		other, err := canvas.New(layerCanvasWidth/2, layerCanvasHeight/2)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scene.Draw(other, 0)

		if scene.layerRuns != 2 {
			t.Errorf("layerRuns after a different canvas size = %d, want 2", scene.layerRuns)
		}
	})
}

// TestBackgroundLayerSnapTolerance checks the reason layerKeyFor rounds the
// receiver's position before comparing it: a self-locate estimate that
// drifts in its last decimal every frame must not cost a redraw, but a
// receiver that has genuinely moved must.
func TestBackgroundLayerSnapTolerance(t *testing.T) {
	t.Parallel()

	t.Run("a move under the grid does not redraw", func(t *testing.T) {
		t.Parallel()

		scene, canv, src := layerScene(t)
		scene.Draw(canv, 0)

		src.frame = layerFrame(layerBaseLat + layerSubGridMove)

		scene.Draw(canv, 0)

		if scene.layerRuns != 1 {
			t.Errorf("layerRuns after a sub-grid move = %d, want 1", scene.layerRuns)
		}
	})

	t.Run("a move past the grid redraws", func(t *testing.T) {
		t.Parallel()

		scene, canv, src := layerScene(t)
		scene.Draw(canv, 0)

		src.frame = layerFrame(layerBaseLat + layerOverGridMove)

		scene.Draw(canv, 0)

		if scene.layerRuns != 2 {
			t.Errorf("layerRuns after a move past the grid = %d, want 2", scene.layerRuns)
		}
	})
}

// TestSnap checks the layer key's own rounding directly: the NaN guard, the
// clamp at either end, and an ordinary value settling on the grid.
func TestSnap(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		degrees float64
		want    int64
	}{
		{name: "NaN folds to zero", degrees: math.NaN(), want: 0},
		{name: "an ordinary value rounds to the grid", degrees: 52.31003, want: 523100},
		{
			name:    "a value past the positive clamp pins to it",
			degrees: maxDegrees + 1, want: int64(maxDegrees * fixPrecision),
		},
		{
			name:    "a value past the negative clamp pins to it",
			degrees: -maxDegrees - 1, want: int64(-maxDegrees * fixPrecision),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := snap(testCase.degrees); got != testCase.want {
				t.Errorf("snap(%v) = %d, want %d", testCase.degrees, got, testCase.want)
			}
		})
	}
}

// TestMinimalGeometry checks the guard on a canvas too small to have a radius
// at all, alongside the ordinary case.
func TestMinimalGeometry(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		bounds image.Rectangle
		wantOK bool
	}{
		{name: "an ordinary canvas has a radius", bounds: image.Rect(0, 0, 200, 200), wantOK: true},
		{name: "a canvas too small to have a radius reports false", bounds: image.Rect(0, 0, 1, 1), wantOK: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			geom, ok := minimalGeometry(testCase.bounds)
			if ok != testCase.wantOK {
				t.Fatalf("minimalGeometry(%v) ok = %v, want %v", testCase.bounds, ok, testCase.wantOK)
			}

			if ok && geom.rangeR <= 0 {
				t.Errorf("minimalGeometry(%v) rangeR = %d, want > 0", testCase.bounds, geom.rangeR)
			}
		})
	}
}

// TestMeasureMinimal checks the two guards measureMinimal has of its own: a
// canvas too small for minimalGeometry to give it a radius, and a receiver
// with no position, the "nothing known" state a Receiver and a Snapshot
// share.
func TestMeasureMinimal(t *testing.T) {
	t.Parallel()

	t.Run("a canvas too small for a radius is not drawable", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(1, 1)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scene := &Scene{}

		if _, drawable := scene.measureMinimal(canv, source.Receiver{}); drawable {
			t.Error("measureMinimal(...) drawable = true, want false on a canvas too small for a radius")
		}
	})

	t.Run("a receiver with no position is not plottable", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(200, 200)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scene := &Scene{scopeRange: scope.New(scope.WithCurrent(layerRangeNm))}

		view, drawable := scene.measureMinimal(canv, source.Receiver{})
		if !drawable {
			t.Fatal("measureMinimal(...) drawable = false, want true")
		}

		if view.plottable {
			t.Error("measureMinimal(...) plottable = true with a (0, 0) receiver, want false")
		}
	})
}

// clipEpsilon is how close two floats have to be to count as equal in the
// clip cases below, whose endpoints come out of a square root rather than
// out of integer arithmetic.
const clipEpsilon = 1e-9

// approxEqual reports whether two floats are within clipEpsilon of each
// other.
func approxEqual(got, want float64) bool {
	return math.Abs(got-want) <= clipEpsilon
}

// TestCircleClip drives every branch of clip: wholly inside, wholly outside
// in the two different ways a segment can miss, a crossing chord, one end on
// each side in both directions, a zero-length segment on each side, and a
// tangent that touches the circle without leaving anything wide enough to
// draw.
func TestCircleClip(t *testing.T) {
	t.Parallel()

	ring := circle{centerX: 0, centerY: 0, radius: 10}

	for _, testCase := range []struct {
		name       string
		ring       circle
		seg        segment
		wantInside bool
		want       segment
	}{
		{
			name:       "wholly inside is returned unchanged",
			ring:       ring,
			seg:        segment{fromX: -1, fromY: 0, toX: 1, toY: 0},
			wantInside: true,
			want:       segment{fromX: -1, fromY: 0, toX: 1, toY: 0},
		},
		{
			name:       "wholly outside, missing the circle entirely",
			ring:       ring,
			seg:        segment{fromX: 20, fromY: 20, toX: 30, toY: 20},
			wantInside: false,
		},
		{
			name:       "wholly outside, its line crosses the circle beyond the segment's own span",
			ring:       ring,
			seg:        segment{fromX: 20, fromY: 0, toX: 30, toY: 0},
			wantInside: false,
		},
		{
			name:       "a chord with both ends outside crosses through the middle",
			ring:       ring,
			seg:        segment{fromX: -20, fromY: 0, toX: 20, toY: 0},
			wantInside: true,
			want:       segment{fromX: -10, fromY: 0, toX: 10, toY: 0},
		},
		{
			name:       "one end inside, one end outside, entering",
			ring:       ring,
			seg:        segment{fromX: 0, fromY: 0, toX: 20, toY: 0},
			wantInside: true,
			want:       segment{fromX: 0, fromY: 0, toX: 10, toY: 0},
		},
		{
			name:       "one end outside, one end inside, the other way round",
			ring:       ring,
			seg:        segment{fromX: 20, fromY: 0, toX: 0, toY: 0},
			wantInside: true,
			want:       segment{fromX: 10, fromY: 0, toX: 0, toY: 0},
		},
		{
			name:       "a zero-length segment inside",
			ring:       ring,
			seg:        segment{fromX: 1, fromY: 1, toX: 1, toY: 1},
			wantInside: true,
			want:       segment{fromX: 1, fromY: 1, toX: 1, toY: 1},
		},
		{
			name:       "a zero-length segment outside",
			ring:       ring,
			seg:        segment{fromX: 20, fromY: 20, toX: 20, toY: 20},
			wantInside: false,
		},
		{
			name:       "a tangent touches the circle at one point, too narrow to draw",
			ring:       circle{centerX: 0, centerY: 0, radius: 5},
			seg:        segment{fromX: -10, fromY: 5, toX: 10, toY: 5},
			wantInside: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, inside := testCase.ring.clip(testCase.seg)
			if inside != testCase.wantInside {
				t.Fatalf("clip(%+v) inside = %v, want %v", testCase.seg, inside, testCase.wantInside)
			}

			if !inside {
				return
			}

			same := approxEqual(got.fromX, testCase.want.fromX) &&
				approxEqual(got.fromY, testCase.want.fromY) &&
				approxEqual(got.toX, testCase.want.toX) &&
				approxEqual(got.toY, testCase.want.toY)
			if !same {
				t.Errorf("clip(%+v) = %+v, want %+v", testCase.seg, got, testCase.want)
			}
		})
	}
}

// TestDrawShoreLineDecimation checks the two things the shore's own
// decimation does that a plain per-segment draw would not: points closer
// together than shoreMinSegment are folded into the next segment rather than
// dropped, and the last point of a polyline is always reached even when it is
// one of the close ones. A polyline of fewer than two points draws nothing at
// all.
func TestDrawShoreLineDecimation(t *testing.T) {
	t.Parallel()

	scene := &Scene{pal: theme.Night}
	proj := projector{centerX: 100, centerY: 100, radius: 90, scopeNm: 60, limitNm: 60, cosLat0: 1, scale: 1}
	ring := circle{centerX: 100, centerY: 100, radius: 90}

	t.Run("fewer than two points draws nothing", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(200, 200)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scene.drawShoreLine(canv, proj, ring, nil)

		if got := colourCount(canv, canv.Bounds(), theme.Night.Shore); got != 0 {
			t.Errorf("drawShoreLine with a nil polyline painted %d pixels, want 0", got)
		}
	})

	t.Run("close points are folded in and the last point is still reached", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(200, 200)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		// The middle point sits 0.006 pixels from the first, well inside
		// shoreMinSegment, so it is folded into the segment that follows
		// rather than drawn on its own. The last point sits 30 pixels out,
		// which is what draws the line this test looks for.
		line := shore.Polyline{{Lat: 0, Lon: 0}, {Lat: 0, Lon: 0.0001}, {Lat: 0, Lon: 0.5}}

		scene.drawShoreLine(canv, proj, ring, line)

		if got := colourCount(canv, canv.Bounds(), theme.Night.Shore); got == 0 {
			t.Error("drawShoreLine with folded points painted 0 pixels, want the line to the last point")
		}
	})
}

// TestApplyRangePinsTheScope checks the other half of Settings.RangeNm that
// TestApply does not: a positive value pins the scope and blocks auto range
// from moving it even with a far aircraft on screen, and zero leaves auto
// range in charge.
func TestApplyRangePinsTheScope(t *testing.T) {
	t.Parallel()

	// pinnedRangeNm is comfortably inside the scope's own limits, and
	// farAircraftNm is far enough out that auto range would certainly widen
	// past it, so a held range proves the pin rather than a coincidence.
	const (
		pinnedRangeNm = 50.0
		farAircraftNm = 300.0
	)

	farFrame := func() source.Frame {
		frame := layerFrame(layerBaseLat)
		frame.Planes = []airplane.Snapshot{
			{ICAO: icaoSampleReal, Latitude: layerBaseLat + farAircraftNm/nmPerDegree, Longitude: layerBaseLon},
		}

		return frame
	}

	t.Run("a positive RangeNm pins the scope and blocks auto range", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scopeRange := scope.New(scope.WithCurrent(pinnedRangeNm))
		scene := New(layerTestFaces(t), &stubSource{frame: farFrame()}, scopeRange)
		scene.Apply(Settings{RangeNm: pinnedRangeNm})

		scene.Draw(canv, 0)

		if got := scopeRange.GetCurrent(); got != pinnedRangeNm {
			t.Errorf("range after Apply(RangeNm: %g) with a %g nm contact = %g, want it held at %g",
				pinnedRangeNm, farAircraftNm, got, pinnedRangeNm)
		}
	})

	t.Run("a zero RangeNm leaves auto range fitting the fleet", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scopeRange := scope.New(scope.WithCurrent(pinnedRangeNm))
		scene := New(layerTestFaces(t), &stubSource{frame: farFrame()}, scopeRange)
		scene.Apply(Settings{RangeNm: 0})

		scene.Draw(canv, 0)

		if got := scopeRange.GetCurrent(); got == pinnedRangeNm {
			t.Errorf("range after Apply(RangeNm: 0) with a %g nm contact = %g, want auto range to widen it",
				farAircraftNm, got)
		}
	})
}

// BenchmarkBackground measures the background layer on its own, at a narrow
// and a wide range, against the real embedded coastline rather than a
// synthetic one. BenchmarkDraw already prices a whole frame; this isolates
// the one part of it that does not run every frame, so a regression in the
// rings, the shore or the airports shows up here rather than being lost in
// the aircraft's own cost.
//
// It lives in this file, rather than in the _test package, because it calls
// renderLayer and layerKeyFor directly, both unexported.
func BenchmarkBackground(b *testing.B) {
	set, err := shore.Load()
	if err != nil {
		b.Fatalf("shore.Load: %v", err)
	}

	for _, testCase := range []struct {
		name    string
		rangeNm float64
	}{
		{name: "40nm", rangeNm: 40},
		{name: "400nm", rangeNm: 400},
	} {
		b.Run(testCase.name, func(b *testing.B) {
			canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
			if err != nil {
				b.Fatalf("canvas.New: %v", err)
			}

			frame := layerFrame(layerBaseLat)
			scene := New(layerTestFaces(b), &stubSource{frame: frame},
				scope.New(scope.WithCurrent(testCase.rangeNm)), WithShore(set))

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				scene.renderLayer(canv, scene.layerKeyFor(canv, frame), frame)
			}
		})
	}
}

// TestSceneSpeed checks the velocity sentinel: uAirwaves marks a velocity no
// message has carried with -1, and the scope drew that as "-1" until this
// formatter existed. Every negative figure reads as unknown, because none of
// them is a speed.
func TestSceneSpeed(t *testing.T) {
	t.Parallel()

	const (
		cruise     = 484.0
		justUnder  = -0.5
		sentinel   = -1.0
		veryFast   = 999.4
		unknownFig = "---"
	)

	for _, testCase := range []struct {
		name  string
		value float64
		want  string
	}{
		{name: "the sentinel reads as unknown", value: sentinel, want: unknownFig},
		{name: "any negative reads as unknown", value: justUnder, want: unknownFig},
		{name: "not a number reads as unknown", value: math.NaN(), want: unknownFig},
		{name: "minus infinity reads as unknown", value: math.Inf(-1), want: unknownFig},
		{name: "a standstill is a reading, not a sentinel", value: 0, want: "0"},
		{name: "one knot is a reading", value: 1, want: "1"},
		{name: "a cruise speed rounds to whole knots", value: cruise, want: "484"},
		{name: "a fast one rounds up", value: veryFast, want: "999"},
		{name: "infinity falls back to whole's own dash", value: math.Inf(1), want: "-"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			if got := string(scene.speed(testCase.value)); got != testCase.want {
				t.Errorf("speed(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestKnownHeading checks the heading sentinel at the boundary. Zero is due
// north and a real course; only a negative figure means nothing was decoded.
func TestKnownHeading(t *testing.T) {
	t.Parallel()

	const (
		sentinel  = -1.0
		justUnder = -0.0001
		northWest = 315.0
		wrapped   = 360.0
	)

	for _, testCase := range []struct {
		name  string
		value float64
		want  bool
	}{
		{name: "the sentinel is unknown", value: sentinel, want: false},
		{name: "any negative is unknown", value: justUnder, want: false},
		{name: "not a number is unknown", value: math.NaN(), want: false},
		{name: "due north is known", value: 0, want: true},
		{name: "one degree is known", value: 1, want: true},
		{name: "north west is known", value: northWest, want: true},
		{name: "a full circle is known", value: wrapped, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := knownHeading(testCase.value); got != testCase.want {
				t.Errorf("knownHeading(%v) = %v, want %v", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestTrailFade checks the ramp segmentAlpha runs from a mode's floor up to
// the head. The floors themselves are TestTrailPlan's subject.
func TestTrailFade(t *testing.T) {
	t.Parallel()

	const span = 4.0

	for index := range int(span) + 1 {
		if got := segmentAlpha(trailMaxAlpha, index, span); got != trailMaxAlpha {
			t.Errorf("segmentAlpha(flat, %d, %v) = %v, want %v", index, span, got, trailMaxAlpha)
		}
	}

	tail := segmentAlpha(trailMinAlpha, 0, span)
	head := segmentAlpha(trailMinAlpha, int(span), span)

	if tail != trailMinAlpha {
		t.Errorf("segmentAlpha at the tail = %v, want %v", tail, trailMinAlpha)
	}

	if head != trailMaxAlpha {
		t.Errorf("segmentAlpha at the head = %v, want %v", head, trailMaxAlpha)
	}

	if middle := segmentAlpha(trailMinAlpha, int(span)/2, span); middle <= tail || middle >= head {
		t.Errorf("segmentAlpha in the middle = %v, want between %v and %v", middle, tail, head)
	}
}

// tablelessPSF1 builds a 256-glyph PSF1 font with no unicode table, which is
// how the kernel ships a font that only covers Latin-1. Resolving a code point
// is then the code point itself, so anything past 255 is simply absent, which
// is the one way a test can hand cutMarker a face with no ellipsis: all four
// embedded Terminus faces carry U+2026.
func tablelessPSF1(tb testing.TB) *psf.Font {
	tb.Helper()

	const (
		glyphs   = 256
		charsize = 16
		header   = 4
	)

	data := make([]byte, header+glyphs*charsize)
	data[0], data[1] = 0x36, 0x04
	data[2], data[3] = 0, charsize

	font, err := psf.Parse(data)
	if err != nil {
		tb.Fatalf("psf.Parse of a synthetic PSF1: %v", err)
	}

	return font
}

// TestCutMarker checks both markers a cut label can end with: the ellipsis on
// a face that carries one, and the full stop on a face that does not.
func TestCutMarker(t *testing.T) {
	t.Parallel()

	small, err := fonts.Small()
	if err != nil {
		t.Fatalf("fonts.Small: %v", err)
	}

	if got := cutMarker(small); got != cutEllipsis {
		t.Errorf("cutMarker(Terminus small) = %q, want %q", got, cutEllipsis)
	}

	if got := cutMarker(tablelessPSF1(t)); got != cutDot {
		t.Errorf("cutMarker(a face with no ellipsis) = %q, want %q", got, cutDot)
	}
}

// TestFitLabel checks the measured cut: a label that fits is never touched, a
// label that does not loses exactly enough runes for the marker, and a width
// with no room for even one glyph draws nothing at all.
func TestFitLabel(t *testing.T) {
	t.Parallel()

	small, err := fonts.Small()
	if err != nil {
		t.Fatalf("fonts.Small: %v", err)
	}

	const label = "BEAST 192.168.1.159:30005"

	glyph := small.Width()
	step := glyph + labelTracking
	whole := trackedWidth(small, len([]rune(label)))

	for _, testCase := range []struct {
		name       string
		width      int
		wantHead   string
		wantMarker string
	}{
		{name: "no room at all draws nothing", width: 0, wantHead: "", wantMarker: ""},
		{name: "less than one glyph draws nothing", width: glyph - 1, wantHead: "", wantMarker: ""},
		{name: "exactly the label is left whole", width: whole, wantHead: label, wantMarker: ""},
		{name: "more than the label is left whole", width: whole + step, wantHead: label, wantMarker: ""},
		{
			name: "one glyph short loses two runes and gains a marker",
			// One rune of room is given back to the marker, so a label cut at
			// n runes shows n-1 of its own.
			width: whole - step, wantHead: label[:len(label)-2], wantMarker: cutEllipsis,
		},
		{name: "one glyph of room is all marker", width: glyph, wantHead: "", wantMarker: cutEllipsis},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			head, marker := fitLabel(small, label, testCase.width)
			if head != testCase.wantHead || marker != testCase.wantMarker {
				t.Errorf("fitLabel(%q, %d) = (%q, %q), want (%q, %q)",
					label, testCase.width, head, marker, testCase.wantHead, testCase.wantMarker)
			}
		})
	}
}

// TestTrackedWidth checks the inverse of fitRunes, including the zero and
// negative counts a right-aligned run of no glyphs at all would ask for.
func TestTrackedWidth(t *testing.T) {
	t.Parallel()

	small, err := fonts.Small()
	if err != nil {
		t.Fatalf("fonts.Small: %v", err)
	}

	step := small.Width() + labelTracking

	for _, testCase := range []struct {
		name  string
		runes int
		want  int
	}{
		{name: "a negative count is no width", runes: -1, want: 0},
		{name: "no glyphs is no width", runes: 0, want: 0},
		{name: "one glyph carries no tracking after it", runes: 1, want: small.Width()},
		{name: "two glyphs carry one gap", runes: 2, want: 2*small.Width() + labelTracking},
		{name: "four glyphs carry three gaps", runes: 4, want: 4*step - labelTracking},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := trackedWidth(small, testCase.runes); got != testCase.want {
				t.Errorf("trackedWidth(%d) = %d, want %d", testCase.runes, got, testCase.want)
			}
		})
	}
}

// TestSyncSelectionUnpinsALostAircraft checks the one branch the external
// selection tests cannot reach from outside: a pinned aircraft that leaves the
// list takes its pin with it, because there is nothing left to hold.
func TestSyncSelectionUnpinsALostAircraft(t *testing.T) {
	t.Parallel()

	scene := &Scene{pinned: true, selICAO: "GONE01", selIndex: 2}

	scene.syncSelection(source.Frame{Planes: []airplane.Snapshot{
		{ICAO: icaoFirst}, {ICAO: icaoSecond},
	}})

	if scene.pinned {
		t.Error("the pin survived the aircraft leaving the list")
	}

	if scene.selICAO != icaoFirst || scene.selIndex != 0 {
		t.Errorf("selection = %q at %d, want the nearest aircraft at 0", scene.selICAO, scene.selIndex)
	}
}

// TestCapEngaged checks which softkeys show the engaged bar: the genuine
// on-or-off settings follow their own state, and everything else answers
// false, because a value cap (colour, theme, trails, filter) and a key with
// no setting behind it at all have no off position for the bar to mark.
func TestCapEngaged(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		scene  Scene
		toggle capToggle
		want   bool
	}{
		{name: "auto on", scene: Scene{autoRange: true}, toggle: capAuto, want: true},
		{name: "auto off", scene: Scene{}, toggle: capAuto, want: false},
		{name: "airports on", scene: Scene{airports: true}, toggle: capAirports, want: true},
		{name: "airports off", scene: Scene{}, toggle: capAirports, want: false},
		{name: "shore on", scene: Scene{shoreOn: true}, toggle: capShore, want: true},
		{name: "shore off", scene: Scene{}, toggle: capShore, want: false},
		{name: "orbit on", scene: Scene{orbiting: true}, toggle: capOrbit, want: true},
		{name: "orbit off", scene: Scene{}, toggle: capOrbit, want: false},
		{name: "envelope on", scene: Scene{envelope: true}, toggle: capEnvelope, want: true},
		{name: "envelope off", scene: Scene{}, toggle: capEnvelope, want: false},
		{name: "bias-tee on", scene: Scene{biasEnabled: true}, toggle: capBiasTee, want: true},
		{name: "bias-tee off", scene: Scene{}, toggle: capBiasTee, want: false},
		{name: "wide on", scene: Scene{wide: true}, toggle: capWide, want: true},
		{name: "wide off", scene: Scene{}, toggle: capWide, want: false},
		{name: "quit has no setting behind it", scene: Scene{}, toggle: capAlways, want: false},
		{name: "the colour cap carries a value, not a state", scene: Scene{colour: ColourAirline}, toggle: capColour},
		{name: "the theme cap carries a value, not a state", scene: Scene{light: true}, toggle: capTheme},
		{
			// The K cap always names the look it is drawn in, the same way C
			// names the colour mode and L the theme. A bar under it would be
			// claiming an off position the key does not have.
			name:   "the look cap carries a value, not a state",
			scene:  Scene{look: theme.LookPhosphor},
			toggle: capLook,
		},
		{name: "the trail cap carries a value, not a state", scene: Scene{trail: trailAll}, toggle: capTrails},
		{
			name:   "the filter cap carries a value, not a state",
			scene:  Scene{filter: filterState{kind: filterBand, band: bandLow}},
			toggle: capFilter,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := testCase.scene
			if got := scene.capEngaged(testCase.toggle); got != testCase.want {
				t.Errorf("capEngaged(%d) = %v, want %v", testCase.toggle, got, testCase.want)
			}
		})
	}
}

// fakeToggler counts how many times it is asked to flip, which is what lets
// TestToggleBiasTee prove the key reaches the Toggler exactly once and does
// nothing more, without any of it touching a real dongle.
type fakeToggler struct {
	calls int
}

// Toggle records that it was called. It does no work of its own: a real
// implementation would talk to the USB device, and that is exactly what
// these tests must not do.
func (f *fakeToggler) Toggle() { f.calls++ }

// TestToggleBiasTee checks the three answers b has to give: nothing wired, a
// toggler wired to a source with no bias-tee, and a toggler wired to one that
// has it.
//
// The first two report false rather than swallowing the key. That is what
// lets b fall through to the run loop exactly as the camera keys already do
// outside the 3D view: nothing in the loop binds b either, so the press is a
// no-op either way, but a key that silently ate itself would be a key whose
// effect turned up as a surprise later rather than as nothing happening at
// all.
func TestToggleBiasTee(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		wireUp    bool
		supported bool
		wantTaken bool
		wantCalls int
	}{
		{name: "no toggler wired reports false and calls nothing"},
		{
			name:      "a toggler wired to an unsupported source reports false and calls nothing",
			wireUp:    true,
			supported: false,
		},
		{
			name:      "a toggler wired to a supported source reports true and is called once",
			wireUp:    true,
			supported: true,
			wantTaken: true,
			wantCalls: 1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			toggler := &fakeToggler{}
			scene := &Scene{biasSupported: testCase.supported}

			if testCase.wireUp {
				scene.biasTee = toggler
			}

			if got := scene.toggleBiasTee(); got != testCase.wantTaken {
				t.Errorf("toggleBiasTee() = %v, want %v", got, testCase.wantTaken)
			}

			if toggler.calls != testCase.wantCalls {
				t.Errorf("Toggle() called %d times, want %d", toggler.calls, testCase.wantCalls)
			}
		})
	}
}

// TestDrawCopiesBiasTeeFromFrame checks that Draw takes biasSupported and
// biasEnabled off the frame it was just handed rather than asking the source
// again. The pair rides on the frame for the same reason elapsed does: the b
// key reads them between two frames, and reaching back through the seam here
// would put a USB control transfer on a path that has to stay free of one.
// stubSource.SetBiasTee reports an error sentinel for exactly this reason:
// nothing in the scene is allowed to call it.
func TestDrawCopiesBiasTeeFromFrame(t *testing.T) {
	t.Parallel()

	frame := layerFrame(layerBaseLat)
	frame.BiasTee = source.BiasTeeState{Supported: true, Enabled: true}

	src := &stubSource{frame: frame}
	scene := New(layerTestFaces(t), src, scope.New(scope.WithCurrent(layerRangeNm)))

	canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(canv, 0)

	if !scene.biasSupported {
		t.Error("biasSupported = false after Draw, want it copied from the frame")
	}

	if !scene.biasEnabled {
		t.Error("biasEnabled = false after Draw, want it copied from the frame")
	}
}

// TestCapLabel checks what the two cycling keys say. They carry the value they
// are on rather than the name of the setting, because a cap reading COLOUR
// says there is a colour mode without saying which one.
func TestCapLabel(t *testing.T) {
	t.Parallel()

	// The five cycling entries, taken from the bar itself rather than written
	// out again, so a rename of any of their labels cannot leave this table
	// testing a cap that is no longer on screen.
	trailCap, colourCap, filterCap, themeCap, lookCap := keyCaps[4], keyCaps[7], keyCaps[8], keyCaps[9], keyCaps[10]

	for _, testCase := range []struct {
		name  string
		scene Scene
		entry keyCap
		want  string
	}{
		{name: "altitude mode", scene: Scene{colour: ColourAltitude}, entry: colourCap, want: labelAltitude},
		{name: "airline mode", scene: Scene{colour: ColourAirline}, entry: colourCap, want: labelAirline},
		{
			name:  "the zero colour mode reads as altitude",
			scene: Scene{}, entry: colourCap, want: labelAltitude,
		},
		{name: caseNight, scene: Scene{}, entry: themeCap, want: labelNight},
		{name: caseDay, scene: Scene{light: true}, entry: themeCap, want: labelDay},
		{name: "the glass look", scene: Scene{look: theme.LookGlass}, entry: lookCap, want: labelGlass},
		{name: "the phosphor look", scene: Scene{look: theme.LookPhosphor}, entry: lookCap, want: labelPhosphor},
		{name: "the mono look", scene: Scene{look: theme.LookMono}, entry: lookCap, want: labelMono},
		{name: "the zero look reads as glass", scene: Scene{}, entry: lookCap, want: labelGlass},
		{
			name:  "an unrecognised look reads as glass",
			scene: Scene{look: theme.Look("sepia")}, entry: lookCap, want: labelGlass,
		},
		{name: "the long trail mode", scene: Scene{trail: trailLong}, entry: trailCap, want: labelTrailLong},
		{name: "the short trail mode", scene: Scene{trail: trailShort}, entry: trailCap, want: labelTrailShort},
		{name: "the all trail mode", scene: Scene{trail: trailAll}, entry: trailCap, want: labelTrailAll},
		{name: "the off trail mode", scene: Scene{trail: trailOff}, entry: trailCap, want: labelTrailOff},
		{
			name:  "the zero trail mode reads as long",
			scene: Scene{}, entry: trailCap, want: labelTrailLong,
		},
		{name: "the filter on everything", scene: Scene{}, entry: filterCap, want: labelFilterAll},
		{
			name:  "the filter on a band",
			scene: Scene{filter: filterState{kind: filterBand, band: bandMid}},
			entry: filterCap, want: labelFilterMid,
		},
		{
			name:  "the filter on an operator",
			scene: Scene{filter: filterState{kind: filterOperator, icao: sampleICAO}},
			entry: filterCap, want: sampleICAO,
		},
		{
			name:  "the filter on the uncoloured aircraft",
			scene: Scene{filter: filterState{kind: filterOther}},
			entry: filterCap, want: legendOther,
		},
		{
			name: "a plain toggle keeps its own label", scene: Scene{},
			entry: keyCap{key: "R", label: "AUTO", state: capAuto}, want: "AUTO",
		},
		{
			name: "so does a key that is not a toggle", scene: Scene{},
			entry: keyCap{key: "Q", label: "QUIT"}, want: "QUIT",
		},
		{
			name:  "the wide cap keeps its own label whichever way it is set",
			scene: Scene{wide: true},
			entry: keyCaps[12], want: "WIDE",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := testCase.scene
			if got := scene.capLabel(testCase.entry); got != testCase.want {
				t.Errorf("capLabel(%+v) = %q, want %q", testCase.entry, got, testCase.want)
			}
		})
	}
}

// TestSetPaletteKeepsPalLightAndLookInStep checks that pal, light and look
// move together: SetPalette takes light and look off the palette it is
// handed rather than being told either on its own, so the three can never
// disagree about which colours and which look are on screen.
//
// A scene told its look by one path and its colours by another would sooner
// or later draw one look's colours under the other one's cap. One setter is
// what rules that out.
func TestSetPaletteKeepsPalLightAndLookInStep(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		pal       theme.Palette
		wantLook  theme.Look
		wantLight bool
	}{
		{name: "glass night", pal: theme.Night, wantLook: theme.LookGlass, wantLight: false},
		{name: "glass day", pal: theme.Day, wantLook: theme.LookGlass, wantLight: true},
		{name: "phosphor night", pal: theme.PhosphorNight, wantLook: theme.LookPhosphor, wantLight: false},
		{name: "phosphor day", pal: theme.PhosphorDay, wantLook: theme.LookPhosphor, wantLight: true},
		{name: "mono night", pal: theme.MonoNight, wantLook: theme.LookMono, wantLight: false},
		{name: "mono day", pal: theme.MonoDay, wantLook: theme.LookMono, wantLight: true},
		{
			// A palette nobody named reads as the zero value, not as glass:
			// that reading only happens where a Look is asked what it is
			// called, which is lookLabel's job and not SetPalette's.
			name:      "a hand-built palette carries the zero look, not a name it was never given",
			pal:       theme.Palette{},
			wantLook:  theme.Look(""),
			wantLight: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			scene.SetPalette(testCase.pal)

			if scene.pal != testCase.pal {
				t.Errorf("pal = %+v, want %+v", scene.pal, testCase.pal)
			}

			if scene.look != testCase.wantLook {
				t.Errorf("look = %q, want %q", scene.look, testCase.wantLook)
			}

			if scene.light != testCase.wantLight {
				t.Errorf("light = %v, want %v", scene.light, testCase.wantLight)
			}
		})
	}
}

// TestNewStartsOnGlassNightWithoutAPalette checks that a scene built with no
// WithPalette option still comes up with its look and light flag set: New
// calls SetPalette(theme.Night) itself rather than assigning pal in its
// struct literal, so the three fields agree from the first frame even when
// nobody has picked a palette.
//
// A literal that assigned pal on its own would leave look and light on their
// zero values, and the K cap would open on a scene that had never been told
// which look it was drawn in.
func TestNewStartsOnGlassNightWithoutAPalette(t *testing.T) {
	t.Parallel()

	scene := New(layerTestFaces(t), &stubSource{}, scope.New(scope.WithCurrent(layerRangeNm)))

	if scene.pal != theme.Night {
		t.Errorf("pal = %+v, want theme.Night", scene.pal)
	}

	if scene.light {
		t.Error("light = true on a fresh scene, want false: glass night is not a light field")
	}

	if scene.look != theme.LookGlass {
		t.Errorf("look = %q, want %q", scene.look, theme.LookGlass)
	}
}

// TestDrawCapsFitsAtItsWidestState measures the bar the way the scene itself
// draws it, at the widest state it ever reaches: the 3D view, a dongle with a
// bias-tee, airline colour, the short trail, the mid-band filter and the
// phosphor look, the longest of the three look words.
//
// drawCaps stops after the cap that reaches the right margin rather than
// wrapping or eliding one, so a bar that had grown past its budget would lose
// its last control silently instead of complaining. This is what would have
// caught the K cap doing that: it checks both that the bar stops short of the
// margin and that it still reaches the last control, because a bar that
// dropped a cap would also "fit".
func TestDrawCapsFitsAtItsWidestState(t *testing.T) {
	t.Parallel()

	// widestBarLastCapEnd is where the tilt cap, the last one in the bar,
	// ends at exactly this state. It is named so a change to any cap's word
	// shows up here rather than as a number nobody can place.
	const widestBarLastCapEnd = 1195

	canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene := &Scene{
		faces:         layerTestFaces(t),
		shown:         View3D,
		biasSupported: true,
		colour:        ColourAirline,
		trail:         trailShort,
		filter:        filterState{kind: filterBand, band: bandMid},
	}
	scene.SetPalette(theme.PhosphorNight)

	lay := layout{dst: canv, right: layerCanvasWidth - baseMargin}

	pen := scene.drawCaps(&lay, baseMargin, 0, keyCaps[:])
	pen = scene.drawCaps(&lay, pen, 0, biasCap[:])
	pen = scene.drawCaps(&lay, pen, 0, view3DCaps[:])

	if pen >= lay.right {
		t.Errorf("the widest bar reached x=%d, want it to stop before the %d margin", pen, lay.right)
	}

	if pen < widestBarLastCapEnd {
		t.Errorf("the widest bar ended at x=%d, before the last cap at %d", pen, widestBarLastCapEnd)
	}
}

// --- minimal mode's recentring -------------------------------------------

// The fixture the centring tests work around: a point and a second point two
// degrees north and four degrees east of it, so every interpolation in
// between lands on numbers that are exact in float64.
var (
	//nolint:gochecknoglobals // a fixture is data, and a struct cannot be const.
	followStart = geo{lat: 51, lon: 4}

	//nolint:gochecknoglobals // see above.
	followTarget = geo{lat: 53, lon: 8}
)

// followPlane is one aircraft at a position, with nothing else filled in. The
// centroid only reads the two coordinates.
func followPlane(latitude, longitude float64) airplane.Snapshot {
	return airplane.Snapshot{ICAO: icaoSampleReal, Latitude: latitude, Longitude: longitude}
}

func TestCentroidOf(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		planes airplanes.List
		want   geo
		wantOK bool
	}{
		{name: "nothing on the field at all"},
		{
			name:   "one aircraft is its own centroid",
			planes: airplanes.List{followPlane(52, 4)},
			want:   geo{lat: 52, lon: 4}, wantOK: true,
		},
		{
			name:   "two aircraft average",
			planes: airplanes.List{followPlane(51, 3), followPlane(53, 5)},
			want:   geo{lat: 52, lon: 4}, wantOK: true,
		},
		{
			name: "an undecoded position is skipped rather than counted as (0, 0)",
			planes: airplanes.List{
				followPlane(51, 3), followPlane(0, 0), followPlane(53, 5),
			},
			want: geo{lat: 52, lon: 4}, wantOK: true,
		},
		{
			name:   "a NaN coordinate is skipped for the same reason",
			planes: airplanes.List{followPlane(51, 3), followPlane(math.NaN(), 5), followPlane(53, 5)},
			want:   geo{lat: 52, lon: 4}, wantOK: true,
		},
		{
			name:   "a fleet with no positions at all has no centroid",
			planes: airplanes.List{followPlane(0, 0), followPlane(0, 0)},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// A zero Scene filters nothing, so this measures the arithmetic
			// and not the predicate; TestCentroidSkipsFilteredAircraft is what
			// covers the other half.
			got, ok := (&Scene{}).centroidOf(testCase.planes)
			if ok != testCase.wantOK {
				t.Fatalf("centroidOf(...) ok = %v, want %v", ok, testCase.wantOK)
			}

			if ok && got != testCase.want {
				t.Errorf("centroidOf(...) = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

func TestPositioned(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		lat, lon float64
		want     bool
	}{
		{name: "a real position", lat: 52, lon: 4, want: true},
		{name: "the undecoded sentinel", lat: 0, lon: 0},
		{name: "a latitude of zero on a real meridian", lat: 0, lon: 4, want: true},
		{name: "a longitude of zero on a real parallel", lat: 52, lon: 0, want: true},
		{name: "a NaN latitude", lat: math.NaN(), lon: 4},
		{name: "a NaN longitude", lat: 52, lon: math.NaN()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := positioned(testCase.lat, testCase.lon); got != testCase.want {
				t.Errorf("positioned(%v, %v) = %v, want %v", testCase.lat, testCase.lon, got, testCase.want)
			}
		})
	}
}

// TestEase pins the two things the glide curve has to be: exact at both ends
// and at the middle, so the centre lands where it was aimed, and slower than
// linear at the start and faster at the end, which is what makes it an ease
// rather than a ramp.
func TestEase(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		progress float64
		want     float64
	}{
		{name: "the start is exact", progress: 0, want: 0},
		{name: "the middle is exact", progress: 0.5, want: 0.5},
		{name: "the end is exact", progress: 1, want: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := ease(testCase.progress); got != testCase.want {
				t.Errorf("ease(%v) = %v, want exactly %v", testCase.progress, got, testCase.want)
			}
		})
	}

	t.Run("it eases in and out rather than ramping", func(t *testing.T) {
		t.Parallel()

		if got := ease(0.25); got >= 0.25 {
			t.Errorf("ease(0.25) = %v, want less than 0.25", got)
		}

		if got := ease(0.75); got <= 0.75 {
			t.Errorf("ease(0.75) = %v, want more than 0.75", got)
		}
	})
}

func TestBetween(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		along float64
		want  geo
	}{
		{name: "nothing along is the start", along: 0, want: followStart},
		{name: "halfway is halfway", along: 0.5, want: geo{lat: 52, lon: 6}},
		{name: "all the way is the target", along: 1, want: followTarget},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := between(followStart, followTarget, testCase.along); got != testCase.want {
				t.Errorf("between(%+v, %+v, %v) = %+v, want %+v",
					followStart, followTarget, testCase.along, got, testCase.want)
			}
		})
	}
}

// TestGlide walks the centre along one move and reads it off at the three
// points the curve is pinned at. The scene is aimed twice on purpose: the
// first aim snaps, because there is nothing on screen for a glide to keep
// continuous, and only the second one starts a path.
func TestGlide(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		elapsed     time.Duration
		want        geo
		wantGliding bool
	}{
		{
			name: "at the start it has not moved", elapsed: 0,
			want: followStart, wantGliding: true,
		},
		{
			name: "an elapsed behind the start pins rather than reversing", elapsed: -glideSpan,
			want: followStart, wantGliding: true,
		},
		{
			name: "halfway through it is halfway there", elapsed: glideSpan / 2,
			want: geo{lat: 52, lon: 6}, wantGliding: true,
		},
		{
			name: "at the end it is there and done", elapsed: glideSpan,
			want: followTarget,
		},
		{
			name:    "a frame that arrived late lands at the end, not past it",
			elapsed: 4 * glideSpan, want: followTarget,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: ViewMinimal, recentre: time.Minute}
			scene.aim(followStart, 0)
			scene.aim(followTarget, 0)

			scene.glide(testCase.elapsed)

			if scene.centre != testCase.want {
				t.Errorf("centre after glide(%v) = %+v, want %+v", testCase.elapsed, scene.centre, testCase.want)
			}

			if scene.gliding != testCase.wantGliding {
				t.Errorf("gliding after glide(%v) = %v, want %v",
					testCase.elapsed, scene.gliding, testCase.wantGliding)
			}
		})
	}

	t.Run("a scene that is not gliding is left alone", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{shown: ViewMinimal, recentre: time.Minute}
		scene.aim(followStart, 0)
		scene.glide(glideSpan)

		if scene.centre != followStart {
			t.Errorf("centre = %+v, want the snapped %+v", scene.centre, followStart)
		}
	})
}

// TestDue checks the cadence rule: the first centring is owed immediately so
// a scope that has just started does not sit on the receiver, and every one
// after it waits out the whole interval.
func TestDue(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	every := time.Minute

	for _, testCase := range []struct {
		name       string
		haveCentre bool
		at         time.Duration
		want       bool
	}{
		{name: "the first centring is owed at once", want: true},
		{name: "nothing is owed a moment later", haveCentre: true, at: time.Second},
		{name: "nothing is owed one tick short of the interval", haveCentre: true, at: every - time.Nanosecond},
		{name: "the interval itself is due", haveCentre: true, at: every, want: true},
		{name: "and so is anything past it", haveCentre: true, at: 10 * every, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{recentre: every, haveCentre: testCase.haveCentre, lastFit: base}
			if got := scene.due(base.Add(testCase.at)); got != testCase.want {
				t.Errorf("due(lastFit + %v) = %v, want %v", testCase.at, got, testCase.want)
			}
		})
	}
}

// TestFollowLeavesAnEmptySkyAlone checks that a frame with nothing on it is
// not a centring: the cadence stays owed, so the first aircraft to arrive
// with a position is centred on immediately rather than after an interval of
// waiting.
func TestFollowLeavesAnEmptySkyAlone(t *testing.T) {
	t.Parallel()

	scene := &Scene{shown: ViewMinimal, recentre: time.Minute, scopeRange: scope.New(), autoRange: true}
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)

	scene.follow(source.Frame{Now: now}, 0)

	if scene.haveCentre {
		t.Fatal("an empty sky produced a centre, want none")
	}

	scene.follow(source.Frame{Planes: airplanes.List{followPlane(52, 4)}, Now: now}, 0)

	if !scene.haveCentre {
		t.Fatal("the first aircraft with a position did not produce a centre")
	}

	if want := (geo{lat: 52, lon: 4}); scene.centre != want {
		t.Errorf("centre = %+v, want %+v", scene.centre, want)
	}
}

func TestFollowing(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		view     View
		recentre time.Duration
		want     bool
	}{
		{name: "minimal with a cadence follows", view: ViewMinimal, recentre: time.Minute, want: true},
		{name: "minimal with the cadence off does not", view: ViewMinimal},
		{name: "the full scope never does, cadence or no cadence", recentre: time.Minute},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: testCase.view, recentre: testCase.recentre}
			if got := scene.following(); got != testCase.want {
				t.Errorf("following() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestMinimalOrigin checks which point minimal mode projects from. The
// receiver is the answer until the cadence has chosen a centre, which is what
// makes --recenter 0 the behaviour minimal mode had before the flag existed.
func TestMinimalOrigin(t *testing.T) {
	t.Parallel()

	receiver := source.Receiver{Latitude: 52.3105, Longitude: 4.7683}
	centred := geo{lat: 53, lon: 6}

	for _, testCase := range []struct {
		name       string
		view       View
		recentre   time.Duration
		haveCentre bool
		want       geo
	}{
		{
			name: "the cadence off keeps the receiver", view: ViewMinimal,
			haveCentre: true, want: geo{lat: receiver.Latitude, lon: receiver.Longitude},
		},
		{
			name: "the cadence on with nothing chosen yet keeps the receiver too",
			view: ViewMinimal, recentre: time.Minute,
			want: geo{lat: receiver.Latitude, lon: receiver.Longitude},
		},
		{
			name: "the full scope is never recentred", recentre: time.Minute,
			haveCentre: true, want: geo{lat: receiver.Latitude, lon: receiver.Longitude},
		},
		{
			name: "minimal following a chosen centre projects from it", view: ViewMinimal,
			recentre: time.Minute, haveCentre: true, want: centred,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{
				shown: testCase.view, recentre: testCase.recentre,
				haveCentre: testCase.haveCentre, centre: centred,
			}

			if got := scene.minimalOrigin(receiver); got != testCase.want {
				t.Errorf("minimalOrigin(%+v) = %+v, want %+v", receiver, got, testCase.want)
			}
		})
	}
}

// TestApplyRecentreForgetsTheOldCentre checks that a fresh settings block
// does not leave the scene gliding towards somewhere the new cadence never
// chose, and that turning the cadence off puts the projection back on the
// receiver rather than leaving it parked over the traffic.
func TestApplyRecentreForgetsTheOldCentre(t *testing.T) {
	t.Parallel()

	scene := &Scene{shown: ViewMinimal, recentre: time.Minute, scopeRange: scope.New()}
	scene.aim(followStart, 0)
	scene.aim(followTarget, 0)

	scene.applyRecentre(0)

	if scene.recentre != 0 || scene.haveCentre || scene.gliding {
		t.Errorf("after applyRecentre(0): recentre = %v, haveCentre = %v, gliding = %v; want 0, false, false",
			scene.recentre, scene.haveCentre, scene.gliding)
	}

	if scene.centre != (geo{}) {
		t.Errorf("centre after applyRecentre(0) = %+v, want the zero point", scene.centre)
	}
}

// TestFollowAllocations is the promise the draw path makes on the frames the
// cadence actually fires on, which BenchmarkDraw's fixed clock never reaches:
// working out a centroid, aiming at it and refitting the range are all done
// on the stack.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestFollowAllocations(t *testing.T) {
	scene := &Scene{shown: ViewMinimal, recentre: time.Second, scopeRange: scope.New(), autoRange: true}
	frame := source.Frame{
		Planes: airplanes.List{followPlane(52, 4), followPlane(53, 5), followPlane(0, 0)},
	}

	base := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	tick := 0

	// Every call steps the frame clock a whole minute, so the cadence is due
	// on each one and the expensive half of follow runs every time.
	if got := testing.AllocsPerRun(50, func() {
		tick++
		frame.Now = base.Add(time.Duration(tick) * time.Minute)
		scene.follow(frame, time.Duration(tick)*time.Second)
	}); got != 0 {
		t.Errorf("follow allocated %.1f times per frame, want 0", got)
	}
}

// TestOverlayToggles checks that m and a reach whichever of the two pairs the
// view on screen reads, and that neither pair can be moved from the other
// view. Minimal starts bare and the scope starts as a map; a shared pair
// would mean every trip between them began with two key presses to undo.
func TestOverlayToggles(t *testing.T) {
	t.Parallel()

	t.Run("minimal's pair starts off and the scope's starts on", func(t *testing.T) {
		t.Parallel()

		scene := New(Faces{}, source.Empty{}, scope.New())
		if !scene.shoreDrawn() || !scene.airportsDrawn() {
			t.Error("the full scope started with an overlay off, want both on")
		}

		scene.shown = ViewMinimal

		if scene.shoreDrawn() || scene.airportsDrawn() {
			t.Error("minimal started with an overlay on, want both off")
		}
	})

	t.Run("a press in minimal leaves the scope's pair alone", func(t *testing.T) {
		t.Parallel()

		scene := New(Faces{}, source.Empty{}, scope.New())
		scene.shown = ViewMinimal

		scene.toggleShore()
		scene.toggleAirports()

		if !scene.shoreDrawn() || !scene.airportsDrawn() {
			t.Error("minimal's toggles did not turn its own overlays on")
		}

		scene.shown = ViewScope

		if !scene.shoreDrawn() || !scene.airportsDrawn() {
			t.Error("a press in minimal changed the full scope's overlays, want them untouched")
		}
	})

	t.Run("a press in the full scope leaves minimal's pair alone", func(t *testing.T) {
		t.Parallel()

		scene := New(Faces{}, source.Empty{}, scope.New())
		scene.toggleShore()
		scene.toggleAirports()

		if scene.shoreDrawn() || scene.airportsDrawn() {
			t.Error("the full scope's toggles did not turn its own overlays off")
		}

		scene.shown = ViewMinimal

		if scene.shoreDrawn() || scene.airportsDrawn() {
			t.Error("a press in the full scope changed minimal's overlays, want them untouched")
		}
	})
}

// arrowCanvas draws one direction arrow at a known centre on a canvas big
// enough to hold it with room to spare, and hands back the canvas and the
// centre pixel.
//
// The arrow is drawn through drawArrow rather than by reaching for its three
// corners, so what the cases below read is the shape the rows and the panel
// actually get.
func arrowCanvas(tb testing.TB, degrees float64) (*canvas.Canvas, int, int) {
	tb.Helper()

	const side = 41

	canv, err := canvas.New(side, side)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)

	middle := side / 2
	scene := &Scene{}

	// drawArrow takes the left edge of the cell and centres the arrow in it,
	// so the left edge is half a cell back from where the middle is wanted.
	scene.drawArrow(canv, middle-arrowCell/2, middle, degrees, theme.Night.Ink)

	return canv, middle, middle
}

// TestArrowPointsAtTheAngle checks the four cardinal directions land where a
// compass says they do: up at zero, right at ninety, down at a half turn and
// left at three quarters.
//
// It reads the apex pixel rather than counting ink, because the apex is the
// whole point of the shape. The old arrow was a nine-pixel bitmap turned by
// nearest neighbour, which put the point in roughly the right place at these
// four angles and nowhere near it in between; a triangle worked out from the
// angle lands it exactly, and this is what says so.
func TestArrowPointsAtTheAngle(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		degrees float64
		offX    int
		offY    int
	}{
		{name: "north points up the screen", degrees: 0, offY: -int(arrowApex)},
		{name: "east points right", degrees: 90, offX: int(arrowApex)},
		{name: "south points down", degrees: 180, offY: int(arrowApex)},
		{name: "west points left", degrees: 270, offX: -int(arrowApex)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, centreX, centreY := arrowCanvas(t, testCase.degrees)
			apexX, apexY := centreX+testCase.offX, centreY+testCase.offY

			if got := canv.Image().RGBAAt(apexX, apexY); got != theme.Night.Ink {
				t.Errorf("the pixel at the apex (%d, %d) is %v, want the ink %v",
					apexX, apexY, got, theme.Night.Ink)
			}

			// One pixel past the apex is outside the shape, which is what says
			// the arrow stops where it is meant to rather than running on.
			pastX, pastY := centreX+testCase.offX*2, centreY+testCase.offY*2
			if got := canv.Image().RGBAAt(pastX, pastY); got == theme.Night.Ink {
				t.Errorf("the pixel past the apex (%d, %d) is ink, want the arrow to end at the apex",
					pastX, pastY)
			}
		})
	}
}

// TestArrowHasNoGapsAtAnyAngle walks the whole compass in fifteen-degree steps
// and checks the shaft is solid at each one.
//
// This is the failure the bitmap arrow actually had. Nearest-neighbour
// rotation of a nine-pixel sprite dropped a pixel out of the shaft at most
// angles off the four axes, so the arrow broke into a dotted line exactly
// where it was being asked to say something a compass letter could not. A
// filled triangle cannot do that, and a step of fifteen degrees is fine enough
// to have caught it when it could.
func TestArrowHasNoGapsAtAnyAngle(t *testing.T) {
	t.Parallel()

	const (
		step   = 15.0
		steps  = int(degreesPerCircle / step)
		sample = 2.0
	)

	for index := range steps {
		degrees := float64(index) * step

		t.Run(strconv.FormatFloat(degrees, 'f', 0, 64), func(t *testing.T) {
			t.Parallel()

			canv, centreX, centreY := arrowCanvas(t, degrees)

			// A point on the axis between the centre and the apex. Inside a
			// solid needle this is ink at every angle; inside a broken one it
			// is where the break shows.
			sin, cos := math.Sincos(degrees * math.Pi / halfCircle)
			atX := centreX + round(sin*sample)
			atY := centreY - round(cos*sample)

			if got := canv.Image().RGBAAt(atX, atY); got != theme.Night.Ink {
				t.Errorf("at %g degrees the pixel %g px along the shaft (%d, %d) is %v, want ink",
					degrees, sample, atX, atY, got)
			}

			// The centre itself is inside the base as well, so a triangle that
			// had collapsed would fail here even if the sample above landed on
			// a surviving corner.
			if got := canv.Image().RGBAAt(centreX, centreY); got != theme.Night.Ink {
				t.Errorf("at %g degrees the arrow's own centre is %v, want ink", degrees, got)
			}
		})
	}
}

// TestRound checks the conversion drawArrow rounds its three corners through,
// including the half-way case that separates rounding from truncation.
func TestRound(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		in   float64
		want int
	}{
		{in: 0, want: 0},
		{in: 0.4, want: 0},
		{in: 0.5, want: 1},
		{in: 2.5, want: 3},
		{in: -0.4, want: 0},
		{in: -0.5, want: -1},
		{in: -2.5, want: -3},
	} {
		t.Run(strconv.FormatFloat(testCase.in, 'f', -1, 64), func(t *testing.T) {
			t.Parallel()

			if got := round(testCase.in); got != testCase.want {
				t.Errorf("round(%g) = %d, want %d", testCase.in, got, testCase.want)
			}
		})
	}
}

// TestReceiverLineOpensWithLoc checks every form of the header's receiver line
// starts with the same word.
//
// Without it the line was a mode word and two numbers with nothing saying what
// they were of, and next to the aircraft position on the card it read as
// another aeroplane. LOC is what a chart calls a location. It is drawn in the
// band's own ink whatever the fix mode is, because the word is a label and
// does not change: only the mode word after it carries the fix colour.
//
// The check renders the line and compares its first four characters against a
// canvas carrying nothing but the prefix, drawn at the same place in the same
// face. That is stricter than counting pixels and it reads the thing on
// screen rather than the constant behind it.
func TestReceiverLineOpensWithLoc(t *testing.T) {
	t.Parallel()

	const (
		side = 240
		top  = 8
		left = 4
	)

	faces := Faces{Small: rowPlanSmall(t)}
	prefixWidth, prefixHeight := text.Measure(faces.Small, locPrefix)
	prefixBox := image.Rect(left, top, left+prefixWidth, top+prefixHeight)

	reference, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	reference.Clear(theme.Night.Field)
	text.Draw(reference, faces.Small, left, top, locPrefix, theme.Night.BandInk)

	for _, testCase := range []struct {
		name     string
		receiver source.Receiver
	}{
		{
			name: "a position the operator gave",
			receiver: source.Receiver{
				Latitude: 52.31, Longitude: 4.77, HasFix: true,
				Label: source.LabelManual, Mode: source.FixManual,
			},
		},
		{
			name: "a GPS fix",
			receiver: source.Receiver{
				Latitude: 52.31, Longitude: 4.77, HasFix: true,
				Label: source.LabelGPS, Mode: source.FixGPS3D,
			},
		},
		{
			name: "a self-locate estimate",
			receiver: source.Receiver{
				Latitude: 52.31, Longitude: 4.77, ConfidenceNm: 22,
				Label: source.LabelEstimate, Mode: source.FixEstimated,
			},
		},
		{
			name:     "nothing known yet",
			receiver: source.Receiver{Label: source.LabelNone, Mode: source.FixNone},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			canv.Clear(theme.Night.Field)

			scene := &Scene{faces: faces}
			scene.SetPalette(theme.Night)
			scene.drawReceiverLine(&layout{dst: canv, left: left}, top, testCase.receiver)

			if !samePixels(canv, reference, prefixBox) {
				t.Errorf("the line does not open with %q", locPrefix)
			}

			// The rest of the line has to be there as well, or a line that
			// drew the prefix and stopped would pass the comparison above.
			rest := image.Rect(prefixBox.Max.X, top, side, top+prefixHeight)
			if colourCount(canv, rest, theme.Night.Field) == rest.Dx()*rest.Dy() {
				t.Error("nothing was drawn after the prefix, want the mode word")
			}
		})
	}
}

// samePixels reports whether two canvases agree everywhere inside box.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func samePixels(got, want *canvas.Canvas, box image.Rectangle) bool {
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if got.Image().RGBAAt(x, y) != want.Image().RGBAAt(x, y) {
				return false
			}
		}
	}

	return true
}

// TestReceiverLineGPSStates checks the word each GPS state renders as on the
// receiver line, pixel for pixel against a reference built the same way the
// line itself is, which is what TestReceiverLineOpensWithLoc also does and
// for the same reason: it reads what is on screen rather than the constant
// behind it.
//
// FixGPSNoFix is the case that matters here. Its word changed from the three
// dashes it used to be to GPS LOST, because the state itself changed meaning:
// it used to be unreachable, and now it is what a gpsd that had a fix and
// lost it reports for the half minute its last position is still good enough
// to centre a scope on.
func TestReceiverLineGPSStates(t *testing.T) {
	t.Parallel()

	const (
		side = 240
		top  = 8
		left = 4
	)

	for _, testCase := range []struct {
		name string
		mode source.FixMode
		word string
	}{
		{name: "a full fix", mode: source.FixGPS3D, word: gps3DText},
		{name: "a fix without altitude", mode: source.FixGPS2D, word: gps2DText},
		{name: "a fix lost and held", mode: source.FixGPSNoFix, word: gpsNoFixText},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			faces := Faces{Small: rowPlanSmall(t)}
			scene := &Scene{faces: faces}
			scene.SetPalette(theme.Night)

			receiver := source.Receiver{
				Latitude: 52.31, Longitude: 4.77,
				Label: source.LabelGPS, Mode: testCase.mode,
			}

			canv, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			canv.Clear(theme.Night.Field)
			scene.drawReceiverLine(&layout{dst: canv, left: left}, top, receiver)

			reference, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			reference.Clear(theme.Night.Field)

			pen := text.Draw(reference, faces.Small, left, top, locPrefix, theme.Night.BandInk)
			ink := scene.fixColour(testCase.mode, theme.Night.BandInk)
			text.Draw(reference, faces.Small, pen, top, testCase.word, ink)

			prefixWidth, height := text.Measure(faces.Small, locPrefix)
			wordWidth, _ := text.Measure(faces.Small, testCase.word)
			box := image.Rect(left, top, left+prefixWidth+wordWidth, top+height)

			if !samePixels(canv, reference, box) {
				t.Errorf("the line does not read %q after %q for %s", testCase.word, locPrefix, testCase.name)
			}
		})
	}
}

// TestReceiverLineDoubtMarker checks that an estimate the self-locator does
// not fully believe grows a doubt marker after its radius, set in the same
// caution colour the rest of the estimate line already carries.
//
// Violated counts the self-locator's own observations whose radio horizon
// does not reach the estimate it produced, so the marker is what says the
// confidence radius already had to be widened to cover a disagreement rather
// than being loose for no stated reason.
func TestReceiverLineDoubtMarker(t *testing.T) {
	t.Parallel()

	const (
		side = 240
		top  = 8
		left = 4
	)

	faces := Faces{Small: rowPlanSmall(t)}
	scene := &Scene{faces: faces}
	scene.SetPalette(theme.Night)

	receiverAgrees := source.Receiver{
		Latitude: 52.31, Longitude: 4.77, ConfidenceNm: 22,
		Label: source.LabelEstimate, Mode: source.FixEstimated,
	}
	receiverDoubts := receiverAgrees
	receiverDoubts.Violated = 3

	canvAgrees, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	canvAgrees.Clear(theme.Night.Field)
	scene.drawReceiverLine(&layout{dst: canvAgrees, left: left}, top, receiverAgrees)

	canvDoubts, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	canvDoubts.Clear(theme.Night.Field)
	scene.drawReceiverLine(&layout{dst: canvDoubts, left: left}, top, receiverDoubts)

	if samePixels(canvAgrees, canvDoubts, canvAgrees.Bounds()) {
		t.Error("an estimate under doubt drew the same line as one nothing disagrees with")
	}

	// The reference is built from the same calls drawReceiverLine makes for
	// the estimate branch, so the test follows the layout instead of a pixel
	// offset worked out by hand.
	reference, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	reference.Clear(theme.Night.Field)

	ink := scene.fixColour(source.FixEstimated, theme.Night.BandInk)
	pen := text.Draw(reference, faces.Small, left, top, locPrefix, theme.Night.BandInk)
	pen = text.Draw(reference, faces.Small, pen, top, estimatePrefix, ink)
	pen = drawBytes(reference, faces.Small, pen, top, scene.whole(receiverDoubts.ConfidenceNm), ink)
	pen = text.Draw(reference, faces.Small, pen, top, rangeUnit, ink)

	markerWidth, markerHeight := text.Measure(faces.Small, doubtMarker)
	markerBox := image.Rect(pen, top, pen+markerWidth, top+markerHeight)

	text.Draw(reference, faces.Small, pen, top, doubtMarker, theme.Night.Caution)

	if !samePixels(canvDoubts, reference, reference.Bounds()) {
		t.Error("the doubted estimate does not match the reference line with its marker")
	}

	if colourCount(canvAgrees, markerBox, theme.Night.Field) != markerBox.Dx()*markerBox.Dy() {
		t.Error("an estimate nothing disagrees with carries something past its radius, want only field")
	}

	if colourCount(canvDoubts, markerBox, theme.Night.Caution) == 0 {
		t.Error("a doubted estimate carries no caution pixels past its radius")
	}
}

// TestDrawDoubt checks both of drawDoubt's branches directly: nothing drawn
// when nothing disagrees with the estimate, and the marker drawn in the
// caution colour when something does.
func TestDrawDoubt(t *testing.T) {
	t.Parallel()

	const (
		side = 64
		top  = 8
		pen  = 4
	)

	face := rowPlanSmall(t)
	scene := &Scene{}
	scene.SetPalette(theme.Night)

	t.Run("nothing to doubt leaves the canvas untouched", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(side, side)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)
		scene.drawDoubt(canv, face, pen, top, 0)

		clean, err := canvas.New(side, side)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		clean.Clear(theme.Night.Field)

		if !samePixels(canv, clean, canv.Bounds()) {
			t.Error("drawDoubt changed the canvas with nothing to doubt")
		}
	})

	t.Run("a violation draws the marker in the caution colour", func(t *testing.T) {
		t.Parallel()

		const violated = 1

		canv, err := canvas.New(side, side)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)
		scene.drawDoubt(canv, face, pen, top, violated)

		reference, err := canvas.New(side, side)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		reference.Clear(theme.Night.Field)
		text.Draw(reference, face, pen, top, doubtMarker, theme.Night.Caution)

		if !samePixels(canv, reference, canv.Bounds()) {
			t.Error("drawDoubt did not draw the marker in the caution colour")
		}
	})
}
