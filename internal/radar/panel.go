package radar

import (
	"image"
	"image/color"
	"time"
	"unicode/utf8"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// The header band's fixed strings and spacing.
const (
	appTitle    = "USCOPE"
	clockFormat = "15:04"

	// sourceGap is the air between the wordmark and the source dot, dotRadius
	// the dot itself, and dotGap the air before the source label.
	sourceGap = 14
	dotRadius = 3
	dotGap    = 6

	// estimatePrefix opens the receiver line when the position was worked out
	// from the aircraft rather than sensed. The plus-minus says out loud that
	// this is a guess with a radius, not a fix.
	estimatePrefix = "EST ±"

	// noFixText is the receiver line when nothing is known. It is set in caps
	// like every other label in the scene rather than in the sentence case
	// uAirwaves uses, because next to USCOPE and EST a lower-case line reads
	// as a different kind of thing.
	noFixText = "NO FIX"

	// The mode words the receiver line opens with when there are coordinates
	// to follow. They say what the two numbers after them are worth, which the
	// numbers themselves cannot.
	manualText     = "MANUAL"
	gps2DText      = "GPS 2D"
	gps3DText      = "GPS 3D"
	gpsNoFixText   = "GPS ---"
	unknownFixText = "???"

	// modeGap is the air between the mode word and the coordinates.
	modeGap = 8

	// coordinateSeparator sits between the two halves of a position.
	coordinateSeparator = " / "

	// sourceLabelGap is the air the source label leaves between itself and the
	// clocks on the other side of the band. It is what stops a long label
	// running up against the UTC label rather than stopping short of it.
	sourceLabelGap = 24

	// cutEllipsis marks a label the band had to cut, and cutDot stands in on a
	// face with no ellipsis glyph. The four embedded Terminus faces are
	// console fonts and nothing guarantees U+2026, which is the check the
	// vertical-rate triangle and the degree sign both went through.
	cutEllipsis  = "…"
	cutDot       = "."
	ellipsisRune = '…'
)

// capToggle names the setting a key cap reflects.
//
// The bar used to draw every cap the same way, so nothing on screen said
// whether auto range was on. It was, and the scope had widened itself to 180
// nautical miles because the feed hears traffic that far out, which is exactly
// the state a bar of identical caps cannot explain.
type capToggle uint8

// The settings a cap can reflect. capAlways is the default, which is what a
// key that is not a toggle gets: q has no off state, so its cap is always
// filled.
const (
	capAlways capToggle = iota
	capAuto
	capTrails
	capAirports
	capShore
	capColour
	capTheme
	capOrbit
	capEnvelope
)

// The two cycling keys are labelled with the value they are on rather than
// with the name of the setting. A cap reading COLOUR says there is a colour
// mode without saying which one, which is the question it was being asked.
const (
	labelAltitude = "ALT"
	labelAirline  = "AIRLINE"
	labelNight    = "NIGHT"
	labelPaper    = "PAPER"
)

// keyCap is one entry in the bottom bar: the cap, then what the key does, then
// which setting its state comes from.
type keyCap struct {
	key   string
	label string
	state capToggle
}

// keyCaps is the key legend, in the order the keys are worth reaching for:
// leaving, then moving through the list, then the five things that change what
// is on the field, then the two that change how it looks.
//
// Ten caps and their labels come to 736 pixels of the 1248 the panel leaves
// between its margins, so nothing here has to be shortened to fit.
//
// The select cap is drawn as the two arrow glyphs rather than as N/P. Both are
// bound, but the arrows are what a hand reaches for first, and all four
// embedded faces carry U+2191 and U+2193, which was checked before this was
// written rather than assumed.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var keyCaps = [...]keyCap{
	{key: "Q", label: "QUIT"},
	{key: "↑↓", label: "SELECT"},
	{key: "+/-", label: "RANGE"},
	{key: "R", label: "AUTO", state: capAuto},
	{key: "T", label: "TRAILS", state: capTrails},
	{key: "A", label: "AIRPORTS", state: capAirports},
	{key: "M", label: "SHORE", state: capShore},
	{key: "C", label: "COLOUR", state: capColour},
	{key: "L", label: "THEME", state: capTheme},
	{key: "V", label: "VIEW"},
}

// view3DCaps are the two caps the 3D view adds to the end of the bar.
//
// They are appended rather than living in keyCaps because neither key does
// anything in the other two views, and a cap for a key with no effect is
// furniture pretending to be a control. In the 3D view the bar comes to about
// 900 pixels of the 1248 the panel leaves between its margins, so the pair
// fits without anything above having to give way.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var view3DCaps = [...]keyCap{
	{key: "O", label: "ORBIT", state: capOrbit},
	{key: "E", label: "ENVELOPE", state: capEnvelope},
}

// drawKeyBar puts the key legend on the bottom edge and takes the room it
// used out of the layout, so everything above flows into what is left.
//
// It runs first for that reason: the bar belongs to the bottom of the screen
// whatever else is on it, and a bar pushed down by a block above would end up
// somewhere in the middle.
func (s *Scene) drawKeyBar(lay *layout) {
	reserved := s.keyBarHeight(lay)
	if reserved == 0 {
		return
	}

	top := lay.bottom - (reserved - blockGap)

	pen := s.drawCaps(lay, lay.left, top, keyCaps[:])
	if s.perspective() && pen < lay.right {
		s.drawCaps(lay, pen, top, view3DCaps[:])
	}

	lay.bottom -= reserved
}

// drawCaps draws a run of caps from pen and returns the x just past the last
// one it drew.
//
// It stops after the cap that reaches the right margin rather than before it,
// which is what the bar has always done: one cap running into the margin says
// there was more, where stopping short of it just looks like the bar ended.
func (s *Scene) drawCaps(lay *layout, pen, top int, caps []keyCap) int {
	face := s.faces.Small

	for _, entry := range caps {
		pen = s.drawCap(lay.dst, pen, top, entry.key, s.capOn(entry.state))
		pen += capGap
		pen = text.Draw(lay.dst, face, pen, top+capPadY, s.capLabel(entry), s.pal.Muted)
		pen += entryGap

		if pen >= lay.right {
			break
		}
	}

	return pen
}

// keyBarHeight is the room the key bar takes off the bottom, blockGap
// included, or zero when there is no face to set it in or no room left for it.
//
// It is separate from drawKeyBar because the background layer has to carve the
// frame the same way without drawing a bar of its own: the layer holds the
// scope, and the scope only sits where it does because the bar took its room
// off the bottom first.
func (s *Scene) keyBarHeight(lay *layout) int {
	capHeight := lineHeight(s.faces.Small)
	if capHeight == 0 {
		return 0
	}

	height := capHeight + 2*capPadY
	if !lay.fits(height) {
		return 0
	}

	return height + blockGap
}

// drawCap draws one key cap and returns the x just past it.
//
// A cap for a setting that is on is the letter knocked out of a filled box,
// which is what every cap used to look like. One that is off is the same box
// as a hairline outline with the letter in ink, so the two read as a switch
// thrown and a switch not thrown rather than as two different words.
//
//nolint:revive // flag-parameter: on picks which of two caps to draw, not a mode to branch deeper on.
func (s *Scene) drawCap(dst *canvas.Canvas, left, top int, key string, on bool) int {
	face := s.faces.Small
	width, height := text.Measure(face, key)
	box := image.Rect(left, top, left+width+2*capPadX, top+height+2*capPadY)

	if !on {
		dst.Rect(box, s.pal.Ink)
		text.Draw(dst, face, left+capPadX, top+capPadY, key, s.pal.Ink)

		return box.Max.X
	}

	dst.FillRect(box, s.pal.Ink)
	text.Draw(dst, face, left+capPadX, top+capPadY, key, s.pal.Field)

	return box.Max.X
}

// capOn reports whether a cap is drawn filled. Everything that is not a
// toggle is, because there is no state for it to be in.
func (s *Scene) capOn(which capToggle) bool {
	switch which {
	case capAuto:
		return s.autoRange
	case capTrails:
		return s.trails
	case capAirports:
		return s.airports
	case capShore:
		return s.shoreOn
	case capOrbit:
		return s.orbiting
	case capEnvelope:
		return s.envelope
	case capAlways, capColour, capTheme:
		fallthrough
	default:
		return true
	}
}

// capLabel is what one entry's cap says.
//
// The theme is read off the palette rather than kept as a second field, for
// the reason SetPalette takes the light flag off the palette: two fields can
// disagree about which theme is on and one cannot.
func (s *Scene) capLabel(entry keyCap) string {
	switch entry.state {
	case capColour:
		if s.colour == ColourAirline {
			return labelAirline
		}

		return labelAltitude
	case capTheme:
		if s.light {
			return labelPaper
		}

		return labelNight
	case capAlways, capAuto, capTrails, capAirports, capShore, capOrbit, capEnvelope:
		fallthrough
	default:
		return entry.label
	}
}

// drawHeader draws the band across the top: the wordmark and the source on
// the left with the receiver position under them, the two clocks and the
// battery on the right, a hairline under all of it.
//
// The band is filled with pal.Band and everything on it is set in pal.BandInk
// rather than the scene's usual muted/ink split, because paper's band is a
// navy strip and needs its own contrast rather than the page's. Night's Band
// and BandInk repeat Field and Ink, so there the band is invisible and the
// text reads exactly as it did before the band existed.
//
// The fill bleeds to all three edges it touches rather than sitting inside
// the layout margin, and the hairline under it runs the full width with it.
// Inset, the band read as a navy rectangle with paper showing above and to
// the left of it, which is a box on a page rather than the masthead it is
// meant to be. The type does not move: what was the layout's margin is now
// the band's own inner padding, so the wordmark sits exactly where it sat and
// the scope, the column and the key bar keep their margins untouched.
func (s *Scene) drawHeader(lay *layout, frame source.Frame) {
	total := s.headerHeight(lay)
	if total == 0 {
		return
	}

	markHeight := lineHeight(s.faces.BodyBold)
	band := total - ruleHeight - blockGap
	bounds := lay.dst.Bounds()
	rule := lay.top + band

	lay.dst.FillRect(image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, rule), s.pal.Band)

	top := lay.top + headerPadY

	// The right of the band is drawn first because it is the only thing that
	// can say where the left of it has to stop. Its width depends on whether
	// there is a battery and on how wide the two clocks set, so measuring it
	// any other way would mean measuring it twice.
	edge := s.drawHeaderRight(lay, lay.top+band/2, frame.Now)

	s.drawWordmark(lay, top, frame, edge-sourceLabelGap)
	s.drawReceiverLine(lay, top+markHeight+rowLead, frame.Receiver)

	lay.dst.FillRect(image.Rect(bounds.Min.X, rule, bounds.Max.X, rule+ruleHeight), s.pal.Rule)

	lay.top += total
}

// headerHeight is the room the header band takes off the top, its hairline and
// blockGap included, or zero when one of its three faces is missing or there
// is no room left for it.
//
// It exists for the same reason keyBarHeight does: the background layer has to
// push the scope down by exactly what the header will push it down by, without
// drawing a band of its own over the one the frame draws.
func (s *Scene) headerHeight(lay *layout) int {
	markHeight := lineHeight(s.faces.BodyBold)
	smallHeight := lineHeight(s.faces.Small)
	clockHeight := lineHeight(s.faces.Large)

	if markHeight == 0 || smallHeight == 0 || clockHeight == 0 {
		return 0
	}

	band := max(markHeight+rowLead+smallHeight, clockHeight) + 2*headerPadY

	total := band + ruleHeight + blockGap
	if !lay.fits(total) {
		return 0
	}

	return total
}

// drawWordmark sets USCOPE, then the ingest source beside it behind a dot.
//
// The dot is filled when the source is connected and hollow when it is not,
// which is one glance rather than a word to read. It is the low-altitude
// green rather than the accent, because the accent marks the selected
// aircraft and nothing else, and it is left in its own colour rather than
// moved onto the band's BandInk/Muted split: it is a status light, not text.
func (s *Scene) drawWordmark(lay *layout, top int, frame source.Frame, limit int) {
	// Both faces are known to be present: drawHeader drops the whole band
	// unless all three of its faces loaded, so there is nothing to check here.
	face := s.faces.Small
	markHeight := lineHeight(s.faces.BodyBold)

	pen := text.Draw(lay.dst, s.faces.BodyBold, lay.left, top, appTitle, s.pal.BandInk,
		text.WithSpacing(headerTracking))

	pen += sourceGap
	middle := top + markHeight/2

	if frame.Source.Connected {
		lay.dst.FillCircle(pen+dotRadius, middle, dotRadius, s.pal.AltLow)
	} else {
		lay.dst.Circle(pen+dotRadius, middle, dotRadius, s.pal.Muted)
	}

	pen += 2*dotRadius + dotGap
	label := top + (markHeight-face.Height())/2

	head, marker := fitLabel(face, frame.Source.Label, limit-pen)
	pen = text.Draw(lay.dst, face, pen, label, head, s.pal.BandInk, text.WithSpacing(labelTracking))
	text.Draw(lay.dst, face, pen, label, marker, s.pal.BandInk, text.WithSpacing(labelTracking))
}

// fitLabel cuts a label to the pixel width it has been given, and says what
// marker belongs after it.
//
// A source label is whatever the operator typed after --beast, so nothing here
// can know its length in advance. It used to be cut at twenty-four runes,
// which turned "BEAST 192.168.1.159:30005" into a label ending ":3000" on a
// band with two hundred pixels to spare, because a rune count cannot see how
// much room there is. Measuring can, and a label that fits is never cut.
//
// An empty marker means nothing was cut, so the caller draws the label and
// then draws nothing, which costs one call that measures zero.
func fitLabel(face *psf.Font, label string, width int) (string, string) {
	room := fitRunes(face, width, labelTracking)
	if room <= 0 {
		return "", ""
	}

	if utf8.RuneCountInString(label) <= room {
		return label, ""
	}

	return clip(label, room-1), cutMarker(face)
}

// cutMarker is the single glyph a cut label ends with: an ellipsis where the
// face has one, and a full stop where it does not.
func cutMarker(face *psf.Font) string {
	if _, has := face.Glyph(ellipsisRune); has {
		return cutEllipsis
	}

	return cutDot
}

// drawReceiverLine says where the scope is centred and how much that is
// worth.
//
// The three forms are deliberately different lengths and shapes so they
// cannot be confused at a glance: a fix reads as a mode word and two
// coordinates, an estimate as a radius, and nothing at all as two words.
//
// The mode takes the same colour the home marker's ring does, so the word in
// the header and the ring on the field are one signal read twice rather than
// two facts to reconcile. The coordinates after it stay in BandInk: they are
// the same two numbers whatever produced them, and colouring those as well
// would make the whole line shout.
//
// The face is known to be present for the same reason drawWordmark's is:
// drawHeader drops the band rather than half of it.
func (s *Scene) drawReceiverLine(lay *layout, top int, receiver source.Receiver) {
	face := s.faces.Small
	ink := s.fixColour(receiver.Mode, s.pal.BandInk)

	switch receiver.Label {
	case source.LabelEstimate:
		pen := text.Draw(lay.dst, face, lay.left, top, estimatePrefix, ink)
		pen = drawBytes(lay.dst, face, pen, top, s.whole(receiver.ConfidenceNm), ink)
		text.Draw(lay.dst, face, pen, top, rangeUnit, ink)
	case source.LabelNone:
		text.Draw(lay.dst, face, lay.left, top, noFixText, ink)
	default:
		pen := text.Draw(lay.dst, face, lay.left, top, modeWord(receiver.Mode), ink) + modeGap
		pen = drawBytes(lay.dst, face, pen, top, s.coordinate(receiver.Latitude, 'N', 'S'), s.pal.BandInk)
		pen = text.Draw(lay.dst, face, pen, top, coordinateSeparator, s.pal.BandInk)
		drawBytes(lay.dst, face, pen, top, s.coordinate(receiver.Longitude, 'E', 'W'), s.pal.BandInk)
	}
}

// modeWord opens the receiver line when there are coordinates after it.
//
// The estimate and the no-fix cases never reach here: their whole line is the
// mode, so they are written where they are decided rather than prefixed to a
// pair of numbers they do not have.
func modeWord(mode source.FixMode) string {
	switch mode {
	case source.FixManual:
		return manualText
	case source.FixGPS2D:
		return gps2DText
	case source.FixGPS3D:
		return gps3DText
	case source.FixGPSNoFix:
		return gpsNoFixText
	case source.FixNone, source.FixEstimated:
		fallthrough
	default:
		return unknownFixText
	}
}

// clip cuts a string to a rune count without allocating.
//
// Everything drawn here that came off the air goes through this: a callsign
// is eight characters by the standard, but the standard is not what arrives
// when a frame is half corrupt, and a long one would run over the column
// beside it.
func clip(value string, runes int) string {
	count := 0

	for offset := range value {
		if count == runes {
			return value[:offset]
		}

		count++
	}

	return value
}

// The two clocks on the right of the band.
const (
	// labelUTC and labelLocal sit before their own clock rather than above it,
	// which keeps the band one line of type tall. Aviation runs on UTC, so the
	// two are shown together rather than one being a mode the other hides in.
	labelUTC   = "UTC"
	labelLocal = "LCL"

	// utcSuffix is the Zulu marker. It is appended as a byte rather than
	// written into the layout string, because a bare Z in a time layout is
	// close enough to the Z0700 zone pattern to be worth not relying on.
	utcSuffix = 'Z'

	// clockLabelGap is the air between a clock's label and the clock, and
	// clockGap the air between the two clocks.
	clockLabelGap = 5
	clockGap      = 16

	// bandMutedAlpha is how far the band's quiet ink sits from the band itself
	// towards its reading colour. Just over half is the same step the page's
	// muted makes from its field towards its ink.
	bandMutedAlpha = 0.55
)

// The battery indicator.
const (
	// The glyph: a rounded-off cell with a nub on the right, sized so it reads
	// as a battery at arm's length on a five inch panel without competing with
	// the clock beside it.
	batteryWidth  = 22
	batteryHeight = 11
	batteryNubW   = 2
	batteryNubH   = 5

	// batteryInset is the wall between the outline and the fill.
	batteryInset = 2

	// The two levels the fill changes colour at. They match what a phone does,
	// because that is the reading everyone already has.
	batteryLow      = 20
	batteryCritical = 10

	batteryFull = 100

	// batteryGap is the air between the glyph and its percentage, and
	// batteryField the widest percentage the field has to hold. The field is
	// fixed so the glyph does not walk left as the battery drains.
	batteryGap   = 8
	batteryField = "100%"

	percentSign = "%"

	// boltInset is how far the charging bolt sits inside the glyph, and
	// boltWaist where its middle stroke turns. Three straight lines is as much
	// lightning as eleven pixels of height can carry.
	boltInset = 2
	boltWaist = 4
)

// bandMuted is the header band's quiet ink.
//
// The palette has no fourth band colour and does not need one. Muted on the
// page is a grey picked against the field, and on paper's navy strip that grey
// has less contrast than the band's own ink does, so a label set in it would
// read as damage rather than as chrome. Mixing the band's ink back towards the
// band gives the same "present but not read" step on either theme without a
// colour to keep in step by hand.
func (s *Scene) bandMuted() color.RGBA {
	return color.RGBA{
		R: mix(s.pal.Band.R, s.pal.BandInk.R, bandMutedAlpha),
		G: mix(s.pal.Band.G, s.pal.BandInk.G, bandMutedAlpha),
		B: mix(s.pal.Band.B, s.pal.BandInk.B, bandMutedAlpha),
		A: opaque,
	}
}

// drawHeaderRight fills the right of the band: the battery, then the local
// clock, then UTC beside it.
//
// It is laid out right to left because the right edge is the only fixed point
// there. The battery's width depends on whether there is a battery at all, and
// the clocks are set in two different faces, so anything measured from the
// left would have to be measured twice.
// It returns the x the whole block starts at, which is where the source label
// on the other side of the band has to stop.
func (s *Scene) drawHeaderRight(lay *layout, middle int, now time.Time) int {
	pen := lay.right
	if left, drawn := s.drawBattery(lay, pen, middle); drawn {
		pen = left - clockGap
	}

	local := now.AppendFormat(s.clock[:0], clockFormat)
	pen = s.drawClock(lay, pen, middle, labelLocal, s.faces.Large, local) - clockGap

	utc := append(now.UTC().AppendFormat(s.clock[:0], clockFormat), utcSuffix)

	return s.drawClock(lay, pen, middle, labelUTC, s.faces.Body, utc)
}

// drawClock sets one labelled clock ending at rightX and returns the x its
// label starts at.
//
// The pair is measured and then drawn left to right rather than drawn right to
// left, so the label stays quiet and the clock stays in the band's reading
// colour without the two drifting apart as the minutes change width.
func (s *Scene) drawClock(
	lay *layout, rightX, middle int, label string, face *psf.Font, value []byte,
) int {
	small := s.faces.Small

	labelWidth, labelHeight := text.Measure(small, label, text.WithSpacing(labelTracking))
	valueWidth := measureBytes(face, value)
	left := rightX - labelWidth - clockLabelGap - valueWidth

	text.Draw(lay.dst, small, left, middle-labelHeight/2, label, s.bandMuted(), text.WithSpacing(labelTracking))
	drawBytes(lay.dst, face, left+labelWidth+clockLabelGap, middle-lineHeight(face)/2, value, s.pal.BandInk)

	return left
}

// drawBattery draws the battery glyph and its percentage ending at rightX, and
// reports the x the indicator starts at and whether there was one to draw.
//
// With no reader wired up, or a percentage below zero, nothing is drawn: a
// machine with no battery should not carry an empty box where one would be,
// and a reading that has not arrived yet is not a flat battery. The caller
// needs to know which happened, because the clock beside it only wants a gap
// when there is something on the other side of it.
func (s *Scene) drawBattery(lay *layout, rightX, middle int) (int, bool) {
	if s.battery == nil {
		return rightX, false
	}

	percent := s.battery.GetPercentage()
	if percent < 0 {
		return rightX, false
	}

	face := s.faces.Small
	fieldWidth, fieldHeight := text.Measure(face, batteryField)

	left := rightX - fieldWidth - batteryGap - batteryWidth - batteryNubW
	top := middle - batteryHeight/2

	s.drawBatteryShell(lay.dst, left, top)

	inner := image.Rect(left+batteryInset, top+batteryInset,
		left+batteryWidth-batteryInset, top+batteryHeight-batteryInset)

	if s.battery.IsCharging() {
		s.drawChargingBolt(lay.dst, inner)
	} else {
		s.drawBatteryLevel(lay.dst, inner, int(percent))
	}

	pen := drawBytes(lay.dst, face, rightX-fieldWidth, middle-fieldHeight/2, s.count(int(percent)), s.pal.BandInk)
	text.Draw(lay.dst, face, pen, middle-fieldHeight/2, percentSign, s.pal.BandInk)

	return left, true
}

// drawBatteryShell draws the cell and its nub, which is the part that looks
// the same whatever the battery is doing.
func (s *Scene) drawBatteryShell(dst *canvas.Canvas, left, top int) {
	outline := s.bandMuted()

	dst.Rect(image.Rect(left, top, left+batteryWidth, top+batteryHeight), outline)
	dst.FillRect(image.Rect(
		left+batteryWidth, top+(batteryHeight-batteryNubH)/2,
		left+batteryWidth+batteryNubW, top+(batteryHeight+batteryNubH)/2), outline)
}

// drawBatteryLevel fills the cell in proportion to the charge left.
//
// A charging battery gets the bolt instead of this. The level is what the wall
// socket is already fixing, and a bar that has to be watched to see which way
// it is going says less at a glance than a mark that means "on power".
func (s *Scene) drawBatteryLevel(dst *canvas.Canvas, inner image.Rectangle, percent int) {
	charge := min(max(percent, 0), batteryFull)

	dst.FillRect(image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+inner.Dx()*charge/batteryFull, inner.Max.Y),
		s.batteryInk(charge))
}

// batteryInk is the fill colour for a level.
//
// The altitude bands are reused rather than given colours of their own. They
// are already the palette's "getting worse" ramp, and a fourth amber that only
// the battery used would be one more colour to keep in step across two themes.
func (s *Scene) batteryInk(percent int) color.RGBA {
	switch {
	case percent <= batteryCritical:
		return s.pal.AltHigh
	case percent <= batteryLow:
		return s.pal.AltMid
	default:
		return s.pal.BandInk
	}
}

// drawChargingBolt draws the lightning mark inside a charging battery.
func (s *Scene) drawChargingBolt(dst *canvas.Canvas, inner image.Rectangle) {
	ink := s.pal.AltLow
	topX := inner.Min.X + inner.Dx()/2 + boltInset
	waistX := inner.Min.X + inner.Dx()/2 - boltInset
	waistY := inner.Min.Y + boltWaist

	dst.Line(topX, inner.Min.Y, waistX, waistY, ink)
	dst.Line(waistX, waistY, topX, waistY, ink)
	dst.Line(topX, waistY, waistX, inner.Max.Y-1, ink)
}
