package radar

import (
	"math"

	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/source"
)

// Handle runs the scene's own key bindings and reports whether it took the
// key.
//
// A key it does not take falls through to the run loop, which is what keeps q
// working while the radar is on screen. Both letter cases are bound because
// caps lock is easy to hit by accident on the uConsole's keyboard.
//
// Esc is taken rather than passed on, so it no longer quits while the radar is
// up: it is the way back to following the nearest aircraft, which is the
// commoner thing to want once a selection has been pinned. Ctrl-C and q still
// quit, and both are reachable without letting go of the keyboard.
func (s *Scene) Handle(key input.Key) bool {
	switch key.Kind {
	case input.Down:
		s.step(1)

		return true
	case input.Up:
		s.step(-1)

		return true
	case input.Esc:
		s.unpin()

		return true
	case input.Left:
		return s.nudgeOrbit(-azimuthStep)
	case input.Right:
		return s.nudgeOrbit(azimuthStep)
	case input.Rune:
		return s.handleRune(key.Rune)
	case input.Enter, input.CtrlC:
		fallthrough
	default:
		return false
	}
}

// handleRune maps a printable key onto one of the scene's controls.
//
// The unshifted twins of + and - are bound as well, because reaching for
// shift to change the range on a thumb keyboard is a nuisance.
//
// m and a work in minimal mode and draw there, on minimal's own pair of
// toggles rather than on the scope's. v is what cycles between the three
// views, w takes the column away from the two of them that have one, and
// anything not bound here falls through to the camera keys, which only the 3D
// view takes.
//
// c and f are one pair rather than two keys: c picks what a colour means and f
// picks one of the colours, so c is also what puts the filter back to ALL. See
// cycleColour.
func (s *Scene) handleRune(value rune) bool {
	switch value {
	case 'n', 'N':
		s.step(1)
	case 'p', 'P':
		s.step(-1)
	case '+', '=':
		s.stepRange(1)
	case '-', '_':
		s.stepRange(-1)
	case 'r', 'R':
		s.autoRange = !s.autoRange
	case 't', 'T':
		s.trail = s.trail.next()
	case 'a', 'A':
		s.toggleAirports()
	case 'm', 'M':
		s.toggleShore()
	case 'v', 'V':
		s.shown = s.shown.Next()
	case 'w', 'W':
		return s.toggleWide()
	case 'c', 'C':
		s.cycleColour()
	case 'f', 'F':
		s.cycleFilter()
	case 'b', 'B':
		return s.toggleBiasTee()
	default:
		return s.handle3DRune(value)
	}

	return true
}

// toggleWide hides the right column and gives the picture the whole width, or
// puts the column back, which is what w does.
//
// It reports false in the two bare views, so the key falls through to the run
// loop there exactly as the camera keys do outside the 3D views, and for the
// same reason: a bare view has no column to hide and no key bar to say what
// happened, so a press that quietly flipped a flag would turn up as a surprise
// the next time v came back round to the scope.
func (s *Scene) toggleWide() bool {
	if s.bare() {
		return false
	}

	s.wide = !s.wide

	return true
}

// cycleColour moves to the other colour mode and puts the filter back to ALL,
// which is what c does.
//
// The filter travels with it because a filter value is a legend entry, and
// pressing c replaces the legend. Carrying the value across would leave the
// cap naming an airline while the scope was coloured by height, and the field
// showing whichever aircraft matched a designator nothing on screen mentioned
// any more.
func (s *Scene) cycleColour() {
	s.colour = s.colour.Next()
	s.filter = filterState{}
}

// toggleBiasTee asks for the dongle's LNA power to flip, which is what b
// does.
//
// It does no work itself and it waits for nothing. Setting a bias-tee is a USB
// control transfer, and this runs on the goroutine that draws: a dongle that
// takes its time answering would take the scope down with it. The Toggler
// takes it away and reports its own failures.
//
// It reports false on a run with no dongle behind it, so b falls through to
// the run loop exactly as an unbound key does, for the reason the camera keys
// fall through outside the 3D view. Nothing in the loop binds b either, so
// the press is a no-op, but a key that silently ate itself would be a key
// whose effect turned up as a surprise later.
func (s *Scene) toggleBiasTee() bool {
	if s.biasTee == nil || !s.biasSupported {
		return false
	}

	s.biasTee.Toggle()

	return true
}

// handle3DRune maps the camera keys, which only the two 3D views bind.
//
// They belong to the picture rather than to the scene, so e, o and the
// brackets fall through to the run loop in the flat views, where there is no
// camera to move and no envelope to toggle. A key that quietly changed state
// nothing on screen could show would be a key whose effect turned up as a
// surprise three presses later. e is refused in the bare 3D view for the same
// reason, one level further in; see toggleEnvelope.
func (s *Scene) handle3DRune(value rune) bool {
	if !s.perspective() {
		return false
	}

	switch value {
	case 'e', 'E':
		return s.toggleEnvelope()
	case 'o', 'O':
		s.toggleOrbit()
	case '[':
		s.tilt(-tiltStep)
	case ']':
		s.tilt(tiltStep)
	default:
		return false
	}

	return true
}

// toggleEnvelope draws the receiving envelope or takes it away, which is what
// e does.
//
// It reports false in the bare 3D view, where the envelope is never drawn
// whatever the flag says. That view exists to have no furniture, and the
// envelope is the largest piece of it; a key that flipped a setting nothing on
// screen could show would be a key whose effect turned up three presses later,
// which is the rule the camera keys already follow outside the 3D views.
func (s *Scene) toggleEnvelope() bool {
	if s.bare() {
		return false
	}

	s.envelope = !s.envelope

	return true
}

// nudgeOrbit turns the camera by one step and stops it turning on its own,
// which is what Left and Right do in the 3D views.
//
// It reports false in the flat views so the key falls through to the run loop
// exactly as it did before the view existed. Stopping the orbit is the point
// rather than a side effect: nudging a camera that then walks away from where
// it was put is not what pressing an arrow meant.
func (s *Scene) nudgeOrbit(degrees float64) bool {
	if !s.perspective() {
		return false
	}

	s.azimuth = wrapDegrees(s.cameraAzimuth(s.elapsed) + degrees)
	s.orbiting = false

	return true
}

// toggleOrbit starts the camera turning or stops it where it is, which is what
// o does.
//
// It used to only start it. The orbit is on from the first frame and turns at
// one revolution every two minutes, so the commonest press was one that
// rebased an azimuth already where it was and changed nothing anyone could
// see, which made o read as a key that did nothing. The useful half was always
// the other one: holding the camera still on the side of the envelope being
// read, or on an aircraft being followed across it.
//
// The cap says which state it is in, because capOrbit reads s.orbiting, so
// the bar answers the question the key used to leave open.
func (s *Scene) toggleOrbit() {
	if !s.orbiting {
		s.startOrbit()

		return
	}

	// Freeze the camera where the picture actually has it, not where the last
	// keypress left s.azimuth. cameraAzimuth winds forward from azimuthAt
	// while the orbit runs, so keeping the stored value would snap the view
	// back to wherever the orbit began on the very next frame.
	s.azimuth = s.cameraAzimuth(s.elapsed)
	s.orbiting = false
}

// startOrbit sets the camera turning again from wherever it is now.
//
// It rebases rather than resuming, so the azimuth it starts from is the one on
// screen and the camera does not jump when the orbit picks up again.
func (s *Scene) startOrbit() {
	s.azimuth = s.cameraAzimuth(s.elapsed)
	s.azimuthAt = s.elapsed
	s.orbiting = true
}

// tilt moves the camera's elevation by one step, clamped rather than refused.
//
// Clamping is right here where ParseRecentre refuses: this is a key held down
// against the end of its travel, not a value somebody typed, and there is
// nobody to tell.
func (s *Scene) tilt(degrees float64) {
	s.elevation = min(max(s.elevation+degrees, minElevation), maxElevation)
}

// toggleShore flips whichever coastline switch the view on screen reads.
//
// The two bare views keep their own, off at the start and independent of the
// scope's. Pressing m in one of them is a choice about the bare pair, not a
// change to the view you get back when you press v, and the same the other way
// round. The pair is shared between flat and tilted because it is one choice
// about how much furniture a bare picture carries, and the two are a single
// press apart.
func (s *Scene) toggleShore() {
	if s.bare() {
		s.minimalShore = !s.minimalShore

		return
	}

	s.shoreOn = !s.shoreOn
}

// toggleAirports is toggleShore for the airfield markers, on the same rule
// and for the same reason.
func (s *Scene) toggleAirports() {
	if s.bare() {
		s.minimalAirports = !s.minimalAirports

		return
	}

	s.airports = !s.airports
}

// shoreDrawn and airportsDrawn are the two toggles that apply to whatever is
// on screen. Everything that draws an overlay or keys the background layer
// asks these rather than reading a field, so the bare pair and the scope pair
// cannot be mixed up between the drawing and the cache.
func (s *Scene) shoreDrawn() bool {
	if s.bare() {
		return s.minimalShore
	}

	return s.shoreOn
}

func (s *Scene) airportsDrawn() bool {
	if s.bare() {
		return s.minimalAirports
	}

	return s.airports
}

// step moves the selection through the list, wrapping at both ends, and pins
// it to whichever aircraft it lands on.
//
// It works on the ICAO list the last frame left behind rather than asking the
// source for a new one, so a key press never costs a snapshot of the whole
// fleet and the index the operator sees is the index that moves.
//
// Pinning here rather than on a key of its own is what makes the rule one
// rule: an operator who has not chosen an aircraft is shown the nearest one,
// and pressing a select key is the choosing.
// The list it steps through is the one the filter left, so n and p walk the
// rows on screen and never land on an aeroplane nothing is drawing.
func (s *Scene) step(delta int) {
	count := len(s.icaos)
	if count == 0 {
		return
	}

	s.selIndex = ((s.selIndex+delta)%count + count) % count
	s.selICAO = s.icaos[s.selIndex]
	s.pinned, s.selHidden = true, false
}

// unpin hands the selection back to the nearest aircraft, which is what Esc
// does. The next frame's syncSelection is what actually moves it, so this only
// has to forget the choice.
func (s *Scene) unpin() {
	s.pinned, s.selHidden = false, false
	s.selICAO, s.selIndex, s.rowStart = "", 0, 0
}

// stepRange changes the range by one increment and turns auto off, because
// asking for a range and having it overridden on the next frame is not what
// pressing the key meant.
func (s *Scene) stepRange(delta int) {
	s.autoRange = false

	increment := s.scopeRange.GetIncrement()
	s.scopeRange.Update(scope.WithCurrent(s.scopeRange.GetCurrent() + float64(delta)*increment))
}

// syncSelection rebuilds the ICAO index from the frame and works out which
// aircraft the panel is about.
//
// Two rules, and which one applies is the pin. Unpinned, the selection is the
// nearest contact, which is index zero because the list arrives sorted by
// distance; the rows go back to the top with it. That is the useful default on
// a live feed, where the first aircraft to arrive with a position used to keep
// the panel for as long as it stayed in range, however far away it drifted.
//
// Pinned, the selection is kept by ICAO rather than by position, because one
// aircraft overtaking another would otherwise hand the panel to a different
// aeroplane without anyone pressing a key. A pinned aircraft that leaves the
// list takes its pin with it: there is nothing left to hold, so the selection
// goes back to following the nearest.
//
// The index holds only the aircraft the filter is showing, which is what makes
// "the nearest" mean the nearest one on screen. An unpinned selection standing
// on an aeroplane the filter then hid would be a panel about something nobody
// can see, and the row list would carry an accent bar on a line that is not
// there.
func (s *Scene) syncSelection(frame source.Frame) {
	s.icaos = s.icaos[:0]

	for _, plane := range frame.Planes {
		if !s.visible(plane) {
			continue
		}

		s.icaos = append(s.icaos, plane.ICAO)
	}

	if s.pinned && s.keepPinned(frame) {
		return
	}

	if len(s.icaos) == 0 {
		s.selICAO, s.selIndex, s.rowStart, s.selHidden = "", -1, 0, false

		return
	}

	s.selIndex, s.rowStart, s.selHidden = 0, 0, false
	s.selICAO = s.icaos[0]
}

// keepPinned holds the selection on the aircraft the operator chose, and
// reports whether it managed to.
//
// Three states, and the filter is what adds the middle one. The aircraft is in
// the index, so it has a row and the panel points at it. It is filtered out
// but still in the frame, so the pin holds, the panel still draws it and says
// FILTERED, and it has no row for the index to point at. Or it has gone off
// the list altogether, and there is nothing left to hold: the pin goes and the
// selection falls back to the nearest contact.
//
// The middle case is why this takes the frame rather than working off the ICAO
// index alone. The index is what the filter left, and the whole question here
// is about an aeroplane that is not in it.
func (s *Scene) keepPinned(frame source.Frame) bool {
	if index := indexOf(s.icaos, s.selICAO); index >= 0 {
		s.selIndex, s.selHidden = index, false

		return true
	}

	if _, flying := findPlane(frame, s.selICAO); flying {
		s.selIndex, s.selHidden = notSelected, true

		return true
	}

	s.pinned, s.selHidden = false, false

	return false
}

// indexOf finds an ICAO in the list, or reports -1.
func indexOf(icaos []string, want string) int {
	if want == "" {
		return -1
	}

	for index, icao := range icaos {
		if icao == want {
			return index
		}
	}

	return -1
}

// fitRange widens or narrows the scope to hold the farthest aircraft that has
// a position, rounded up to a whole increment.
//
// Aircraft with no position are skipped rather than counted as far away:
// HaversineDistance reports MaxFloat64 for an unknown position, and one of
// those would pin the scope at its maximum range for as long as the aircraft
// was in the list. A frame where nothing has a position leaves the range
// alone, so the scope does not snap back to its minimum every time the feed
// goes quiet.
//
// Aircraft the filter is hiding are skipped for a different reason: the range
// is what the scope has room for, and sizing it around an aeroplane nothing
// draws would leave the traffic on screen in a small ring in the middle of an
// empty field. Filtering to the low band on a scope reaching 180 nautical
// miles is meant to pull the range in with it.
func (s *Scene) fitRange(frame source.Frame) {
	s.fitRangeAround(frame, geo{lat: frame.Receiver.Latitude, lon: frame.Receiver.Longitude})
}

// fitRangeAround is fitRange with the point to measure from handed in.
//
// Minimal mode following the traffic measures from the centroid rather than
// from the receiver, because the centroid is what it has centred on. Sizing
// the scope by a distance the picture no longer shows would leave the whole
// fleet in a small ring in the middle of an otherwise empty field.
func (s *Scene) fitRangeAround(frame source.Frame, origin geo) {
	if !s.autoRange {
		return
	}

	farthest := 0.0

	for _, plane := range frame.Planes {
		if !s.visible(plane) {
			continue
		}

		distance := airplanes.HaversineDistance(origin.lat, origin.lon, plane.Latitude, plane.Longitude)
		if distance == math.MaxFloat64 || math.IsNaN(distance) {
			continue
		}

		farthest = max(farthest, distance)
	}

	if farthest <= 0 {
		return
	}

	increment := s.scopeRange.GetIncrement()
	if increment <= 0 {
		s.scopeRange.Update(scope.WithCurrent(farthest))

		return
	}

	s.scopeRange.Update(scope.WithCurrent(math.Ceil(farthest/increment) * increment))
}
