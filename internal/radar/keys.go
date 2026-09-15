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
// and s working while the radar is on screen. Both letter cases are bound
// because caps lock is easy to hit by accident on the uConsole's keyboard.
func (s *Scene) Handle(key input.Key) bool {
	switch key.Kind {
	case input.Down:
		s.step(1)

		return true
	case input.Up:
		s.step(-1)

		return true
	case input.Rune:
		return s.handleRune(key.Rune)
	case input.Left, input.Right, input.Enter, input.Esc, input.CtrlC:
		fallthrough
	default:
		return false
	}
}

// handleRune maps a printable key onto one of the scene's controls.
//
// The unshifted twins of + and - are bound as well, because reaching for
// shift to change the range on a thumb keyboard is a nuisance.
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
		s.airports = !s.airports
	case 'c', 'C':
		s.colour = s.colour.Next()
	default:
		return false
	}

	return true
}

// step moves the selection through the list, wrapping at both ends.
//
// It works on the ICAO list the last frame left behind rather than asking the
// source for a new one, so a key press never costs a snapshot of the whole
// fleet and the index the operator sees is the index that moves.
func (s *Scene) step(delta int) {
	count := len(s.icaos)
	if count == 0 {
		return
	}

	s.selIndex = ((s.selIndex+delta)%count + count) % count
	s.selICAO = s.icaos[s.selIndex]
}

// stepRange changes the range by one increment and turns auto off, because
// asking for a range and having it overridden on the next frame is not what
// pressing the key meant.
func (s *Scene) stepRange(delta int) {
	s.autoRange = false

	increment := s.scopeRange.GetIncrement()
	s.scopeRange.Update(scope.WithCurrent(s.scopeRange.GetCurrent() + float64(delta)*increment))
}

// syncSelection rebuilds the ICAO index from the frame and re-finds the
// selected aircraft in it.
//
// The selection is kept by ICAO rather than by position, because the list is
// sorted by distance and one aircraft overtaking another would otherwise move
// the selection to a different aeroplane without anyone pressing a key. When
// the selected aircraft disappears, or nothing is selected yet, the nearest
// wins: the list is already sorted, so that is index zero.
func (s *Scene) syncSelection(frame source.Frame) {
	s.icaos = s.icaos[:0]
	for _, plane := range frame.Planes {
		s.icaos = append(s.icaos, plane.ICAO)
	}

	if len(s.icaos) == 0 {
		s.selICAO, s.selIndex, s.rowStart = "", -1, 0

		return
	}

	// A miss reports -1, and max pulls that up to index zero, which is the
	// nearest aircraft because the list arrives sorted by distance.
	s.selIndex = max(indexOf(s.icaos, s.selICAO), 0)
	s.selICAO = s.icaos[s.selIndex]
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
	if !s.autoRange {
		return
	}

	farthest := 0.0

	for _, plane := range frame.Planes {
		distance := airplanes.HaversineDistance(
			frame.Receiver.Latitude, frame.Receiver.Longitude, plane.Latitude, plane.Longitude)
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
