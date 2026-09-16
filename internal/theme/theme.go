// Package theme holds the colour palettes uScope draws with.
//
// It is data and nothing else, so a scene can be handed a palette instead of
// reaching for named colours, and so the two themes DESIGN.md calls for can
// be swapped at run time without touching a scene.
//
// Night is the default because the uConsole is a backlit handheld: a light
// field is a torch in the face at night and eats battery all day. Paper is
// modelled on the Flightscanner cards: light field, dark ink, a navy header
// band. `l` cycles between them at run time and `--theme` picks the one to
// start on.
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

	// Ink is the reading colour: callsigns, figures, anything that is data.
	Ink color.RGBA

	// Muted is for chrome that has to be present but not read: labels, units,
	// the wordmark, column headings.
	Muted color.RGBA

	// Accent is the one colour that draws the eye. It marks the selected
	// aircraft, and in the 3D view the measured half of the receiving
	// envelope. Those are the only two, and they never look alike: one is a
	// ring on a contact with a callsign hanging off it, the other a wireframe
	// around the outside of the whole picture.
	Accent color.RGBA

	// Rule is for hairlines and borders, a step above the field rather than
	// a step below the ink.
	Rule color.RGBA

	// Shore is the coastline under the scope, and the quietest colour either
	// theme has. Against the field it carries a little over 40% of the
	// contrast Muted does, which is the level it was picked for: the rings
	// carry a number and have to be read, the coast only has to be
	// recognised, and a coastline as loud as the furniture turns the scope
	// into a map that happens to have aircraft on it.
	Shore color.RGBA

	// The altitude bands. Aircraft are coloured by how high they are, which
	// is the one piece of information a top-down scope cannot show by
	// position. Unused until the radar slice; they live here so the palette
	// is complete rather than growing a field per slice.
	AltLow  color.RGBA
	AltMid  color.RGBA
	AltHigh color.RGBA

	// Band is the header band's own background, and BandInk the text set on
	// it. They are separate from Field and Ink because the paper theme's
	// band is a solid navy strip rather than the page colour, and text on it
	// needs its own contrast rather than the page's.
	Band    color.RGBA
	BandInk color.RGBA
}

// The Night palette as packed 0xRRGGBB values. They are named constants
// rather than literals in the palette below because a bare hex number in an
// argument list tells a reader nothing about which colour it is.
const (
	nightField   = 0x0A0E14
	nightInk     = 0xD8DEE9
	nightMuted   = 0x5C7080
	nightAccent  = 0xF0A030
	nightRule    = 0x2A3B4A
	nightShore   = 0x1E3440
	nightAltLow  = 0x4CAF6E
	nightAltMid  = 0xE0A93B
	nightAltHigh = 0xD05A4A
)

// The Paper palette, packed the same way. Modelled on an e-paper flight
// display: dark ink on warm paper, a couple of accents, and a navy band
// across the header rather than the page colour showing through.
const (
	paperField   = 0xF3F1EA
	paperInk     = 0x1C2230
	paperMuted   = 0x6E7681
	paperAccent  = 0xC8700A
	paperRule    = 0xC9C5BA
	paperShore   = 0xBFB9AA
	paperAltLow  = 0x2E8B4F
	paperAltMid  = 0xB8770B
	paperAltHigh = 0xB83A2E
	paperBand    = 0x24304F
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

// Night is the dark theme: a near-black field with a trace of blue in it, so
// it reads as night sky rather than as a dead panel, and light grey ink that
// stops short of white because pure white on near-black blooms on an LCD at
// this size.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var Night = Palette{
	Field:   rgb(nightField),
	Ink:     rgb(nightInk),
	Muted:   rgb(nightMuted),
	Accent:  rgb(nightAccent),
	Rule:    rgb(nightRule),
	Shore:   rgb(nightShore),
	AltLow:  rgb(nightAltLow),
	AltMid:  rgb(nightAltMid),
	AltHigh: rgb(nightAltHigh),

	// Band and BandInk repeat Field and Ink: at night the header band sits
	// flush with the field and its text reads exactly as it did before the
	// band existed.
	Band:    rgb(nightField),
	BandInk: rgb(nightInk),
}

// Paper is the light theme DESIGN.md calls for: dark ink on warm paper with
// a navy header band, modelled on an e-paper flight display.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var Paper = Palette{
	Field:   rgb(paperField),
	Ink:     rgb(paperInk),
	Muted:   rgb(paperMuted),
	Accent:  rgb(paperAccent),
	Rule:    rgb(paperRule),
	Shore:   rgb(paperShore),
	AltLow:  rgb(paperAltLow),
	AltMid:  rgb(paperAltMid),
	AltHigh: rgb(paperAltHigh),
	Band:    rgb(paperBand),

	// BandInk matches Field rather than getting a colour of its own: the
	// band is a solid navy strip, and setting its text in the paper's own
	// colour reads as a cut-out rather than as a second ink to keep track of.
	BandInk: rgb(paperField),
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
// has to be lifted off night's near-black field and pushed down onto paper's,
// and the palette is the only thing the scene is handed that says which of the
// two it is drawing on. Anything that is not Paper reads as dark, which is the
// rule Kind.Palette and Kind.Next already follow, so a palette assembled by
// hand in a test behaves like night rather than like neither.
func (p Palette) Light() bool { return p == Paper }

// Kind names one of the two themes. It is what --theme parses into and what
// Config carries as the theme to start on.
type Kind string

// The two spellings --theme accepts. Named KindNight and KindPaper rather
// than Night and Paper so they do not collide with the Palette variables
// above, which are what a Kind resolves to.
const (
	KindNight Kind = "night"
	KindPaper Kind = "paper"
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
	case KindPaper:
		return KindPaper, nil
	default:
		return KindNight, fmt.Errorf("%w: %q", ErrUnknown, text)
	}
}

// Palette resolves a Kind to its colours. Anything that is not KindPaper
// reads as night, which is what makes the zero value of Kind, the empty
// string, a usable default rather than a value that has to be special-cased
// wherever a Kind is read.
func (k Kind) Palette() Palette {
	if k == KindPaper {
		return Paper
	}

	return Night
}

// Next cycles to the other theme. As with Palette, anything that is not
// KindPaper is treated as night and moves to paper, so the zero value cycles
// the same way KindNight does.
func (k Kind) Next() Kind {
	if k == KindPaper {
		return KindNight
	}

	return KindPaper
}
