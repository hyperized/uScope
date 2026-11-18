package source

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airports"
)

// TrackClass is what a finished track did relative to the airfield: landed
// there, took off from there, or neither.
type TrackClass uint8

// The three classes. Transit is the zero value, because a track classified
// with no airfield to measure against has done nothing relative to one.
const (
	// TrackTransit is a track that neither began nor ended near the field.
	TrackTransit TrackClass = iota

	// TrackInbound is a track whose last fix is near the field.
	TrackInbound

	// TrackOutbound is a track whose first fix is near the field and whose
	// last fix is not.
	TrackOutbound
)

const (
	// DefaultCorridorHalfLife is how long a lost track takes to count for half
	// as much, and how long a route goes on being drawn once its traffic has
	// stopped. Two hours holds a runway in use through the gaps between its
	// flights and clears one the wind has turned the traffic off within two
	// hours of its last arrival.
	DefaultCorridorHalfLife = 2 * time.Hour

	// corridorFieldNm is how close to the field a track has to end to count as
	// an arrival, or begin to count as a departure. Ten miles takes in a
	// final approach from the outer marker and a departure until its first
	// turn, and stops short of the next airfield over in most of Europe.
	corridorFieldNm = 10.0

	// corridorExposureLives is how many half-lives a lost track is kept for.
	// After six it is drawn at a sixty-fourth of the strongest tint, which is
	// under the radar's drawing threshold, so keeping it any longer would
	// spend memory on something nobody can see.
	corridorExposureLives = 6

	// corridorExpireEvery is how often the ghost ring is checked for tracks
	// that have aged out. Expiry walks from the oldest end and stops at the
	// first track still young enough, so a check costs next to nothing; the
	// cadence is there so the frame path does not even do that much thirty
	// times a second for a fade measured in hours.
	corridorExpireEvery = time.Minute

	// corridorFieldReachNm is how far the receiver may be from the nearest
	// field for that field to be chosen. Past it the antenna cannot hear the
	// approaches, and every track would come out as transit.
	corridorFieldReachNm = 150.0

	// corridorEstimateDoubtNm is the widest self-locate doubt radius the
	// field may be picked from. Major airfields in the Netherlands are
	// twenty-odd miles apart, so an estimate that has settled to ten miles
	// names the same one a fix would; a looser one waits.
	corridorEstimateDoubtNm = 10.0

	// corridorMinHalfLife and corridorMaxHalfLife bound the half-life. Under
	// ten minutes a track fades before the next arrival has followed it down;
	// past a day nothing ever fades and a runway change never shows.
	corridorMinHalfLife = 10 * time.Minute
	corridorMaxHalfLife = 24 * time.Hour
)

// ErrUnknownAirfield is returned by NewLive and NewDemo for a pinned airfield
// that is not in the embedded airport list.
var ErrUnknownAirfield = errors.New("source: unknown airfield")

// ErrCorridorHalfLife is returned for a corridor half-life outside the range
// the fade can work at.
var ErrCorridorHalfLife = errors.New("source: not a corridor half-life")

// Airfield is the fixed point a lost track is classified against.
type Airfield struct {
	ICAO      string
	Latitude  float64
	Longitude float64
}

// Corridors is what a frame tells the radar about the corridor exposure: the
// airfield the tracks were classified against and how fast they fade.
//
// The tracks themselves are the frame's Ghosts. A zero Field, which is all
// Empty ever carries, means no airfield has been pinned or found yet, and the
// radar draws nothing until there is one.
type Corridors struct {
	Field    Airfield
	HalfLife time.Duration
}

// Placed reports whether there is an airfield to have classified against.
func (c Corridors) Placed() bool { return c.Field.ICAO != "" }

// corridors is a source's half of the exposure: the airfield, once known, and
// the half-life. It lives on the ghost ring, which classifies a track by it
// when the track is lost and ages it out by the half-life.
type corridors struct {
	field    Airfield
	placed   bool
	halfLife time.Duration
}

// newCorridors settles the airfield. An empty icao leaves it to be found from
// the receiver; anything else has to be in the airport list.
func newCorridors(halfLife time.Duration, icao string) (corridors, error) {
	exposure := corridors{halfLife: halfLife}

	if icao == "" {
		return exposure, nil
	}

	field, ok := lookupAirfield(icao)
	if !ok {
		return corridors{}, fmt.Errorf("%w: %q", ErrUnknownAirfield, icao)
	}

	exposure.field, exposure.placed = field, true

	return exposure, nil
}

// CheckCorridorHalfLife reports whether d is a half-life the fade can work
// at, returning an error wrapping ErrCorridorHalfLife when it is not.
func CheckCorridorHalfLife(d time.Duration) error {
	if d < corridorMinHalfLife || d > corridorMaxHalfLife {
		return fmt.Errorf("%w: got %s, want %s to %s",
			ErrCorridorHalfLife, d, corridorMinHalfLife, corridorMaxHalfLife)
	}

	return nil
}

// ParseCorridorHalfLife reads a half-life in Go's duration syntax, such as
// 2h or 90m. Zero is refused along with everything else out of range: a
// track that fades instantly is not a setting anybody means.
func ParseCorridorHalfLife(text string) (time.Duration, error) {
	halfLife, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a duration", ErrCorridorHalfLife, text)
	}

	if err := CheckCorridorHalfLife(halfLife); err != nil {
		return 0, err
	}

	return halfLife, nil
}

// public is the exposure as a Frame carries it.
func (c *corridors) public() Corridors {
	return Corridors{Field: c.field, HalfLife: c.halfLife}
}

// resolve picks the field from the receiver the first time the receiver's
// position is trusted, and reports whether there is a field.
//
// A fresh estimate is not good enough: it can be tens of miles out, which is
// the difference between two airfields in most of Europe, and a field picked
// wrongly stays picked for the rest of the run. One that has settled inside
// corridorEstimateDoubtNm is, and it has to be, because a receiver with no
// GPS and no --lat is on an estimate for the whole run.
func (c *corridors) resolve(receiver Receiver) bool {
	if c.placed || !receiver.placed() {
		return c.placed
	}

	field, ok := nearestAirfield(receiver.Latitude, receiver.Longitude)
	if !ok {
		return false
	}

	c.field, c.placed = field, true

	return true
}

// classify decides what a finished track did, from its two ends.
//
// It used to be done fix by fix from each fix's heading against the bearing
// to the field, which got the easy cases right and the rest wrong: an arrival
// on a downwind leg points away from the runway it is about to land on, and a
// departure turning back over the field points at it. A track that is over
// has an answer to the question the heading was guessing at. If it ended near
// the field the aircraft landed there, or at least went below the antenna's
// horizon on the way down, and if it began there it took off. Ending is
// tested first, so a circuit that starts and finishes at the field counts as
// the arrival it ends in.
//
// uAirwaves keeps the newest 256 fixes of a track, about forty minutes, so a
// departure whose first fixes have been dropped reads as transit. By then it
// is a few hundred miles out and well past anything the antenna can still
// hear, so the case does not come up on a real feed.
func (c *corridors) classify(points []airplane.PositionEntry) TrackClass {
	if !c.placed || len(points) == 0 {
		return TrackTransit
	}

	if c.near(points[len(points)-1]) {
		return TrackInbound
	}

	if c.near(points[0]) {
		return TrackOutbound
	}

	return TrackTransit
}

// near reports whether a fix is within corridorFieldNm of the field, on the
// same local equirectangular distance the radar draws with.
func (c *corridors) near(fix airplane.PositionEntry) bool {
	return c.field.distanceNm(fix.Latitude, fix.Longitude) <= corridorFieldNm
}

// distanceNm is the distance from the field to a position, on the local
// equirectangular frame at the field's latitude.
func (f Airfield) distanceNm(latitude, longitude float64) float64 {
	cosLat := math.Cos(f.Latitude * math.Pi / halfCircle)
	eastNm := wrapLongitude(longitude-f.Longitude) * nmPerDegree * cosLat
	northNm := (latitude - f.Latitude) * nmPerDegree

	return math.Hypot(eastNm, northNm)
}

// bearingFrom is the compass bearing in degrees from a position to the field,
// in the same frame as distanceNm.
func (f Airfield) bearingFrom(latitude, longitude float64) float64 {
	cosLat := math.Cos(f.Latitude * math.Pi / halfCircle)
	eastNm := wrapLongitude(f.Longitude-longitude) * nmPerDegree * cosLat
	northNm := (f.Latitude - latitude) * nmPerDegree

	return math.Atan2(eastNm, northNm) * halfCircle / math.Pi
}

const (
	// inboundConeDeg is how far off the field's bearing an aircraft may point
	// and still read as heading for it. A flight lined up for an approach or
	// vectored towards the field points within a few tens of degrees of it;
	// any wider and a crossing airway starts to read as inbound.
	inboundConeDeg = 30.0

	// outboundConeDeg is the angle past which an aircraft points almost
	// directly away from the field.
	outboundConeDeg = 150.0

	// terminalAltitudeFt is the height below which traffic near a major field
	// is arriving or departing. Below FL100 in Europe the speed limit marks the
	// terminal area, so a level aircraft pointed at the field is on its way in.
	terminalAltitudeFt = 10000.0

	// climbRateFpm is the vertical rate that counts as a climb or a descent.
	// uAirwaves treats anything inside plus or minus 300 ft/min as level
	// flight, and this keeps the two ideas of level the same.
	climbRateFpm = 300.0

	// terminalRadiusNm is how far from the field the terminal-area rule in
	// Classify reaches. Thirty miles holds a downwind and a base leg at a
	// field the size of Schiphol, which is where the cones lose arrivals: a
	// downwind points away from the field and a base leg across it.
	terminalRadiusNm = 30.0

	// fullTurnDeg is a full circle, for wrapping an angle.
	fullTurnDeg = 360.0
)

// angleDiffDeg is the absolute angular difference between two compass angles,
// wrapped into 0 to 180.
func angleDiffDeg(a, b float64) float64 {
	diff := math.Mod(math.Abs(a-b), fullTurnDeg)
	if diff > halfCircle {
		diff = fullTurnDeg - diff
	}

	return diff
}

// Classify predicts what a live aircraft is doing relative to the field: on
// its way in, on its way out, or passing through. It is a guess from the
// aircraft's present state, where classify judges a finished track from its
// ends. Without a placed field, a position or a decoded heading the answer is
// TrackTransit. A history that began near the field and now lies outside it
// is a departure, which is the same ends test classify applies, run on the
// history so far. Below terminalAltitudeFt and within terminalRadiusNm of the
// field, a descent is an arrival and a climb a departure whatever the
// heading. Otherwise the heading against the bearing to the field
// decides, with altitude and vertical rate separating a flight in the
// terminal area from one overhead. Classify allocates nothing and is safe for
// concurrent use.
func (c Corridors) Classify(plane airplane.Snapshot) TrackClass {
	if !c.Placed() || (plane.Latitude == 0 && plane.Longitude == 0) || plane.Heading < 0 {
		return TrackTransit
	}

	if c.leftField(plane.PositionHistory) {
		return TrackOutbound
	}

	if class, decided := c.terminal(plane); decided {
		return class
	}

	off := c.headingOff(plane)

	if off <= inboundConeDeg && (plane.Altitude < terminalAltitudeFt || plane.VertRate <= -climbRateFpm) {
		return TrackInbound
	}

	if off > outboundConeDeg && plane.VertRate >= climbRateFpm {
		return TrackOutbound
	}

	return TrackTransit
}

// terminal is the terminal-area rule: below terminalAltitudeFt and within
// terminalRadiusNm of the field, a descent is inbound and a climb is outbound
// whatever the heading. Nothing low near a busy field descends without
// meaning to land there, and the heading is what misleads on a downwind or a
// base leg. Level flight reports false and is left to the cones, because a
// level aircraft in the terminal area may be holding, crossing or vectored,
// and the heading is the only thing left to read.
func (c Corridors) terminal(plane airplane.Snapshot) (TrackClass, bool) {
	if plane.Altitude >= terminalAltitudeFt ||
		c.Field.distanceNm(plane.Latitude, plane.Longitude) > terminalRadiusNm {
		return TrackTransit, false
	}

	switch {
	case plane.VertRate <= -climbRateFpm:
		return TrackInbound, true
	case plane.VertRate >= climbRateFpm:
		return TrackOutbound, true
	default:
		return TrackTransit, false
	}
}

// leftField reports whether a history began within corridorFieldNm of the
// field and its newest fix is no longer there.
func (c Corridors) leftField(history []airplane.PositionEntry) bool {
	if len(history) == 0 {
		return false
	}

	first, last := history[0], history[len(history)-1]

	return c.Field.distanceNm(first.Latitude, first.Longitude) <= corridorFieldNm &&
		c.Field.distanceNm(last.Latitude, last.Longitude) > corridorFieldNm
}

// headingOff is how far, in degrees from 0 to 180, the aircraft's heading is
// from the bearing to the field.
func (c Corridors) headingOff(plane airplane.Snapshot) float64 {
	return angleDiffDeg(c.Field.bearingFrom(plane.Latitude, plane.Longitude), plane.Heading)
}

// placed reports whether the receiver's position is good enough to pick an
// airfield from: a fix, or an estimate whose doubt radius has come inside
// corridorEstimateDoubtNm. An estimate with no radius at all is a locator
// that has not measured anything yet.
func (r Receiver) placed() bool {
	if r.HasFix {
		return true
	}

	doubt := r.DoubtNm()

	return r.Mode == FixEstimated && doubt > 0 && doubt <= corridorEstimateDoubtNm
}

// nearestAirfield is the airport in the embedded list closest to a position,
// on the same equirectangular distance near uses, and false when none is
// within corridorFieldReachNm.
func nearestAirfield(latitude, longitude float64) (Airfield, bool) {
	cosLat := math.Cos(latitude * math.Pi / halfCircle)
	bestNm := corridorFieldReachNm

	var (
		best  Airfield
		found bool
	)

	for _, airport := range airports.All() {
		eastNm := wrapLongitude(airport.Longitude-longitude) * nmPerDegree * cosLat
		northNm := (airport.Latitude - latitude) * nmPerDegree

		distanceNm := math.Hypot(eastNm, northNm)
		if distanceNm <= bestNm {
			best = Airfield{ICAO: airport.ICAO, Latitude: airport.Latitude, Longitude: airport.Longitude}
			bestNm, found = distanceNm, true
		}
	}

	return best, found
}

// lookupAirfield finds an airport by ICAO code, matched exactly against the
// upper-case codes the list stores.
func lookupAirfield(icao string) (Airfield, bool) {
	for _, airport := range airports.All() {
		if airport.ICAO == icao {
			return Airfield{ICAO: airport.ICAO, Latitude: airport.Latitude, Longitude: airport.Longitude}, true
		}
	}

	return Airfield{}, false
}
