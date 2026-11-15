package source

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
)

// The invented history the demo's corridors start with.
//
// The demo fleet loses one aircraft ninety seconds in, which is a ghost and
// not a corridor: a route only shows once several tracks have flown it. So the
// first frame seeds the ring with tracks finished over the last two hours
// around the demo's airfield, which is what --png and the pictures in docs/
// show.
const (
	// demoHistoryTracks is how many finished tracks the ring starts with.
	// Seventy spread over six arrival routes, eight departure routes and six
	// airway directions puts at least two tracks on nearly every route, which
	// is what a tube needs before it is drawn as one.
	demoHistoryTracks = 70

	// demoHistorySpan is how far back the oldest of them was lost.
	demoHistorySpan = 2 * time.Hour

	// demoHistorySalt keeps the history's random draws apart from the
	// fleet's, so a change to one does not reshuffle the other.
	demoHistorySalt = 0x5ca1ab1e

	// The fixes are ten seconds apart, uAirwaves' own spacing, at a ground
	// speed for each kind of track: 230 knots on an arrival, 280 on a
	// departure and 440 at cruise.
	demoArrivalStepNm   = 230.0 / 360
	demoDepartureStepNm = 280.0 / 360
	demoTransitStepNm   = 440.0 / 360

	// demoThresholdFt and demoLiftOffFt are where the two ends of a track sit.
	// Neither is zero, which the radar reads as an altitude nobody decoded.
	demoThresholdFt = 150.0
	demoLiftOffFt   = 300.0
)

// The mix of the three, as the share of draws under which each is picked:
// forty-five in a hundred arrivals, thirty departures and the rest overflights,
// about what a busy field shows a receiver near it.
const (
	demoArrivalShare   = 0.45
	demoDepartureShare = 0.75
)

// demoRunway is one runway direction in use: its threshold or lift-off point
// in nautical miles from the airfield's reference point, its course, and how
// often it is the one in use.
type demoRunway struct {
	course  float64
	eastNm  float64
	northNm float64
	share   float64
}

// demoPoint is a fix an invented route is laid through, as a bearing and a
// distance from the airfield.
type demoPoint struct {
	bearing float64
	nm      float64
}

// The runway directions are Schiphol's in a south-westerly: arrivals on 18R
// and 06, departures off 24 and 36L, joined from three initial approach fixes
// and left by four exits, with three airways crossing overhead. Moved to
// another airfield by WithDemoLocation or WithDemoCorridorField they are only
// a plausible layout, which is all a demo needs.
//
//nolint:gochecknoglobals,mnd // a table, read only; the numbers are the layout.
var (
	demoArrivals = [...]demoRunway{
		{course: 183, eastNm: -2.3, northNm: 3.2, share: 0.7},
		{course: 58, eastNm: -1.0, northNm: -1.0, share: 1},
	}
	demoDepartures = [...]demoRunway{
		{course: 238, eastNm: 1.0, northNm: 0.6, share: 0.6},
		{course: 4, eastNm: -2.6, northNm: 1.0, share: 1},
	}
	demoApproachFixes = [...]demoPoint{{42, 40}, {305, 38}, {214, 36}}
	demoExits         = [...]demoPoint{{60, 45}, {100, 50}, {225, 45}, {350, 48}}
	demoAirways       = [...][2]demoPoint{
		{{285, 95}, {105, 95}},
		{{210, 95}, {25, 95}},
		{{65, 95}, {250, 95}},
	}
	demoCallsigns = [...]string{"KLM", "TRA", "EZY", "DLH", "BAW", "AFR", "RYR", "SAS"}
)

// waypoint is a point on an invented route, in nautical miles east and north
// of the airfield, with the altitude the track passes it at.
type waypoint struct {
	east  float64
	north float64
	alt   float64
}

// toward is the point distanceNm along a compass bearing from w, at the same
// altitude.
func (w waypoint) toward(bearing, distanceNm float64) waypoint {
	radians := bearing * math.Pi / halfCircle

	return waypoint{
		east:  w.east + distanceNm*math.Sin(radians),
		north: w.north + distanceNm*math.Cos(radians),
		alt:   w.alt,
	}
}

// at is w at another altitude.
func (w waypoint) at(altitudeFt float64) waypoint {
	w.alt = altitudeFt

	return w
}

// demoHistory invents the tracks, from one seeded sequence.
type demoHistory struct {
	rng    *rand.Rand
	field  Airfield
	cosLat float64
	points []airplane.PositionEntry
}

// seedExposure pushes the invented history into the ghost ring, oldest first,
// which is the order the ring keeps and the order expiry relies on.
//
// It does nothing without an airfield: there is nothing to have arrived at.
func (d *Demo) seedExposure(now time.Time) {
	if !d.ghosts.corridors.placed {
		return
	}

	field := d.ghosts.corridors.field
	history := demoHistory{
		rng:    rand.New(rand.NewPCG(d.seed, d.seed^demoHistorySalt)), //nolint:gosec // a demo, not a key.
		field:  field,
		cosLat: max(math.Cos(field.Latitude*math.Pi/halfCircle), minCosLatitude),

		// One backing array for every track, sized for the longest route each
		// could be, so no track is cut from an array a later one outgrew.
		points: make([]airplane.PositionEntry, 0, demoHistoryTracks*demoHistoryMaxFixes),
	}
	slot := demoHistorySpan / demoHistoryTracks

	for index := range demoHistoryTracks {
		points := history.track()

		// Each track is lost somewhere inside its own slot of the span, so the
		// order they are pushed in is the order of their LostAt.
		lostAt := now.Add(-demoHistorySpan + time.Duration(index)*slot +
			time.Duration(history.rng.Float64()*float64(slot)))

		d.ghosts.push(Trail{
			ICAO:     fmt.Sprintf("%06X", demoHistoryICAO+index),
			Callsign: fmt.Sprintf("%s%d", demoCallsigns[index%len(demoCallsigns)], history.rng.IntN(demoFlightNumbers)),
			Altitude: points[len(points)-1].Altitude,
			Points:   points,
			LostAt:   lostAt,
			Class:    d.ghosts.corridors.classify(points),
		})
	}
}

const (
	// demoHistoryICAO is the first invented address. The fleet's own are all
	// in the 3 and 4 blocks, so these can never revive one of its ghosts.
	demoHistoryICAO = 0xD00000

	// demoFlightNumbers bounds the number after the callsign prefix.
	demoFlightNumbers = 9000

	// demoHistoryMaxFixes is the most fixes a track keeps, uAirwaves' own
	// history length. The longest invented route comes to about 160, so the
	// cut only matters if the layout grows.
	demoHistoryMaxFixes = 256
)

// track invents one route, lays fixes along it and keeps the newest
// demoHistoryMaxFixes of them, as uAirwaves would.
func (h *demoHistory) track() []airplane.PositionEntry {
	start := len(h.points)
	draw := h.rng.Float64()

	switch {
	case draw < demoArrivalShare:
		h.arrival()
	case draw < demoDepartureShare:
		h.departure()
	default:
		h.overflight()
	}

	start = max(start, len(h.points)-demoHistoryMaxFixes)

	return h.points[start:len(h.points):len(h.points)]
}

// runway picks the runway in use from a list by the shares it carries.
func (h *demoHistory) runway(runways []demoRunway) *demoRunway {
	draw := h.rng.Float64()

	for index := range runways[:len(runways)-1] {
		if draw < runways[index].share {
			return &runways[index]
		}
	}

	return &runways[len(runways)-1]
}

// Where the invented routes start, end and fly, each as the lowest value and
// the span above it a seeded draw may add, or the spread a normal draw is
// scaled by.
const (
	// An arrival enters the picture 68 to 76 miles out at FL260 to FL340,
	// crosses its initial approach fix at FL70 to FL100, turns base at 3,000
	// feet seven miles abeam a point fifteen miles out, and joins the final
	// eleven miles out at the same height.
	demoEntryNm      = 68.0
	demoEntrySpanNm  = 8.0
	demoEntryFt      = 26000.0
	demoEntrySpanFt  = 8000.0
	demoFixFt        = 7000.0
	demoFixSpanFt    = 3000.0
	demoBaseFt       = 3000.0
	demoBaseOutNm    = 15.0
	demoBaseAbeamNm  = 7.0
	demoFinalFt      = 3000.0
	demoFinalNm      = 11.0
	demoEntryJitter  = 4.0
	demoFixJitterNm  = 2.5
	demoBaseJitterNm = 1.0
	demoLineJitterNm = 0.15

	// A departure climbs out five miles on the runway heading to 2,500 feet,
	// turns thirteen miles out at 7,000 feet towards its exit, crosses it at
	// FL190 to FL240 and leaves the picture 78 miles out at FL290 to FL360.
	demoClimbOutNm    = 5.0
	demoClimbOutFt    = 2500.0
	demoTurnNm        = 13.0
	demoTurnFt        = 7000.0
	demoTurnShare     = 0.6
	demoExitFt        = 19000.0
	demoExitSpanFt    = 5000.0
	demoLeaveNm       = 78.0
	demoCruiseFt      = 29000.0
	demoCruiseSpanFt  = 7000.0
	demoExitJitterNm  = 3.0
	demoLeaveJitterDg = 4.0

	// An overflight keeps to its airway within a couple of miles at one of
	// five levels from FL290 up, and one in four is descending eight thousand
	// feet across the picture towards somewhere else.
	demoAirwayJitterNm = 2.5
	demoAirwayLevels   = 5
	demoAirwayLevelFt  = 2000.0
	demoDescentShare   = 0.25
	demoDescentFt      = 8000.0

	// quarterTurn is a right angle in degrees.
	quarterTurn = 90.0
)

// arrival comes in from the edge of the picture over one of the initial
// approach fixes, turns base on the side that fix is on and lands on the
// runway in use. Every arrival on a runway converges on the same final, which
// is what makes its route a cone.
func (h *demoHistory) arrival() {
	runway := h.runway(demoArrivals[:])
	fix := demoApproachFixes[h.rng.IntN(len(demoApproachFixes))]
	threshold := waypoint{east: runway.eastNm, north: runway.northNm, alt: demoThresholdFt}
	back := reciprocal(runway.course)

	side := quarterTurn
	if angleBetween(fix.bearing, runway.course) < 0 {
		side = -quarterTurn
	}

	entry := waypoint{}.toward(fix.bearing+h.rng.NormFloat64()*demoEntryJitter,
		h.between(demoEntryNm, demoEntrySpanNm)).at(h.between(demoEntryFt, demoEntrySpanFt))
	approach := h.jitter(waypoint{}.toward(fix.bearing, fix.nm), demoFixJitterNm).
		at(h.between(demoFixFt, demoFixSpanFt))
	base := h.jitter(threshold.toward(back, demoBaseOutNm).toward(runway.course+side, demoBaseAbeamNm),
		demoBaseJitterNm).at(demoBaseFt)
	final := h.jitter(threshold.toward(back, demoFinalNm), demoLineJitterNm).at(demoFinalFt)

	h.walk([]waypoint{entry, approach, base, final, threshold}, demoArrivalStepNm)
}

// departure lifts off the runway in use, climbs out on its heading, turns
// part of the way towards one of the exits and leaves the picture through it.
func (h *demoHistory) departure() {
	runway := h.runway(demoDepartures[:])
	exit := demoExits[h.rng.IntN(len(demoExits))]
	liftOff := waypoint{east: runway.eastNm, north: runway.northNm, alt: demoLiftOffFt}

	climb := h.jitter(liftOff.toward(runway.course, demoClimbOutNm), demoLineJitterNm).at(demoClimbOutFt)
	turn := h.jitter(waypoint{}.toward(runway.course+demoTurnShare*angleBetween(exit.bearing, runway.course),
		demoTurnNm), demoBaseJitterNm).at(demoTurnFt)
	out := h.jitter(waypoint{}.toward(exit.bearing, exit.nm), demoExitJitterNm).
		at(h.between(demoExitFt, demoExitSpanFt))
	leave := waypoint{}.toward(exit.bearing+h.rng.NormFloat64()*demoLeaveJitterDg, demoLeaveNm).
		at(h.between(demoCruiseFt, demoCruiseSpanFt))

	h.walk([]waypoint{liftOff, climb, turn, out, leave}, demoDepartureStepNm)
}

// overflight crosses the picture along one of the airways, in either
// direction, a normal draw of a couple of miles off its centreline.
func (h *demoHistory) overflight() {
	airway := demoAirways[h.rng.IntN(len(demoAirways))]
	origin, destination := airway[0], airway[1]

	if h.rng.Float64() < 1.0/2 {
		origin, destination = destination, origin
	}

	start := waypoint{}.toward(origin.bearing, origin.nm)
	end := waypoint{}.toward(destination.bearing, destination.nm)
	course := bearingDeg(end.east-start.east, end.north-start.north)
	offNm := h.rng.NormFloat64() * demoAirwayJitterNm
	levelFt := demoCruiseFt + float64(h.rng.IntN(demoAirwayLevels))*demoAirwayLevelFt

	endFt := levelFt
	if h.rng.Float64() < demoDescentShare {
		endFt -= demoDescentFt
	}

	h.walk([]waypoint{
		start.toward(course+quarterTurn, offNm).at(levelFt),
		end.toward(course+quarterTurn, offNm).at(endFt),
	}, demoTransitStepNm)
}

// between is a seeded draw somewhere in the span above from.
func (h *demoHistory) between(from, span float64) float64 {
	return from + h.rng.Float64()*span
}

// jitter moves a point a normal draw of up to about spreadNm in a uniformly
// drawn direction.
func (h *demoHistory) jitter(point waypoint, spreadNm float64) waypoint {
	return point.toward(h.between(0, degreesPerCircle), math.Abs(h.rng.NormFloat64())*spreadNm)
}

// walk lays a fix every stepNm along a route, its altitude interpolated
// between the waypoints either side. The last waypoint always gets a fix of
// its own, so an arrival ends on its threshold and not a fraction of a step
// short of it.
func (h *demoHistory) walk(route []waypoint, stepNm float64) {
	carry := 0.0

	for leg := 1; leg < len(route); leg++ {
		start, end := route[leg-1], route[leg]
		legNm := math.Hypot(end.east-start.east, end.north-start.north)

		along := carry
		for ; along < legNm; along += stepNm {
			share := along / legNm
			h.fix(waypoint{
				east:  start.east + (end.east-start.east)*share,
				north: start.north + (end.north-start.north)*share,
				alt:   start.alt + (end.alt-start.alt)*share,
			})
		}

		carry = along - legNm
	}

	h.fix(route[len(route)-1])
}

// fix appends one fix at a route point, turned back into degrees around the
// airfield on the same local projection the radar draws with.
func (h *demoHistory) fix(at waypoint) {
	h.points = append(h.points, airplane.PositionEntry{
		Latitude:  h.field.Latitude + at.north/nmPerDegree,
		Longitude: wrapLongitude(h.field.Longitude + at.east/(nmPerDegree*h.cosLat)),
		Altitude:  at.alt,
	})
}
