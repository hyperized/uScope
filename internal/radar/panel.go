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
	// appTitle is the wordmark on the left of the header band.
	//
	// It is the project's own spelling, lower-case u and capital S, rather than
	// the all-caps every other label in the scene is set in. A wordmark is a
	// name and not a label: setting it USCOPE made the band disagree with the
	// binary, the repository and the README about what the thing is called.
	// Terminus carries the lower case at every size, which was checked before
	// this was written rather than assumed.
	appTitle    = "uScope"
	clockFormat = "15:04"

	// sourceGap is the air between the wordmark and the source dot, dotRadius
	// the dot itself, and dotGap the air before the source label.
	sourceGap = 14
	dotRadius = 3
	dotGap    = 6

	// locPrefix opens the receiver line, whatever the line goes on to say.
	//
	// Without it the line was a mode word and two numbers with nothing to say
	// what they were of, and next to the aircraft position on the card they
	// read as another aeroplane. LOC is what a chart calls a location, and it
	// is drawn in the band's own ink rather than in the fix colour: the word
	// is a label and does not change, so colouring it would be a signal that
	// never says anything. It carries its own trailing space, the way
	// autoPrefix and estimatePrefix carry theirs.
	locPrefix = "LOC "

	// estimatePrefix opens the receiver line when the position was worked out
	// from the aircraft rather than sensed. The plus-minus says out loud that
	// this is a guess with a radius, not a fix.
	estimatePrefix = "EST ±"

	// noFixText is the receiver line when nothing is known. It is set in caps
	// like every other label in the scene rather than in the sentence case
	// uAirwaves uses, because next to EST and MANUAL a lower-case line reads
	// as a different kind of thing. The wordmark is the one exception, and it
	// is a name rather than a label; see appTitle.
	noFixText = "NO FIX"

	// The mode words the receiver line opens with when there are coordinates
	// to follow. They say what the two numbers after them are worth, which the
	// numbers themselves cannot.
	manualText     = "MANUAL"
	gps2DText      = "GPS 2D"
	gps3DText      = "GPS 3D"
	gpsNoFixText   = "GPS LOST"
	unknownFixText = "???"

	// doubtMarker follows the estimate's radius when the self-locator's own
	// observations disagree with the answer it produced. A question mark says
	// that in one glyph, in a line that has no room for a sentence.
	doubtMarker = " ?"

	// modeGap is the air between the mode word and the coordinates.
	modeGap = 8

	// coordinateSeparator sits between the two halves of a position.
	coordinateSeparator = " / "

	// sweepSuffix follows the source label while the gain auto-sweep is
	// running. It is drawn in the caution colour because that is what it is:
	// the sweep walks the gain grid for a few seconds and decodes nothing while
	// it does, so the scope beside it is empty for a reason that has not gone
	// wrong. A receiver that looks broken with nothing on screen to say why is
	// the bug report this line prevents.
	sweepSuffix = " SWEEP"

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
	capFilter
	capTheme
	capOrbit
	capEnvelope
	capBiasTee
	capWide
)

// The cycling keys are labelled with the value they are on rather than with
// the name of the setting. A cap reading COLOUR says there is a colour mode
// without saying which one, which is the question it was being asked. The
// trail cap is the third of them and gets its four words from trailMode.label,
// and the filter cap the fourth, from filterState.label.
const (
	labelAltitude = "ALT"
	labelAirline  = "AIRLINE"
	labelNight    = "NIGHT"
	labelDay      = "DAY"

	// trailBarLabel is what the trail cap falls back to. It is never drawn:
	// capLabel answers capTrails from the mode itself, and every mode has a
	// word. It is here so the entry in keyCaps is not the one row of the
	// table with an empty label in it.
	trailBarLabel = "TRAILS"
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
// Twelve softkeys come to 728 pixels of the 1248 the panel leaves between its
// margins, with every cycling key on its longest word, so nothing here has to
// be shortened to fit. See drawSoftkey for how one box is measured and
// view3DCaps for the widest the bar ever gets.
//
// F sits next to C rather than at the end of the row, because the filter is
// the colour mode's own legend with one entry picked out of it and the two
// keys are reached for together.
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
	{key: "T", label: trailBarLabel, state: capTrails},
	{key: "A", label: "AIRPORTS", state: capAirports},
	{key: "M", label: "SHORE", state: capShore},
	{key: "C", label: "COLOUR", state: capColour},
	{key: "F", label: filterBarLabel, state: capFilter},
	{key: "L", label: "THEME", state: capTheme},
	{key: "V", label: "VIEW"},
	{key: "W", label: "WIDE", state: capWide},
}

// view3DCaps are the caps the 3D view adds to the end of the bar.
//
// They are appended rather than living in keyCaps because none of these keys
// does anything in the two flat views, and a cap for a key with no effect is
// furniture pretending to be a control. The bare 3D view draws no bar at all,
// so the question does not arise there.
//
// All four of the view's keys are listed, not just the two toggles. The camera
// keys were bound and undocumented on screen, so the only way to find out the
// arrows turned the camera and the brackets tilted it was to read the README,
// which is not where anyone looks while holding the device.
//
// The arrows are the glyphs rather than shapes drawn by hand, matching the
// SELECT cap two rows up, and all four embedded faces carry U+2190 and U+2192.
// That was checked before this was written rather than assumed, the same way
// the up and down pair was.
//
// Sixteen softkeys come to about 1041 pixels of the 1248 the panel leaves
// between its margins, or about 1103 with the bias-tee key as well, so nothing
// here has to be shortened: ENVELOPE and AIRPORTS both fit at their full
// length with about 145 pixels to spare. That is the whole bar at its longest,
// in the 3D view on a dongle that has a bias-tee. See the note on keyCaps.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var view3DCaps = [...]keyCap{
	{key: "O", label: "ORBIT", state: capOrbit},
	{key: "E", label: "ENVELOPE", state: capEnvelope},
	{key: "←→", label: "TURN"},
	{key: "[ ]", label: "TILT"},
}

// biasCap is the cap the bar adds when the source has a bias-tee to flip.
//
// It is drawn conditionally for the same reason view3DCaps are: a cap for a
// control that does nothing is furniture pretending to be a switch. Under
// --demo, --beast and --replay-iq there is no dongle in the loop, so the bar
// never mentions one. It is a one-element array rather than a bare keyCap so
// drawCaps can take it as a slice without the caller allocating per frame.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var biasCap = [...]keyCap{
	{key: "B", label: "BIAS-T", state: capBiasTee},
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

	// The bias-tee cap goes before the camera keys rather than after them, so
	// it keeps its place in the bar when v switches to the 3D view and the
	// pair on the end appears. A control that moved every time the view
	// changed would be one the eye has to hunt for.
	if s.biasSupported && pen < lay.right {
		pen = s.drawCaps(lay, pen, top, biasCap[:])
	}

	if s.perspective() && pen < lay.right {
		s.drawCaps(lay, pen, top, view3DCaps[:])
	}

	lay.bottom -= reserved
}

// drawCaps draws a run of softkeys from pen and returns the x just past the
// last one it drew.
//
// It stops after the key that reaches the right margin rather than before it,
// which is what the bar has always done: one box running into the margin says
// there was more, where stopping short of it just looks like the bar ended.
func (s *Scene) drawCaps(lay *layout, pen, top int, caps []keyCap) int {
	for _, entry := range caps {
		pen = s.drawSoftkey(lay.dst, pen, top, entry) + keyGap

		if pen >= lay.right {
			break
		}
	}

	return pen
}

// keyBarHeight is the room the softkey bar takes off the bottom, blockGap
// included, or zero when one of its two faces is missing or there is no room
// left for it.
//
// It is separate from drawKeyBar because the background layer has to carve the
// frame the same way without drawing a bar of its own: the layer holds the
// scope, and the scope only sits where it does because the bar took its room
// off the bottom first.
//
// The box is sized on the cap letter's face rather than on the label's. The
// letter is the taller of the two and it is what the box has to hold; the
// label is centred against it.
func (s *Scene) keyBarHeight(lay *layout) int {
	capHeight := lineHeight(s.faces.BodyBold)
	if capHeight == 0 || lineHeight(s.faces.Small) == 0 {
		return 0
	}

	height := capHeight + 2*keyPadY
	if !lay.fits(height) {
		return 0
	}

	return height + blockGap
}

// drawSoftkey draws one key as a softkey and returns the x just past its box.
//
// A softkey is the whole control in one grey box: the cap letter in the bold
// face, the word for what the key does in the small one beside it, and a green
// bar along the bottom when the setting is engaged. That is the grammar of the
// row along the bottom of a glass panel, and it answers the question the old
// bar could not. A row of filled and hollow caps said a setting was on by the
// weight of one letter, which is a difference nobody sees without comparing
// two caps side by side; a bar under a box is a switch that is visibly thrown.
//
// The box is sized from the type rather than set to a fixed width. The caps
// are one, two and three characters long and the labels four to eight, so a
// grid of equal boxes would be sized for TILT and ENVELOPE at once and waste
// most of the bar on the short ones.
func (s *Scene) drawSoftkey(dst *canvas.Canvas, left, top int, entry keyCap) int {
	bold, small := s.faces.BodyBold, s.faces.Small
	word := s.capLabel(entry)

	capWidth, capHeight := text.Measure(bold, entry.key)
	labelWidth, labelHeight := text.Measure(small, word, text.WithSpacing(labelTracking))

	box := image.Rect(left, top,
		left+2*keyPadX+capWidth+keyCapGap+labelWidth, top+2*keyPadY+capHeight)

	dst.FillRect(box, s.pal.Key)

	pen := text.Draw(dst, bold, left+keyPadX, top+keyPadY, entry.key, s.pal.Ink)

	// The label rides the middle of the cap letter's line rather than its
	// baseline. The two faces are four pixels apart in height, and a small
	// label set on the bold one's baseline sits low enough in the box to read
	// as a second row.
	text.Draw(dst, small, pen+keyCapGap, top+keyPadY+(capHeight-labelHeight)/2, word, s.pal.Ink,
		text.WithSpacing(labelTracking))

	s.drawEngaged(dst, box, entry.state)

	return box.Max.X
}

// drawEngaged puts the green bar along the inside of a softkey's bottom edge
// when its setting is on, and nothing at all when it is off.
func (s *Scene) drawEngaged(dst *canvas.Canvas, box image.Rectangle, which capToggle) {
	if !s.capEngaged(which) {
		return
	}

	bar := box.Max.Y - keyPadY

	dst.FillRect(image.Rect(
		box.Min.X+keyEngagedInset, bar,
		box.Max.X-keyEngagedInset, bar+keyEngagedHeight), s.pal.OK)
}

// capEngaged reports whether a softkey shows its engaged bar.
//
// Only the genuine on-or-off settings do. A key that carries a value rather
// than a state has nothing to be engaged about: C says which colour mode is on,
// L which theme, T how much trail and F which slice of the traffic, and every
// one of those is always on something. A bar under them would be claiming an
// off position they do not have, and the cap already says which value it is on.
// The keys with no setting behind them at all, q and the three that step
// through something, are the same case one step further out.
//
// It is the bar and not the box that says so. Every key is drawn in the same
// grey whatever its state, because the box is the switch and the bar is the
// light on it.
func (s *Scene) capEngaged(which capToggle) bool {
	switch which {
	case capAuto:
		return s.autoRange
	case capAirports:
		return s.airports
	case capShore:
		return s.shoreOn
	case capOrbit:
		return s.orbiting
	case capEnvelope:
		return s.envelope
	case capBiasTee:
		return s.biasEnabled
	case capWide:
		return s.wide
	case capAlways, capColour, capTheme, capTrails, capFilter:
		fallthrough
	default:
		return false
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
			return labelDay
		}

		return labelNight
	case capTrails:
		return s.trail.label()
	case capFilter:
		return s.filter.label()
	case capAlways, capAuto, capAirports, capShore, capOrbit, capEnvelope, capBiasTee, capWide:
		fallthrough
	default:
		return entry.label
	}
}

// drawHeader draws the band across the top: the wordmark and the source on
// the left with the receiver position under them, the two clocks and the
// battery on the right, a hairline under all of it.
//
// The band is filled with pal.Band and the type on it is split three ways
// rather than by the scene's usual muted and ink pair. The wordmark and the
// fixed words are BandInk, because the band is a near-black strip on both
// themes and needs its own contrast rather than the page's. Everything that is
// a reading about the machine, the source label, the coordinates, both clocks
// and the battery percentage, is Data: on a glass panel cyan is what a data
// field is set in, and these are the only readings on screen that are about the
// receiver rather than about an aeroplane. The mode word is whichever of OK,
// Caution and Muted says what the position under it is worth.
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

	// The type is centred in what the band covers rather than in the band the
	// layout reserved. The two differ by the whole margin, because the fill
	// bleeds up to the canvas edge and the reserved band starts below the
	// margin, and the type used to be placed against the second: twenty-two
	// pixels of air over the wordmark against six under the receiver line, on
	// a band that reads as a masthead and so has to look level.
	//
	// Centring the two line boxes centres the ink in them. Terminus leaves two
	// blank rows over a capital and two under a baseline with no descender, in
	// the bold face and the small one alike, so the ink sits the same distance
	// inside the stack at both ends. TestHeaderTypeIsCentredInTheBand measures
	// the ink rather than the boxes, which is what would catch a face whose
	// metrics are not so even.
	stack := markHeight + rowLead + lineHeight(s.faces.Small)
	top := bounds.Min.Y + (rule-bounds.Min.Y-stack)/2

	// The right of the band is drawn first because it is the only thing that
	// can say where the left of it has to stop. Its width depends on whether
	// there is a battery and on how wide the two clocks set, so measuring it
	// any other way would mean measuring it twice.
	//
	// It is centred on the same middle the type is, which is the middle of
	// what the fill covers.
	edge := s.drawHeaderRight(lay, (bounds.Min.Y+rule)/2, frame.Now)

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

// drawWordmark sets the wordmark, then the ingest source beside it behind a
// dot, and SWEEP after that while the gain sweep is running.
//
// The dot is filled when the source is connected and hollow when it is not,
// which is one glance rather than a word to read. It is OK green, because a
// connected feed is the engaged state of the one thing the whole scope depends
// on, and it keeps its own colour rather than joining the band's BandInk and
// Data split: it is a status light, not text.
//
// The label itself is Data. It says which feed the aircraft came off, which is
// a reading about the machine rather than about any aeroplane on the field.
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
		lay.dst.FillCircle(pen+dotRadius, middle, dotRadius, s.pal.OK)
	} else {
		lay.dst.Circle(pen+dotRadius, middle, dotRadius, s.pal.Muted)
	}

	pen += 2*dotRadius + dotGap
	label := top + (markHeight-face.Height())/2

	// The sweep marker takes its room out of the label's budget before the
	// label is cut rather than being appended after it. Appended, a long
	// --beast address would push SWEEP across the clocks on the other side of
	// the band, which is the one thing sourceLabelGap exists to prevent.
	reserved := 0
	if frame.Sweeping {
		reserved, _ = text.Measure(face, sweepSuffix, text.WithSpacing(labelTracking))
	}

	head, marker := fitLabel(face, frame.Source.Label, limit-pen-reserved)
	pen = text.Draw(lay.dst, face, pen, label, head, s.pal.Data, text.WithSpacing(labelTracking))
	pen = text.Draw(lay.dst, face, pen, label, marker, s.pal.Data, text.WithSpacing(labelTracking))

	if frame.Sweeping {
		text.Draw(lay.dst, face, pen, label, sweepSuffix, s.pal.Caution, text.WithSpacing(labelTracking))
	}
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
// Every form opens with LOC and then differs: a fix reads as a mode word and
// two coordinates, an estimate as a radius, and nothing at all as two words.
// The three are deliberately different lengths and shapes so they cannot be
// confused at a glance.
//
// A GPS that has lost its lock is still the first form: the coordinates are
// real and were sensed, so they are drawn like any other fix, and GPS LOST is
// what says they are a memory rather than a reading.
//
// The mode takes the same colour the home marker's ring does, so the word in
// the header and the ring on the field are one signal read twice rather than
// two facts to reconcile. LOC stays in BandInk because it is a label that never
// changes, and the coordinates after it are Data like every other reading about
// the machine: they are the same two numbers whatever produced them, and it is
// the mode word in front of them that says what they are worth.
//
// The face is known to be present for the same reason drawWordmark's is:
// drawHeader drops the band rather than half of it.
func (s *Scene) drawReceiverLine(lay *layout, top int, receiver source.Receiver) {
	face := s.faces.Small
	ink := s.fixColour(receiver.Mode, s.pal.BandInk)
	left := text.Draw(lay.dst, face, lay.left, top, locPrefix, s.pal.BandInk)

	switch receiver.Label {
	case source.LabelEstimate:
		pen := text.Draw(lay.dst, face, left, top, estimatePrefix, ink)
		pen = drawBytes(lay.dst, face, pen, top, s.whole(receiver.ConfidenceNm), ink)
		pen = text.Draw(lay.dst, face, pen, top, rangeUnit, ink)
		s.drawDoubt(lay.dst, face, pen, top, receiver.Violated)
	case source.LabelNone:
		text.Draw(lay.dst, face, left, top, noFixText, ink)
	default:
		pen := text.Draw(lay.dst, face, left, top, modeWord(receiver.Mode), ink) + modeGap
		pen = drawBytes(lay.dst, face, pen, top, s.coordinate(receiver.Latitude, 'N', 'S'), s.pal.Data)
		pen = text.Draw(lay.dst, face, pen, top, coordinateSeparator, s.pal.Data)
		drawBytes(lay.dst, face, pen, top, s.coordinate(receiver.Longitude, 'E', 'W'), s.pal.Data)
	}
}

// drawDoubt marks an estimate the self-locator does not fully believe.
//
// Violated counts the aircraft whose radio horizon does not reach the position
// that was worked out from it, which means the circles are mutually
// inconsistent and the radius has already been widened to cover the
// disagreement. The wider radius alone reads as "loose but sound"; the marker
// is what says the answer is a compromise between constraints that cannot all
// be true.
//
// It is drawn in the caution colour, which is what the whole estimate line
// already carries, so it reads as part of the same doubt rather than as a
// second thing to decode.
func (s *Scene) drawDoubt(dst *canvas.Canvas, face *psf.Font, pen, top, violated int) {
	if violated == 0 {
		return
	}

	text.Draw(dst, face, pen, top, doubtMarker, s.pal.Caution)
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
// left, so the label stays quiet and the clock stays in the data colour without
// the two drifting apart as the minutes change width. The clock is a reading
// about the machine, which is what puts it in Data beside the source label and
// the coordinates rather than in the band's own ink.
func (s *Scene) drawClock(
	lay *layout, rightX, middle int, label string, face *psf.Font, value []byte,
) int {
	small := s.faces.Small

	labelWidth, labelHeight := text.Measure(small, label, text.WithSpacing(labelTracking))
	valueWidth := measureBytes(face, value)
	left := rightX - labelWidth - clockLabelGap - valueWidth

	text.Draw(lay.dst, small, left, middle-labelHeight/2, label, s.bandMuted(), text.WithSpacing(labelTracking))
	drawBytes(lay.dst, face, left+labelWidth+clockLabelGap, middle-lineHeight(face)/2, value, s.pal.Data)

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

	pen := drawBytes(lay.dst, face, rightX-fieldWidth, middle-fieldHeight/2, s.count(int(percent)), s.pal.Data)
	text.Draw(lay.dst, face, pen, middle-fieldHeight/2, percentSign, s.pal.Data)

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
// It is the palette's own caution and warning rather than colours of the
// battery's own, which is the whole point of naming those two: a flat battery
// on a handheld is a warning in the same sense an emergency squawk is, and one
// getting low is a caution in the same sense an estimated position is. Above
// both it is Data, matching the percentage beside it, so a healthy battery is
// one colour and reads as a reading rather than as a state.
func (s *Scene) batteryInk(percent int) color.RGBA {
	switch {
	case percent <= batteryCritical:
		return s.pal.Warn
	case percent <= batteryLow:
		return s.pal.Caution
	default:
		return s.pal.Data
	}
}

// drawChargingBolt draws the lightning mark inside a charging battery.
func (s *Scene) drawChargingBolt(dst *canvas.Canvas, inner image.Rectangle) {
	ink := s.pal.OK
	topX := inner.Min.X + inner.Dx()/2 + boltInset
	waistX := inner.Min.X + inner.Dx()/2 - boltInset
	waistY := inner.Min.Y + boltWaist

	dst.Line(topX, inner.Min.Y, waistX, waistY, ink)
	dst.Line(waistX, waistY, topX, waistY, ink)
	dst.Line(topX, waistY, waistX, inner.Max.Y-1, ink)
}
