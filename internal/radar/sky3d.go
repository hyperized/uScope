package radar

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/sun"
)

// skyEvery is how long the sky's sun answer stands before it is asked again.
//
// The sky slides through twilight at about a degree every six or seven
// minutes at Dutch latitudes, and a minute's step there moves the horizon
// three or four levels at once across the whole frame, which reads as a
// flicker on a still picture. Ten seconds keeps every step under one level,
// and the sum is a few hundred nanoseconds. The auto theme asks on the same
// interval, so its swap at the horizon is never late against the sky.
const skyEvery = 10 * time.Second

// The sky's keyframes, as the sky itself looks: packed 0xRRGGBB values before
// any palette has had its say. They are named rather than written inline for
// the reason the palettes in internal/theme are: a bare hex number in a table
// says nothing about which colour it is meant to be.
const (
	// nauticalHorizon is the band of deep blue that rises along the horizon
	// before anything warm does, once the sun is six degrees down.
	nauticalHorizon = 0x1E3270

	// civilZenith and civilHorizon are the sky two thirds of the way through
	// civil twilight: a zenith only just off black, and the horizon at its
	// most orange. Dawn and dusk are drawn alike; telling them apart would
	// need the sun's azimuth against the camera's, and the camera orbits.
	civilZenith  = 0x0A1430
	civilHorizon = 0xC8603A

	// riseZenith and riseHorizon are the sky with the sun on the horizon,
	// the glow turned from orange to gold.
	riseZenith  = 0x183A78
	riseHorizon = 0xF0A050

	// dayZenith and dayHorizon are the sky once the sun is ten degrees up and
	// from there on: blue overhead, nearly white along the horizon.
	dayZenith  = 0x3A78C8
	dayHorizon = 0xC8DCF0
)

// The sun elevations the keyframes sit at, in degrees.
const (
	// nightElevation is astronomical twilight's edge, below which the sky is
	// the field: nothing a dark picture should show.
	nightElevation = -12.0

	// civilPeakElevation is where the horizon is most orange. Linear between
	// deep blue at -6 and gold at 0, the middle of civil twilight came out a
	// muddy mauve and never looked like a dusk at all.
	civilPeakElevation = -4.0

	// lowSunElevation is the top of the low sun, past which the sky holds.
	lowSunElevation = 10.0
)

// How far each palette lets the sky off its field.
const (
	// nightSkyShare is how much of the sky a dark palette shows, the rest
	// being its own field. A third keeps the brightest sky any night palette
	// draws, a daytime horizon under --theme night, at a luma in the low
	// eighties, well under mid-grey, so white ink and the orange high band
	// still stand off it.
	nightSkyShare = 0.35

	// daySkyShare is the same for a light palette. Under half keeps the sky
	// a tint of the grey field rather than a picture fighting it, and enough
	// that a dusk horizon still reads as warm.
	daySkyShare = 0.45

	// skyDepthDeg is how high above the horizon, in degrees, the sky reaches
	// its zenith colour. The real gradient runs to ninety degrees, but the
	// camera at a shallow ten degree tilt shows under six degrees
	// of sky, so the gradient is fitted to what the camera can see; measured
	// in degrees rather than in rows it does not change as the view resizes.
	skyDepthDeg = 6.0
)

// Rec. 601 luma weights, which is what the ceiling on the night sky is
// measured in, and what the one-hued looks map the sky's brightness onto
// their own hue by.
const (
	lumaRed   = 0.299
	lumaGreen = 0.587
	lumaBlue  = 0.114
)

// skyTone is one colour in the keyframe table: either a colour of the sky's
// own or, with lit false, the palette's field.
type skyTone struct {
	rgb uint32
	lit bool
}

// skyKey is the sky at one sun elevation.
type skyKey struct {
	elevation float64
	zenith    skyTone
	horizon   skyTone
}

// skyKeys is the sky across the day, in rising elevation. Between two
// neighbours the colours slide linearly in elevation; below the first and
// above the last they hold.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var skyKeys = [...]skyKey{
	{elevation: nightElevation},
	{elevation: sun.CivilLimit, horizon: skyTone{rgb: nauticalHorizon, lit: true}},
	{
		elevation: civilPeakElevation,
		zenith:    skyTone{rgb: civilZenith, lit: true},
		horizon:   skyTone{rgb: civilHorizon, lit: true},
	},
	{elevation: 0, zenith: skyTone{rgb: riseZenith, lit: true}, horizon: skyTone{rgb: riseHorizon, lit: true}},
	{
		elevation: lowSunElevation,
		zenith:    skyTone{rgb: dayZenith, lit: true},
		horizon:   skyTone{rgb: dayHorizon, lit: true},
	},
}

// sky3 is the tilted views' sky: what the sun was last asked from, the two
// colours that answer came to in the palette on screen, and the rows painted
// from them.
//
// The colours are mixed when the sun is asked or the palette changes, never
// per frame, and the rows when the camera's tilt, the box or the colours
// change, so a steady frame does nothing here but compare.
type sky3 struct {
	sunFix

	// elevation is the sun's height at the receiver in degrees, as last
	// asked, and only meaningful while known is true.
	elevation float64

	// pal is the palette zenith and horizon were mixed for, and mixed false
	// until they have been mixed at all for the answer on hand.
	pal     theme.Palette
	mixed   bool
	zenith  color.RGBA
	horizon color.RGBA

	// rows is the gradient one colour per row from the top of the window
	// down to the horizon, and rowsFor what it was worked out from.
	rows    []color.RGBA
	rowsFor skyRowsKey
}

// skyRowsKey is everything the sky's rows depend on.
type skyRowsKey struct {
	box       image.Rectangle
	window    image.Rectangle
	elevation float64
	zenith    color.RGBA
	horizon   color.RGBA
}

// resolveSky is the sky's per-frame check, beside resolveTheme and after it,
// so the palette the sky is mixed in is the one this frame is drawn in.
//
// The flat views never ask: they have no horizon to put a sky over.
func (s *Scene) resolveSky(frame source.Frame) {
	if !s.perspective() {
		return
	}

	if s.sky.stale(frame.Now, frame.Receiver, skyEvery) {
		s.sky.remember(frame.Now, frame.Receiver)
		s.sky.elevation = sun.Elevation(s.sky.lat, s.sky.lon, frame.Now)
		s.sky.mixed = false
	}

	if !s.sky.mixed || s.sky.pal != s.pal {
		s.sky.zenith, s.sky.horizon = s.pal.Field, s.pal.Field
		if s.sky.known {
			s.sky.zenith, s.sky.horizon = skyInks(s.pal, s.sky.elevation)
		}

		s.sky.pal, s.sky.mixed = s.pal, true
	}
}

// skyClears reports whether the sky is the frame's clear rather than a paint
// over one. The bare tilted view is the picture edge to edge, so the sky can
// be the only write those pixels get; the full view has a header, a column
// and a key bar around its box that want the field, so its frame is cleared
// to that first and the sky painted over the box alone.
func (s *Scene) skyClears() bool {
	return s.perspective() && s.bare()
}

// paintSky clears the tilted view's window: the gradient from the top of the
// window to the horizon, and the field below it.
//
// It is the frame's clear rather than a layer, one colour per row written
// once. With no sun the full view has nothing to add to the field the frame
// was already cleared to; the bare one, which skipped that clear, still
// clears here, to the field.
//
// Below the horizon is the field whatever the sun is doing. The ground disc
// and the land are drawn over it, and past the disc nothing models the
// ground at all: the field there puts a dark land against the sky, which is
// what a horizon is, where a floor in the horizon's own colour would make a
// sunset the loudest thing in the picture.
func (s *Scene) paintSky(window *canvas.Canvas, box image.Rectangle) {
	if !s.sky.known && !s.skyClears() {
		return
	}

	window.ClearRows(s.skyRows(box, window.Bounds()), s.pal.Field)
}

// skyRows is the sky's gradient for a window onto box, one colour per row
// from the top of the window down to the horizon, and nothing without a sun.
//
// Each row takes the colour of its own angle above the horizon, measured up
// the middle of the picture, so the gradient stays where it is in the sky as
// the tilt changes rather than squeezing into however many rows are left.
// The rows are kept and only worked out again when the box, the window, the
// tilt or the colours move.
func (s *Scene) skyRows(box, window image.Rectangle) []color.RGBA {
	if !s.sky.known {
		return nil
	}

	key := skyRowsKey{box: box, window: window, elevation: s.elevation, zenith: s.sky.zenith, horizon: s.sky.horizon}
	if key == s.sky.rowsFor && s.sky.rows != nil {
		return s.sky.rows
	}

	horizon := horizonRow(box, s.elevation)
	focal := float64(box.Dx())
	centreY := float64(box.Min.Y + box.Dy()/2)
	tilt := s.elevation * math.Pi / halfCircle

	s.sky.rows = s.sky.rows[:0]

	for y := window.Min.Y; y < min(horizon, window.Max.Y); y++ {
		above := (math.Atan((centreY-float64(y))/focal) - tilt) * halfCircle / math.Pi
		s.sky.rows = append(s.sky.rows, blendRGBA(s.sky.horizon, s.sky.zenith, above/skyDepthDeg))
	}

	// A window wholly under the horizon has no rows, and an empty but non-nil
	// slice is what tells the next frame the answer is already on hand.
	if s.sky.rows == nil {
		s.sky.rows = make([]color.RGBA, 0, 1)
	}

	s.sky.rowsFor = key

	return s.sky.rows
}

// skyInks is the zenith and the horizon for a sun elevation in a palette.
//
// The keyframes are mixed into the palette first and interpolated after, so
// a keyframe that is the field is exactly the field and the slide away from
// it starts from the palette's own black or grey rather than from a black
// that only the glass night palette has.
func skyInks(pal theme.Palette, elevation float64) (color.RGBA, color.RGBA) {
	lower, upper, along := skyKeysAround(elevation)

	return blendRGBA(skyInk(pal, lower.zenith), skyInk(pal, upper.zenith), along),
		blendRGBA(skyInk(pal, lower.horizon), skyInk(pal, upper.horizon), along)
}

// skyKeysAround is the pair of keyframes an elevation falls between and how
// far from the lower to the upper it lies, clamped to the ends of the table.
// A NaN lands on the night keyframe, so a sum gone wrong shows the field.
func skyKeysAround(elevation float64) (skyKey, skyKey, float64) {
	last := len(skyKeys) - 1
	if elevation >= skyKeys[last].elevation {
		return skyKeys[last], skyKeys[last], 0
	}

	for index := 1; index <= last; index++ {
		if elevation < skyKeys[index].elevation {
			lower, upper := skyKeys[index-1], skyKeys[index]

			return lower, upper, max((elevation-lower.elevation)/(upper.elevation-lower.elevation), 0)
		}
	}

	return skyKeys[0], skyKeys[0], 0
}

// skyInk is one keyframe colour as a palette draws it: the field for an unlit
// tone, and otherwise the sky's colour in the palette's look, let off the
// field by the palette's share.
func skyInk(pal theme.Palette, tone skyTone) color.RGBA {
	if !tone.lit {
		return pal.Field
	}

	share := nightSkyShare
	if pal.Light() {
		share = daySkyShare
	}

	return blendRGBA(pal.Field, lookTint(pal, unpackRGB(tone.rgb)), share)
}

// lookTint is a sky colour in a palette's look. Glass shows the sky's own
// colours. Phosphor and mono are one hue each by design, so the sky keeps
// its brightness and takes their hue, read off the palette's Data colour,
// which is the green of the phosphor look and the plain grey ink of the mono
// one: a green dusk on the one and a grey dusk on the other.
func lookTint(pal theme.Palette, col color.RGBA) color.RGBA {
	if pal.Look != theme.LookPhosphor && pal.Look != theme.LookMono {
		return col
	}

	scale := luma(col) / max(luma(pal.Data), 1)

	return color.RGBA{
		R: scaleChannel(pal.Data.R, scale),
		G: scaleChannel(pal.Data.G, scale),
		B: scaleChannel(pal.Data.B, scale),
		A: opaque,
	}
}

// scaleChannel multiplies one channel, saturating at full.
func scaleChannel(channel uint8, scale float64) uint8 {
	return uint8(min(math.Round(float64(channel)*scale), opaque)) //nolint:gosec // in range.
}

// luma is a colour's Rec. 601 luma, nought to 255.
func luma(col color.RGBA) float64 {
	return lumaRed*float64(col.R) + lumaGreen*float64(col.G) + lumaBlue*float64(col.B)
}

// blendRGBA is from mixed towards to by along, clamped to the two ends.
func blendRGBA(from, to color.RGBA, along float64) color.RGBA {
	return color.RGBA{
		R: mix(from.R, to.R, along),
		G: mix(from.G, to.G, along),
		B: mix(from.B, to.B, along),
		A: opaque,
	}
}

// unpackRGB turns a packed 0xRRGGBB value into an opaque colour.
func unpackRGB(packed uint32) color.RGBA {
	return color.RGBA{
		R: uint8(packed >> skyRedShift),   //nolint:gosec // masked by the shift.
		G: uint8(packed >> skyGreenShift), //nolint:gosec // truncation is the point.
		B: uint8(packed),                  //nolint:gosec // truncation is the point.
		A: opaque,
	}
}

// Where the channels sit in a packed colour.
const (
	skyGreenShift = 8
	skyRedShift   = 16
)
