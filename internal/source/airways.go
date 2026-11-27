package source

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
)

// How lost overflights are bundled into airways.
//
// The routes bundle arrivals and departures by stations counted out from the
// field, which fits traffic that starts or ends there and does not fit traffic
// that only passes over. An airway has no end at the field, so a track is cut
// into near-straight legs instead, each leg is described by its direction of
// travel and where it lies across that direction, and legs that agree on both
// are bundled.
//
// Opposite directions are kept apart. Over Europe the two directions of one
// airway are flown at different levels (the semicircular rule), so merging
// them would give a mean altitude nobody flies.
const (
	// AirwayFloorFt is the altitude a fix has to be at or above to count
	// towards an airway: the bottom of the radar's high band. Under it the
	// traffic is climbing out or descending in, which the routes draw.
	AirwayFloorFt = 25000.0

	// airwayTurnDeg is how far a segment may point away from the leg it
	// extends before it starts a new leg. Airways bend at their waypoints by
	// ten to twenty degrees; eight cuts there and not in the few degrees of
	// noise a 1.2 nm cruise segment carries.
	airwayTurnDeg = 8.0

	// airwaySettleNm is how long a leg runs before a turn can cut it. Before
	// that the leg's own direction rests on one or two segments and is no
	// steadier than the segment it would be compared with.
	airwaySettleNm = 3.0

	// airwayMinLegNm is the shortest leg that counts. A shorter one is a turn
	// or a track clipped at the edge of coverage, and its direction says
	// little about which airway it was on.
	airwayMinLegNm = 20.0

	// airwayBearingDeg and airwayOffsetNm are how close a leg's direction and
	// its midpoint's distance from an airway's line must be to join it. Real
	// traffic on one airway sits within a mile or two of the centreline, and
	// two parallel airways are rarely closer than ten miles.
	airwayBearingDeg = 8.0
	airwayOffsetNm   = 4.0

	// airwayMinTracks is the fewest tracks an airway needs to be drawn, the
	// same three a route needs.
	airwayMinTracks = 3

	// airwayMaxBundles is how many candidate bundles one compute may open.
	// Most hold one or two stray tracks and are never drawn; the cap only
	// bounds the work and the memory.
	airwayMaxBundles = 128

	// airwayMax is how many airways a frame carries at most.
	airwayMax = 32

	// airwayFlatDet is the determinant under which the members' ends are
	// taken to sit at one place along the line, so the fit has no slope to
	// find and keeps the first leg's direction.
	airwayFlatDet = 1e-9
)

// Airway is one bundle of overflights at cruise: a straight centreline from
// where the traffic enters to where it leaves, and the members' mean
// altitude.
type Airway struct {
	// FromLatitude and FromLongitude are the upstream end, ToLatitude and
	// ToLongitude the downstream one: the traffic flies from From to To.
	FromLatitude  float64
	FromLongitude float64
	ToLatitude    float64
	ToLongitude   float64

	// Altitude is the members' weighted mean in feet.
	Altitude float64

	// Tracks is how many lost tracks flew it.
	Tracks int

	// Strength is from 0 to 1 and fades the way a route's does: the members'
	// weights over three fresh tracks, times how recently it was last flown.
	Strength float64
}

// airwayHead is one bundle while it is being built: the line of its first
// leg, which later legs are matched against and measured in, and weighted
// sums over its members.
//
// The line does not move as members join, for the reason a route's signature
// does not: a line that followed its members could creep sideways onto the
// next airway over.
type airwayHead struct {
	originX, originY float64
	dirX, dirY       float64
	bearing          float64

	// Over every member's two ends: weight, along-track t, across-track o.
	w, wt, wtt, wo, wto float64

	// Over every member leg: weight, start and end along the line, altitude.
	lw, lt0, lt1, la float64

	weight    float64
	tracks    int
	lastTrail int
	newest    time.Time
}

// flatPoint is a place in a fieldFrame, in nautical miles east and north of
// its origin.
type flatPoint struct {
	x, y float64
}

// sub is the vector from other to p.
func (p flatPoint) sub(other flatPoint) flatPoint {
	return flatPoint{x: p.x - other.x, y: p.y - other.y}
}

// length is the vector's length.
func (p flatPoint) length() float64 { return math.Hypot(p.x, p.y) }

// bearing is the vector's bearing in degrees clockwise from north.
func (p flatPoint) bearing() float64 { return bearingDeg(p.x, p.y) }

// offsetOf is a fix's place in the frame.
func offsetOf(frame fieldFrame, fix *airplane.PositionEntry) flatPoint {
	x, y := frame.offset(fix)

	return flatPoint{x: x, y: y}
}

// airwayBook bundles the ring's overflights and keeps everything between
// computes, so nothing is allocated after the first.
type airwayBook struct {
	heads [airwayMaxBundles]airwayHead
	count int

	out   [airwayMax]Airway
	fades [airwayMax]routeFade
	n     int

	// trail is the index of the trail whose legs are being added, and its
	// weight and loss time.
	trail  int
	weight float64
	lostAt time.Time

	// longest is the longest leg of the current trail that joined a bundle,
	// as fix indices, so its hairline can be handed over to the airway.
	longestNm        float64
	longFrom, longTo int
	longHead         int

	// members is the bundle each trail's longest leg joined, or -1.
	members [maxGhostTrails]int16
	spans   [maxGhostTrails]routeSpan

	// lines and order are the second pass's scratch: every first-pass
	// bundle's fitted line, and the bundles by how many tracks they took.
	lines [airwayMaxBundles]airwayHead
	order [airwayMaxBundles]int

	// settled is true on the second pass, when legs only join the nearest
	// existing line and never open one.
	settled bool

	// at, generation and computed are when the last compute ran, the
	// routes' generation it ran for, and whether one has run at all.
	at         time.Time
	generation uint64
	computed   bool
}

// refresh recomputes when the routes did, which is when the ring moved or a
// minute passed, and ages the strengths to now on every frame. Following the
// routes' generation rather than keeping a clock of its own keeps the two
// layers from disagreeing about which tracks they were built from.
func (b *airwayBook) refresh(trails []Trail, generation uint64, now time.Time, field *corridors) []Airway {
	if !b.computed || generation != b.generation {
		b.at, b.generation, b.computed = now, generation, true
		b.compute(trails, now, field)
	}

	b.age(now, field.halfLife)

	return b.out[:b.n]
}

func (b *airwayBook) age(now time.Time, halfLife time.Duration) {
	decay := trackWeight(now.Sub(b.at), halfLife)

	for index := range b.n {
		fade := &b.fades[index]
		b.out[index].Strength = min(1, fade.weight*decay/routeFullTracks) *
			routeRecency(now.Sub(fade.newest), halfLife)
	}
}

// compute bundles every overflight's legs and emits the bundles that reached
// airwayMinTracks.
//
// It takes two passes. The first is greedy, newest track first, and each
// bundle keeps the line of the leg that opened it, which is how a route is
// built too. That line is one track's, and when that track flew two miles off
// the centreline the next ones were measured against the wrong line and an
// airway of ten tracks came out as two of six and four. So the first pass only
// finds the lines: each bundle's fitted centreline is kept, a line that
// repeats one with more tracks is dropped, and the second pass puts every leg
// on the nearest line left, opening none.
func (b *airwayBook) compute(trails []Trail, now time.Time, field *corridors) {
	b.count, b.n, b.settled = 0, 0, false

	frame, ok := airwayFrame(trails, field)
	if !ok {
		return
	}

	limit := min(len(trails), maxGhostTrails)
	b.pass(trails[:limit], now, field.halfLife, frame)
	b.settle()
	b.pass(trails[:limit], now, field.halfLife, frame)
	b.emit(frame)
	b.cover(trails[:limit])
}

// pass offers every overflight's legs to the bundles, newest first.
func (b *airwayBook) pass(trails []Trail, now time.Time, halfLife time.Duration, frame fieldFrame) {
	for index := len(trails) - 1; index >= 0; index-- {
		b.members[index] = -1

		trail := &trails[index]
		if trail.Class != TrackTransit || len(trail.Points) < 2 {
			continue
		}

		b.trail, b.weight, b.lostAt = index, trackWeight(now.Sub(trail.LostAt), halfLife), trail.LostAt
		b.longestNm, b.longHead = 0, -1
		b.legs(trail.Points, frame)

		if b.longHead >= 0 {
			b.members[index] = int16(b.longHead) //nolint:gosec // under airwayMaxBundles.
			b.spans[index] = routeSpan{from: b.longFrom, to: b.longTo}
		}
	}
}

// settle replaces the first pass's bundles with their fitted lines, most
// tracks first, dropping any line that another with more tracks already
// covers, and empties them for the second pass.
func (b *airwayBook) settle() {
	for index := range b.count {
		b.order[index] = index
	}

	order := b.order[:b.count]
	slices.SortStableFunc(order, func(left, right int) int {
		return cmp.Compare(b.heads[right].tracks, b.heads[left].tracks)
	})

	kept := 0

	for _, index := range order {
		line := b.heads[index].line()
		if b.covered(&line, kept) {
			continue
		}

		b.lines[kept] = line
		kept++
	}

	copy(b.heads[:kept], b.lines[:kept])
	b.count, b.settled = kept, true
}

// covered reports whether a line repeats one of the first kept lines.
func (b *airwayBook) covered(line *airwayHead, kept int) bool {
	for index := range kept {
		other := &b.lines[index]
		if math.Abs(angleBetween(line.bearing, other.bearing)) < airwayBearingDeg &&
			math.Abs(other.offset(line.originX, line.originY)) < airwayOffsetNm {
			return true
		}
	}

	return false
}

// along is how far a point lies along the head's line from its origin.
func (h *airwayHead) along(p flatPoint) float64 {
	return (p.x-h.originX)*h.dirX + (p.y-h.originY)*h.dirY
}

// offset is how far a point lies across the head's line, to the right of
// its direction of travel.
func (h *airwayHead) offset(x, y float64) float64 {
	return (x-h.originX)*h.dirY - (y-h.originY)*h.dirX
}

// line is an empty head on the bundle's fitted centreline, its origin at
// the middle of the stretch the members flew.
func (h *airwayHead) line() airwayHead {
	entry, exit := h.ends()
	run := exit.sub(entry)

	length := run.length()
	if length == 0 {
		return airwayHead{
			originX: h.originX, originY: h.originY, dirX: h.dirX, dirY: h.dirY, bearing: h.bearing, lastTrail: -1,
		}
	}

	return airwayHead{
		originX: (entry.x + exit.x) / 2, originY: (entry.y + exit.y) / 2,
		dirX: run.x / length, dirY: run.y / length,
		bearing: run.bearing(), lastTrail: -1,
	}
}

// cover hands the longest airway leg of every member over to its airway, so
// the flat views can stop drawing that stretch as a hairline when the
// airways are on. Only transit trails are ever members.
func (b *airwayBook) cover(trails []Trail) {
	for index := range trails {
		trail := &trails[index]
		if trail.Class != TrackTransit {
			continue
		}

		trail.Airway, trail.AirwayFrom, trail.AirwayTo = false, 0, 0

		head := b.members[index]
		if head < 0 || b.heads[head].tracks < airwayMinTracks {
			continue
		}

		span := b.spans[index]
		trail.Airway, trail.AirwayFrom, trail.AirwayTo = true, span.from, span.to
	}
}

// airwayFrame is the projection the legs are measured in: the field when
// there is one, otherwise the first fix of the newest trail. Nothing depends
// on where the origin is beyond keeping the flat-earth error small.
func airwayFrame(trails []Trail, field *corridors) (fieldFrame, bool) {
	if field.placed {
		return newFieldFrame(field.field), true
	}

	for _, trail := range slices.Backward(trails) {
		if len(trail.Points) > 0 {
			fix := trail.Points[0]

			return newFieldFrame(Airfield{Latitude: fix.Latitude, Longitude: fix.Longitude}), true
		}
	}

	return fieldFrame{}, false
}

// legs walks one track and hands every near-straight stretch above the floor
// to close. A fix under the floor or undecoded ends the stretch; a segment that
// turns more than airwayTurnDeg off the stretch so far ends it and starts the
// next at the turn.
func (b *airwayBook) legs(points []airplane.PositionEntry, frame fieldFrame) {
	start := -1

	for index := range points {
		if points[index].Altitude < AirwayFloorFt {
			b.close(points, start, index-1, frame)
			start = -1

			continue
		}

		if start < 0 {
			start = index

			continue
		}

		if turns(points, start, index, frame) {
			b.close(points, start, index-1, frame)
			start = index - 1
		}
	}

	b.close(points, start, len(points)-1, frame)
}

// turns reports whether the segment into fix index bends off the leg from
// start to the fix before it by more than airwayTurnDeg, once the leg has
// settled.
func turns(points []airplane.PositionEntry, start, index int, frame fieldFrame) bool {
	settled := offsetOf(frame, &points[index-1]).sub(offsetOf(frame, &points[start]))
	if settled.length() < airwaySettleNm {
		return false
	}

	next := offsetOf(frame, &points[index]).sub(offsetOf(frame, &points[index-1]))

	return math.Abs(angleBetween(next.bearing(), settled.bearing())) > airwayTurnDeg
}

// close offers the leg from fix start to fix end to the bundles.
func (b *airwayBook) close(points []airplane.PositionEntry, start, end int, frame fieldFrame) {
	if start < 0 || end <= start {
		return
	}

	entry, exit := offsetOf(frame, &points[start]), offsetOf(frame, &points[end])

	length := exit.sub(entry).length()
	if length < airwayMinLegNm {
		return
	}

	altitude := 0.0
	for index := start; index <= end; index++ {
		altitude += points[index].Altitude
	}

	altitude /= float64(end - start + 1)

	head := b.match(entry, exit, length)
	if head < 0 {
		return
	}

	b.add(head, entry, exit, altitude)

	if length > b.longestNm {
		b.longestNm, b.longFrom, b.longTo, b.longHead = length, start, end, head
	}
}

// match is the first bundle the leg fits, or a new one opened on it, or -1
// when the book is full.
func (b *airwayBook) match(entry, exit flatPoint, length float64) int {
	run := exit.sub(entry)
	bearing := run.bearing()
	midX, midY := (entry.x+exit.x)/2, (entry.y+exit.y)/2

	best, bestNm := -1, airwayOffsetNm

	for index := range b.count {
		head := &b.heads[index]
		if math.Abs(angleBetween(bearing, head.bearing)) >= airwayBearingDeg {
			continue
		}

		offset := math.Abs(head.offset(midX, midY))
		if offset >= bestNm {
			continue
		}

		if !b.settled {
			return index
		}

		best, bestNm = index, offset
	}

	if b.settled || best >= 0 || b.count == airwayMaxBundles {
		return best
	}

	index := b.count
	b.count++
	b.heads[index] = airwayHead{
		originX: midX, originY: midY,
		dirX: run.x / length, dirY: run.y / length,
		bearing: bearing, lastTrail: -1,
	}

	return index
}

// add folds one leg into a bundle, measured in the bundle's own line: t along
// it and o across it, to the right of the direction of travel.
func (b *airwayBook) add(index int, entry, exit flatPoint, altitude float64) {
	head := &b.heads[index]
	weight := b.weight

	for _, end := range [2]flatPoint{entry, exit} {
		along, across := head.along(end), head.offset(end.x, end.y)

		head.w += weight
		head.wt += weight * along
		head.wtt += weight * along * along
		head.wo += weight * across
		head.wto += weight * along * across
	}

	head.lw += weight
	head.lt0 += weight * head.along(entry)
	head.lt1 += weight * head.along(exit)
	head.la += weight * altitude

	if head.lastTrail != b.trail {
		head.lastTrail = b.trail
		head.tracks++
		head.weight += weight

		if b.lostAt.After(head.newest) {
			head.newest = b.lostAt
		}
	}
}

// emit turns every bundle with enough tracks into an Airway, most tracks
// first, which is the order settle left the lines in.
func (b *airwayBook) emit(frame fieldFrame) {
	for index := range b.count {
		if b.n == airwayMax {
			return
		}

		head := &b.heads[index]
		if head.tracks < airwayMinTracks {
			continue
		}

		b.out[b.n] = head.airway(frame)
		b.fades[b.n] = routeFade{weight: head.weight, newest: head.newest}
		b.n++
	}
}

// airway is the bundle's centreline: a weighted least-squares line through
// its members' ends, measured across its first leg's line, from the members'
// mean start to their mean end.
func (h *airwayHead) airway(frame fieldFrame) Airway {
	entry, exit := h.ends()
	upstream := frame.place(&tubeStation{x: entry.x, y: entry.y})
	downstream := frame.place(&tubeStation{x: exit.x, y: exit.y})

	return Airway{
		FromLatitude:  upstream.Latitude,
		FromLongitude: upstream.Longitude,
		ToLatitude:    downstream.Latitude,
		ToLongitude:   downstream.Longitude,
		Altitude:      h.la / h.lw,
		Tracks:        h.tracks,
	}
}

// fit is the weighted least-squares line through the members' ends, as the
// distance across the head's line at a distance along it: across = intercept
// + slope*along.
func (h *airwayHead) fit() (float64, float64) {
	if h.w == 0 {
		return 0, 0
	}

	slope := 0.0
	if det := h.w*h.wtt - h.wt*h.wt; det > airwayFlatDet {
		slope = (h.w*h.wto - h.wt*h.wo) / det
	}

	return slope, (h.wo - slope*h.wt) / h.w
}

// ends is the fitted centreline from the members' mean start to their mean
// end, in the frame the legs were measured in.
func (h *airwayHead) ends() (flatPoint, flatPoint) {
	if h.lw == 0 {
		origin := flatPoint{x: h.originX, y: h.originY}

		return origin, origin
	}

	slope, intercept := h.fit()
	place := func(along float64) flatPoint {
		across := intercept + slope*along

		return flatPoint{x: h.originX + h.dirX*along + h.dirY*across, y: h.originY + h.dirY*along - h.dirX*across}
	}

	return place(h.lt0 / h.lw), place(h.lt1 / h.lw)
}
