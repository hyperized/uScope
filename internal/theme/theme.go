// Package theme holds the colour palettes uScope draws with.
//
// It is data and nothing else, so a scene can be handed a palette instead of
// reaching for named colours, and so the looks DESIGN.md calls for can be
// swapped at run time without touching a scene.
//
// There are two axes and eight palettes. A Look is the whole character of the
// picture and there are four: Glass, Phosphor, Mono and Vector. A Kind is
// night or day inside whichever look is on. The layout grammar underneath them never
// moves, whichever pair is picked: the same flight strips, the same data
// block, the same tag hanging off the selected aircraft, the same row of
// softkeys. Only the colours change. `k` cycles the look, `l` cycles night and
// day, and --look and --theme pick the pair to start on.
//
// Glass is the default and the only look with a vocabulary behind it, which
// Vector borrows whole. It follows the glass cockpit's colour grammar, where a
// hue is a meaning rather than a decoration: magenta is the active thing, cyan is a data field,
// green is engaged or valid, amber is a caution, red is a warning, and
// controls sit in grey softkeys. A Garmin panel teaches that vocabulary to
// everyone who has flown behind one, and uScope maps onto it without inventing
// anything of its own. See the semantic fields on Palette for what each hue is
// allowed to mark.
//
// Phosphor and Mono keep that grammar and change what it is spoken in; see
// LookPhosphor and LookMono for what each one is and what it gives up.
// Vector is a brighter Glass, with the night field lifted off black and the
// sea and the land in tints of their own; see LookVector.
//
// Night is the default inside every look, because the uConsole is a backlit
// handheld: a light field is a torch in the face at night and eats battery all
// day. Day is the same grammar on a light field, which is what a panel app
// does for its daytime page.
package theme

import (
	"errors"
	"fmt"
	"image/color"
	"math"
)

// Palette is one complete set of drawing colours. A scene reads from it and
// names no colours of its own, so a new look is a new value here rather than
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
	// Phosphor sets it in bright phosphor rather than magenta. Mono has no
	// accent hue at all and sets it to the ink, which is what makes selection
	// there a matter of contrast rather than of colour; see LookMono.
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
	//
	// It is red in every look, Mono included. An emergency is a semantic
	// rather than an accent, and it is the one word on the board that has to
	// be read from across a room.
	Warn color.RGBA

	// Key is the softkey box the whole bottom bar is built from. It is a step
	// off the field rather than a step towards the ink, so a row of them reads
	// as hardware under the picture instead of as more type on it.
	Key color.RGBA

	// Rule is for hairlines and borders, a step above the field rather than
	// a step below the ink.
	Rule color.RGBA

	// Shore is the coastline under the scope, and the quietest colour any of
	// the eight palettes has. Against the field it carries well under half the
	// contrast Muted does, which is the level it was picked for: the rings
	// carry a number and have to be read, the coast only has to be
	// recognised, and a coastline as loud as the furniture turns the scope
	// into a map that happens to have aircraft on it.
	Shore color.RGBA

	// Water and Land are what the sea and the land are filled with under the
	// shore overlay. They are finished tints, already mixed into Field, and a
	// scene paints them as they are rather than fading them again. They live
	// on the palette because the right strength is not the same everywhere:
	// Glass wants ground that is barely there, and Vector wants sea and land
	// that stay apart under brightly coloured route tubes.
	Water color.RGBA
	Land  color.RGBA

	// Airway is the ink of the high airways, the overflights at cruise
	// bundled by the line they flew. It has to stay apart from the arrivals'
	// Data, the departures' Caution and the transit hairlines' Muted, and hold
	// on the ground, the water and the sky alike, so it is a cool violet on
	// the glass and vector looks.
	Airway color.RGBA

	// The altitude bands. Aircraft are coloured by how high they are, which
	// is the one piece of information a top-down scope cannot show by
	// position.
	//
	// Glass takes the ramp from the cockpit: green low, white in the middle,
	// orange high, so it reads as height rather than as a traffic light. There
	// AltLow deliberately repeats OK and AltMid deliberately repeats Ink; both
	// pairs mean the same thing in the same picture, and a second green or a
	// second white would be one more colour to keep in step for no gain
	// anybody could see.
	//
	// Phosphor cannot borrow that ramp, because green is the ground there
	// rather than a band: low goes cyan, mid is the pale phosphor itself, high
	// is an orange burn. Mono has no other colour on screen at all, so its
	// three bands are the picture's only hues and stay green, amber and red.
	AltLow  color.RGBA
	AltMid  color.RGBA
	AltHigh color.RGBA

	// Band is the header band's own background, and BandInk the text set on
	// it. They are separate from Field and Ink because a band is a strip
	// rather than a piece of the page, and text on it needs its own contrast.
	// Glass and Mono day set the band darker than the page so the masthead
	// reads as a strip on both themes; the two dark looks that already sit on
	// a near-black field set it flush and let the hairline under it do the
	// separating.
	Band    color.RGBA
	BandInk color.RGBA

	// Look is which of the four this palette belongs to, so a scene that has
	// been handed one can name it on the K softkey without being told twice.
	// It rides on the palette for the reason Light is worked out from the
	// palette: one value cannot disagree with itself, where a scene holding a
	// palette and a separate look would sooner or later draw one look's
	// colours under the other one's cap.
	//
	// The zero value is the empty string, which reads as LookGlass wherever a
	// Look is read, so a palette assembled by hand in a test names the default
	// rather than nothing.
	Look Look
}

// The Glass Night palette as packed 0xRRGGBB values. They are named constants
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

// The Vector Night palette: Glass night with the floor lifted. Glass night
// has its field at 0, its band at 16 and its shore at 55, and draws the land
// in the field, so at a glance land, water and coast run together into one
// dark sheet. These are the structural colours that sit near the floor. The
// field lands just over 20, and the band, the softkeys, the rules and the
// muted chrome keep roughly the steps they had above it in Glass. The shore
// is lifted further, to about 106, because the coast is the one line between
// land and water that has to read at a glance. The hues (ink, accent, data,
// OK, caution, warn, the high band) are Glass night's own, so the grammar is
// the same in both looks; Glass keeps its true black for the battery and for
// eyes adjusted to the dark.
//
// The ground tints are much stronger here than Glass's groundShare. Under
// the 3D route tubes at 8 per cent, land and sea came out at luma 33 and 35:
// two greys nobody could tell apart, under full-chroma cyan and amber. The
// sea takes a blue rather than the IN tube's cyan, so an inbound tube over
// water stands off it by hue, and the land a low-chroma grey-green that
// carries neither tube's hue. Both sit at about luma 50. The coast is lifted
// with them so it keeps its step over the stronger ground.
//
// Vector day keeps Day's colours except for the same three, pushed the
// other way off a light field.
const (
	vectorNightField = 0x0F161C
	vectorNightBand  = 0x1C262E
	vectorNightRule  = 0x4A555E
	vectorNightKey   = 0x3A444C
	vectorNightMuted = 0xA8B2BA
	vectorNightShore = 0x8FB0C4
	vectorNightWater = 0x173557
	vectorNightLand  = 0x2A3634

	vectorDayShore = 0x5A7A90
	vectorDayWater = 0xB4CDE3
	vectorDayLand  = 0xD4DDD8
)

// The Glass Day palette, packed the same way: the same grammar on a cool light
// grey, with every hue pulled down far enough to hold its meaning against a
// light field. The magenta deepens, the cyan becomes a teal, and the greens
// and reds darken rather than changing what they say.
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

// The Phosphor Night palette. Field, ink and rules are one green, so the band
// is the field and the band's ink is the page's; the hairline under the
// masthead is what separates them. The three bands are off the green on
// purpose, because green is the ground here rather than a reading.
const (
	phosphorNightField   = 0x05100A
	phosphorNightInk     = 0xBFE3C8
	phosphorNightMuted   = 0x3F6B4C
	phosphorNightRule    = 0x163021
	phosphorNightAccent  = 0x7DFF9E
	phosphorNightData    = 0x9FE0B4
	phosphorNightOK      = 0x4FCF7A
	phosphorNightCaution = 0xFFB300
	phosphorNightWarn    = 0xFF5A3C
	phosphorNightKey     = 0x0F2418
	phosphorNightAltLow  = 0x59C9D6
	phosphorNightAltMid  = 0xE7F2E4
	phosphorNightAltHigh = 0xFF9B52
	phosphorNightShore   = 0x143323
)

// The Phosphor Day palette: pale green chart paper under a dark green band.
// The accent and the OK green land on the same value, which is deliberate
// rather than a slip; see the repeat noted on PhosphorDay.
const (
	phosphorDayField   = 0xEEF3EA
	phosphorDayInk     = 0x12291B
	phosphorDayMuted   = 0x5E7A66
	phosphorDayRule    = 0xC5D3C4
	phosphorDayBand    = 0x163A26
	phosphorDayAccent  = 0x1E8F4A
	phosphorDayData    = 0x2A7A5A
	phosphorDayCaution = 0xB8770B
	phosphorDayWarn    = 0xC2571C
	phosphorDayKey     = 0xD3E0D2
	phosphorDayAltLow  = 0x1F8A96
	phosphorDayAltMid  = 0x6B7A6B
	phosphorDayShore   = 0xB9C9B7
)

// The Mono Night palette. There is no accent hue in it: the ink does that job
// by contrast, so Accent and Data are both the ink, and OK and Caution are two
// greys a step under it rather than a green and an amber. The warning red and
// the three altitude bands are the only colour left.
const (
	monoNightField   = 0x0B0B0D
	monoNightInk     = 0xECECEC
	monoNightMuted   = 0x6C6C72
	monoNightRule    = 0x2A2A2F
	monoNightOK      = 0xB8B8BE
	monoNightCaution = 0xB0B0B6
	monoNightWarn    = 0xD05A4A
	monoNightAltLow  = 0x4CAF6E
	monoNightAltMid  = 0xE0A93B
	monoNightShore   = 0x2B2B31
)

// The Mono Day palette: warm paper, near-black ink, and the same rule about
// the accent. The band is the ink rather than a colour of its own, which is
// what gives the day masthead its strip.
const (
	monoDayField   = 0xF2EFE9
	monoDayInk     = 0x1A1A1A
	monoDayMuted   = 0x6F6F6F
	monoDayRule    = 0xC9C6BE
	monoDayOK      = 0x4A4A4A
	monoDayCaution = 0x5A5A5A
	monoDayWarn    = 0xB83A2E
	monoDayKey     = 0xDAD6CE
	monoDayAltLow  = 0x2E8B4F
	monoDayAltMid  = 0xB8770B
	monoDayShore   = 0xBDB9B0
)

// The airway inks, one per palette.
const (
	nightAirway         = 0xA99BFF
	dayAirway           = 0x5B47C8
	phosphorNightAirway = 0xC8B4FF
	phosphorDayAirway   = 0x5B47A8
	monoNightAirway     = 0xA4A4C4
	monoDayAirway       = 0x4C4C6C
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

// groundShare is how far the handheld palettes pull their water off the field
// towards Data, and their land towards OK.
//
// Eight per cent is the whole argument. The tint has to say at a glance which
// side of a coastline is sea without becoming something anybody reads: an
// aircraft two pixels wide has to stand off it, the range rings have to stay
// the loudest lines on the scope, and the coast, the quietest colour in the
// palette, still has to show against it. Mixing towards Data and OK rather
// than towards colours of their own keeps each look's character with no table:
// cyan-black sea on Glass night, deeper green on Phosphor, grey on Mono, and
// land and sea at the same depth off the field so neither is the louder.
const groundShare = 0.08

// Night is Glass's dark palette: a true black field with white ink on it,
// which is the contrast an instrument panel is read at in the dark.
//
// It and Day keep their bare names, without the Glass prefix the other four
// carry, because Glass is the look everything starts on and these two are what
// every scene test names.
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
	Water:   tint(nightField, nightData),
	Land:    tint(nightField, nightOK),
	Airway:  rgb(nightAirway),

	// AltLow repeats OK and AltMid repeats Ink; see the note on the bands.
	AltLow:  rgb(nightOK),
	AltMid:  rgb(nightInk),
	AltHigh: rgb(nightAltHigh),

	Band:    rgb(nightBand),
	BandInk: rgb(nightInk),

	Look: LookGlass,
}

// Day is Glass's light palette: the same grammar on a cool light grey, keeping
// the near-black header band so the masthead reads the same on either theme.
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
	Water:   tint(dayField, dayData),
	Land:    tint(dayField, dayOK),
	Airway:  rgb(dayAirway),

	AltLow:  rgb(dayOK),
	AltMid:  rgb(dayInk),
	AltHigh: rgb(dayAltHigh),

	Band: rgb(dayBand),

	// BandInk is white rather than the page colour. The band is a near-black
	// strip on both themes, so its text is the same white on both, and the day
	// page is far too light to read as ink on it.
	BandInk: rgb(dayBandInk),

	Look: LookGlass,
}

// VectorNight is Glass night brightened: the same hues on a field lifted to
// a dark slate, with the band, rules, softkeys, muted chrome
// and shore lifted with it. The deliberate repeats are Glass night's.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var VectorNight = Palette{
	Field:   rgb(vectorNightField),
	Ink:     rgb(nightInk),
	Muted:   rgb(vectorNightMuted),
	Rule:    rgb(vectorNightRule),
	Accent:  rgb(nightAccent),
	Data:    rgb(nightData),
	OK:      rgb(nightOK),
	Caution: rgb(nightCaution),
	Warn:    rgb(nightWarn),
	Key:     rgb(vectorNightKey),
	Shore:   rgb(vectorNightShore),
	Water:   rgb(vectorNightWater),
	Land:    rgb(vectorNightLand),
	Airway:  rgb(nightAirway),

	AltLow:  rgb(nightOK),
	AltMid:  rgb(nightInk),
	AltHigh: rgb(nightAltHigh),

	Band:    rgb(vectorNightBand),
	BandInk: rgb(nightInk),

	Look: LookVector,
}

// VectorDay is Glass's Day under the Vector name. A light field has no
// floor to lift, so it is Day's colours with Vector's stronger ground and the
// coast darkened to hold against it; Look differs too, which keeps the K cap
// naming the look that is on.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var VectorDay = vectorDay()

// PhosphorNight is the CRT at night: one green for the field, the ink and the
// rules, with the selection in bright phosphor on top of it.
//
// Band repeats Field and BandInk repeats Ink. A dark look on a field this
// close to black has nothing to gain from a band a shade off it, and the
// hairline under the masthead already says where the strip ends.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var PhosphorNight = Palette{
	Field:   rgb(phosphorNightField),
	Ink:     rgb(phosphorNightInk),
	Muted:   rgb(phosphorNightMuted),
	Rule:    rgb(phosphorNightRule),
	Accent:  rgb(phosphorNightAccent),
	Data:    rgb(phosphorNightData),
	OK:      rgb(phosphorNightOK),
	Caution: rgb(phosphorNightCaution),
	Warn:    rgb(phosphorNightWarn),
	Key:     rgb(phosphorNightKey),
	Shore:   rgb(phosphorNightShore),
	Water:   tint(phosphorNightField, phosphorNightData),
	Land:    tint(phosphorNightField, phosphorNightOK),
	Airway:  rgb(phosphorNightAirway),

	AltLow:  rgb(phosphorNightAltLow),
	AltMid:  rgb(phosphorNightAltMid),
	AltHigh: rgb(phosphorNightAltHigh),

	Band:    rgb(phosphorNightField),
	BandInk: rgb(phosphorNightInk),

	Look: LookPhosphor,
}

// PhosphorDay is the same world on chart paper: a pale green page under a dark
// green band.
//
// Accent repeats OK, and AltHigh repeats Warn. Both are the price of holding
// one hue family on a light field: there is only so much room between the page
// and black for a green to be both the active thing and the valid thing, and
// the burnt orange a high aircraft is drawn in is the same burnt orange the
// warning box is filled with. Neither pair ever shares a piece of the picture,
// so neither costs a distinction anybody could see.
//
// BandInk repeats Field, because the band's text is the page lifted onto the
// strip rather than a white of its own.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var PhosphorDay = Palette{
	Field:   rgb(phosphorDayField),
	Ink:     rgb(phosphorDayInk),
	Muted:   rgb(phosphorDayMuted),
	Rule:    rgb(phosphorDayRule),
	Accent:  rgb(phosphorDayAccent),
	Data:    rgb(phosphorDayData),
	OK:      rgb(phosphorDayAccent),
	Caution: rgb(phosphorDayCaution),
	Warn:    rgb(phosphorDayWarn),
	Key:     rgb(phosphorDayKey),
	Shore:   rgb(phosphorDayShore),
	Water:   tint(phosphorDayField, phosphorDayData),
	Land:    tint(phosphorDayField, phosphorDayAccent),
	Airway:  rgb(phosphorDayAirway),

	AltLow:  rgb(phosphorDayAltLow),
	AltMid:  rgb(phosphorDayAltMid),
	AltHigh: rgb(phosphorDayWarn),

	Band:    rgb(phosphorDayBand),
	BandInk: rgb(phosphorDayField),

	Look: LookPhosphor,
}

// MonoNight is the dark look with the colour taken out: near-black field,
// near-white ink, and no accent hue anywhere.
//
// Accent and Data both repeat Ink, which is the whole point of the look rather
// than an economy: selection is made with contrast, and a data field is data
// whichever machine it is about. Key repeats Rule, so a softkey is the same
// step off the field a hairline is. AltHigh repeats Warn, the one red left.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var MonoNight = Palette{
	Field:   rgb(monoNightField),
	Ink:     rgb(monoNightInk),
	Muted:   rgb(monoNightMuted),
	Rule:    rgb(monoNightRule),
	Accent:  rgb(monoNightInk),
	Data:    rgb(monoNightInk),
	OK:      rgb(monoNightOK),
	Caution: rgb(monoNightCaution),
	Warn:    rgb(monoNightWarn),
	Key:     rgb(monoNightRule),
	Shore:   rgb(monoNightShore),
	Water:   tint(monoNightField, monoNightInk),
	Land:    tint(monoNightField, monoNightOK),
	Airway:  rgb(monoNightAirway),

	AltLow:  rgb(monoNightAltLow),
	AltMid:  rgb(monoNightAltMid),
	AltHigh: rgb(monoNightWarn),

	Band:    rgb(monoNightField),
	BandInk: rgb(monoNightInk),

	Look: LookMono,
}

// MonoDay is the same rule on warm paper. Band repeats Ink and BandInk repeats
// Field, so the masthead is the page inverted rather than a colour: on a look
// with no hues to spend, the strongest strip available is the one made out of
// the two colours already on screen.
//
//nolint:gochecknoglobals // a palette is data, and color.RGBA cannot be const.
var MonoDay = Palette{
	Field:   rgb(monoDayField),
	Ink:     rgb(monoDayInk),
	Muted:   rgb(monoDayMuted),
	Rule:    rgb(monoDayRule),
	Accent:  rgb(monoDayInk),
	Data:    rgb(monoDayInk),
	OK:      rgb(monoDayOK),
	Caution: rgb(monoDayCaution),
	Warn:    rgb(monoDayWarn),
	Key:     rgb(monoDayKey),
	Shore:   rgb(monoDayShore),
	Water:   tint(monoDayField, monoDayInk),
	Land:    tint(monoDayField, monoDayOK),
	Airway:  rgb(monoDayAirway),

	AltLow:  rgb(monoDayAltLow),
	AltMid:  rgb(monoDayAltMid),
	AltHigh: rgb(monoDayWarn),

	Band:    rgb(monoDayInk),
	BandInk: rgb(monoDayField),

	Look: LookMono,
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

// WCAG relative luminance, which Light is measured with: each channel is
// linearised out of its sRGB encoding before the three are weighted and
// summed, which is what makes the result track how bright a colour actually
// looks rather than what its raw byte says.
//
// pkg/airlines works an airline's brand colour out with the same arithmetic.
// It is nine lines, and a palette package that imported the airline database
// to ask how bright a colour is would be a stranger dependency than the
// repetition is a cost.
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

	// midLuminance is where Light puts the line between a dark field and a
	// light one: halfway up the range from black to white. Nothing sits near
	// it. The four night fields measure under 0.008 and the four day fields
	// over 0.86, so the threshold has more than eight tenths of the range as
	// margin on either side, and it would take a palette that is genuinely
	// mid-grey to make the answer a close call.
	midLuminance = 0.5

	// minBandGap is the luminance a colour has to put between itself and Band
	// before OnBand will let it be set on the masthead. The measurements are
	// on OnBand.
	minBandGap = 0.10
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

// Light reports whether this palette draws on a light field.
//
// A scene needs it when it has to adapt a colour that did not come out of the
// palette. An airline's brand colour is the case that forced it: the same navy
// has to be lifted off a black field and pushed down onto a paper one, and the
// palette is the only thing the scene is handed that says which of the two it
// is drawing on.
//
// It measures the field rather than comparing the palette against a known
// value. It used to be Day and nothing else, which was answerable while there
// were two palettes and became wrong the moment there were six: PhosphorDay
// and MonoDay are light fields that are not Day, and an airline colour pushed
// the wrong way on either of them would come out as ink-on-ink. Measuring also
// means a palette built by hand in a test gets the answer its own field earns
// rather than the answer a table happens to hold for it.
func (p Palette) Light() bool {
	return relativeLuminance(p.Field) > midLuminance
}

// OnBand adapts one of this palette's colours for the header band, returning
// either the colour itself or BandInk.
//
// Every hue in a palette is picked against Field, and the band is not the
// field: it is a strip with a background and an ink of its own. Most of the
// time the two agree well enough and the colour comes back untouched, which is
// what keeps Glass's cyan clocks cyan on the masthead. Where they do not, the
// band's own ink comes back instead, because a reading nobody can see is worse
// than a reading in the wrong colour.
//
// One palette needs this and the other seven do not. MonoDay has no accent and
// no data hue, so its Data is its Ink, and its band is an inverted strip
// filled with that same ink: every reading on the masthead would be black on
// black, at a measured gap of exactly zero. Its OK and Caution greys are
// nearly as bad there, at 0.058 and 0.092. Everything the other seven set on
// their bands clears the floor with room, the tightest being PhosphorDay's
// Data at 0.118, so nothing that was judged on a picture moves because this
// exists.
//
// It is the argument internal/radar's bandMuted already makes for the band's
// quiet chrome, one step further out: a colour chosen against the page cannot
// be assumed to read on the strip.
func (p Palette) OnBand(col color.RGBA) color.RGBA {
	if math.Abs(relativeLuminance(col)-relativeLuminance(p.Band)) >= minBandGap {
		return col
	}

	return p.BandInk
}

// as is a copy of the palette that names another look.
func (p Palette) as(look Look) Palette {
	p.Look = look

	return p
}

// vectorDay is Day with Vector's ground and coast.
func vectorDay() Palette {
	day := Day.as(LookVector)
	day.Shore = rgb(vectorDayShore)
	day.Water = rgb(vectorDayWater)
	day.Land = rgb(vectorDayLand)

	return day
}

// tint is ink mixed into field by groundShare, one channel at a time and
// rounded to the nearest byte, which is the same arithmetic internal/radar's
// fade uses. The handheld's ground was drawn that way before the tints moved
// onto the palette, so mixing them identically keeps its flat frames
// identical.
func tint(field, ink uint32) color.RGBA {
	from, to := rgb(field), rgb(ink)

	return color.RGBA{
		R: tintChannel(from.R, to.R),
		G: tintChannel(from.G, to.G),
		B: tintChannel(from.B, to.B),
		A: opaque,
	}
}

// tintChannel mixes one channel. groundShare is in [0, 1], so the result
// cannot leave the byte.
func tintChannel(field, ink uint8) uint8 {
	mixed := float64(field) + (float64(ink)-float64(field))*groundShare

	return uint8(math.Round(mixed)) //nolint:gosec // a mix of bytes.
}

// Kind names a theme: one of the two a look carries, or Auto, which picks
// between them from the sun. It is what --theme parses into and what Config
// holds as the theme to start on.
type Kind string

// The three spellings --theme accepts. Named KindNight and KindDay rather than
// Night and Day so they do not collide with the Palette variables above, which
// are what a Kind resolves to under the Glass look.
const (
	KindNight Kind = "night"
	KindDay   Kind = "day"

	// KindAuto is night or day by the sun at the receiver, decided by the
	// scene that knows where the receiver is. Everything in this package
	// that has to turn it into colours without that knowledge treats it as
	// night, so Look.Palette(KindAuto) is the night palette.
	KindAuto Kind = "auto"
)

// ErrUnknown is returned for a --theme value that is none of the spellings.
var ErrUnknown = errors.New("theme: unknown theme")

// ErrUnknownLook is the same for a --look value that names none of the four.
var ErrUnknownLook = errors.New("theme: unknown look")

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
	case KindAuto:
		return KindAuto, nil
	default:
		return KindNight, fmt.Errorf("%w: %q", ErrUnknown, text)
	}
}

// Next cycles night to day to auto and back to night, which is what the l
// key is bound to. Anything that is none of the three is treated as night and
// moves to day, so the zero value cycles the same way KindNight does.
func (k Kind) Next() Kind {
	switch k {
	case KindDay:
		return KindAuto
	case KindAuto:
		return KindNight
	case KindNight:
		fallthrough
	default:
		return KindDay
	}
}

// Look names one of the four looks uScope can wear. It is what --look
// parses into, what the k key cycles, and what a Palette carries so a scene
// can say which one it is holding.
type Look string

// The four spellings --look accepts.
const (
	// LookGlass is the cockpit palette and the default: magenta for the
	// active thing, cyan for a data field, green for engaged or valid, amber
	// for a caution, red for a warning, controls in grey softkeys. It is the
	// only look whose hues carry meanings somebody already knows
	// before they read the legend.
	LookGlass Look = "glass"

	// LookPhosphor commits to the CRT. The whole picture becomes a green
	// phosphor world: field, ink and rules are one hue, and the selection is
	// bright phosphor rather than magenta. Because green is now the ground,
	// the altitude bands have to move off it, so low goes cyan, mid is the
	// pale phosphor itself and high is an orange burn. Day is the same world
	// as pale green chart paper under a dark green band.
	//
	// What it buys is a picture nobody mistakes for anything else, and a
	// monochrome ground on which every coloured thing is data. What it costs
	// is airline mode, where brand colours land on a green field that was not
	// chosen with them in mind.
	LookPhosphor Look = "phosphor"

	// LookMono has no accent hue. Selection is made with contrast instead,
	// which is the strongest mark a pixel display has and the one thing that
	// survives any palette: the selected row is an inverted block, ink on
	// field turned field on ink. The only colour left on screen is the three
	// altitude bands and, in airline mode, the operators.
	//
	// Red stays on the warning. An emergency is a semantic rather than an
	// accent, and it is the single thing on the board that has to be read from
	// across a room, so taking it down to a grey would be giving up the one
	// colour in the look that is genuinely earning its place.
	LookMono Look = "mono"

	// LookVector is a brighter Glass. Its day is Glass day with stronger
	// ground. Its night keeps every Glass hue but lifts the field, the band,
	// the rules and the shore off black, and fills the sea and the land with
	// tints of their own, so the map under the traffic reads as a map on a
	// monitor or a projector rather than as one black sheet. What it costs is
	// the true black Glass keeps for the battery and for night vision, which
	// is why it is a look of its own rather than a change to Glass.
	LookVector Look = "vector"
)

// ParseLook turns a --look value into a Look.
//
// The match is case sensitive for the reason Parse's is: --look is an allow
// list rather than free text, so "Mono" is refused the same way "monochrome"
// is.
func ParseLook(text string) (Look, error) {
	switch Look(text) {
	case LookGlass:
		return LookGlass, nil
	case LookPhosphor:
		return LookPhosphor, nil
	case LookMono:
		return LookMono, nil
	case LookVector:
		return LookVector, nil
	default:
		return LookGlass, fmt.Errorf("%w: %q", ErrUnknownLook, text)
	}
}

// Next cycles to the next look, glass to phosphor to mono to vector and
// back. This is what the k key is bound to.
//
// Anything that is not one of the three later spellings is treated as glass
// and moves to phosphor, so the zero value cycles the same way LookGlass does.
func (l Look) Next() Look {
	switch l {
	case LookPhosphor:
		return LookMono
	case LookMono:
		return LookVector
	case LookVector:
		return LookGlass
	case LookGlass:
		fallthrough
	default:
		return LookPhosphor
	}
}

// Palette resolves a look and a theme to the one palette that pair names.
//
// Each axis falls back to its own default rather than to a special case: an
// unrecognised look draws Glass and anything that is not KindDay draws night.
// That is what makes the zero value of both usable as "whatever the program
// starts on" instead of a value every caller has to check first.
func (l Look) Palette(kind Kind) Palette {
	night, day := l.palettes()

	if kind == KindDay {
		return day
	}

	return night
}

// palettes is one look's pair, night first.
func (l Look) palettes() (Palette, Palette) {
	switch l {
	case LookPhosphor:
		return PhosphorNight, PhosphorDay
	case LookMono:
		return MonoNight, MonoDay
	case LookVector:
		return VectorNight, VectorDay
	case LookGlass:
		fallthrough
	default:
		return Night, Day
	}
}
