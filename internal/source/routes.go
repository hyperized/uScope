package source

import (
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
)

// How lost tracks are bundled into routes.
//
// A corridor is not any one track but the bundle many of them make, so the
// ring's trails are grouped by where they flew and each group is reduced to a
// mean path with a spread around it. The radar draws that as a tube: an
// arrival route narrows onto its runway because the tracks in it converge
// there, a departure route opens out, and an airway is a pipe of about one
// width all the way across.
const (
	// routeStationNm is how far apart a track is sampled. Every track in a
	// route is measured at the same distances, which is what lets the members
	// be averaged station by station. Two miles is about three fixes at an
	// approach speed, so no fix is skipped over in a turn that matters.
	routeStationNm = 2.0

	// routeMaxRoutes is how many routes are kept. A busy field shows a dozen
	// or two, and a track that would open the sixty-fifth is drawn as a
	// hairline instead.
	routeMaxRoutes = 64

	// routeMaxStations is how many stations one route can hold: 320 miles at
	// two miles a station. A track is 256 fixes at most, about forty minutes,
	// which at a cruise of 450 knots is just over 300 miles. Arrivals and
	// departures count from the field; an airway counts from the point abeam
	// the field, half the stations either way, so a station more than 160
	// miles along it is left out of the tube.
	routeMaxStations = 160

	// RouteMaxStations is routeMaxStations for a caller that keeps a fixed
	// array of one value per station, so it can size it without allocating.
	RouteMaxStations = routeMaxStations

	// routeSignatureDeg is how far apart two arrivals or two departures may be
	// in bearing from the field, at each of the signature distances, and still
	// be one route. Thirty degrees keeps the two sides of a downwind leg apart
	// at twelve miles and lets a spread of entry points at fifty miles share a
	// route.
	routeSignatureDeg = 30.0

	// routeTransitHeadingDeg and routeTransitAbeamNm are what an overflight
	// has to share with a route to join it: a heading within twenty degrees,
	// so the two directions of one airway are two routes, and a line passing
	// the field within eight miles of the route's own.
	routeTransitHeadingDeg = 20.0
	routeTransitAbeamNm    = 8.0

	// routeMinMembers is the fewest tracks a station needs before it is part
	// of the tube, and the tube stops at the first station out from the field
	// that has fewer. Two was the first value and on a live feed it let one
	// pair of tracks carry the tube forty miles out, where the mean jumped
	// from pair to pair as members dropped away. Three was picked on the
	// mock-up against a live feed with fan-out.
	routeMinMembers = 3

	// routeSmoothStations is how many stations, centred on each one, its
	// position, altitude and both spreads are averaged over: two either side,
	// fewer at the ends of the run. Without it the mean jumps a mile from one
	// station to the next where a member joins or leaves, and the rings either
	// side of the jump twist against each other. Five was picked on the
	// mock-up with the other two.
	routeSmoothStations = 5

	// routeMaxSpreadNm is the spread past which the tracks no longer agree on
	// a path, and the tube stops at the first smoothed station out from the
	// field that exceeds it. Arrivals that share a final fan out to their own
	// entry points further out, ten miles or more apart at forty miles, and a
	// tube drawn round that is a spike rather than a corridor. Four miles was
	// picked on the mock-up with the other two.
	routeMaxSpreadNm = 4.0

	// routeMinStations is the fewest stations a tube needs once the three
	// rules above have cut it. A route left with fewer has no tube, and its
	// tracks are left to the hairlines.
	routeMinStations = 2

	// routeFullTracks is how many fresh tracks make a route full strength.
	// Three recent arrivals down the same approach are a corridor; one is an
	// aeroplane that happened to come that way.
	routeFullTracks = 3.0
)

// routeSignatureNm is where an arrival or a departure is compared with a
// route: just off the runway, at the end of a typical final or climb-out,
// where the arrivals have come off their initial approach fixes, and at the
// edge of the terminal area.
//
//nolint:gochecknoglobals // a table, read only.
var routeSignatureNm = [...]float64{2, 12, 30, 50}

// Route is one corridor: the lost tracks of one class that flew the same way,
// reduced to a mean path with the spread of the bundle around it.
//
// Only routes with a tube at least two stations long are carried. The tube is
// the stretch out from the field (from abeam it, for an overflight) where at
// least three tracks agree to within four miles; a track in no route, and the
// part of a member's track past the end of its tube, is drawn on its own, and
// its Trail says which part that is.
type Route struct {
	Class TrackClass

	// Strength is how much of a corridor the route makes, from 0 to 1: the
	// members' weights summed, each halving every half-life since it was lost,
	// over three fresh tracks.
	Strength float64

	// Stations is the mean path, smoothed, outward from the field for an
	// arrival or a departure and along the direction of flight for an
	// overflight.
	Stations []Station
}

// Station is one point on a route's mean path.
type Station struct {
	Latitude  float64
	Longitude float64

	// Altitude is the weighted mean of the members' altitudes in feet, over
	// the members that had one decoded here. Zero means none had, which the
	// radar reads as undecoded and does not draw.
	Altitude float64

	// SpreadNm is the weighted RMS distance of the members from the mean on
	// the ground, and SpreadFt the same in altitude. They are one standard
	// deviation, measured and then smoothed with the position; how wide that
	// is drawn is the radar's business.
	SpreadNm float64
	SpreadFt float64

	// Members is how many tracks passed this station, before smoothing.
	Members int
}

// stationPoint is one track sampled at one station, in nautical miles east
// and north of the field. alt is zero when either fix around it had no
// altitude decoded. seg is the lower index, in the track's own order, of the
// two fixes the station lies between, which is how a member learns which of
// its fixes the tube covers.
type stationPoint struct {
	k   int
	seg int
	x   float64
	y   float64
	alt float64
}

// tubeStation is one station's statistics in the field's frame: the mean
// position and altitude and the two spreads. It is what the smoothing
// averages, before the result goes back into degrees.
type tubeStation struct {
	x, y, alt, nm, ft float64
	n                 int
}

// stationSum is one station of a route while the members are being added up:
// weighted sums and sums of squares, so the mean and the spread both come out
// of one pass over the tracks. The altitude has a weight of its own because
// not every member has one at every station.
type stationSum struct {
	w, wx, wy, wxx, wyy float64
	wa, wal, waa        float64
	n                   int
}

// add folds one member's point into the station.
func (s *stationSum) add(point *stationPoint, weight float64) {
	s.w += weight
	s.wx += weight * point.x
	s.wy += weight * point.y
	s.wxx += weight * point.x * point.x
	s.wyy += weight * point.y * point.y
	s.n++

	if point.alt > 0 {
		s.wa += weight
		s.wal += weight * point.alt
		s.waa += weight * point.alt * point.alt
	}
}

// routeHead is what a route is matched by, taken from its first member, and
// the weight its members add up to.
type routeHead struct {
	class     TrackClass
	signature [len(routeSignatureNm)]float64
	heading   float64
	abeamNm   float64
	weight    float64
}

// trackShape is one track's half of the match: the signature bearings for an
// arrival or a departure, the heading and the abeam distance for an
// overflight.
type trackShape struct {
	signature [len(routeSignatureNm)]float64
	heading   float64
	abeamNm   float64
}

// routeSpan is where one route's stations sit in routeBook.stations while the
// slice is still growing.
type routeSpan struct {
	from, to int
}

// routeBook bundles the ring's trails into routes and keeps everything it
// needs between computes, so a compute after the first allocates only when
// the ring holds more than it ever has.
//
// It is recomputed when the ring changes and once a minute otherwise, because
// the members' weights age: a route's strength and its mean path both lean
// towards the newer tracks as the older ones fade. Between computes the same
// routes go back out.
type routeBook struct {
	heads [routeMaxRoutes]routeHead
	sums  [routeMaxRoutes]*[routeMaxStations]stationSum
	count int

	// member is which route each trail joined, by its index in the ring's
	// list, or -1 for none.
	member []int

	// segs holds, for every trail, routeMaxStations entries: the fix segment
	// each of its stations was laid on, by slot. slots is the range of slots
	// each trail wrote this compute. Both walks lay stations on consecutive
	// slots (an overflight that doubles back only resumes after the last one
	// it laid), so every entry inside the range is this compute's. It is how
	// cover finds the fixes a tube covers without walking the track a second
	// time, at two bytes a slot: 640 kB on a full ring.
	segs  []uint16
	slots []routeSpan

	// track is one track's stations, refilled for every trail.
	track []stationPoint

	stations []Station
	spans    [routeMaxRoutes]routeSpan
	out      []Route

	// cuts is each route's tube as a range of station slots, empty when the
	// route has none. raw and smooth are one route's stations before and
	// after the smoothing, reused for every route.
	cuts   [routeMaxRoutes]routeSpan
	raw    [routeMaxStations]tubeStation
	smooth [routeMaxStations]tubeStation

	// version is the ring version the routes were computed at, at when, and
	// generation moves every time they are.
	version    uint64
	at         time.Time
	computed   bool
	generation uint64
}

// stale reports whether the routes need computing again: never computed, the
// ring has moved, a minute has passed, or the clock has gone backwards.
func (b *routeBook) stale(version uint64, now time.Time) bool {
	if !b.computed || version != b.version {
		return true
	}

	since := now.Sub(b.at)

	return since < 0 || since >= corridorExpireEvery
}

// refresh recomputes the routes when they are stale and hands back the ones
// to draw. trails is the ring's own list, and the Tubed flag on each is
// written here.
func (b *routeBook) refresh(trails []Trail, version uint64, now time.Time, field *corridors) []Route {
	if !b.stale(version, now) {
		return b.out
	}

	b.version, b.at, b.computed = version, now, true
	b.generation++
	b.compute(trails, now, field)

	return b.out
}

// compute groups every trail and reduces each group to its stations.
//
// Grouping is one greedy pass in the ring's order, oldest first, and a trail
// joins the first route it matches. The route's signature is its first
// member's and does not move as others join, which keeps a route from
// creeping round the field one track at a time until it has swallowed its
// neighbour.
func (b *routeBook) compute(trails []Trail, now time.Time, field *corridors) {
	b.reset(len(trails))

	for index := range trails {
		trails[index].Tubed, trails[index].TubeFrom, trails[index].TubeTo = false, 0, 0
	}

	if !field.placed {
		return
	}

	frame := newFieldFrame(field.field)

	for index := range trails {
		b.member[index] = b.join(&trails[index], index, now, field.halfLife, frame)
	}

	b.reduce(frame)

	for index, route := range b.member {
		if route >= 0 && b.spans[route].to > b.spans[route].from {
			b.cover(&trails[index], index, b.cuts[route])
		}
	}
}

// cover marks the fixes of one member that its route's tube covers: the
// fixes either side of the segments its stations inside the cut were laid
// on, read back from what join recorded. A member whose stations all lie
// outside the cut, an overflight lost before it came abeam say, is left
// untubed and drawn whole as a hairline.
func (b *routeBook) cover(trail *Trail, index int, cut routeSpan) {
	slots := b.slots[index]
	segs := b.segs[index*routeMaxStations : (index+1)*routeMaxStations]
	first, last := len(trail.Points), -1

	for slot := max(cut.from, slots.from); slot < min(cut.to, slots.to); slot++ {
		seg := int(segs[slot])
		first, last = min(first, seg), max(last, seg+1)
	}

	if last < 0 {
		return
	}

	trail.Tubed, trail.TubeFrom, trail.TubeTo = true, first, last
}

// reset empties the book for a fresh compute, keeping every buffer.
func (b *routeBook) reset(trails int) {
	for route := range b.count {
		*b.sums[route] = [routeMaxStations]stationSum{}
	}

	b.count = 0
	b.out = b.out[:0]
	b.stations = b.stations[:0]

	if cap(b.member) < trails {
		b.member = make([]int, trails)
		b.slots = make([]routeSpan, trails)
		b.segs = make([]uint16, trails*routeMaxStations)
	}

	b.member = b.member[:trails]
	b.slots = b.slots[:trails]
	b.segs = b.segs[:trails*routeMaxStations]
}

// join stations one trail, finds or opens its route and adds it in, and
// returns the route's index, or -1 when the trail is in none.
func (b *routeBook) join(trail *Trail, index int, now time.Time, halfLife time.Duration, frame fieldFrame) int {
	b.slots[index] = routeSpan{}

	shape, ok := b.station(trail.Points, trail.Class, frame)
	if !ok {
		return -1
	}

	route := b.match(trail.Class, &shape)
	if route < 0 {
		return -1
	}

	weight := trackWeight(now.Sub(trail.LostAt), halfLife)
	b.heads[route].weight += weight

	base := 0
	if trail.Class == TrackTransit {
		base = routeMaxStations / 2
	}

	sums := b.sums[route]
	segs := b.segs[index*routeMaxStations : (index+1)*routeMaxStations]

	for at := range b.track {
		point := &b.track[at]
		slot := point.k + base
		sums[slot].add(point, weight)
		segs[slot] = uint16(min(point.seg, math.MaxUint16)) //nolint:gosec // clamped on the same line.
	}

	b.slots[index] = routeSpan{from: b.track[0].k + base, to: b.track[len(b.track)-1].k + base + 1}

	return route
}

// trackWeight is how much a track lost age ago counts for: one when fresh,
// halving every half-life. A track lost after now, which a clock stepped
// backwards produces, counts as fresh rather than more than fresh, and a
// half-life that is not positive never fades anything.
func trackWeight(age, halfLife time.Duration) float64 {
	if halfLife <= 0 {
		return 1
	}

	return math.Exp2(-max(age, 0).Seconds() / halfLife.Seconds())
}

// match is the first route of the same class the shape fits, or a new one
// started from it, or -1 when the book is full.
func (b *routeBook) match(class TrackClass, shape *trackShape) int {
	for route := range b.count {
		if b.heads[route].class == class && b.heads[route].fits(shape) {
			return route
		}
	}

	if b.count == routeMaxRoutes {
		return -1
	}

	route := b.count
	b.count++
	b.heads[route] = routeHead{
		class: class, signature: shape.signature, heading: shape.heading, abeamNm: shape.abeamNm,
	}

	if b.sums[route] == nil {
		b.sums[route] = new([routeMaxStations]stationSum)
	}

	return route
}

// fits reports whether a track's shape belongs to the route. An overflight
// matches on heading and abeam distance; anything else matches on every
// signature bearing, where a bearing either side does not have counts as a
// match, since a short track cannot disagree about a distance it never flew.
func (h *routeHead) fits(shape *trackShape) bool {
	if h.class == TrackTransit {
		return math.Abs(angleBetween(shape.heading, h.heading)) < routeTransitHeadingDeg &&
			math.Abs(shape.abeamNm-h.abeamNm) < routeTransitAbeamNm
	}

	for index, bearing := range shape.signature {
		mine := h.signature[index]
		if !math.IsNaN(bearing) && !math.IsNaN(mine) && math.Abs(angleBetween(bearing, mine)) >= routeSignatureDeg {
			return false
		}
	}

	return true
}

// reduce turns every route's sums into its tube and emits the routes that
// have one.
//
// The stations are collected into one slice with spans beside it and resolved
// into slices afterwards, because a span cannot become a slice while the
// slice it points into may still grow.
func (b *routeBook) reduce(frame fieldFrame) {
	for route := range b.count {
		from := len(b.stations)
		cut := b.tube(route)

		for index := cut.from; index < cut.to; index++ {
			b.stations = append(b.stations, frame.place(&b.smooth[index]))
		}

		b.cuts[route] = cut
		b.spans[route] = routeSpan{from: from, to: len(b.stations)}
	}

	for route := range b.count {
		span := b.spans[route]
		if span.to == span.from {
			continue
		}

		b.out = append(b.out, Route{
			Class:    b.heads[route].class,
			Strength: min(1, b.heads[route].weight/routeFullTracks),
			Stations: b.stations[span.from:span.to:span.to],
		})
	}
}

// tube is the range of station slots one route's tube covers, with the
// smoothed stations left in b.smooth, or an empty range when the route has
// no tube. The rules run in a fixed order, the same as the mock-up they were
// judged on:
//
//   - The run starts at the anchor, the station at the field for an arrival
//     or a departure and the one nearest abeam the field for an overflight,
//     and grows away from it while every station has routeMinMembers.
//   - Each station on the run is averaged over routeSmoothStations around it,
//     the window clipped to the run.
//   - The run is cut again, from the same anchor, at the first smoothed
//     station spread wider than routeMaxSpreadNm.
//
// A tube is a claim that the tracks agree on a path. Where too few of them
// flew, or where they have fanned out to their own entry points, they do not,
// and there is no corridor to draw.
func (b *routeBook) tube(route int) routeSpan {
	sums := b.sums[route]

	anchor := 0
	if b.heads[route].class == TrackTransit {
		anchor = abeam(sums)
	}

	run := membersRun(sums, anchor)
	if run.to-run.from < routeMinStations {
		return routeSpan{}
	}

	for index := run.from; index < run.to; index++ {
		b.raw[index] = stationStats(&sums[index])
	}

	b.smoothRun(run)

	cut := spreadRun(&b.smooth, anchor, run)
	if cut.to-cut.from < routeMinStations {
		return routeSpan{}
	}

	return cut
}

// abeam is the slot nearest the point abeam the field that any member passed,
// taking the earlier slot on a tie. It is the middle slot when no member
// passed any, which no route can be, and that slot then fails the members
// floor.
func abeam(sums *[routeMaxStations]stationSum) int {
	base := routeMaxStations / 2

	for offset := range base + 1 {
		if sums[base-offset].n > 0 {
			return base - offset
		}

		if base+offset < routeMaxStations && sums[base+offset].n > 0 {
			return base + offset
		}
	}

	return base
}

// membersRun grows a run of slots both ways from the anchor while each has at
// least routeMinMembers members. It is empty when the anchor itself has
// fewer. An arrival or a departure anchors at slot zero, so it only grows
// outward.
func membersRun(sums *[routeMaxStations]stationSum, anchor int) routeSpan {
	if sums[anchor].n < routeMinMembers {
		return routeSpan{}
	}

	low, high := anchor, anchor+1

	for low > 0 && sums[low-1].n >= routeMinMembers {
		low--
	}

	for high < routeMaxStations && sums[high].n >= routeMinMembers {
		high++
	}

	return routeSpan{from: low, to: high}
}

// spreadRun cuts a run down to the slots, grown both ways from the anchor,
// whose smoothed spread is within routeMaxSpreadNm. It is empty when the
// anchor's own is not.
func spreadRun(smooth *[routeMaxStations]tubeStation, anchor int, run routeSpan) routeSpan {
	if smooth[anchor].nm > routeMaxSpreadNm {
		return routeSpan{}
	}

	low, high := anchor, anchor+1

	for low > run.from && smooth[low-1].nm <= routeMaxSpreadNm {
		low--
	}

	for high < run.to && smooth[high].nm <= routeMaxSpreadNm {
		high++
	}

	return routeSpan{from: low, to: high}
}

// smoothRun averages every station on the run over the window around it into
// b.smooth. The altitude and its spread are averaged only over the stations
// that have one, so a station where no member decoded an altitude does not
// drag its neighbours towards the ground; a window with none keeps none.
func (b *routeBook) smoothRun(run routeSpan) {
	const half = routeSmoothStations / 2

	for index := run.from; index < run.to; index++ {
		var out tubeStation

		count, withAlt := 0.0, 0.0

		for _, near := range b.raw[max(run.from, index-half):min(run.to, index+half+1)] {
			out.x += near.x
			out.y += near.y
			out.nm += near.nm
			count++

			if near.alt > 0 {
				out.alt += near.alt
				out.ft += near.ft
				withAlt++
			}
		}

		out.x, out.y, out.nm = out.x/count, out.y/count, out.nm/count
		if withAlt > 0 {
			out.alt, out.ft = out.alt/withAlt, out.ft/withAlt
		}

		out.n = b.raw[index].n
		b.smooth[index] = out
	}
}

// station samples one track every routeStationNm into b.track and measures
// its shape, reporting false for a track with fewer than two fixes or with no
// station at all.
func (b *routeBook) station(points []airplane.PositionEntry, class TrackClass, frame fieldFrame) (trackShape, bool) {
	b.track = b.track[:0]

	if len(points) < 2 {
		return trackShape{}, false
	}

	if class == TrackTransit {
		return b.stationAlong(points, frame)
	}

	b.stationOut(points, class == TrackInbound, frame)

	var shape trackShape

	last := len(b.track) - 1
	for index, nm := range routeSignatureNm {
		point := &b.track[min(int(math.Round(nm/routeStationNm)), last)]
		shape.signature[index] = bearingDeg(point.x, point.y)
	}

	return shape, true
}

// stationOut walks a track outward from the field, an arrival from its last
// fix back and a departure from its first, laying a station every
// routeStationNm of distance flown. The first station is the end at the field,
// so station k of every arrival is k*2 miles of track from the runway.
//
//nolint:varnamelen,revive // k and the ends' ax, ay, bx, by read as the geometry; reverse picks an end, not a mode.
func (b *routeBook) stationOut(points []airplane.PositionEntry, reverse bool, frame fieldFrame) {
	walked, k := 0.0, 0
	count := len(points)

	for index := 0; index+1 < count && k < routeMaxStations; index++ {
		from, to, seg := &points[index], &points[index+1], index
		if reverse {
			from, to, seg = &points[count-1-index], &points[count-2-index], count-2-index
		}

		ax, ay := frame.offset(from)
		bx, by := frame.offset(to)
		legNm := math.Hypot(bx-ax, by-ay)

		for ; k < routeMaxStations && float64(k)*routeStationNm <= walked+legNm; k++ {
			share := 0.0
			if legNm > 0 {
				share = (float64(k)*routeStationNm - walked) / legNm
			}

			point := interpolate(k, ax, ay, bx, by, from, to, share)
			point.seg = seg
			b.track = append(b.track, point)
		}

		walked += legNm
	}
}

// stationAlong walks an overflight along its own axis, the line from its first
// fix to its last, with the point abeam the field as station zero. Stations
// before that point have negative numbers.
//
// A track that doubles back over stations it has already passed keeps the
// first pass, which on an airway is the only one there is.
//
//nolint:varnamelen // k, the axis dx, dy, the ends and s0, s1 along it read as the geometry.
func (b *routeBook) stationAlong(points []airplane.PositionEntry, frame fieldFrame) (trackShape, bool) {
	firstX, firstY := frame.offset(&points[0])
	lastX, lastY := frame.offset(&points[len(points)-1])

	dx, dy := lastX-firstX, lastY-firstY
	length := math.Hypot(dx, dy)

	if length == 0 {
		length = 1
	}

	dx, dy = dx/length, dy/length
	half := routeMaxStations / 2
	lastK := math.MinInt

	for index := 0; index+1 < len(points); index++ {
		from, to := &points[index], &points[index+1]
		ax, ay := frame.offset(from)
		bx, by := frame.offset(to)
		s0, s1 := ax*dx+ay*dy, bx*dx+by*dy
		span := s1 - s0

		for k := int(math.Ceil(s0 / routeStationNm)); float64(k)*routeStationNm <= s1; k++ {
			if k <= lastK || k < -half || k >= half {
				continue
			}

			share := 0.0
			if span != 0 {
				share = (float64(k)*routeStationNm - s0) / span
			}

			point := interpolate(k, ax, ay, bx, by, from, to, share)
			point.seg = index
			b.track = append(b.track, point)
			lastK = k
		}
	}

	if len(b.track) == 0 {
		return trackShape{}, false
	}

	return trackShape{heading: bearingDeg(dx, dy), abeamNm: -firstX*dy + firstY*dx}, true
}

// interpolate is the point share of the way from one fix to the next. The
// altitude is only interpolated between two decoded altitudes; between a
// decoded one and an undecoded one there is nothing to say.
//
//nolint:varnamelen // ax, ay, bx, by are the two ends in the field's frame.
func interpolate(k int, ax, ay, bx, by float64, from, to *airplane.PositionEntry, share float64) stationPoint {
	point := stationPoint{k: k, x: ax + (bx-ax)*share, y: ay + (by-ay)*share}

	if from.Altitude > 0 && to.Altitude > 0 {
		point.alt = from.Altitude + (to.Altitude-from.Altitude)*share
	}

	return point
}

// fieldFrame is the local projection around the field the routes are
// measured in: nautical miles east and north of it, on the same
// equirectangular approximation the radar draws with.
type fieldFrame struct {
	field  Airfield
	cosLat float64
}

func newFieldFrame(field Airfield) fieldFrame {
	return fieldFrame{field: field, cosLat: max(math.Cos(field.Latitude*math.Pi/halfCircle), minCosLatitude)}
}

// offset is a fix in the field's frame.
func (f fieldFrame) offset(fix *airplane.PositionEntry) (float64, float64) {
	return wrapLongitude(fix.Longitude-f.field.Longitude) * nmPerDegree * f.cosLat,
		(fix.Latitude - f.field.Latitude) * nmPerDegree
}

// stationStats turns one station's sums into the mean and the spread in the
// field's frame. The variances are clamped at zero because the sum-of-squares
// form can come out a hair negative when every member sat on the same point.
func stationStats(sum *stationSum) tubeStation {
	meanX, meanY := sum.wx/sum.w, sum.wy/sum.w
	spread := sum.wxx/sum.w + sum.wyy/sum.w - meanX*meanX - meanY*meanY

	station := tubeStation{x: meanX, y: meanY, nm: math.Sqrt(max(spread, 0)), n: sum.n}

	if sum.wa > 0 {
		meanAlt := sum.wal / sum.wa
		station.alt = meanAlt
		station.ft = math.Sqrt(max(sum.waa/sum.wa-meanAlt*meanAlt, 0))
	}

	return station
}

// place puts a station back into degrees.
func (f fieldFrame) place(station *tubeStation) Station {
	return Station{
		Latitude:  f.field.Latitude + station.y/nmPerDegree,
		Longitude: wrapLongitude(f.field.Longitude + station.x/(nmPerDegree*f.cosLat)),
		Altitude:  station.alt,
		SpreadNm:  station.nm,
		SpreadFt:  station.ft,
		Members:   station.n,
	}
}

// bearingDeg is the compass bearing of a point east and north of the origin,
// from 0 to 360.
func bearingDeg(east, north float64) float64 {
	return math.Mod(math.Atan2(east, north)*halfCircle/math.Pi+degreesPerCircle, degreesPerCircle)
}

// angleBetween is the signed difference between two bearings, folded into
// -180 to 180.
func angleBetween(a, b float64) float64 {
	return math.Mod(math.Mod(a-b, degreesPerCircle)+degreesPerCircle+halfCircle, degreesPerCircle) - halfCircle
}
