package source

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
)

// The invented high overflights WithDemoAirways adds.
//
// They come from a sequence of their own, so the seventy tracks the default
// demo invents are drawn exactly as before and every still without the
// option stays byte for byte what it was.
const (
	demoAirwaySalt   = 0xa1e5a1e5
	demoAirwayICAO   = demoHistoryICAO + 0x1000
	demoAirwaySideNm = 1.5
	demoStrayTracks  = 4
	demoStrayNm      = 90.0
	demoStrayFt      = 33000.0
	demoStraySpanFt  = 6000.0

	// demoStrayTurnDeg and demoStrayTurnSpanDeg are how far a stray's exit
	// lies off the reciprocal of its entry, so it crosses the picture at an
	// angle rather than through the receiver.
	demoStrayTurnDeg     = 30.0
	demoStrayTurnSpanDeg = 60.0
)

// demoAirway is one invented airway: where it enters and leaves the picture,
// the flight levels its traffic keeps and how many tracks flew it.
type demoAirway struct {
	from, to demoPoint
	levels   []float64
	tracks   int
}

// Five airways at FL300 to FL400 and different bearings. The second and the
// fourth pass close over the field and cross the arrival and departure tubes;
// the first and the fifth are one line flown both ways at different levels.
//
//nolint:gochecknoglobals,mnd // a table, read only; the numbers are the layout.
var demoHighAirways = [...]demoAirway{
	{from: demoPoint{300, 95}, to: demoPoint{75, 85}, levels: []float64{36000, 38000}, tracks: 10},
	{from: demoPoint{165, 95}, to: demoPoint{335, 90}, levels: []float64{34000}, tracks: 7},
	{from: demoPoint{235, 95}, to: demoPoint{125, 90}, levels: []float64{38000, 40000}, tracks: 5},
	{from: demoPoint{20, 95}, to: demoPoint{195, 95}, levels: []float64{30000, 32000}, tracks: 4},
	{from: demoPoint{75, 85}, to: demoPoint{300, 95}, levels: []float64{35000, 37000}, tracks: 6},
}

// overflights invents the airways' tracks and a few strays, each lost at a
// random moment in the last two hours.
func (d *Demo) overflights(now time.Time, field Airfield) []Trail {
	total := demoStrayTracks
	for _, airway := range demoHighAirways {
		total += airway.tracks
	}

	history := demoHistory{
		rng:    rand.New(rand.NewPCG(d.seed, d.seed^demoAirwaySalt)), //nolint:gosec // a demo, not a key.
		field:  field,
		cosLat: max(math.Cos(field.Latitude*math.Pi/halfCircle), minCosLatitude),
		points: make([]airplane.PositionEntry, 0, total*demoHistoryMaxFixes),
	}
	trails := make([]Trail, 0, total)

	for _, airway := range demoHighAirways {
		for range airway.tracks {
			trails = append(trails, history.trail(len(trails), now, func() {
				history.highOverflight(airway)
			}, d))
		}
	}

	for range demoStrayTracks {
		trails = append(trails, history.trail(len(trails), now, history.stray, d))
	}

	return trails
}

// trail wraps one invented track as a lost Trail.
func (h *demoHistory) trail(index int, now time.Time, invent func(), demo *Demo) Trail {
	start := len(h.points)

	invent()

	start = max(start, len(h.points)-demoHistoryMaxFixes)
	points := h.points[start:len(h.points):len(h.points)]

	return Trail{
		ICAO:     fmt.Sprintf("%06X", demoAirwayICAO+index),
		Callsign: fmt.Sprintf("%s%d", demoCallsigns[index%len(demoCallsigns)], h.rng.IntN(demoFlightNumbers)),
		Altitude: points[len(points)-1].Altitude,
		Points:   points,
		LostAt:   now.Add(-time.Duration(h.rng.Float64() * float64(demoHistorySpan))),
		Class:    demo.ghosts.corridors.classify(points),
	}
}

// highOverflight flies one track along an airway at one of its levels, each
// end a normal draw of a mile or two to one side of the airway's own. Drawing
// the two ends apart gives the course a degree or so of scatter as well; a
// course scattered by several degrees on its own would put the far end of a
// 180-mile track ten miles off, which is a direct, not an airway.
func (h *demoHistory) highOverflight(airway demoAirway) {
	from := waypoint{}.toward(airway.from.bearing, airway.from.nm)
	to := waypoint{}.toward(airway.to.bearing, airway.to.nm)
	course := bearingDeg(to.east-from.east, to.north-from.north)
	level := airway.levels[h.rng.IntN(len(airway.levels))]

	start := from.toward(course+quarterTurn, h.rng.NormFloat64()*demoAirwaySideNm).at(level)
	end := to.toward(course+quarterTurn, h.rng.NormFloat64()*demoAirwaySideNm).at(level)

	h.walk([]waypoint{start, end}, demoTransitStepNm)
}

// stray crosses the picture high on a bearing of its own, which no airway
// should claim.
func (h *demoHistory) stray() {
	bearing := h.between(0, degreesPerCircle)
	start := waypoint{}.toward(bearing, demoStrayNm).at(h.between(demoStrayFt, demoStraySpanFt))
	exit := bearing + halfCircle - h.between(demoStrayTurnDeg, demoStrayTurnSpanDeg)
	end := waypoint{}.toward(exit, demoStrayNm).at(start.alt)

	h.walk([]waypoint{start, end}, demoTransitStepNm)
}

// mergeByLoss puts both sets of trails in the order they were lost, which is
// the order the ring keeps. A stable sort keeps the default history's own
// order among itself.
func (*Demo) mergeByLoss(history, extra []Trail) []Trail {
	merged := slices.Concat(history, extra)
	slices.SortStableFunc(merged, func(left, right Trail) int { return left.LostAt.Compare(right.LostAt) })

	return merged
}
