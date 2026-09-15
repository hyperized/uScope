// Package theme holds the colour palettes uScope draws with.
//
// It is data and nothing else, so a scene can be handed a palette instead of
// reaching for named colours, and so the two themes DESIGN.md calls for can
// be swapped at run time later without touching a scene.
//
// Night is the only palette so far. It is the default because the uConsole is
// a backlit handheld: a light field is a torch in the face at night and eats
// battery all day. Paper, the light theme modelled on the Flightscanner
// cards, comes with the radar slice.
package theme

import "image/color"

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
	// aircraft and nothing else, which is why there is exactly one of it.
	Accent color.RGBA

	// Rule is for hairlines and borders, a step above the field rather than
	// a step below the ink.
	Rule color.RGBA

	// The altitude bands. Aircraft are coloured by how high they are, which
	// is the one piece of information a top-down scope cannot show by
	// position. Unused until the radar slice; they live here so the palette
	// is complete rather than growing a field per slice.
	AltLow  color.RGBA
	AltMid  color.RGBA
	AltHigh color.RGBA
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
	nightAltLow  = 0x4CAF6E
	nightAltMid  = 0xE0A93B
	nightAltHigh = 0xD05A4A
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
	AltLow:  rgb(nightAltLow),
	AltMid:  rgb(nightAltMid),
	AltHigh: rgb(nightAltHigh),
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
