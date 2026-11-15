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

// The three modes, in the order the c key cycles them.
const (
	// ColourAltitude is the default: three bands of height, which is the one
	// piece of information a top-down scope cannot show by position.
	ColourAltitude ColourMode = "altitude"

	// ColourAirline paints each aircraft in its operator's colour instead.
	// Height then has to be read off the card and the rows, and what the scope
	// shows at a glance is who is flying rather than how high.
	ColourAirline ColourMode = "airline"

	// ColourCorridor paints each aircraft by what it looks to be doing about
	// the corridor airfield: arriving in the IN tube's colour, leaving in the
	// OUT tube's, and anything else, overflights included, muted. It is the
	// strip board's DIR column read straight off the picture. With no field
	// placed every aircraft is transit and the whole fleet draws muted.
	ColourCorridor ColourMode = "corridor"
)

// ErrColour is returned for a --colour value that is none of the spellings.
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
	case ColourCorridor:
		return ColourCorridor, nil
	default:
		return ColourAltitude, fmt.Errorf("%w: %q", ErrColour, text)
	}
}

// Next cycles to the next mode: altitude, airline, corridor and back.
// Anything that is not one of the last two moves to airline, so the zero
// value of ColourMode cycles the way ColourAltitude does and never has to be
// special-cased where one is read.
func (m ColourMode) Next() ColourMode {
	switch m {
	case ColourAirline:
		return ColourCorridor
	case ColourCorridor:
		return ColourAltitude
	case ColourAltitude:
		fallthrough
	default:
		return ColourAirline
	}
}

// BandFilter is the filter as --filter and the control port spell it.
//
// It names the values f walks in altitude colouring and in corridor
// colouring: the three bands, the ceiling under the high band and the three
// track classes. The ceiling belongs to both cycles. The operator values
// airline colouring offers have no spelling, because they are read off the
// legend on screen: an operator that is not on the field has no slot to be,
// and a setting that could name one would be a setting that hides the whole
// fleet on a quiet afternoon.
//
// A spelling only means something in the colouring whose cycle holds it.
// Handed to Apply beside any other colour mode it reads as FilterAll.
type BandFilter string

// The spellings, the altitude cycle's in the order f walks them and then the
// corridor cycle's.
const (
	// FilterAll draws every aircraft, which is where a run starts.
	FilterAll BandFilter = "all"

	// FilterLow keeps the band under 10,000 feet, the cap's <10K.
	FilterLow BandFilter = "low"

	// FilterMid keeps 10,000 to 25,000 feet, the cap's 10-25K.
	FilterMid BandFilter = "mid"

	// FilterHigh keeps 25,000 feet and up, the cap's >25K.
	FilterHigh BandFilter = "high"

	// FilterBelowHigh keeps the low and the mid band together, the cap's
	// <25K. It is the one that hides the overflights.
	FilterBelowHigh BandFilter = "below-high"

	// FilterIn keeps the aircraft that look to be arriving at the corridor
	// airfield, the cap's IN.
	FilterIn BandFilter = "in"

	// FilterOut keeps the ones that look to be leaving it, the cap's OUT.
	FilterOut BandFilter = "out"

	// FilterOther keeps everything that is neither, overflights and anything
	// the classifier cannot place, the cap's OTHER.
	FilterOther BandFilter = "other"
)

// ErrFilter is returned for a --filter value that is none of the spellings.
var ErrFilter = errors.New("radar: unknown filter")

// ParseFilter turns a --filter value into a BandFilter.
//
// The match is case sensitive, for the reason ParseColour gives.
func ParseFilter(text string) (BandFilter, error) {
	switch filter := BandFilter(text); filter {
	case FilterAll, FilterLow, FilterMid, FilterHigh, FilterBelowHigh, FilterIn, FilterOut, FilterOther:
		return filter, nil
	default:
		return FilterAll, fmt.Errorf("%w: %q", ErrFilter, text)
	}
}

// state is the scene's own value for the spelling. Anything that is not one
// of the narrowing spellings reads as ALL, so the zero value needs no case of
// its own.
func (f BandFilter) state() filterState {
	switch f {
	case FilterLow:
		return filterState{kind: filterBand, band: bandLow}
	case FilterMid:
		return filterState{kind: filterBand, band: bandMid}
	case FilterHigh:
		return filterState{kind: filterBand, band: bandHigh}
	case FilterBelowHigh:
		return filterState{kind: filterBelow, band: bandMid}
	case FilterIn:
		return filterState{kind: filterClass, class: source.TrackInbound}
	case FilterOut:
		return filterState{kind: filterClass, class: source.TrackOutbound}
	case FilterOther:
		return filterState{kind: filterClass, class: source.TrackTransit}
	case FilterAll:
		fallthrough
	default:
		return filterState{}
	}
}

// bandFilterOf spells a filter value back. A value the spellings do not
// cover, an operator or the airline legend's OTHER, comes back as FilterAll,
// which is only reached by Current in airline mode and is what it reports
// there anyway.
func bandFilterOf(f filterState) BandFilter {
	for _, spelling := range [...]BandFilter{
		FilterLow, FilterMid, FilterHigh, FilterBelowHigh, FilterIn, FilterOut, FilterOther,
	} {
		if spelling.state() == f {
			return spelling
		}
	}

	return FilterAll
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

	// Filter is the filter f cycles while Colour is altitude or corridor.
	// Zero reads as FilterAll. A spelling outside Colour's own cycle reads as
	// FilterAll too. Airline colouring's operator filter has no spelling and
	// is reached from f alone, so Apply ignores this field while Colour is
	// airline.
	Filter BandFilter

	// Airports is whether the airfield markers start on. Zero reads as on,
	// which is the scope at its most useful before anyone touches a key.
	Airports Toggle

	// Shore is whether the coastline starts on. Zero reads as on, the same way
	// Airports does, and it draws nothing at all unless the scene was also
	// handed the data through WithShore.
	Shore Toggle

	// MinimalShore and MinimalAirports are the bare views' own pair, the one m
	// and a flip in minimal and minimal3d. They are plain bools rather than
	// Toggles because their default is off, and a Toggle's zero value reads as
	// on: false here is the bare field those views open on.
	MinimalShore    bool
	MinimalAirports bool

	// MinimalBar is whether the bare views start with their bar across the
	// top, the one h flips there. A plain bool for the reason the pair above
	// is one: off is the default, and a bare view opens bare.
	MinimalBar bool

	// Board is whether the bare 3D view carries the flight strip board in a
	// column down its right quarter, the one j flips there. A plain bool for
	// the reason MinimalBar is one: off is the default, and a bare view opens
	// bare. The full views have their own column and never read it.
	Board bool

	// Lights is whether the aircraft carry their exterior lights: red and
	// green on the wingtips, white on the tail, the strobes and the beacon.
	// A plain bool like the bare views' pair, because the lights start off
	// and a Toggle's zero value reads as on.
	Lights bool

	// RangeNm pins the scope to a range in nautical miles and turns auto range
	// off with it. Zero means nobody asked, which leaves the scope fitting
	// itself to the fleet.
	RangeNm float64

	// View is which of the four pictures the scene starts on. Zero reads as
	// ViewScope, which is where a run begins before anyone presses v.
	View View

	// Exaggerate is how far the 3D view stretches altitude into height. Zero
	// means nobody asked, which leaves DefaultExaggerate in place.
	Exaggerate float64

	// Zoom is how many range increments inside the fit the auto range lands,
	// MinZoom to MaxZoom; negative widens. Zero is the fit itself. It is
	// ignored while RangeNm pins the scope, because there is no fit then to
	// be inside of. Values past the bounds are clamped rather than refused:
	// the flag parser refuses them for the operator, and a library caller
	// gets the nearest zoom the scene allows.
	Zoom int

	// Tilt is the 3D camera's starting elevation in degrees. Zero means
	// nobody asked, which leaves DefaultTilt in place; anything else is
	// clamped to MinTilt to MaxTilt the way the bracket keys clamp it. It is
	// a start value only: [ and ] step from it and nothing puts it back.
	Tilt float64

	// Orbit is whether the 3D camera turns on its own. Zero reads as on,
	// which is how the camera has always started.
	Orbit Toggle

	// Stars is whether the tilted views' night sky carries its stars. Zero
	// reads as on, the way Orbit does, because the stars are what show the
	// orbit turning.
	Stars Toggle

	// Corridors is whether the tracks of lost aircraft are drawn as a long
	// exposure, fading with age: cyan for the ones that ended at the nearest
	// airfield, amber for the ones that began there, grey for the rest. Zero
	// reads as on, the way Stars does, because the tracks are faint enough to
	// leave on and a source with no airfield draws none whatever this says.
	Corridors Toggle

	// Azimuth is where the 3D camera stands, in degrees clockwise from north:
	// the bearing Left and Right step. With Orbit on it is where the turning
	// starts from, and with Orbit off it is where the camera stays. Values
	// outside 0 to 360 are wrapped onto the compass rather than refused, for
	// the reason Zoom gives; NaN and the infinities read as north.
	Azimuth float64

	// Recentre is how often a bare view refits itself on the traffic. Zero is
	// off, which is the scene's own default and keeps both bare views centred
	// on the receiver.
	//
	// --recenter holds DefaultRecentre instead, because a directional antenna
	// puts every contact in one half of the canvas and following them is what
	// the flag is for. The two differ on purpose: a library caller building a
	// Scene gets the plain projection until it asks for the other one, and the
	// command line asks on its behalf.
	Recentre time.Duration
}

// Apply sets the whole block on a scene that is already built, which is how
// the flags reach a scene the run loop built before it read the config, and
// how the control port changes one that is already drawing.
//
// The filter follows the colour mode the block leaves on screen. In altitude
// and corridor colouring it is set from Filter, which describes those filters
// completely, and a spelling the mode's cycle does not hold reads as ALL. A
// filter value belongs to a legend, so a band, a class or an operator does
// not survive a block that changes the colour mode: Current spells it, and
// the new mode's cycle does not hold it. The ceiling under the high band is
// in both the altitude and the corridor cycle and means the same thing in
// each, so it carries across a switch between the two. In airline colouring
// Filter has nothing to say, so the filter goes back to ALL when the block
// changes the colour mode, and a block that keeps airline colouring leaves an
// operator filter alone, so changing the tilt over the control port does not
// undo an f pressed a minute earlier.
//
// Apply(Current()) changes nothing anyone can see, in either colour mode,
// which is what lets the control port read the block back, change one field
// and hand it over whole.
func (s *Scene) Apply(set Settings) {
	s.applyFilter(set.Colour, set.Filter)

	s.colour = set.Colour
	s.airports = set.Airports.On()
	s.shoreOn = set.Shore.On()
	s.minimalShore = set.MinimalShore
	s.minimalAirports = set.MinimalAirports
	s.minimalBar = set.MinimalBar
	s.board = set.Board
	s.lights = set.Lights
	s.shown = set.View
	s.applyRange(set.RangeNm)
	s.applyRecentre(set.Recentre)
	s.applyExaggerate(set.Exaggerate)
	s.zoom = min(max(set.Zoom, MinZoom), MaxZoom)
	s.applyTilt(set.Tilt)
	s.applyOrbit(set.Orbit.On(), set.Azimuth)
	s.stars = set.Stars.On()
	s.corridors = set.Corridors.On()
}

// Current reads the scene's live state back as a settings block: the tilt the
// brackets left, the azimuth the camera is at on the last frame drawn, the
// view v left on screen, and so on. RangeNm is zero while auto range is on and
// the range on screen while it is off.
//
// Handing the result straight back to Apply changes nothing anyone can see.
// Fields whose zero value stands for a default come back spelled out, so a
// caller reading the block does not have to know what the zero means.
//
// Like every other method on Scene it belongs to the goroutine that draws.
func (s *Scene) Current() Settings {
	set := Settings{
		Colour:          s.colour.orDefault(),
		Filter:          s.currentFilter(),
		Airports:        ToggleOf(s.airports),
		Shore:           ToggleOf(s.shoreOn),
		MinimalShore:    s.minimalShore,
		MinimalAirports: s.minimalAirports,
		MinimalBar:      s.minimalBar,
		Board:           s.board,
		Lights:          s.lights,
		View:            s.shown.orDefault(),
		Exaggerate:      s.exaggerate,
		Zoom:            s.zoom,
		Tilt:            s.elevation,
		Recentre:        s.recentre,
		Orbit:           ToggleOf(s.orbiting),
		Azimuth:         s.cameraAzimuth(s.elapsed),
		Stars:           ToggleOf(s.stars),
		Corridors:       ToggleOf(s.corridors),
	}

	if !s.autoRange {
		set.RangeNm = s.scopeRange.GetCurrent()
	}

	return set
}

// applyFilter is Apply's rule for the filter, run before the colour mode
// changes so it can still see the old one.
func (s *Scene) applyFilter(colour ColourMode, filter BandFilter) {
	switch {
	case colour.orDefault() != ColourAirline:
		s.setFilter(colour.admit(filter.state()))
	case s.colour.orDefault() != ColourAirline:
		s.setFilter(filterState{})
	default:
		// Airline colouring kept: the operator f picked stays picked.
	}
}

// admit is the filter value if this mode's f cycle holds it, and ALL if not.
// The ceiling is in the altitude and the corridor cycle and in no other.
// Airline colouring's cycle has no spelled values, so nothing is admitted
// there.
func (m ColourMode) admit(filter filterState) filterState {
	var held bool

	switch filter.kind {
	case filterBelow:
		held = m.orDefault() == ColourAltitude || m == ColourCorridor
	case filterBand:
		held = m.orDefault() == ColourAltitude
	case filterClass:
		held = m == ColourCorridor
	case filterAll, filterOperator, filterOther:
		fallthrough
	default:
		held = false
	}

	if !held {
		return filterState{}
	}

	return filter
}

// currentFilter is the filter as Current reports it: spelled out in altitude
// and corridor colouring, and FilterAll in airline colouring, where the value
// on screen has no spelling and Apply ignores the field.
func (s *Scene) currentFilter() BandFilter {
	if s.colour.orDefault() == ColourAirline {
		return FilterAll
	}

	return bandFilterOf(s.filter)
}

// ToggleOf spells a bool as the Toggle that reads back as it.
//
//nolint:revive // turning a bool into its spelling is the whole job, not control coupling.
func ToggleOf(on bool) Toggle {
	if on {
		return ToggleOn
	}

	return ToggleOff
}

// orDefault spells the zero ColourMode as the mode it reads as.
func (m ColourMode) orDefault() ColourMode {
	if m == "" {
		return ColourAltitude
	}

	return m
}

// applyOrbit sets whether the camera turns and where it stands.
//
// The orbit is rebased on the last frame drawn, the way startOrbit does it, so
// a block whose azimuth came from Current puts the camera where it already is
// and the picture does not jump. A camera that was not turning and is handed
// the azimuth it is at stays put; one that is told to turn sets off from
// there.
func (s *Scene) applyOrbit(turning bool, degrees float64) {
	if math.IsNaN(degrees) || math.IsInf(degrees, 0) {
		degrees = 0
	}

	s.azimuth = wrapDegrees(degrees)
	s.azimuthAt = s.elapsed
	s.orbiting = turning
}

// applyTilt sets where the 3D camera's elevation starts.
//
// Zero is nobody asking, for the reason applyExaggerate gives. NaN reads as
// zero too: it came from a caller rather than from the flag, which refuses it,
// and a NaN elevation would put every projected point at a NaN depth.
func (s *Scene) applyTilt(degrees float64) {
	if math.IsNaN(degrees) || degrees == 0 {
		s.elevation = defaultElevation

		return
	}

	s.elevation = min(max(degrees, minElevation), maxElevation)
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
// trail, its callsign on its own strip and the little model beside it.
func (s *Scene) aircraftColour(plane airplane.Snapshot) color.RGBA {
	return s.contactColour(plane.Altitude, plane.Callsign, s.colourClass(plane))
}

// colourClass is the aircraft's track class when the colour mode reads one,
// and transit otherwise. Classify runs a bearing and a couple of distances,
// and the colour is asked for several times a frame per aircraft, so the
// other two modes do not pay for an answer they ignore.
func (s *Scene) colourClass(plane airplane.Snapshot) source.TrackClass {
	if s.colour != ColourCorridor {
		return source.TrackTransit
	}

	return s.exposure.corridors.Classify(plane)
}

// ghostColour is the colour the trail of a lost aircraft keeps.
//
// It is the same rule live traffic is coloured by, read off the last altitude
// and callsign the aircraft reported and the class the ring gave the track. A
// ghost drawn by some other rule would be a second thing to learn about a
// field that is already showing a track nobody is flying.
func (s *Scene) ghostColour(ghost source.Trail) color.RGBA {
	return s.contactColour(ghost.Altitude, ghost.Callsign, ghost.Class)
}

// contactColour is the rule both of those share: the altitude band, the
// operator's own colour in airline mode, or the track class's in corridor
// mode.
func (s *Scene) contactColour(altitude float64, callsign string, class source.TrackClass) color.RGBA {
	switch s.colour {
	case ColourAirline:
		return s.airlineColour(callsign)
	case ColourCorridor:
		return s.classColour(class)
	case ColourAltitude:
		fallthrough
	default:
		return s.bandColour(altitude)
	}
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
// the list beside it. Corridor mode does the same with the track class.
func (s *Scene) callsignInk(plane airplane.Snapshot) color.RGBA {
	switch s.colour {
	case ColourAirline:
		return s.airlineColour(plane.Callsign)
	case ColourCorridor:
		return s.classColour(s.colourClass(plane))
	case ColourAltitude:
		fallthrough
	default:
		return s.pal.Ink
	}
}
