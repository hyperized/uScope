package radar

import (
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/airlines"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// The window overlay: three pieces of type set straight on the bare 3D
// picture for someone watching from a distance, with no panel behind them. A clock
// with the corridor airfield under it at the top right, a caption naming the
// aircraft coming in or climbing out at the bottom left, and the hour's
// arrivals and departures at the bottom right. While the caption shows, the
// aircraft it names carries a ring, so a viewer can find it among the rest.
//
// It is drawn on every frame after the picture and touches nothing the
// picture is drawn from, which is why none of it is in layerKey: the bare 3D
// view has no layer to go stale in the first place.
const (
	// windowUnitHeight is how many canvas rows make one step of glyph scale.
	// The scene's own uiScale steps at every 1080 rows, which leaves 1080p
	// and 1440p at the same pixel sizes; the overlay is sized as a share of
	// the picture instead, and 360 divides 720, 1080, 1440 and 2160 alike, so
	// each of those gets a whole scale and the same proportions.
	windowUnitHeight = 360

	// windowMarginDivisor puts the type a twenty-fifth of the width, four
	// percent, in from every edge of the picture.
	windowMarginDivisor = 25

	// windowLineGap is the air between two lines of one piece, in glyph
	// pixels, so it grows with the type.
	windowLineGap = 2

	// windowColumnGap is the air between the two figures of the hourly count,
	// in glyph pixels.
	windowColumnGap = 16

	// windowCountGap is the least room left between the caption and the
	// count, in glyph pixels. A caption that would come closer is shortened
	// until it does not, so the count never leaves the frame.
	windowCountGap = 12

	// captionRingShare is the radius of the ring around the captioned
	// aircraft as a share of the model's drawn span: half the span would
	// touch the wingtips, and three quarters leaves them some air.
	captionRingShare = 0.75

	// windowTracking spaces the small capitals out by one glyph pixel.
	windowTracking = 1

	// arrivalReachKm and departureReachKm are how close to the field an
	// aircraft has to be for the caption to name it. An arrival is worth
	// watching from further out, since it is coming towards the field.
	arrivalReachKm   = 25.0
	departureReachKm = 15.0

	// captionHold is the shortest time the caption stays on one aircraft, so
	// it is on screen long enough to read before it moves to another.
	captionHold = 8 * time.Second

	// captionFade is how long the caption takes to come up or go away.
	captionFade = time.Second

	// movementWindow is how far back the hourly count looks: a rolling hour,
	// so the figure does not drop to zero on the hour.
	movementWindow = time.Hour

	// kmPerNm and metresPerFoot turn the scene's nautical units into the
	// metric ones a general audience reads.
	kmPerNm       = 1.852
	metresPerFoot = 0.3048

	// metresStep rounds a height to tens of metres. A pressure altitude is
	// reported in 25 ft steps, so the last metre would be a figure nobody
	// measured.
	metresStep = 10

	// tenthsBelowKm is the distance under which the caption gives tenths of
	// a kilometre, because a final approach covers the last ten in a few
	// minutes and a whole number would sit still for most of it.
	tenthsBelowKm = 10

	// callsignPrefix is how many letters of a callsign name the operator.
	callsignPrefix = 3
)

// The overlay's words. The caption's units follow the owner's ruling that the
// audience is the general public: metres up and kilometres out.
const (
	captionInText    = "COMING IN NOW"
	captionOutText   = "CLIMBING OUT"
	landedLabel      = "LANDED THIS HOUR"
	tookOffLabel     = "TOOK OFF"
	captionSeparator = " · "
	captionUp        = " m up"
	captionOut       = " km out"
	captionSpace     = " "
)

// caption is the aircraft the bottom-left caption is on and what it last
// said about it.
//
// The figures are kept rather than read again each frame because an aircraft
// that drops off the frame while the caption is fading out has to go on being
// described for the rest of the fade. Every string here points into the
// airline table or into a snapshot's callsign, so holding one copies nothing.
type caption struct {
	icao     string
	class    source.TrackClass
	name     string
	number   string
	callsign string
	metres   float64
	km       float64

	// latitude, longitude and altitudeFt are where the aircraft was last
	// seen, which is where its ring stays while the caption fades out after
	// the aircraft has left the frame.
	latitude, longitude, altitudeFt float64

	// shownAt is the render clock the caption came up at, and leaveAt the
	// one it started going away at, which only means anything while leaving.
	shownAt time.Duration
	leaveAt time.Duration
	leaving bool
}

// windowShown reports whether the overlay is on screen: the bare 3D view with
// --minimal-window on.
func (s *Scene) windowShown() bool { return s.shown == ViewMinimal3D && s.overlay }

// windowLayout is where the overlay's pieces go on this frame.
type windowLayout struct {
	box    image.Rectangle
	scale  int
	shadow int
}

// newWindowLayout fits the overlay into the scope box, which is the picture
// less the bar and the board when either is on.
func newWindowLayout(lay *layout) windowLayout {
	bounds := lay.dst.Bounds()
	scale := max(1, bounds.Dy()/windowUnitHeight)
	margin := bounds.Dx() / windowMarginDivisor

	return windowLayout{
		box:    lay.scope.Inset(margin),
		scale:  scale,
		shadow: (scale + 1) / 2,
	}
}

// drawWindow draws the overlay over the picture, or nothing while it is off.
func (s *Scene) drawWindow(lay *layout, frame source.Frame, elapsed time.Duration) {
	if !s.windowShown() {
		return
	}

	win := newWindowLayout(lay)
	s.drawWindowClock(lay, &win, frame)

	if !frame.Corridors.Placed() {
		s.caption = caption{}

		return
	}

	s.followCaption(frame, elapsed)
	s.drawCaptionRing(elapsed)

	room := s.measureMovements(&win, frame) - win.scale*windowCountGap
	s.drawCaption(lay, &win, elapsed, room)
	s.drawMovements(lay, &win, frame)
}

// captionView is what the 3D view drew the frame's aircraft through, kept by
// draw3D so the overlay, drawn after it, can put a ring on the one the
// caption is about. ok is false on a frame with no aircraft drawn.
type captionView struct {
	view scene3
	dst  *canvas.Canvas
	ok   bool
}

// drawCaptionRing rings the aircraft the caption is on, fading with the
// caption.
//
// The ring is a circle on the screen around the model, not one on the
// ground, so it stays round at any tilt and sits on the aircraft rather than
// under it. It is drawn through the 3D view's own clipped window, so an
// aircraft at the edge of the picture gets part of a ring.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawCaptionRing(elapsed time.Duration) {
	held := &s.caption
	shot := &s.captionView

	if held.icao == "" || !shot.ok {
		return
	}

	x, y, ok := shot.view.project(held.latitude, held.longitude, held.altitudeFt)
	if !ok {
		return
	}

	radius := int(shot.view.cam.modelSpan * captionRingShare)
	shot.dst.BlendStrokeCircle(x, y, radius, max(shot.view.weight, 1), s.pal.Accent, held.alpha(elapsed))
}

// drawWindowClock sets the local time at the top right, and under it the
// corridor airfield the caption and the count are about.
func (s *Scene) drawWindowClock(lay *layout, win *windowLayout, frame source.Frame) {
	clock := frame.Now.AppendFormat(s.clock[:0], clockFormat)
	pen := s.windowPen(lay, win, s.faces.Large, 0)
	width := pen.measureBytes(clock)
	pen.x, pen.y = win.box.Max.X-width, win.box.Min.Y
	pen.drawBytes(clock, s.pal.Ink, 1)

	field := frame.Corridors.Field.ICAO
	if field == "" {
		return
	}

	under := pen.y + pen.height() + win.scale*windowLineGap
	small := s.windowPen(lay, win, s.faces.Small, windowTracking)
	small.x, small.y = win.box.Max.X-small.measure(field), under
	small.draw(field, s.pal.Ink, 1)
}

// followCaption decides which aircraft the caption is on for this frame.
//
// The nearest arrival within arrivalReachKm wins, then the nearest departure
// within departureReachKm. Once on an aircraft the caption keeps it for at
// least captionHold, then goes when the aircraft stops qualifying or when a
// departure is held and an arrival has turned up. It always fades out before
// it changes aircraft, so two names never cross on screen. An aircraft that
// leaves the frame takes the caption with it at once, still fading.
func (s *Scene) followCaption(frame source.Frame, elapsed time.Duration) {
	held := &s.caption
	field := frame.Corridors

	// A render clock that ran backwards, which a scene coming back on
	// screen can see, would hold the caption invisible until it caught up.
	if held.icao != "" && (elapsed < held.shownAt || (held.leaving && elapsed < held.leaveAt)) {
		held.shownAt, held.leaving = elapsed-captionFade, false
	}

	best := s.captionCandidate(frame)

	if held.icao != "" {
		s.keepCaption(frame, field, elapsed, best)
	}

	if held.icao == "" && best.found {
		*held = caption{icao: best.plane.ICAO, class: best.class, shownAt: elapsed}
		held.describe(best.plane, field.Field)

		// The first frame the scene draws comes up whole, so a still
		// rendered with --png shows the caption rather than its first frame
		// of fade.
		if !s.captionStarted {
			held.shownAt = elapsed - captionFade
		}
	}

	s.captionStarted = true
}

// keepCaption refreshes the held aircraft's figures and starts the fade out
// when it is time to let go. A caption whose fade out has finished is
// cleared, which leaves room for the next one on the same frame.
func (s *Scene) keepCaption(frame source.Frame, field source.Corridors, elapsed time.Duration, best captionPick) {
	held := &s.caption
	plane := findByICAO(frame.Planes, held.icao)

	if plane != nil {
		held.describe(*plane, field.Field)
	}

	if !held.leaving && s.captionDone(plane, field, elapsed, best) {
		held.leaveAt = elapsed - time.Duration((1-held.alpha(elapsed))*float64(captionFade))
		held.leaving = true
	}

	if held.leaving && held.alpha(elapsed) <= 0 {
		*held = caption{}
	}
}

// captionDone reports whether the held aircraft, nil when it has left the
// frame, should give up the caption.
func (s *Scene) captionDone(
	plane *airplane.Snapshot, field source.Corridors, elapsed time.Duration, best captionPick,
) bool {
	held := &s.caption

	if plane == nil {
		return true
	}

	if elapsed-held.shownAt < captionHold {
		return false
	}

	class, near := s.captionClass(*plane, field)
	if !near || class != held.class {
		return true
	}

	return held.class == source.TrackOutbound && best.found && best.class == source.TrackInbound
}

// captionPick is the aircraft the caption would pick if it were free, and
// its class; found is false when nothing qualifies.
type captionPick struct {
	plane airplane.Snapshot
	class source.TrackClass
	found bool
}

// captionCandidate is the aircraft the caption would pick if it were free:
// the nearest arrival in reach, or failing that the nearest departure.
func (s *Scene) captionCandidate(frame source.Frame) captionPick {
	arrival := captionPick{class: source.TrackInbound}
	departure := captionPick{class: source.TrackOutbound}
	arrivalKm, departureKm := math.Inf(1), math.Inf(1)

	for _, plane := range frame.Planes {
		class, near := s.captionClass(plane, frame.Corridors)
		if !near {
			continue
		}

		distanceKm := frame.Corridors.Field.DistanceToNm(plane.Latitude, plane.Longitude) * kmPerNm

		switch {
		case class == source.TrackInbound && distanceKm < arrivalKm:
			arrival.plane, arrival.found, arrivalKm = plane, true, distanceKm
		case class == source.TrackOutbound && distanceKm < departureKm:
			departure.plane, departure.found, departureKm = plane, true, distanceKm
		default:
		}
	}

	if arrival.found {
		return arrival
	}

	if departure.found {
		return departure
	}

	return captionPick{}
}

// captionClass is an aircraft's track class and whether it is close enough
// to the field, and shown on the picture, for the caption to name it.
func (s *Scene) captionClass(plane airplane.Snapshot, field source.Corridors) (source.TrackClass, bool) {
	if !positioned(plane.Latitude, plane.Longitude) || !s.visible(plane) {
		return source.TrackTransit, false
	}

	class := field.Classify(plane)
	distanceKm := field.Field.DistanceToNm(plane.Latitude, plane.Longitude) * kmPerNm

	switch class {
	case source.TrackInbound:
		return class, distanceKm <= arrivalReachKm
	case source.TrackOutbound:
		return class, distanceKm <= departureReachKm
	case source.TrackTransit:
		fallthrough
	default:
		return class, false
	}
}

// findByICAO is the aircraft with this address on the frame, or nil when it
// is no longer there. The pointer is into the frame's own list.
func findByICAO(planes []airplane.Snapshot, icao string) *airplane.Snapshot {
	for index := range planes {
		if planes[index].ICAO == icao {
			return &planes[index]
		}
	}

	return nil
}

// describe takes the caption's figures off the aircraft. The operator's name
// comes from pkg/airlines when the callsign's prefix is in it, followed by
// the rest of the callsign as the flight number; otherwise the callsign
// stands alone, or the address when there is no callsign at all.
func (c *caption) describe(plane airplane.Snapshot, field source.Airfield) {
	callsign := strings.TrimSpace(plane.Callsign)

	c.name, c.number = callsign, ""
	if callsign == "" {
		c.name = plane.ICAO
	}

	c.callsign = c.name

	if airline, known := airlines.Lookup(callsign); known && len(callsign) > callsignPrefix {
		c.name, c.number = airline.Name, strings.TrimSpace(callsign[callsignPrefix:])
	}

	c.latitude, c.longitude, c.altitudeFt = plane.Latitude, plane.Longitude, plane.Altitude
	c.metres = plane.Altitude * metresPerFoot
	c.km = field.DistanceToNm(plane.Latitude, plane.Longitude) * kmPerNm
}

// alpha is how far the caption has come up, from zero to one.
func (c *caption) alpha(elapsed time.Duration) float64 {
	since := elapsed - c.shownAt
	if c.leaving {
		since = captionFade - (elapsed - c.leaveAt)
	}

	return min(max(float64(since)/float64(captionFade), 0), 1)
}

// captionForm is how much the caption's second line says. Each form is
// shorter than the one before it, and the caption takes the first that ends
// before room.
type captionForm int

const (
	// formFull names the operator and the flight number.
	formFull captionForm = iota

	// formCallsign gives the callsign in place of the operator's name, which
	// is what makes a long name fit.
	formCallsign

	// formNoDistance is the callsign and the height, for when even the
	// callsign form runs into the count.
	formNoDistance
)

// drawCaption sets the caption at the bottom left in the longest form whose
// second line ends at or before room, or the shortest form when none does.
func (s *Scene) drawCaption(lay *layout, win *windowLayout, elapsed time.Duration, room int) {
	held := &s.caption
	if held.icao == "" {
		return
	}

	alpha := held.alpha(elapsed)
	line := s.windowPen(lay, win, s.faces.BodyBold, 0)
	line.x, line.y = win.box.Min.X, win.box.Max.Y-line.height()
	form := s.captionFit(&line, room)

	heading := s.windowPen(lay, win, s.faces.Small, windowTracking)
	heading.x = win.box.Min.X
	heading.y = line.y - win.scale*windowLineGap - heading.height()

	word := captionInText
	if held.class == source.TrackOutbound {
		word = captionOutText
	}

	heading.draw(word, s.classColour(held.class), alpha)
	s.captionLine(&line, alpha, form, true)
}

// captionFit is the longest form of the caption's second line that, set
// from line's pen position, ends at or before room, and the shortest form
// when none does. The count beside it is drawn either way.
func (s *Scene) captionFit(line *windowPen, room int) captionForm {
	form := formFull
	for form < formNoDistance && s.captionLine(line, 1, form, false) > room {
		form++
	}

	return form
}

// captionLine runs the caption's second line in form through pen, measuring
// it when draw is false and setting it when true, and returns the x past its
// end. The pieces go down one at a time so no string is ever built, which is
// also what lets the caption try each form against the room per frame.
func (s *Scene) captionLine(pen *windowPen, alpha float64, form captionForm, draw bool) int {
	held := &s.caption
	start := pen.x
	pen.skip = !draw

	if form == formFull {
		pen.draw(held.name, s.pal.Ink, alpha)

		if held.number != "" {
			pen.draw(captionSpace, s.pal.Ink, alpha)
			pen.draw(held.number, s.pal.Ink, alpha)
		}
	} else {
		pen.draw(held.callsign, s.pal.Ink, alpha)
	}

	if held.metres > 0 {
		pen.draw(captionSeparator, s.pal.Ink, alpha)
		pen.drawBytes(s.metresFigure(held.metres), s.pal.Ink, alpha)
		pen.draw(captionUp, s.pal.Ink, alpha)
	}

	if form != formNoDistance {
		pen.draw(captionSeparator, s.pal.Ink, alpha)
		pen.drawBytes(s.kmFigure(held.km), s.pal.Ink, alpha)
		pen.draw(captionOut, s.pal.Ink, alpha)
	}

	end := pen.x
	pen.x = start

	return end
}

// metresFigure writes a height to the nearest ten metres.
func (s *Scene) metresFigure(metres float64) []byte {
	return s.whole(math.Round(metres/metresStep) * metresStep)
}

// kmFigure writes a distance in tenths under tenthsBelowKm and in whole
// kilometres from there out.
func (s *Scene) kmFigure(km float64) []byte {
	if km < tenthsBelowKm {
		return s.fixed(km, 1)
	}

	return s.whole(km)
}

// measureMovements is the x the hourly count starts at, measured before
// anything is drawn so the caption can tell whether it has room.
func (s *Scene) measureMovements(win *windowLayout, frame source.Frame) int {
	arrivals, departures := source.Movements(frame.Ghosts, frame.Now.Add(-movementWindow))
	right := s.movementColumn(win, departures, tookOffLabel)
	left := s.movementColumn(win, arrivals, landedLabel)

	return win.box.Max.X - right - win.scale*windowColumnGap - left
}

// movementColumn is how wide one column of the count is: its figure or its
// label, whichever is wider.
func (s *Scene) movementColumn(win *windowLayout, count int, label string) int {
	figure, _ := text.Measure(s.faces.Large, string(s.count(count)), text.WithScale(win.scale))
	words, _ := text.Measure(s.faces.Small, label, text.WithScale(win.scale), text.WithSpacing(win.scale))

	return max(figure, words)
}

// drawMovements sets the hourly count at the bottom right: arrivals, then
// departures, each a figure over its label in its corridor colour and
// right-aligned in its column, so the right edge stays put as the figures
// grow a digit.
func (s *Scene) drawMovements(lay *layout, win *windowLayout, frame source.Frame) {
	arrivals, departures := source.Movements(frame.Ghosts, frame.Now.Add(-movementWindow))

	right := win.box.Max.X
	right -= s.drawMovement(lay, win, right, departures, tookOffLabel, s.classColour(source.TrackOutbound))
	right -= win.scale * windowColumnGap
	s.drawMovement(lay, win, right, arrivals, landedLabel, s.classColour(source.TrackInbound))
}

// drawMovement sets one column of the count ending at right and returns its
// width.
func (s *Scene) drawMovement(
	lay *layout, win *windowLayout, right, count int, label string, ink color.RGBA,
) int {
	width := s.movementColumn(win, count, label)

	words := s.windowPen(lay, win, s.faces.Small, windowTracking)
	words.x, words.y = right-words.measure(label), win.box.Max.Y-words.height()
	words.draw(label, ink, 1)

	figure := s.windowPen(lay, win, s.faces.Large, 0)
	value := s.count(count)
	figure.x = right - figure.measureBytes(value)
	figure.y = words.y - win.scale*windowLineGap - figure.height()
	figure.drawBytes(value, ink, 1)

	return width
}

// windowPen sets a run of pieces one after another in one face and size,
// each over a shadow in the field colour so it reads on sea, land and sky
// alike. With skip set it only moves along, which is how a run is measured
// with the same arithmetic that draws it.
type windowPen struct {
	lay     *layout
	face    *psf.Font
	scale   int
	spacing int
	shadow  int
	ground  color.RGBA
	x, y    int
	skip    bool
}

// windowPen is a pen in face at the overlay's scale, with tracking in glyph
// pixels.
func (s *Scene) windowPen(lay *layout, win *windowLayout, face *psf.Font, tracking int) windowPen {
	return windowPen{
		lay:     lay,
		face:    face,
		scale:   win.scale,
		spacing: tracking * win.scale,
		shadow:  win.shadow,
		ground:  s.pal.Field,
	}
}

// height is one line of the pen's face at its scale.
func (p *windowPen) height() int {
	if p.face == nil {
		return 0
	}

	return p.face.Height() * p.scale
}

// measure is how wide value would be.
func (p *windowPen) measure(value string) int {
	width, _ := text.Measure(p.face, value, text.WithScale(p.scale), text.WithSpacing(p.spacing))

	return width
}

// measureBytes is measure for a formatted figure.
func (p *windowPen) measureBytes(value []byte) int { return p.measure(string(value)) }

// draw sets value at the pen and moves the pen past it.
func (p *windowPen) draw(value string, ink color.RGBA, alpha float64) {
	if !p.skip {
		scale, spacing, fade := text.WithScale(p.scale), text.WithSpacing(p.spacing), text.WithAlpha(alpha)
		text.Draw(p.lay.dst, p.face, p.x+p.shadow, p.y+p.shadow, value, p.ground, scale, spacing, fade)
		text.Draw(p.lay.dst, p.face, p.x, p.y, value, ink, scale, spacing, fade)
	}

	p.x += p.measure(value)
}

// drawBytes is draw for a formatted figure.
func (p *windowPen) drawBytes(value []byte, ink color.RGBA, alpha float64) {
	p.draw(string(value), ink, alpha)
}
