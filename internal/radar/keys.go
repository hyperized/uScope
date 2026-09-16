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
// and v working while the radar is on screen. Both letter cases are bound
// because caps lock is easy to hit by accident on the uConsole's keyboard.
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
	case input.Rune:
		return s.handleRune(key.Rune)
	case input.Left, input.Right, input.Enter, input.CtrlC:
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
// toggles rather than on the scope's. z is how you get back out.
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
		s.trails = !s.trails
	case 'a', 'A':
		s.toggleAirports()
	case 'm', 'M':
		s.toggleShore()
	case 'z', 'Z':
		s.minimal = !s.minimal
	case 'c', 'C':
		s.colour = s.colour.Next()
	default:
		return false
	}

	return true
}

// toggleShore flips whichever coastline switch the view on screen reads.
//
// Minimal mode keeps its own, off at the start and independent of the
// scope's. Pressing m in minimal is a choice about minimal, not a change to
// the view you get back when you press z, and the same the other way round.
func (s *Scene) toggleShore() {
	if s.minimal {
		s.minimalShore = !s.minimalShore

		return
	}

	s.shoreOn = !s.shoreOn
}

// toggleAirports is toggleShore for the airfield markers, on the same rule
// and for the same reason.
func (s *Scene) toggleAirports() {
	if s.minimal {
		s.minimalAirports = !s.minimalAirports

		return
	}

	s.airports = !s.airports
}

// shoreDrawn and airportsDrawn are the two toggles that apply to whatever is
// on screen. Everything that draws an overlay or keys the background layer
// asks these rather than reading a field, so the minimal pair and the scope
// pair cannot be mixed up between the drawing and the cache.
func (s *Scene) shoreDrawn() bool {
	if s.minimal {
		return s.minimalShore
	}

	return s.shoreOn
}

func (s *Scene) airportsDrawn() bool {
	if s.minimal {
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
func (s *Scene) step(delta int) {
	count := len(s.icaos)
	if count == 0 {
		return
	}

	s.selIndex = ((s.selIndex+delta)%count + count) % count
	s.selICAO = s.icaos[s.selIndex]
	s.pinned = true
}

// unpin hands the selection back to the nearest aircraft, which is what Esc
// does. The next frame's syncSelection is what actually moves it, so this only
// has to forget the choice.
func (s *Scene) unpin() {
	s.pinned = false
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
func (s *Scene) syncSelection(frame source.Frame) {
	s.icaos = s.icaos[:0]
	for _, plane := range frame.Planes {
		s.icaos = append(s.icaos, plane.ICAO)
	}

	if len(s.icaos) == 0 {
		s.selICAO, s.selIndex, s.rowStart = "", -1, 0

		return
	}

	if s.pinned {
		if index := indexOf(s.icaos, s.selICAO); index >= 0 {
			s.selIndex = index

			return
		}

		s.pinned = false
	}

	s.selIndex, s.rowStart = 0, 0
	s.selICAO = s.icaos[0]
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
