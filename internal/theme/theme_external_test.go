package theme_test

import (
	"errors"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/theme"
)

// wantAlpha is the alpha every palette colour must carry. The framebuffer has
// no alpha channel, so anything translucent would be silently flattened, and
// that is worth catching in a test rather than on the panel.
const wantAlpha = 0xFF

// fullChannel is a colour channel at its maximum, named so mnd does not flag
// a bare 0xFF where a test below builds a colour from scratch instead of
// reading one off a palette.
const fullChannel = 0xFF

// grayBelowMidLuminance and grayAboveMidLuminance are one packed grey value on
// each side of the threshold Light draws its line at: 187 measures 0.496933
// and 188 measures 0.502886 by the WCAG formula below, so the pair checks the
// boundary itself rather than only the extremes every real field lands
// nowhere near.
const (
	grayBelowMidLuminance = 187
	grayAboveMidLuminance = 188
)

// Field names, named once so goconst does not flag their repetition across the
// palette field lists and distinctness tables below.
const (
	nameField   = "Field"
	nameInk     = "Ink"
	nameMuted   = "Muted"
	nameAccent  = "Accent"
	nameData    = "Data"
	nameOK      = "OK"
	nameCaution = "Caution"
	nameWarn    = "Warn"
	nameKey     = "Key"
	nameRule    = "Rule"
	nameShore   = "Shore"
	nameAltLow  = "AltLow"
	nameAltMid  = "AltMid"
	nameAltHigh = "AltHigh"
	nameBand    = "Band"
	nameBandInk = "BandInk"
)

// namedColour pairs a palette field with the name a failure should report.
type namedColour struct {
	name string
	col  color.RGBA
}

// nightColours lists every field of theme.Night once, so the opacity and
// distinctness checks below can both table-drive off the same list.
func nightColours() []namedColour {
	return []namedColour{
		{name: nameField, col: theme.Night.Field},
		{name: nameInk, col: theme.Night.Ink},
		{name: nameMuted, col: theme.Night.Muted},
		{name: nameAccent, col: theme.Night.Accent},
		{name: nameRule, col: theme.Night.Rule},
		{name: nameAltLow, col: theme.Night.AltLow},
		{name: nameAltMid, col: theme.Night.AltMid},
		{name: nameAltHigh, col: theme.Night.AltHigh},
	}
}

func TestNightIsFullyOpaque(t *testing.T) {
	t.Parallel()

	for _, testCase := range nightColours() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.col.A; got != wantAlpha {
				t.Errorf("%s.A = %#x, want %#x", testCase.name, got, wantAlpha)
			}
		})
	}
}

// TestNightCoreColoursAreDistinct checks the five colours a reader has to be
// able to tell apart at a glance: the field, the ink, the muted chrome, the
// one accent, and the rule hairlines. Two of these landing on the same value
// would be invisible in the code but obvious on the panel.
func TestNightCoreColoursAreDistinct(t *testing.T) {
	t.Parallel()

	core := []namedColour{
		{name: nameField, col: theme.Night.Field},
		{name: nameInk, col: theme.Night.Ink},
		{name: nameMuted, col: theme.Night.Muted},
		{name: nameAccent, col: theme.Night.Accent},
		{name: nameRule, col: theme.Night.Rule},
	}

	assertPairwiseDistinct(t, core, nil)
}

// TestNightAltitudeBandsAreDistinct checks the three altitude bands. They are
// unused until the radar slice, but a scope that cannot tell a low aircraft
// from a high one by colour has lost the one channel it has for altitude.
func TestNightAltitudeBandsAreDistinct(t *testing.T) {
	t.Parallel()

	bands := []namedColour{
		{name: nameAltLow, col: theme.Night.AltLow},
		{name: nameAltMid, col: theme.Night.AltMid},
		{name: nameAltHigh, col: theme.Night.AltHigh},
	}

	assertPairwiseDistinct(t, bands, nil)
}

// assertPairwiseDistinct fails the test if any two colours in the list are
// equal, one subtest per pair, except pairs named in allowed: those are a
// palette's deliberate repeats, checked for equality elsewhere instead of
// checked for difference here. Passing nil allows nothing, so every pair is
// checked.
func assertPairwiseDistinct(t *testing.T, colours []namedColour, allowed map[string]bool) {
	t.Helper()

	for first := range colours {
		for second := first + 1; second < len(colours); second++ {
			left, right := colours[first], colours[second]
			pairName := left.name + " vs " + right.name

			if allowed[pairName] {
				continue
			}

			t.Run(pairName, func(t *testing.T) {
				t.Parallel()

				if left.col == right.col {
					t.Errorf("%s and %s are the same colour %v, want them distinct", left.name, right.name, left.col)
				}
			})
		}
	}
}

// luminance is a rough brightness for a palette colour: not the perceptual
// formula, just the sum of the three channels, which is enough to order
// three colours from darkest to lightest.
func luminance(col color.RGBA) int {
	return int(col.R) + int(col.G) + int(col.B)
}

// TestNightInkIsLighterThanField checks the one relationship that makes the
// palette readable at all: the ink has to sit above the field, and the muted
// chrome has to sit between them. A theme where the "ink" is darker than the
// background it sits on would not render as text, it would render as nothing.
func TestNightInkIsLighterThanField(t *testing.T) {
	t.Parallel()

	field := luminance(theme.Night.Field)
	muted := luminance(theme.Night.Muted)
	ink := luminance(theme.Night.Ink)

	if field >= muted {
		t.Errorf("Field luminance %d >= Muted luminance %d, want Field darker", field, muted)
	}

	if muted >= ink {
		t.Errorf("Muted luminance %d >= Ink luminance %d, want Muted darker than Ink", muted, ink)
	}
}

// fieldAccessor names one field of Palette together with a function that
// reads it, so the tests below can walk the whole struct once instead of
// repeating its field list per test.
type fieldAccessor struct {
	name string
	get  func(theme.Palette) color.RGBA
}

// paletteFields lists every color.RGBA field of Palette, in struct order. It
// is the one place that enumerates the struct, so a field added there and
// forgotten here shows up as a gap in coverage rather than as a silently
// untested colour.
//
// Look is deliberately not in it: a look is not a colour, and has nothing to
// say to the opacity, distinctness or contrast checks these fields feed.
func paletteFields() []fieldAccessor {
	return []fieldAccessor{
		{name: nameField, get: func(p theme.Palette) color.RGBA { return p.Field }},
		{name: nameInk, get: func(p theme.Palette) color.RGBA { return p.Ink }},
		{name: nameMuted, get: func(p theme.Palette) color.RGBA { return p.Muted }},
		{name: nameAccent, get: func(p theme.Palette) color.RGBA { return p.Accent }},
		{name: nameData, get: func(p theme.Palette) color.RGBA { return p.Data }},
		{name: nameOK, get: func(p theme.Palette) color.RGBA { return p.OK }},
		{name: nameCaution, get: func(p theme.Palette) color.RGBA { return p.Caution }},
		{name: nameWarn, get: func(p theme.Palette) color.RGBA { return p.Warn }},
		{name: nameKey, get: func(p theme.Palette) color.RGBA { return p.Key }},
		{name: nameRule, get: func(p theme.Palette) color.RGBA { return p.Rule }},
		{name: nameShore, get: func(p theme.Palette) color.RGBA { return p.Shore }},
		{name: nameAltLow, get: func(p theme.Palette) color.RGBA { return p.AltLow }},
		{name: nameAltMid, get: func(p theme.Palette) color.RGBA { return p.AltMid }},
		{name: nameAltHigh, get: func(p theme.Palette) color.RGBA { return p.AltHigh }},
		{name: nameBand, get: func(p theme.Palette) color.RGBA { return p.Band }},
		{name: nameBandInk, get: func(p theme.Palette) color.RGBA { return p.BandInk }},
	}
}

// allFields lists every field of a palette, named, so the opacity check below
// can table-drive off any palette without repeating the field list itself.
func allFields(pal theme.Palette) []namedColour {
	fields := paletteFields()
	colours := make([]namedColour, 0, len(fields))

	for _, field := range fields {
		colours = append(colours, namedColour{name: field.name, col: field.get(pal)})
	}

	return colours
}

// namedPalette pairs a palette with the name a failure should report.
type namedPalette struct {
	name string
	pal  theme.Palette
}

// allPalettes is all six palettes together, for the tests that hold every one
// to the same rule.
func allPalettes() []namedPalette {
	return []namedPalette{
		{name: "Glass Night", pal: theme.Night},
		{name: "Glass Day", pal: theme.Day},
		{name: "Phosphor Night", pal: theme.PhosphorNight},
		{name: "Phosphor Day", pal: theme.PhosphorDay},
		{name: "Mono Night", pal: theme.MonoNight},
		{name: "Mono Day", pal: theme.MonoDay},
	}
}

// TestPalettesAreFullyOpaque checks every field of all six palettes, including
// the fields (Data, OK, Caution, Warn, Key, Shore, Band, BandInk) the older
// Night-only opacity test above predates.
func TestPalettesAreFullyOpaque(t *testing.T) {
	t.Parallel()

	for _, palette := range allPalettes() {
		for _, field := range allFields(palette.pal) {
			t.Run(palette.name+" "+field.name, func(t *testing.T) {
				t.Parallel()

				if got := field.col.A; got != wantAlpha {
					t.Errorf("%s.%s.A = %#x, want %#x", palette.name, field.name, got, wantAlpha)
				}
			})
		}
	}
}

// hueFields lists the colours a reader has to be able to tell apart at a
// glance: the field and ink that frame the whole picture, the muted chrome,
// and the four status hues plus the one accent that all have to read as
// distinct marks rather than shades of each other.
func hueFields(pal theme.Palette) []namedColour {
	return []namedColour{
		{name: nameField, col: pal.Field},
		{name: nameInk, col: pal.Ink},
		{name: nameMuted, col: pal.Muted},
		{name: nameAccent, col: pal.Accent},
		{name: nameData, col: pal.Data},
		{name: nameOK, col: pal.OK},
		{name: nameCaution, col: pal.Caution},
		{name: nameWarn, col: pal.Warn},
	}
}

// TestSemanticHuesAreDistinct checks the hues whose whole job is to be told
// apart at a glance. Two of the status colours landing on the same value, or
// on the accent, ink, muted chrome or field they sit against, would be
// invisible in the struct literal but would erase a distinction the operator
// relies on to read the panel at a glance.
//
// Four of the six palettes carry a deliberate repeat among these hues. Mono
// has no accent hue by design: selection there is made with contrast rather
// than colour, so Ink, Accent and Data are one value in both Mono palettes.
// Phosphor Day runs out of room between a pale green page and black for a
// green that is both the active thing (Accent) and the valid thing (OK), so
// those two land on the same value there and nowhere else.
func TestSemanticHuesAreDistinct(t *testing.T) {
	t.Parallel()

	monoAllowed := map[string]bool{"Ink vs Accent": true, "Ink vs Data": true, "Accent vs Data": true}

	allowedByPalette := map[string]map[string]bool{
		"Phosphor Day": {"Accent vs OK": true},
		"Mono Night":   monoAllowed,
		"Mono Day":     monoAllowed,
	}

	for _, palette := range allPalettes() {
		t.Run(palette.name, func(t *testing.T) {
			t.Parallel()

			assertPairwiseDistinct(t, hueFields(palette.pal), allowedByPalette[palette.name])
		})
	}
}

// TestDeliberateHueRepeatsAreEqual asserts that the pairs
// TestSemanticHuesAreDistinct is told to allow are still actually equal, so a
// repeat that silently stops being a repeat, one half of a pair edited
// without the other, fails here instead of only widening that test's
// exemption and passing unnoticed.
func TestDeliberateHueRepeatsAreEqual(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		got  color.RGBA
		want color.RGBA
	}{
		{name: "Phosphor Day Accent equals OK", got: theme.PhosphorDay.Accent, want: theme.PhosphorDay.OK},
		{name: "Mono Night Ink equals Accent", got: theme.MonoNight.Ink, want: theme.MonoNight.Accent},
		{name: "Mono Night Ink equals Data", got: theme.MonoNight.Ink, want: theme.MonoNight.Data},
		{name: "Mono Night Accent equals Data", got: theme.MonoNight.Accent, want: theme.MonoNight.Data},
		{name: "Mono Day Ink equals Accent", got: theme.MonoDay.Ink, want: theme.MonoDay.Accent},
		{name: "Mono Day Ink equals Data", got: theme.MonoDay.Ink, want: theme.MonoDay.Data},
		{name: "Mono Day Accent equals Data", got: theme.MonoDay.Accent, want: theme.MonoDay.Data},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.got != testCase.want {
				t.Errorf("%s: got %v, want %v", testCase.name, testCase.got, testCase.want)
			}
		})
	}
}

// TestKeyIsDistinctFromFieldAndInk checks the softkey box against the two
// colours it sits between. Landing on Field would make the box invisible
// against the frame it is a step off; landing on Ink would make it read as
// more type rather than as hardware under the picture.
func TestKeyIsDistinctFromFieldAndInk(t *testing.T) {
	t.Parallel()

	for _, palette := range allPalettes() {
		t.Run(palette.name, func(t *testing.T) {
			t.Parallel()

			assertPairwiseDistinct(t, []namedColour{
				{name: nameField, col: palette.pal.Field},
				{name: nameInk, col: palette.pal.Ink},
				{name: nameKey, col: palette.pal.Key},
			}, nil)
		})
	}
}

// TestIntentionalColourRepeats asserts the equalities every palette carries by
// design, so that a future edit which splits one of these pairs into its own
// colour fails here instead of silently adding a shade to the picture. See
// the doc comments on Palette and on each of the six palette vars in
// theme.go for why each pair is meant to match.
func TestIntentionalColourRepeats(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		got  color.RGBA
		want color.RGBA
	}{
		{name: "Night AltLow repeats OK", got: theme.Night.AltLow, want: theme.Night.OK},
		{name: "Night AltMid repeats Ink", got: theme.Night.AltMid, want: theme.Night.Ink},
		{name: "Day AltLow repeats OK", got: theme.Day.AltLow, want: theme.Day.OK},
		{name: "Day AltMid repeats Ink", got: theme.Day.AltMid, want: theme.Day.Ink},
		{
			// Night's band text and its ink are the same white for the same
			// reason AltMid is: text is text, wherever in the picture it sits.
			// Combined with the AltMid case above this ties BandInk, AltMid
			// and Ink to a single white.
			name: "Night BandInk repeats Ink",
			got:  theme.Night.BandInk,
			want: theme.Night.Ink,
		},
		{name: "Phosphor Night Band repeats Field", got: theme.PhosphorNight.Band, want: theme.PhosphorNight.Field},
		{name: "Phosphor Night BandInk repeats Ink", got: theme.PhosphorNight.BandInk, want: theme.PhosphorNight.Ink},
		{name: "Phosphor Day BandInk repeats Field", got: theme.PhosphorDay.BandInk, want: theme.PhosphorDay.Field},
		{name: "Phosphor Day OK repeats Accent", got: theme.PhosphorDay.OK, want: theme.PhosphorDay.Accent},
		{name: "Phosphor Day AltHigh repeats Warn", got: theme.PhosphorDay.AltHigh, want: theme.PhosphorDay.Warn},
		{name: "Mono Night Accent repeats Ink", got: theme.MonoNight.Accent, want: theme.MonoNight.Ink},
		{name: "Mono Night Data repeats Ink", got: theme.MonoNight.Data, want: theme.MonoNight.Ink},
		{name: "Mono Night Key repeats Rule", got: theme.MonoNight.Key, want: theme.MonoNight.Rule},
		{name: "Mono Night AltHigh repeats Warn", got: theme.MonoNight.AltHigh, want: theme.MonoNight.Warn},
		{name: "Mono Night Band repeats Field", got: theme.MonoNight.Band, want: theme.MonoNight.Field},
		{name: "Mono Night BandInk repeats Ink", got: theme.MonoNight.BandInk, want: theme.MonoNight.Ink},
		{name: "Mono Day Accent repeats Ink", got: theme.MonoDay.Accent, want: theme.MonoDay.Ink},
		{name: "Mono Day Data repeats Ink", got: theme.MonoDay.Data, want: theme.MonoDay.Ink},
		{name: "Mono Day Band repeats Ink", got: theme.MonoDay.Band, want: theme.MonoDay.Ink},
		{name: "Mono Day BandInk repeats Field", got: theme.MonoDay.BandInk, want: theme.MonoDay.Field},
		{name: "Mono Day AltHigh repeats Warn", got: theme.MonoDay.AltHigh, want: theme.MonoDay.Warn},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.got != testCase.want {
				// This equality is deliberate; see the doc comment above.
				t.Errorf("%s: got %v, want %v", testCase.name, testCase.got, testCase.want)
			}
		})
	}
}

// TestNightAndDayDiffer walks every field of each look's night and day
// palettes and checks that the two differ, which is the test that would
// catch a copy-paste leaving one theme half built from the other's colours.
// Only Glass is allowed a match, on BandInk, which both Glass themes fix at
// white on purpose; Phosphor and Mono differ in every field, so their allow
// set is empty.
func TestNightAndDayDiffer(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		lookName string
		night    theme.Palette
		day      theme.Palette
		allowed  map[string]bool
	}{
		{lookName: "Glass", night: theme.Night, day: theme.Day, allowed: map[string]bool{nameBandInk: true}},
		{lookName: "Phosphor", night: theme.PhosphorNight, day: theme.PhosphorDay, allowed: nil},
		{lookName: "Mono", night: theme.MonoNight, day: theme.MonoDay, allowed: nil},
	} {
		t.Run(testCase.lookName, func(t *testing.T) {
			t.Parallel()

			for _, field := range paletteFields() {
				if testCase.allowed[field.name] {
					continue
				}

				t.Run(field.name, func(t *testing.T) {
					t.Parallel()

					night := field.get(testCase.night)
					day := field.get(testCase.day)

					if night == day {
						t.Errorf("%s Night.%s == Day.%s == %v, want them to differ",
							testCase.lookName, field.name, field.name, night)
					}
				})
			}
		})
	}
}

// WCAG relative luminance: each channel is linearised from its sRGB encoding
// before the three are weighted and summed, which is what makes the result
// track how bright a colour actually looks rather than its raw byte value.
const (
	channelMax = 255.0

	gammaThreshold     = 0.03928
	gammaLinearDivisor = 12.92
	gammaOffset        = 0.055
	gammaScale         = 1.055
	gammaExponent      = 2.4

	wcagRedWeight   = 0.2126
	wcagGreenWeight = 0.7152
	wcagBlueWeight  = 0.0722

	// minLuminanceGap is the floor for the pairs TestContrastPairsAreLegible
	// checks: half of the full relative-luminance range from black to white.
	// That is a real minimum a text-on-background pair has to clear, not a
	// number picked to match what these six palettes happen to measure: the
	// tightest pair, Phosphor Night's Ink against Field and its BandInk
	// against Band, both measure about 0.698, so even the narrowest palette
	// clears 0.5 with room to spare. The widest, Glass Night's Ink against
	// Field, measures 1.000.
	minLuminanceGap = 0.5

	// minAccentGap is the floor for how far Accent has to sit from Field: the
	// active thing has to read against the page it sits on, in every look.
	// Glass Night's magenta is the tightest of the six at about 0.306, which
	// is what this floor is set from; every other palette clears it with much
	// more room.
	minAccentGap = 0.3
)

// relativeLuminance is the WCAG relative luminance of a colour, in [0, 1].
func relativeLuminance(col color.RGBA) float64 {
	return wcagRedWeight*linearise(col.R) + wcagGreenWeight*linearise(col.G) + wcagBlueWeight*linearise(col.B)
}

// linearise undoes one channel's sRGB gamma, per the WCAG formula.
func linearise(channel uint8) float64 {
	srgb := float64(channel) / channelMax

	if srgb <= gammaThreshold {
		return srgb / gammaLinearDivisor
	}

	return math.Pow((srgb+gammaOffset)/gammaScale, gammaExponent)
}

// TestContrastPairsAreLegible checks the one relationship every palette has to
// hold for its header band to be readable: BandInk against Band has to
// contrast the way Ink against Field already does.
func TestContrastPairsAreLegible(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		lighter color.RGBA
		darker  color.RGBA
	}{
		{name: "Glass Night Ink vs Field", lighter: theme.Night.Ink, darker: theme.Night.Field},
		{name: "Glass Night BandInk vs Band", lighter: theme.Night.BandInk, darker: theme.Night.Band},
		{name: "Glass Day Field vs Ink", lighter: theme.Day.Field, darker: theme.Day.Ink},
		{name: "Glass Day BandInk vs Band", lighter: theme.Day.BandInk, darker: theme.Day.Band},
		{name: "Phosphor Night Ink vs Field", lighter: theme.PhosphorNight.Ink, darker: theme.PhosphorNight.Field},
		{
			name:    "Phosphor Night BandInk vs Band",
			lighter: theme.PhosphorNight.BandInk,
			darker:  theme.PhosphorNight.Band,
		},
		{name: "Phosphor Day Field vs Ink", lighter: theme.PhosphorDay.Field, darker: theme.PhosphorDay.Ink},
		{
			name:    "Phosphor Day BandInk vs Band",
			lighter: theme.PhosphorDay.BandInk,
			darker:  theme.PhosphorDay.Band,
		},
		{name: "Mono Night Ink vs Field", lighter: theme.MonoNight.Ink, darker: theme.MonoNight.Field},
		{name: "Mono Night BandInk vs Band", lighter: theme.MonoNight.BandInk, darker: theme.MonoNight.Band},
		{name: "Mono Day Field vs Ink", lighter: theme.MonoDay.Field, darker: theme.MonoDay.Ink},
		{name: "Mono Day BandInk vs Band", lighter: theme.MonoDay.BandInk, darker: theme.MonoDay.Band},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gap := math.Abs(relativeLuminance(testCase.lighter) - relativeLuminance(testCase.darker))
			if gap < minLuminanceGap {
				t.Errorf("relative luminance gap = %.3f, want at least %.1f", gap, minLuminanceGap)
			}
		})
	}
}

// TestAccentReadsAgainstField checks that the accent, the one colour that
// marks the selected aircraft, actually stands out from the field it sits on
// in every look. An accent too close to the field would still compile and
// still pass every distinctness check above, because those only ask that
// colours differ, not that the difference is one an eye can pick out on a
// small panel in a moving cockpit.
func TestAccentReadsAgainstField(t *testing.T) {
	t.Parallel()

	for _, palette := range allPalettes() {
		t.Run(palette.name, func(t *testing.T) {
			t.Parallel()

			gap := math.Abs(relativeLuminance(palette.pal.Accent) - relativeLuminance(palette.pal.Field))
			if gap < minAccentGap {
				t.Errorf("%s: Accent vs Field relative luminance gap = %.3f, want at least %.1f",
					palette.name, gap, minAccentGap)
			}
		})
	}
}

// The floor OnBand applies, repeated here rather than exported, so a change to
// the one in theme.go that nobody meant has to be made twice.
const minBandGapTest = 0.10

// bandColours lists the palette colours the header band actually sets type in,
// which is the set OnBand has to answer for. Warn is on the list because the
// battery glyph fills with it on a critical charge, which is drawn on the band
// like everything else in the header.
func bandColours(pal theme.Palette) []namedColour {
	return []namedColour{
		{name: nameData, col: pal.Data},
		{name: nameOK, col: pal.OK},
		{name: nameCaution, col: pal.Caution},
		{name: nameWarn, col: pal.Warn},
		{name: nameMuted, col: pal.Muted},
		{name: nameBandInk, col: pal.BandInk},
	}
}

// TestOnBandReturnsSomethingLegible is the claim that matters: whatever OnBand
// hands back can be read on the band it was asked about.
//
// The header used to set its readings straight from the palette, which was
// answerable while every band was a dark strip under light hues. MonoDay broke
// it: that band is filled with the palette's own ink and the palette's Data is
// that same ink, so the source label, both clocks, the coordinates and the
// battery percentage were all drawn black on black. This test fails if any
// palette ever gets back a colour it cannot show.
func TestOnBandReturnsSomethingLegible(t *testing.T) {
	t.Parallel()

	for _, palette := range allPalettes() {
		for _, field := range bandColours(palette.pal) {
			t.Run(palette.name+" "+field.name, func(t *testing.T) {
				t.Parallel()

				got := palette.pal.OnBand(field.col)

				gap := math.Abs(relativeLuminance(got) - relativeLuminance(palette.pal.Band))
				if gap < minBandGapTest {
					t.Errorf("OnBand(%s) = %v, gap against Band = %.3f, want at least %.2f",
						field.name, got, gap, minBandGapTest)
				}
			})
		}
	}
}

// TestOnBandLeavesTheReadableOnesAlone checks the other half. OnBand is a
// guard, not a filter: a colour that already reads on its band has to come
// back untouched, or Glass's cyan clocks would quietly become white ones and
// the cockpit grammar every other palette is measured against would be gone.
//
// The five palettes listed here are every one except MonoDay, and the tightest
// of them is PhosphorDay's Data at 0.118 against a floor of 0.10, so this is a
// real margin rather than a coincidence.
func TestOnBandLeavesTheReadableOnesAlone(t *testing.T) {
	t.Parallel()

	for _, palette := range allPalettes() {
		if palette.pal.Look == theme.LookMono && palette.pal.Light() {
			continue
		}

		for _, field := range bandColours(palette.pal) {
			t.Run(palette.name+" "+field.name, func(t *testing.T) {
				t.Parallel()

				if got := palette.pal.OnBand(field.col); got != field.col {
					t.Errorf("OnBand(%s) = %v, want it returned unchanged as %v", field.name, got, field.col)
				}
			})
		}
	}
}

// TestOnBandLiftsMonoDay names the one palette that needs OnBand and the
// three colours it lifts, so the exemption above cannot quietly grow to cover
// a palette that was merely built wrong.
func TestOnBandLiftsMonoDay(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		col  color.RGBA
		want color.RGBA
	}{
		{name: nameData, col: theme.MonoDay.Data, want: theme.MonoDay.BandInk},
		{name: nameOK, col: theme.MonoDay.OK, want: theme.MonoDay.BandInk},
		{name: nameCaution, col: theme.MonoDay.Caution, want: theme.MonoDay.BandInk},
		{
			// The warning red is the one colour mono day keeps on its band,
			// which is the right one to keep: a critical battery is the only
			// thing up there that has to be seen rather than read.
			name: nameWarn, col: theme.MonoDay.Warn, want: theme.MonoDay.Warn,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := theme.MonoDay.OnBand(testCase.col); got != testCase.want {
				t.Errorf("MonoDay.OnBand(%s) = %v, want %v", testCase.name, got, testCase.want)
			}
		})
	}
}

// TestMonoHasNoAccentHue asserts that Mono's Accent is its Ink, in both
// themes. This is the look's whole point rather than an oversight: Mono
// selects with contrast instead of colour, so there is no accent hue to
// measure, and the ink's own gap against the field is the only gap there is.
func TestMonoHasNoAccentHue(t *testing.T) {
	t.Parallel()

	t.Run("Mono Night", func(t *testing.T) {
		t.Parallel()

		if theme.MonoNight.Accent != theme.MonoNight.Ink {
			t.Errorf("MonoNight.Accent = %v, want it to equal MonoNight.Ink %v",
				theme.MonoNight.Accent, theme.MonoNight.Ink)
		}
	})

	t.Run("Mono Day", func(t *testing.T) {
		t.Parallel()

		if theme.MonoDay.Accent != theme.MonoDay.Ink {
			t.Errorf("MonoDay.Accent = %v, want it to equal MonoDay.Ink %v", theme.MonoDay.Accent, theme.MonoDay.Ink)
		}
	})
}

func TestParse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		text    string
		want    theme.Kind
		wantErr bool
	}{
		{name: "night", text: "night", want: theme.KindNight},
		{name: "day", text: "day", want: theme.KindDay},
		{name: "empty string is rejected", text: "", wantErr: true},
		{
			// --theme is an allow list, not free text, so the exact spelling
			// is what is accepted rather than a case-insensitive match of it.
			name: "wrong case is rejected", text: "Night", wantErr: true,
		},
		{name: "unknown value is rejected", text: "sepia", wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := theme.Parse(testCase.text)

			if testCase.wantErr {
				if !errors.Is(err, theme.ErrUnknown) {
					t.Fatalf("Parse(%q) error = %v, want ErrUnknown", testCase.text, err)
				}

				if got != theme.KindNight {
					t.Errorf("Parse(%q) = %v on error, want %v", testCase.text, got, theme.KindNight)
				}

				return
			}

			if err != nil {
				t.Fatalf("Parse(%q) = %v, want no error", testCase.text, err)
			}

			if got != testCase.want {
				t.Errorf("Parse(%q) = %v, want %v", testCase.text, got, testCase.want)
			}
		})
	}
}

// TestLookPalette checks that every (look, kind) pair resolves to the palette
// that pair names, and that both axes fall back sanely: an unrecognised look
// draws Glass and an unrecognised kind draws night. Get either axis wrong and
// pressing k or l would put the wrong colours on screen for a look that is
// still spelled correctly.
func TestLookPalette(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		look theme.Look
		kind theme.Kind
		want theme.Palette
	}{
		{name: "glass night resolves to Night", look: theme.LookGlass, kind: theme.KindNight, want: theme.Night},
		{name: "glass day resolves to Day", look: theme.LookGlass, kind: theme.KindDay, want: theme.Day},
		{
			name: "phosphor night resolves to PhosphorNight",
			look: theme.LookPhosphor, kind: theme.KindNight, want: theme.PhosphorNight,
		},
		{
			name: "phosphor day resolves to PhosphorDay",
			look: theme.LookPhosphor, kind: theme.KindDay, want: theme.PhosphorDay,
		},
		{
			name: "mono night resolves to MonoNight",
			look: theme.LookMono, kind: theme.KindNight, want: theme.MonoNight,
		},
		{name: "mono day resolves to MonoDay", look: theme.LookMono, kind: theme.KindDay, want: theme.MonoDay},
		{
			name: "the zero look with night resolves to Night",
			look: theme.Look(""), kind: theme.KindNight, want: theme.Night,
		},
		{
			name: "the zero look with day resolves to Day",
			look: theme.Look(""), kind: theme.KindDay, want: theme.Day,
		},
		{
			name: "an unknown look falls back to Glass",
			look: theme.Look("sepia"), kind: theme.KindDay, want: theme.Day,
		},
		{
			name: "an unknown kind falls back to night",
			look: theme.LookPhosphor, kind: theme.Kind("dusk"), want: theme.PhosphorNight,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.look.Palette(testCase.kind); got != testCase.want {
				t.Errorf("%v.Palette(%v) = %v, want %v", testCase.look, testCase.kind, got, testCase.want)
			}
		})
	}
}

func TestKindNext(t *testing.T) {
	t.Parallel()

	t.Run("night moves to day", func(t *testing.T) {
		t.Parallel()

		if got := theme.KindNight.Next(); got != theme.KindDay {
			t.Errorf("KindNight.Next() = %v, want %v", got, theme.KindDay)
		}
	})

	t.Run("day moves back to night, completing the cycle", func(t *testing.T) {
		t.Parallel()

		if got := theme.KindDay.Next(); got != theme.KindNight {
			t.Errorf("KindDay.Next() = %v, want %v", got, theme.KindNight)
		}
	})

	t.Run("the zero value cycles the same way night does", func(t *testing.T) {
		t.Parallel()

		if got := theme.Kind("").Next(); got != theme.KindDay {
			t.Errorf(`Kind("").Next() = %v, want %v`, got, theme.KindDay)
		}
	})
}

// TestParseLook checks --look's allow list the same way TestParse checks
// --theme's: the three spellings are accepted, everything else is rejected
// with ErrUnknownLook, and a rejected value still hands back LookGlass so a
// caller that forgets to check the error does not start the program on a
// zero value nothing resolves cleanly.
func TestParseLook(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		text    string
		want    theme.Look
		wantErr bool
	}{
		{name: "glass", text: "glass", want: theme.LookGlass},
		{name: "phosphor", text: "phosphor", want: theme.LookPhosphor},
		{name: "mono", text: "mono", want: theme.LookMono},
		{name: "empty string is rejected", text: "", wantErr: true},
		{
			// --look is an allow list, not free text, so the exact spelling is
			// what is accepted rather than a case-insensitive match of it.
			name: "wrong case is rejected", text: "Glass", wantErr: true,
		},
		{name: "unknown value is rejected", text: "monochrome", wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := theme.ParseLook(testCase.text)

			if testCase.wantErr {
				if !errors.Is(err, theme.ErrUnknownLook) {
					t.Fatalf("ParseLook(%q) error = %v, want ErrUnknownLook", testCase.text, err)
				}

				if got != theme.LookGlass {
					t.Errorf("ParseLook(%q) = %v on error, want %v", testCase.text, got, theme.LookGlass)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseLook(%q) = %v, want no error", testCase.text, err)
			}

			if got != testCase.want {
				t.Errorf("ParseLook(%q) = %v, want %v", testCase.text, got, testCase.want)
			}
		})
	}
}

// TestLookNext checks the k key's whole cycle: glass to phosphor to mono and
// back to glass, with the zero value cycling the same way glass does and the
// cycle closing rather than merely running forward. Missing the return leg
// would leave a player stuck one look short of the one they started on.
func TestLookNext(t *testing.T) {
	t.Parallel()

	t.Run("glass moves to phosphor", func(t *testing.T) {
		t.Parallel()

		if got := theme.LookGlass.Next(); got != theme.LookPhosphor {
			t.Errorf("LookGlass.Next() = %v, want %v", got, theme.LookPhosphor)
		}
	})

	t.Run("phosphor moves to mono", func(t *testing.T) {
		t.Parallel()

		if got := theme.LookPhosphor.Next(); got != theme.LookMono {
			t.Errorf("LookPhosphor.Next() = %v, want %v", got, theme.LookMono)
		}
	})

	t.Run("mono moves back to glass, completing the cycle", func(t *testing.T) {
		t.Parallel()

		if got := theme.LookMono.Next(); got != theme.LookGlass {
			t.Errorf("LookMono.Next() = %v, want %v", got, theme.LookGlass)
		}
	})

	t.Run("the zero value cycles the same way glass does", func(t *testing.T) {
		t.Parallel()

		if got := theme.Look("").Next(); got != theme.LookPhosphor {
			t.Errorf(`Look("").Next() = %v, want %v`, got, theme.LookPhosphor)
		}
	})

	t.Run("pressing the key three times from glass returns to glass", func(t *testing.T) {
		t.Parallel()

		if got := theme.LookGlass.Next().Next().Next(); got != theme.LookGlass {
			t.Errorf("LookGlass.Next().Next().Next() = %v, want %v", got, theme.LookGlass)
		}
	})
}

// TestPaletteNamesItsOwnLook asserts that every one of the six package-level
// palettes carries the Look value that names it, and that resolving a look
// through Palette hands back a value that still carries that same look. A
// palette handed to a scene is the only thing that says which look is on, so
// one mislabelled literal here would put the wrong word on the K softkey
// while every other colour in the picture stayed correct.
func TestPaletteNamesItsOwnLook(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		pal  theme.Palette
		want theme.Look
	}{
		{name: "Night", pal: theme.Night, want: theme.LookGlass},
		{name: "Day", pal: theme.Day, want: theme.LookGlass},
		{name: "PhosphorNight", pal: theme.PhosphorNight, want: theme.LookPhosphor},
		{name: "PhosphorDay", pal: theme.PhosphorDay, want: theme.LookPhosphor},
		{name: "MonoNight", pal: theme.MonoNight, want: theme.LookMono},
		{name: "MonoDay", pal: theme.MonoDay, want: theme.LookMono},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.pal.Look != testCase.want {
				t.Errorf("%s.Look = %v, want %v", testCase.name, testCase.pal.Look, testCase.want)
			}
		})
	}

	for _, testCase := range []struct {
		name string
		look theme.Look
		kind theme.Kind
	}{
		{name: "Glass night", look: theme.LookGlass, kind: theme.KindNight},
		{name: "Glass day", look: theme.LookGlass, kind: theme.KindDay},
		{name: "Phosphor night", look: theme.LookPhosphor, kind: theme.KindNight},
		{name: "Phosphor day", look: theme.LookPhosphor, kind: theme.KindDay},
		{name: "Mono night", look: theme.LookMono, kind: theme.KindNight},
		{name: "Mono day", look: theme.LookMono, kind: theme.KindDay},
	} {
		t.Run(testCase.name+" resolves to a palette naming itself", func(t *testing.T) {
			t.Parallel()

			pal := testCase.look.Palette(testCase.kind)
			if pal.Look != testCase.look {
				t.Errorf("%v.Palette(%v).Look = %v, want %v", testCase.look, testCase.kind, pal.Look, testCase.look)
			}
		})
	}
}

// TestPaletteLight checks that Light reports true for a light field and false
// for a dark one, across every palette the program can start on plus the
// edge cases a hand-built test palette can produce, and pins down the
// threshold itself with a grey either side of it.
//
// It measures the field rather than comparing against one known palette,
// because there are six palettes now and three of them are light: comparing
// against a single value could only ever answer the question for that one
// palette, and would call PhosphorDay and MonoDay dark because neither of
// them is theme.Day.
func TestPaletteLight(t *testing.T) {
	t.Parallel()

	whiteField := theme.Palette{Field: color.RGBA{R: fullChannel, G: fullChannel, B: fullChannel, A: wantAlpha}}
	darkGrayField := theme.Palette{
		Field: color.RGBA{R: grayBelowMidLuminance, G: grayBelowMidLuminance, B: grayBelowMidLuminance, A: wantAlpha},
	}
	lightGrayField := theme.Palette{
		Field: color.RGBA{R: grayAboveMidLuminance, G: grayAboveMidLuminance, B: grayAboveMidLuminance, A: wantAlpha},
	}

	for _, testCase := range []struct {
		name string
		pal  theme.Palette
		want bool
	}{
		{name: "Glass Day is light", pal: theme.Day, want: true},
		{name: "Phosphor Day is light", pal: theme.PhosphorDay, want: true},
		{name: "Mono Day is light", pal: theme.MonoDay, want: true},
		{name: "Glass Night is not light", pal: theme.Night, want: false},
		{name: "Phosphor Night is not light", pal: theme.PhosphorNight, want: false},
		{name: "Mono Night is not light", pal: theme.MonoNight, want: false},
		{name: "the zero value is not light", pal: theme.Palette{}, want: false},
		{name: "a palette whose only set field is white is light", pal: whiteField, want: true},
		{name: "a field just below the midpoint is not light", pal: darkGrayField, want: false},
		{name: "a field just above the midpoint is light", pal: lightGrayField, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.pal.Light(); got != testCase.want {
				t.Errorf("%s: Light() = %v, want %v", testCase.name, got, testCase.want)
			}
		})
	}
}
