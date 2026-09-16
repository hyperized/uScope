package radar

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/airlines"
)

// ColourMode says what an aircraft's colour on the scope means.
//
// It is a string type for the same reason theme.Kind is: it is what --colour
// parses into, so the flag spelling and the value are the same thing and
// cannot drift apart.
type ColourMode string

// The two modes, in the order the c key cycles them.
const (
	// ColourAltitude is the default: three bands of height, which is the one
	// piece of information a top-down scope cannot show by position.
	ColourAltitude ColourMode = "altitude"

	// ColourAirline paints each aircraft in its operator's colour instead.
	// Height then has to be read off the card and the rows, and what the scope
	// shows at a glance is who is flying rather than how high.
	ColourAirline ColourMode = "airline"
)

// ErrColour is returned for a --colour value that is neither spelling.
var ErrColour = errors.New("radar: unknown colour mode")

// ParseColour turns a --colour value into a ColourMode.
//
// The match is case sensitive, the same way theme.Parse is: --colour is an
// allow list rather than free text, so "Airline" is refused exactly as
// "airlines" would be.
func ParseColour(text string) (ColourMode, error) {
	switch ColourMode(text) {
	case ColourAltitude:
		return ColourAltitude, nil
	case ColourAirline:
		return ColourAirline, nil
	default:
		return ColourAltitude, fmt.Errorf("%w: %q", ErrColour, text)
	}
}

// Next cycles to the other mode. Anything that is not ColourAirline moves to
// it, so the zero value of ColourMode cycles the way ColourAltitude does and
// never has to be special-cased where one is read.
func (m ColourMode) Next() ColourMode {
	if m == ColourAirline {
		return ColourAltitude
	}

	return ColourAirline
}

// Toggle is an on or off setting a flag carries into the scene.
//
// It is a string type rather than a bool for the same reason ColourMode is one:
// the zero value has to be the default, and these settings default to on. A
// bool field left unset in a Config would turn the airfields off, which is not
// what "nobody said" means.
type Toggle string

// The two spellings an on/off flag accepts.
const (
	ToggleOn  Toggle = "on"
	ToggleOff Toggle = "off"
)

// ErrToggle is returned for an on/off value that is neither spelling.
var ErrToggle = errors.New("radar: value must be on or off")

// On reports whether the toggle is on. Anything that is not ToggleOff is,
// which is what makes the zero value usable.
func (t Toggle) On() bool { return t != ToggleOff }

// ParseToggle reads an on/off flag value.
//
// It is an allow list rather than strconv.ParseBool, which also takes "1",
// "t", "TRUE" and five other spellings. A setting with two states should have
// two spellings, and a typo should be refused rather than guessed at.
func ParseToggle(text string) (Toggle, error) {
	switch Toggle(text) {
	case ToggleOn:
		return ToggleOn, nil
	case ToggleOff:
		return ToggleOff, nil
	default:
		return ToggleOn, fmt.Errorf("%w: %q", ErrToggle, text)
	}
}

// RangeAuto is the --range spelling that leaves the scope fitting itself to
// the fleet, which is what it does when nobody says otherwise.
const RangeAuto = "auto"

// ErrRange is returned for a --range value that is neither auto nor a range
// the scope can show. One sentinel covers both, because from the operator's
// side "forty" and "900" are the same mistake: a value the flag will not take.
var ErrRange = errors.New("radar: not a range the scope can show")

// ParseRange reads a --range value, returning zero for auto.
//
// The limits are read off a scope.Scope rather than written out here, because
// that is what enforces them at run time: scope clamps whatever it is given,
// so a flag that accepted 900 would be silently pulled back to the maximum on
// the first frame. Refusing it says so while there is still somebody reading.
func ParseRange(text string) (float64, error) {
	if text == RangeAuto {
		return 0, nil
	}

	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a number", ErrRange, text)
	}

	limits := scope.New()
	low, high := limits.GetMin(), limits.GetMax()

	if math.IsNaN(value) || value < low || value > high {
		return 0, fmt.Errorf("%w: got %s, want %s or %g to %g", ErrRange, text, RangeAuto, low, high)
	}

	return value, nil
}

// Settings are what the command line picked before the scene existed.
//
// They travel as one struct rather than as a setter each, because the radar
// keeps gaining them: a package that grows an interface per flag ends up with
// a shelf of one-method interfaces that all mean "the command line said so".
// Every field's zero value is its default, so a Settings nobody filled in is
// the scene as it starts.
type Settings struct {
	// Colour is what an aircraft's colour means. Zero reads as ColourAltitude.
	Colour ColourMode

	// Airports is whether the airfield markers start on. Zero reads as on,
	// which is the scope at its most useful before anyone touches a key.
	Airports Toggle

	// Shore is whether the coastline starts on. Zero reads as on, the same way
	// Airports does, and it draws nothing at all unless the scene was also
	// handed the data through WithShore.
	Shore Toggle

	// RangeNm pins the scope to a range in nautical miles and turns auto range
	// off with it. Zero means nobody asked, which leaves the scope fitting
	// itself to the fleet.
	RangeNm float64

	// View is which of the three pictures the scene starts on. Zero reads as
	// ViewScope, which is where a run begins before anyone presses v.
	View View

	// Exaggerate is how far the 3D view stretches altitude into height. Zero
	// means nobody asked, which leaves DefaultExaggerate in place.
	Exaggerate float64

	// Recentre is how often minimal mode refits itself on the traffic. Zero is
	// off, which is the scene's own default and keeps minimal mode centred on
	// the receiver.
	//
	// --recenter holds DefaultRecentre instead, because a directional antenna
	// puts every contact in one half of the canvas and following them is what
	// the flag is for. The two differ on purpose: a library caller building a
	// Scene gets the plain projection until it asks for the other one, and the
	// command line asks on its behalf.
	Recentre time.Duration
}

// Apply sets the whole block on a scene that is already built, which is how
// the flags reach a scene the run loop built before it read the config.
func (s *Scene) Apply(set Settings) {
	s.colour = set.Colour
	s.airports = set.Airports.On()
	s.shoreOn = set.Shore.On()
	s.shown = set.View
	s.applyRange(set.RangeNm)
	s.applyRecentre(set.Recentre)
	s.applyExaggerate(set.Exaggerate)
}

// applyExaggerate sets how far the 3D view stretches altitude.
//
// Zero or less is nobody asking rather than a flat world, which puts the
// default back: every field of Settings has to read as the scene's own default
// when it is left unset, or a config built by a caller who only cared about
// one flag would quietly change the rest.
func (s *Scene) applyExaggerate(factor float64) {
	if factor <= 0 {
		s.exaggerate = DefaultExaggerate

		return
	}

	s.exaggerate = factor
}

// applyRange pins the scope to the range the command line asked for.
//
// Auto range goes off with it, because asking for a range and having it
// overridden on the next frame is not what --range meant. Zero is nobody
// asking, which puts auto back on: every field of Settings has to read as the
// scene's own default when it is left unset, or a config built by a caller who
// only cared about one flag would quietly change the rest.
func (s *Scene) applyRange(rangeNm float64) {
	if rangeNm <= 0 {
		s.autoRange = true

		return
	}

	s.autoRange = false
	s.scopeRange.Update(scope.WithCurrent(rangeNm))
}

// WithSettings applies a settings block at construction.
func WithSettings(set Settings) Option {
	return func(s *Scene) { s.Apply(set) }
}

// The legend's operator tally.
const (
	// legendSlots is how many operators the legend names. Four fits across the
	// column at the panel's width with room for a name beside each designator,
	// and a legend longer than that stops being something read at a glance.
	legendSlots = 4

	// maxOperators is how many distinct operators the per-frame counter table
	// holds. A scope covering a few hundred nautical miles sees a few dozen at
	// the outside, and anything past the table counts as OTHER rather than
	// growing a slice on the draw path.
	maxOperators = 64
)

// legendOther is the last legend entry, standing for every aircraft with no
// callsign and every callsign whose operator is not in the database.
const legendOther = "OTHER"

// operatorCount is one airline and how many of it are on the scope.
type operatorCount struct {
	airline airlines.Airline
	count   int
}

// tally counts the operators in one frame.
//
// It lives on the Scene and is reset rather than rebuilt, because the legend
// is recomputed on every frame and the draw path allocates nothing. The table
// is a fixed array for that reason: a map would rehash, and a slice would
// grow the first time a busy frame arrived.
type tally struct {
	seen  [maxOperators]operatorCount
	count int

	// shown is how many of seen hold the ranked entries after rank, and other
	// says whether anything on the field had no colour at all.
	shown int
	other bool
}

// reset empties the table for a new frame.
func (t *tally) reset() {
	t.count, t.shown, t.other = 0, 0, false
}

// add counts one aircraft against its operator.
//
// The scan is linear because the table is short and a frame walks it once per
// aircraft: at a few dozen entries that is cheaper than hashing a key, and it
// costs nothing to set up.
func (t *tally) add(callsign string) {
	airline, known := airlines.Lookup(callsign)
	if !known {
		t.other = true

		return
	}

	for index := range t.count {
		if t.seen[index].airline.ICAO == airline.ICAO {
			t.seen[index].count++

			return
		}
	}

	if t.count == maxOperators {
		t.other = true

		return
	}

	t.seen[t.count] = operatorCount{airline: airline, count: 1}
	t.count++
}

// rank pulls the busiest operators to the front of the table, leaving the
// first shown entries in legend order.
//
// It is a partial selection sort rather than a full one because only four
// entries are ever drawn, and sorting the rest would be work thrown away. The
// table is rebuilt every frame, so reordering it in place costs nothing.
func (t *tally) rank() {
	limit := min(legendSlots, t.count)

	for slot := range limit {
		best := slot

		for index := slot + 1; index < t.count; index++ {
			if outranks(t.seen[index], t.seen[best]) {
				best = index
			}
		}

		t.seen[slot], t.seen[best] = t.seen[best], t.seen[slot]
	}

	t.shown = limit
}

// outranks reports whether left belongs above right in the legend.
//
// Aircraft count decides it, and the designator breaks a tie. The tie-break is
// not cosmetic: the aircraft list arrives sorted by distance, so without it two
// operators level on count would swap places whenever one of them moved, and
// the same input would stop producing the same frame.
func outranks(left, right operatorCount) bool {
	if left.count != right.count {
		return left.count > right.count
	}

	return left.airline.ICAO < right.airline.ICAO
}

// aircraftColour is the colour one aircraft is drawn in: its silhouette, its
// trail, its callsign on the card and its callsign in the compact rows.
func (s *Scene) aircraftColour(plane airplane.Snapshot) color.RGBA {
	return s.contactColour(plane.Altitude, plane.Callsign)
}

// ghostColour is the colour the trail of a lost aircraft keeps.
//
// It is the same rule live traffic is coloured by, read off the last altitude
// and callsign the aircraft reported. A ghost drawn by some other rule would
// be a second thing to learn about a field that is already showing a track
// nobody is flying.
func (s *Scene) ghostColour(ghost source.Trail) color.RGBA {
	return s.contactColour(ghost.Altitude, ghost.Callsign)
}

// contactColour is the rule both of those share: the altitude band, or the
// operator's own colour in airline mode.
func (s *Scene) contactColour(altitude float64, callsign string) color.RGBA {
	if s.colour != ColourAirline {
		return s.bandColour(altitude)
	}

	return s.airlineColour(callsign)
}

// airlineColour is the operator's colour for a callsign.
//
// An aircraft with no callsign, or one whose three-letter prefix is not in the
// database, comes back muted. That is the same answer bandColour gives an
// aircraft with no decoded altitude, and it reads as "nothing known" rather
// than as a colour somebody has to look up.
func (s *Scene) airlineColour(callsign string) color.RGBA {
	airline, known := airlines.Lookup(callsign)
	if !known {
		return s.pal.Muted
	}

	return s.operatorColour(airline)
}

// operatorColour adapts a brand colour to the field it is being drawn on.
//
// A brand colour is picked for print and for a white page, so half of them
// disappear into night's near-black field and the bright half washes out on
// paper. pkg/airlines does the adapting; this only picks which way.
func (s *Scene) operatorColour(airline airlines.Airline) color.RGBA {
	if s.light {
		return airline.OnLight()
	}

	return airline.OnDark()
}

// callsignInk is the colour a callsign is set in.
//
// In airline mode it is the operator's own colour, which is the whole point of
// the mode: the same colour marks the aircraft on the scope and its name in
// the list beside it.
func (s *Scene) callsignInk(plane airplane.Snapshot) color.RGBA {
	if s.colour == ColourAirline {
		return s.airlineColour(plane.Callsign)
	}

	return s.pal.Ink
}

// rowCallsignInk is callsignInk for the compact row at index, where an
// unselected callsign is muted in altitude mode so the eye lands on the
// selection first.
func (s *Scene) rowCallsignInk(plane airplane.Snapshot, index int) color.RGBA {
	if index == s.selIndex || s.colour == ColourAirline {
		return s.callsignInk(plane)
	}

	return s.pal.Muted
}
