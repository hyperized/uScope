// Package theme holds the colour palettes uScope draws with.
//
// It is data and nothing else, so a scene can be handed a palette instead of
// reaching for named colours, and so the two themes DESIGN.md calls for can
// be swapped at run time without touching a scene.
//
// Both themes follow the glass cockpit's colour grammar, where a hue is a
// meaning rather than a decoration: magenta is the active thing, cyan is a
// data field, green is engaged or valid, amber is a caution, red is a warning,
// and controls sit in grey softkeys. A Garmin panel teaches that vocabulary to
// everyone who has flown behind one, and uScope maps onto it without inventing
// anything of its own. See the semantic fields on Palette for what each hue is
// allowed to mark.
//
// Night is the default because the uConsole is a backlit handheld: a light
// field is a torch in the face at night and eats battery all day. Day is the
// same grammar on a cool light grey, which is what a panel app does for its
// daytime page. `l` cycles between them at run time and `--theme` picks the
// one to start on.
package theme

import (
	"errors"
	"fmt"
	"image/color"
)

// Palette is one complete set of drawing colours. A scene reads from it and
// names no colours of its own, so a new theme is a new value here rather than
// an edit in every scene.
type Palette struct {
	// Field is the background the whole frame is cleared to.
	Field color.RGBA

	// Ink is the reading colour: callsigns, figures, anything that is data
	// about an aeroplane.
	Ink color.RGBA

	// Muted is for chrome that has to be present but not read: labels, units,
	// empty states, the coastline's neighbours in the furniture.
	Muted color.RGBA

	// Accent is the active thing, and on a glass panel that means magenta. It
	// marks the selected aircraft and nothing else: the ring on the scope, the
	// three-line tag hanging off it, and the edge down the selected flight
	// strip. Three marks, one aeroplane, one colour.
	//
	// The 3D view's measured envelope used to share it. It does not any more:
	// the mesh is drawn in Data, because it is a record of what the antenna
	// heard rather than the thing the operator has picked out.
	Accent color.RGBA

	// Data is the cyan a glass panel sets a data field in. Here that is the
	// header's source label, the receiver's coordinates, both clocks, the
	// battery percentage and the column headers over the flight strips. Every
	// one of them is a reading about the machine rather than about an
	// aeroplane, which is what separates them from Ink.
	Data color.RGBA

	// OK is green for valid or engaged: a GPS fix word, the bar under an
	// engaged softkey, the source dot while the feed is connected.
	OK color.RGBA

	// Caution is amber for a reading that is a guess rather than a fact: the
	// self-locate estimate and its radius, the question mark it carries when
	// its own observations disagree, the SWEEP marker, and GPS LOST.
	Caution color.RGBA

	// Warn is red, and one thing wears it: EMERGENCY, as a filled box with
	// white text. A warning that looks like every other label is not a warning.
	Warn color.RGBA

	// Key is the softkey box the whole bottom bar is built from. It is a step
	// off the field rather than a step towards the ink, so a row of them reads
	// as hardware under the picture instead of as more type on it.
	Key color.RGBA

	// Rule is for hairlines and borders, a step above the field rather than
	// a step below the ink.
	Rule color.RGBA

	// Shore is the coastline under the scope, and the quietest colour either
	// theme has. Against the field it carries just under 40% of the contrast
	// Muted does, 38% on night and 40% on day, which is the level it was
	// picked for: the rings carry a number and have to be read, the coast only
	// has to be recognised, and a coastline as loud as the furniture turns the
	// scope into a map that happens to have aircraft on it.
	Shore color.RGBA

	// The altitude bands. Aircraft are coloured by how high they are, which
	// is the one piece of information a top-down scope cannot show by
	// position. The ramp is the cockpit's own: green low, white in the middle,
	// orange high, so it reads as height rather than as a traffic light.
	//
	// AltLow deliberately repeats OK and AltMid deliberately repeats Ink. Both
	// pairs mean the same thing in the same picture, and a second green or a
	// second white would be one more colour to keep in step across two themes
	// for no gain anybody could see.
	AltLow  color.RGBA
	AltMid  color.RGBA
	AltHigh color.RGBA

	// Band is the header band's own background, and BandInk the text set on
	// it. They are separate from Field and Ink because both themes set the
	// band as a dark strip, and text on it needs its own contrast rather than
	// the page's. Night's band sits a shade above its black field rather than
	// flush with it, so the masthead reads as a strip on both themes and not
	// only on one.
	Band    color.RGBA
	BandInk color.RGBA
}

// The Night palette as packed 0xRRGGBB values. They are named constants
// rather than literals in the palette below because a bare hex number in an
// argument list tells a reader nothing about which colour it is.
const (
	nightField   = 0x000000
	nightInk     = 0xFFFFFF
	nightMuted   = 0x8C949C
	nightRule    = 0x3A4148
	nightBand    = 0x0E1114
	nightAccent  = 0xF050F0
	nightData    = 0x3FE3FF
	nightOK      = 0x28E05A
	nightCaution = 0xFFB300
	nightWarn    = 0xFF2A2A
	nightKey     = 0x2A3036
	nightAltHigh = 0xFF8A3D
	nightShore   = 0x2C3A44
)

// The Day palette, packed the same way: the same grammar on a cool light grey,
// with every hue pulled down far enough to hold its meaning against a light
// field. The magenta deepens, the cyan becomes a teal, and the greens and reds
// darken rather than changing what they say.
const (
	dayField   = 0xEEF1F4
	dayInk     = 0x101418
	dayMuted   = 0x6B7580
	dayRule    = 0xC4CBD2
	dayBand    = 0x1A1F26
	dayBandInk = 0xFFFFFF
	dayAccent  = 0xB0189F
	dayData    = 0x0B7FA8
	dayOK      = 0x1E8E4A
	dayCaution = 0xB8770B
	dayWarn    = 0xC21F1F
	dayKey     = 0xD5DBE1
	dayAltHigh = 0xC2571C
	dayShore   = 0xB6C0C9
)

// Taking one channel out of a packed colour.
const (
	byteMask   = 0xFF
	greenShift = 8
	redShift   = 16

	// opaque is the alpha every palette colour carries. The framebuffer has
	// no alpha channel, so anything translucent would be flattened anyway.
	opaque = 0xFF
)

// Night is the dark theme: a true black field with white ink on it, which is
// the contrast an instrument panel is read at in the dark.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var Night = Palette{
	Field:   rgb(nightField),
	Ink:     rgb(nightInk),
	Muted:   rgb(nightMuted),
	Rule:    rgb(nightRule),
	Accent:  rgb(nightAccent),
	Data:    rgb(nightData),
	OK:      rgb(nightOK),
	Caution: rgb(nightCaution),
	Warn:    rgb(nightWarn),
	Key:     rgb(nightKey),
	Shore:   rgb(nightShore),

	// AltLow repeats OK and AltMid repeats Ink; see the note on the bands.
	AltLow:  rgb(nightOK),
	AltMid:  rgb(nightInk),
	AltHigh: rgb(nightAltHigh),

	Band:    rgb(nightBand),
	BandInk: rgb(nightInk),
}

// Day is the light theme: the same grammar on a cool light grey, keeping the
// near-black header band so the masthead reads the same on either theme.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var Day = Palette{
	Field:   rgb(dayField),
	Ink:     rgb(dayInk),
	Muted:   rgb(dayMuted),
	Rule:    rgb(dayRule),
	Accent:  rgb(dayAccent),
	Data:    rgb(dayData),
	OK:      rgb(dayOK),
	Caution: rgb(dayCaution),
	Warn:    rgb(dayWarn),
	Key:     rgb(dayKey),
	Shore:   rgb(dayShore),

	AltLow:  rgb(dayOK),
	AltMid:  rgb(dayInk),
	AltHigh: rgb(dayAltHigh),

	Band: rgb(dayBand),

	// BandInk is white rather than the page colour. The band is a near-black
	// strip on both themes, so its text is the same white on both, and the day
	// page is far too light to read as ink on it.
	BandInk: rgb(dayBandInk),
}

// rgb turns a packed 0xRRGGBB value into an opaque colour. Writing the
// palette in hex keeps it comparable with whatever a design tool produces.
func rgb(value uint32) color.RGBA {
	return color.RGBA{
		R: uint8(value >> redShift & byteMask),   //nolint:gosec // the mask leaves one byte.
		G: uint8(value >> greenShift & byteMask), //nolint:gosec // the mask leaves one byte.
		B: uint8(value & byteMask),               //nolint:gosec // the mask leaves one byte.
		A: opaque,
	}
}

// Light reports whether this palette draws on a light field.
//
// A scene needs it when it has to adapt a colour that did not come out of the
// palette. An airline's brand colour is the case that forced it: the same navy
// has to be lifted off night's black field and pushed down onto the day page,
// and the palette is the only thing the scene is handed that says which of the
// two it is drawing on. Anything that is not Day reads as dark, which is the
// rule Kind.Palette and Kind.Next already follow, so a palette assembled by
// hand in a test behaves like night rather than like neither.
func (p Palette) Light() bool { return p == Day }

// Kind names one of the two themes. It is what --theme parses into and what
// Config carries as the theme to start on.
type Kind string

// The two spellings --theme accepts. Named KindNight and KindDay rather than
// Night and Day so they do not collide with the Palette variables above, which
// are what a Kind resolves to.
const (
	KindNight Kind = "night"
	KindDay   Kind = "day"
)

// ErrUnknown is returned for a --theme value that is neither spelling.
var ErrUnknown = errors.New("theme: unknown theme")

// Parse turns a --theme value into a Kind.
//
// The match is case sensitive on purpose. --theme is an allow list, not free
// text: "Night" is rejected the same way "nite" is, rather than accepted as
// a friendly variant of a value that already has an exact spelling.
func Parse(text string) (Kind, error) {
	switch Kind(text) {
	case KindNight:
		return KindNight, nil
	case KindDay:
		return KindDay, nil
	default:
		return KindNight, fmt.Errorf("%w: %q", ErrUnknown, text)
	}
}

// Palette resolves a Kind to its colours. Anything that is not KindDay reads
// as night, which is what makes the zero value of Kind, the empty string, a
// usable default rather than a value that has to be special-cased wherever a
// Kind is read.
func (k Kind) Palette() Palette {
	if k == KindDay {
		return Day
	}

	return Night
}

// Next cycles to the other theme. As with Palette, anything that is not
// KindDay is treated as night and moves to day, so the zero value cycles the
// same way KindNight does.
func (k Kind) Next() Kind {
	if k == KindDay {
		return KindNight
	}

	return KindDay
}
