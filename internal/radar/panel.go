package radar

import (
	"image"
	"image/color"
	"time"

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

	// maxSourceLabel is how much of the source label is drawn. A BEAST
	// address is operator-supplied and could be any length; the stats line is
	// not the place to find that out.
	maxSourceLabel = 24
)

// keyCap is one entry in the bottom bar: the cap, then what the key does.
type keyCap struct {
	key   string
	label string
}

// keyCaps is the key legend, in the order the keys are worth reaching for:
// leaving, then moving through the list, then the four things that change what
// is on the field, then the two that change how it looks.
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
	{key: "R", label: "AUTO"},
	{key: "T", label: "TRAILS"},
	{key: "A", label: "AIRPORTS"},
	{key: "C", label: "COLOUR"},
	{key: "L", label: "THEME"},
	{key: "V", label: "VIEW"},
}

// drawKeyBar puts the key legend on the bottom edge and takes the room it
// used out of the layout, so everything above flows into what is left.
//
// It runs first for that reason: the bar belongs to the bottom of the screen
// whatever else is on it, and a bar pushed down by a block above would end up
// somewhere in the middle.
func (s *Scene) drawKeyBar(lay *layout) {
	face := s.faces.Small

	capHeight := lineHeight(face)
	if capHeight == 0 {
		return
	}

	height := capHeight + 2*capPadY
	if !lay.fits(height) {
		return
	}

	top := lay.bottom - height
	pen := lay.left

	for _, entry := range keyCaps {
		pen = s.drawCap(lay.dst, pen, top, entry.key)
		pen += capGap
		pen = text.Draw(lay.dst, face, pen, top+capPadY, entry.label, s.pal.Muted)
		pen += entryGap

		if pen >= lay.right {
			break
		}
	}

	lay.bottom -= height + blockGap
}

// drawCap draws one key cap, the letter knocked out of a filled box, and
// returns the x just past it.
func (s *Scene) drawCap(dst *canvas.Canvas, left, top int, key string) int {
	width, height := text.Measure(s.faces.Small, key)

	box := image.Rect(left, top, left+width+2*capPadX, top+height+2*capPadY)
	dst.FillRect(box, s.pal.Ink)
	text.Draw(dst, s.faces.Small, left+capPadX, top+capPadY, key, s.pal.Field)

	return box.Max.X
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
func (s *Scene) drawHeader(lay *layout, frame source.Frame) {
	markHeight := lineHeight(s.faces.BodyBold)
	smallHeight := lineHeight(s.faces.Small)
	clockHeight := lineHeight(s.faces.Large)

	if markHeight == 0 || smallHeight == 0 || clockHeight == 0 {
		return
	}

	band := max(markHeight+rowLead+smallHeight, clockHeight) + 2*headerPadY

	total := band + ruleHeight + blockGap
	if !lay.fits(total) {
		return
	}

	lay.dst.FillRect(image.Rect(lay.left, lay.top, lay.right, lay.top+band), s.pal.Band)

	top := lay.top + headerPadY

	s.drawWordmark(lay, top, frame)
	s.drawReceiverLine(lay, top+markHeight+rowLead, frame.Receiver)

	s.drawHeaderRight(lay, lay.top+band/2, frame.Now)

	rule := lay.top + band
	lay.dst.FillRect(image.Rect(lay.left, rule, lay.right, rule+ruleHeight), s.pal.Rule)

	lay.top += total
}

// drawWordmark sets USCOPE, then the ingest source beside it behind a dot.
//
// The dot is filled when the source is connected and hollow when it is not,
// which is one glance rather than a word to read. It is the low-altitude
// green rather than the accent, because the accent marks the selected
// aircraft and nothing else, and it is left in its own colour rather than
// moved onto the band's BandInk/Muted split: it is a status light, not text.
func (s *Scene) drawWordmark(lay *layout, top int, frame source.Frame) {
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
	text.Draw(lay.dst, face, pen, top+(markHeight-face.Height())/2,
		clip(frame.Source.Label, maxSourceLabel), s.pal.BandInk, text.WithSpacing(labelTracking))
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
func (s *Scene) drawHeaderRight(lay *layout, middle int, now time.Time) {
	pen := lay.right
	if left, drawn := s.drawBattery(lay, pen, middle); drawn {
		pen = left - clockGap
	}

	local := now.AppendFormat(s.clock[:0], clockFormat)
	pen = s.drawClock(lay, pen, middle, labelLocal, s.faces.Large, local) - clockGap

	utc := append(now.UTC().AppendFormat(s.clock[:0], clockFormat), utcSuffix)
	s.drawClock(lay, pen, middle, labelUTC, s.faces.Body, utc)
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
