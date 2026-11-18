package source

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
)

//nolint:gochecknoglobals // read-only test fixtures.
var (
	routeEpoch = time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	routeField = corridors{
		field:    Airfield{ICAO: "EHAM", Latitude: 52.3, Longitude: 4.76},
		placed:   true,
		halfLife: time.Hour,
	}
)

// routeFix is a fix eastNm and northNm from the fixture field.
func routeFix(eastNm, northNm, altitudeFt float64) airplane.PositionEntry {
	frame := newFieldFrame(routeField.field)

	return airplane.PositionEntry{
		Latitude:  routeField.field.Latitude + northNm/nmPerDegree,
		Longitude: routeField.field.Longitude + eastNm/(nmPerDegree*frame.cosLat),
		Altitude:  altitudeFt,
	}
}

// routeLine is a track from one point to another in a fix every half mile,
// climbing evenly between two altitudes.
func routeLine(class TrackClass, fromX, fromY, toX, toY, fromFt, toFt float64) Trail {
	const stepNm = 0.5

	steps := int(math.Hypot(toX-fromX, toY-fromY) / stepNm)
	points := make([]airplane.PositionEntry, 0, steps+1)

	for step := 0; step <= steps; step++ {
		share := float64(step) / float64(steps)
		points = append(points, routeFix(fromX+(toX-fromX)*share, fromY+(toY-fromY)*share, fromFt+(toFt-fromFt)*share))
	}

	return Trail{ICAO: "R00001", Points: points, LostAt: routeEpoch, Class: class}
}

// computeRoutes runs one compute over trails at the fixture epoch.
func computeRoutes(trails []Trail) ([]Route, []Trail) {
	var book routeBook

	routes := book.refresh(trails, 1, routeEpoch, &routeField)

	return routes, trails
}

// threeOf is three copies of a trail, the fewest a station needs.
func threeOf(trail Trail) []Trail {
	return []Trail{trail, trail, trail}
}

func TestRoutesGroupByShape(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		trails []Trail
		groups int
		routes int
		tubed  int
	}{
		{
			name: "three arrivals down one final are one route",
			trails: []Trail{
				routeLine(TrackInbound, 0, 30, 0, 0, 9000, 200),
				routeLine(TrackInbound, 1, 30, 0, 0, 9000, 200),
				routeLine(TrackInbound, -1, 30, 0, 0, 9000, 200),
			},
			groups: 1, routes: 1, tubed: 3,
		},
		{
			name: "arrivals from opposite sides are two routes, each of one track",
			trails: []Trail{
				routeLine(TrackInbound, 0, 30, 0, 0, 9000, 200),
				routeLine(TrackInbound, 0, -30, 0, 0, 9000, 200),
			},
			groups: 2,
		},
		{
			name: "an arrival and a departure on one line are two routes",
			trails: []Trail{
				routeLine(TrackInbound, 0, 30, 0, 0, 9000, 200),
				routeLine(TrackOutbound, 0, 0, 0, 30, 200, 9000),
			},
			groups: 2,
		},
		{
			name: "three overflights on one airway are in no route",
			trails: []Trail{
				routeLine(TrackTransit, -40, 10, 40, 10, 30000, 30000),
				routeLine(TrackTransit, -40, 11, 40, 11, 32000, 32000),
				routeLine(TrackTransit, -40, 9, 40, 9, 34000, 34000),
			},
		},
		{
			name: "two arrivals are a route with no tube",
			trails: []Trail{
				routeLine(TrackInbound, 0, 30, 0, 0, 9000, 200),
				routeLine(TrackInbound, 1, 30, 0, 0, 9000, 200),
			},
			groups: 1,
		},
		{
			name:   "a track of one fix is in no route",
			trails: []Trail{{ICAO: "R1", Points: []airplane.PositionEntry{routeFix(1, 1, 1000)}, Class: TrackInbound}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var book routeBook

			trails := testCase.trails

			routes := book.refresh(trails, 1, routeEpoch, &routeField)
			if book.count != testCase.groups || len(routes) != testCase.routes {
				t.Errorf("groups %d, routes %d; want %d and %d",
					book.count, len(routes), testCase.groups, testCase.routes)
			}

			tubed := 0

			for _, trail := range trails {
				if trail.Tubed {
					tubed++
				}
			}

			if tubed != testCase.tubed {
				t.Errorf("tubed = %d, want %d", tubed, testCase.tubed)
			}
		})
	}
}

// TestRouteStationsMeasureTheBundle checks three arrivals two miles apart at
// the far end and on the same final at the near one: the mean between them,
// a spread that grows with the distance and is none at the runway, and the
// altitude where all had one. The last station is the mean of the three
// stations nearest the end, since the smoothing window is clipped there.
func TestRouteStationsMeasureTheBundle(t *testing.T) {
	t.Parallel()

	routes, _ := computeRoutes([]Trail{
		routeLine(TrackInbound, -1, 20, 0, 0, 7000, 200),
		routeLine(TrackInbound, 0, 20, 0, 0, 6000, 200),
		routeLine(TrackInbound, 1, 20, 0, 0, 5000, 200),
	})

	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}

	route := routes[0]
	if route.Class != TrackInbound || route.Strength != 1 || len(route.Stations) != 11 {
		t.Errorf("class %d strength %g with %d stations, want inbound at full strength with 11",
			route.Class, route.Strength, len(route.Stations))
	}

	// Each spread is sqrt(2/3) of the offset at that distance, which grows a
	// twentieth of a mile per mile, and the end station averages 16, 18 and
	// 20 miles out.
	spreadAt := func(nm float64) float64 { return math.Sqrt(2.0/3) * nm / 20 }

	first, last := route.Stations[0], route.Stations[len(route.Stations)-1]
	if math.Abs(first.SpreadNm-(spreadAt(0)+spreadAt(2)+spreadAt(4))/3) > 0.01 || first.Members != 3 {
		t.Errorf("at the runway: spread %g with %d members, want the window's mean with 3",
			first.SpreadNm, first.Members)
	}

	if want := (spreadAt(16) + spreadAt(18) + spreadAt(20)) / 3; math.Abs(last.SpreadNm-want) > 0.01 {
		t.Errorf("out at 20 nm: spread %g nm, want %g", last.SpreadNm, want)
	}

	if math.Abs(last.Longitude-routeField.field.Longitude) > 1e-6 || math.Abs(last.Altitude-5420) > 20 {
		t.Errorf("out at 20 nm: %g E at %g ft, want on the field's meridian near 5420", last.Longitude, last.Altitude)
	}
}

// TestRouteStationsWithoutAltitude checks a station where no member had an
// altitude decoded carries none, and one where only some did averages those.
func TestRouteStationsWithoutAltitude(t *testing.T) {
	t.Parallel()

	blind := routeLine(TrackOutbound, 0, 0, 0, 20, 0, 0)
	half := routeLine(TrackOutbound, 0, 0, 0, 20, 4000, 4000)

	routes, _ := computeRoutes(threeOf(blind))
	if len(routes) != 1 || routes[0].Stations[3].Altitude != 0 {
		t.Fatalf("routes %d, want one with no altitude", len(routes))
	}

	routes, _ = computeRoutes([]Trail{blind, half, blind})
	if got := routes[0].Stations[3].Altitude; math.Abs(got-4000) > 1e-6 {
		t.Errorf("altitude = %g, want the one member that had one", got)
	}
}

func TestRoutesNeedAField(t *testing.T) {
	t.Parallel()

	trails := []Trail{routeLine(TrackInbound, 0, 30, 0, 0, 9000, 200), routeLine(TrackInbound, 0, 30, 0, 0, 9000, 200)}
	trails[0].Tubed = true

	var book routeBook

	unplaced := routeField
	unplaced.placed = false

	if routes := book.refresh(trails, 1, routeEpoch, &unplaced); len(routes) != 0 || trails[0].Tubed {
		t.Errorf("routes = %d, tubed %v; want none without a field", len(routes), trails[0].Tubed)
	}
}

func TestRoutesRefreshCadence(t *testing.T) {
	t.Parallel()

	trails := []Trail{routeLine(TrackInbound, 0, 30, 0, 0, 9000, 200), routeLine(TrackInbound, 0, 30, 0, 0, 9000, 200)}

	var book routeBook

	book.refresh(trails, 1, routeEpoch, &routeField)

	for _, step := range []struct {
		name    string
		version uint64
		at      time.Time
		want    uint64
	}{
		{name: "a frame later", version: 1, at: routeEpoch.Add(time.Second), want: 1},
		{name: "the ring moves", version: 2, at: routeEpoch.Add(time.Second), want: 2},
		{name: "a minute on", version: 2, at: routeEpoch.Add(time.Minute + time.Second), want: 3},
		{name: "the clock steps back", version: 2, at: routeEpoch, want: 4},
	} {
		book.refresh(trails, step.version, step.at, &routeField)

		if book.generation != step.want {
			t.Errorf("%s: generation = %d, want %d", step.name, book.generation, step.want)
		}
	}
}

func TestTrackWeight(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name          string
		age, halfLife time.Duration
		want          float64
	}{
		{name: "fresh", age: 0, halfLife: time.Hour, want: 1},
		{name: "one half-life", age: time.Hour, halfLife: time.Hour, want: 0.5},
		{name: "lost after now", age: -time.Hour, halfLife: time.Hour, want: 1},
		{name: "no half-life", age: time.Hour, halfLife: 0, want: 1},
	} {
		if got := trackWeight(testCase.age, testCase.halfLife); got != testCase.want {
			t.Errorf("%s: weight = %g, want %g", testCase.name, got, testCase.want)
		}
	}
}

// routeBent is a track that leaves the field on one bearing and turns onto
// another twelve miles out, flown out for a departure and in for an arrival:
// two bearings ten degrees apart or more on either leg are a track no route
// of the other shares, which is how a test runs up more routes than the book
// holds.
func routeBent(class TrackClass, outDeg, turnDeg float64, lostAt time.Time) Trail {
	const turnNm, farNm = 12.0, 40.0

	out, turn := outDeg*math.Pi/halfCircle, turnDeg*math.Pi/halfCircle
	turnX, turnY := turnNm*math.Sin(out), turnNm*math.Cos(out)
	farX, farY := turnX+farNm*math.Sin(turn), turnY+farNm*math.Cos(turn)

	near := routeLine(class, 0, 0, turnX, turnY, 200, 4000)
	far := routeLine(class, turnX, turnY, farX, farY, 4000, 12000)
	trail := Trail{ICAO: "R00002", Points: append(near.Points, far.Points[1:]...), LostAt: lostAt, Class: class}

	if class == TrackInbound {
		slices.Reverse(trail.Points)
	}

	return trail
}

// strays is a day of stray arrivals and departures, each on a shape of its
// own and an hour older than the one after it, more than the book holds.
func strays(count int, newest time.Time) []Trail {
	trails := make([]Trail, 0, count)

	for index := range count {
		class := TrackInbound
		if index%2 == 1 {
			class = TrackOutbound
		}

		shape := index / 2
		trails = append(trails, routeBent(class, float64(shape%12)*30, float64(shape/12)*45,
			newest.Add(-time.Duration(count-index)*time.Minute)))
	}

	return trails
}

// TestRouteBookFull checks the book fills to its cap and no further, and that
// the tracks it has no room for are the oldest: grouping runs newest first,
// so every route is opened by a track newer than any it refused.
func TestRouteBookFull(t *testing.T) {
	t.Parallel()

	trails := strays(3*routeMaxRoutes, routeEpoch)

	var book routeBook

	book.refresh(trails, 1, routeEpoch, &routeField)

	if book.count != routeMaxRoutes {
		t.Fatalf("routes = %d, want the cap of %d", book.count, routeMaxRoutes)
	}

	newestRefused, oldestOpener := -1, len(trails)
	opened := make(map[int]bool, routeMaxRoutes)

	for index, route := range slices.Backward(book.member) {
		if route < 0 {
			newestRefused = max(newestRefused, index)
		} else if !opened[route] {
			opened[route] = true
			oldestOpener = min(oldestOpener, index)
		}
	}

	if newestRefused < 0 || newestRefused >= oldestOpener {
		t.Errorf("newest refused track %d, oldest track to open a route %d; want some refused, all older",
			newestRefused, oldestOpener)
	}

	// A second compute reuses the sums it zeroes, which is the path a
	// steady ring takes once a minute.
	book.refresh(trails, 2, routeEpoch, &routeField)

	if book.count != routeMaxRoutes {
		t.Errorf("recomputed routes = %d, want %d", book.count, routeMaxRoutes)
	}
}

// TestRoutesUnderABusySky checks the two ways a full book used to cost the
// newest arrivals their tube: a sky full of overflights lost before them,
// which once opened a route each, and a long day of stray arrivals and
// departures that each opened one of their own. Three arrivals down one
// final, lost last, make a tube either way.
func TestRoutesUnderABusySky(t *testing.T) {
	t.Parallel()

	overflights := make([]Trail, 0, 2*routeMaxRoutes)

	for index := range 2 * routeMaxRoutes {
		// One airway every five miles, flown both ways, so no two would
		// have shared a route.
		northNm := float64(index/2)*5 - 150
		startNm, endNm := -60.0, 60.0

		if index%2 == 1 {
			startNm, endNm = endNm, startNm
		}

		overflight := routeLine(TrackTransit, startNm, northNm, endNm, northNm, 34000, 34000)
		overflight.LostAt = routeEpoch.Add(-time.Hour)
		overflights = append(overflights, overflight)
	}

	final := []Trail{
		routeLine(TrackInbound, 30, 0.5, 4, 0, 9000, 1500),
		routeLine(TrackInbound, 30, 0, 4, 0, 9000, 1500),
		routeLine(TrackInbound, 30, -0.5, 4, 0, 9000, 1500),
	}

	for _, testCase := range []struct {
		name   string
		before []Trail
	}{
		{name: "overflights", before: overflights},
		{name: "a day of strays", before: strays(3*routeMaxRoutes, routeEpoch.Add(-time.Hour))},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			trails := append(slices.Clone(testCase.before), final...)

			_, trails = computeRoutes(trails)

			for index, trail := range trails[len(trails)-len(final):] {
				if !trail.Tubed {
					t.Errorf("arrival %d down the final is in no tube", index)
				}
			}
		})
	}
}

// TestOverflightsDrawWhole checks an overflight comes out of the ring with no
// tube claimed on it, so the flat views draw its whole hairline. Three on one
// airway used to make a route of their own: the radar draws no tube for
// through traffic, and Loose then handed back only the ends either side of
// the tube that was never drawn.
func TestOverflightsDrawWhole(t *testing.T) {
	t.Parallel()

	ring := newGhosts()
	ring.corridors = routeField

	for index, northNm := range []float64{9, 10, 11} {
		trail := routeLine(TrackTransit, -40, northNm, 40, northNm, 30000, 30000)
		trail.ICAO = string(rune('A'+index)) + "00000"
		ring.push(trail)
	}

	got := ring.frame(nil, Receiver{}, routeEpoch)

	if len(got.trails) != 3 || len(got.routes) != 0 {
		t.Fatalf("trails %d, routes %d; want 3 and none", len(got.trails), len(got.routes))
	}

	for index := range got.trails {
		trail := &got.trails[index]

		before, after := trail.Loose()
		if trail.Tubed || len(before) != len(trail.Points) || after != nil {
			t.Errorf("overflight %d: tubed %v, loose %d of %d fixes; want all of it loose",
				index, trail.Tubed, len(before), len(trail.Points))
		}
	}
}

// TestStationOutCaps checks a track longer than the station cap stops at it,
// and a leg of no length still lays its station.
func TestStationOutCaps(t *testing.T) {
	t.Parallel()

	frame := newFieldFrame(routeField.field)

	var book routeBook

	long := routeLine(TrackOutbound, 0, 0, 0, 2*routeMaxStations+10, 300, 30000)
	book.stationOut(long.Points, false, frame)

	if len(book.track) != routeMaxStations {
		t.Errorf("stations = %d, want the cap of %d", len(book.track), routeMaxStations)
	}

	book.track = book.track[:0]
	book.stationOut([]airplane.PositionEntry{routeFix(0, 0, 300), routeFix(0, 0, 300)}, false, frame)

	if len(book.track) != 1 {
		t.Errorf("stations on a still track = %d, want 1", len(book.track))
	}
}

func TestAngleBetween(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ a, b, want float64 }{
		{a: 10, b: 350, want: 20},
		{a: 350, b: 10, want: -20},
		{a: 90, b: 90, want: 0},
		{a: 0, b: 180, want: -180},
	} {
		if got := angleBetween(testCase.a, testCase.b); math.Abs(got-testCase.want) > 1e-9 {
			t.Errorf("angleBetween(%g, %g) = %g, want %g", testCase.a, testCase.b, got, testCase.want)
		}
	}
}

// handSums is a route's sums written by hand: one station per entry of
// members, each with that many members, its mean a mile east per station and
// its spread from spreads, so the tube rules can be driven station by station.
func handSums(members []int, spreads []float64) *[routeMaxStations]stationSum {
	var sums [routeMaxStations]stationSum

	for index, count := range members {
		eastNm := float64(index)
		sums[index] = stationSum{
			w: 1, wx: eastNm, wxx: eastNm*eastNm + spreads[index]*spreads[index],
			wa: 1, wal: 30000, waa: 30000 * 30000, n: count,
		}
	}

	return &sums
}

// TestTubeRules checks the three rules in their order on hand-built sums: the
// members floor stops the run, the spread cut stops it again from the field
// after smoothing, and a run left under two stations is no tube.
func TestTubeRules(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		class   TrackClass
		members []int
		spreads []float64
		want    routeSpan
	}{
		{
			name:  "the floor stops an arrival at the first thin station",
			class: TrackInbound, members: []int{5, 4, 3, 2, 3, 3},
			spreads: []float64{0, 0, 0, 0, 0, 0},
			want:    routeSpan{from: 0, to: 3},
		},
		{
			name:  "a thin station at the field is no tube",
			class: TrackOutbound, members: []int{2, 3, 3},
			spreads: []float64{0, 0, 0},
		},
		{
			name:  "the spread cuts an arrival where the smoothed bundle passes four miles",
			class: TrackInbound, members: []int{3, 3, 3, 3, 3, 3, 3},
			spreads: []float64{0, 1, 2, 3, 4, 8, 8},
			// Smoothed: 1, 1.5, 2, 3.6, 5, 5.75, 6.67.
			want: routeSpan{from: 0, to: 4},
		},
		{
			name:  "a bundle too wide at the field is no tube",
			class: TrackInbound, members: []int{3, 3, 3},
			spreads: []float64{3, 6, 6},
		},
		{
			name:  "a cut to one station is no tube",
			class: TrackInbound, members: []int{3, 3, 3, 3},
			spreads: []float64{0, 0, 12, 12},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var book routeBook

			book.count = 1
			book.heads[0].class = testCase.class
			book.sums[0] = handSums(testCase.members, testCase.spreads)

			if got := book.tube(0); got != testCase.want {
				t.Errorf("tube = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// TestSmoothRun checks the window: five stations in the middle, clipped at
// either end of the run and never reaching past it, and the altitude averaged
// only over the stations that have one.
func TestSmoothRun(t *testing.T) {
	t.Parallel()

	var book routeBook

	for index := range 10 {
		book.raw[index] = tubeStation{x: float64(index), nm: float64(index), n: index}
	}

	book.raw[4].alt, book.raw[4].ft = 1000, 100
	book.raw[6].alt, book.raw[6].ft = 3000, 300

	book.smoothRun(routeSpan{from: 0, to: 10})

	for _, testCase := range []struct {
		slot   int
		x, alt float64
	}{
		{slot: 0, x: 1},
		{slot: 1, x: 1.5},
		{slot: 2, x: 2, alt: 1000},
		{slot: 5, x: 5, alt: 2000},
		{slot: 8, x: 7.5, alt: 3000},
		{slot: 9, x: 8},
	} {
		got := book.smooth[testCase.slot]
		if got.x != testCase.x || got.nm != testCase.x || got.alt != testCase.alt || got.n != testCase.slot {
			t.Errorf("slot %d: x %g nm %g alt %g n %d, want x and nm %g alt %g n %d",
				testCase.slot, got.x, got.nm, got.alt, got.n, testCase.x, testCase.alt, testCase.slot)
		}
	}

	if ft := book.smooth[5].ft; ft != 200 {
		t.Errorf("slot 5: altitude spread %g, want 200 from the two stations that had one", ft)
	}

	book.smoothRun(routeSpan{from: 3, to: 8})

	if got := book.smooth[3].x; got != 4 {
		t.Errorf("slot 3 on a run from 3: x %g, want 4 from slots 3 to 5 only", got)
	}
}

// TestRouteCutAtTheFan checks a final that three arrivals share, fanning out
// to entry points ten miles apart at forty miles, just inside one route's
// bearing tolerance: the tube stops where the smoothed
// spread passes four miles, and every member arrives with its own track past
// that point loose, to be drawn as a hairline, and the stretch inside it
// covered.
func TestRouteCutAtTheFan(t *testing.T) {
	t.Parallel()

	routes, trails := computeRoutes([]Trail{
		routeLine(TrackInbound, -10, 40, 0, 0, 20000, 200),
		routeLine(TrackInbound, 0, 40, 0, 0, 20000, 200),
		routeLine(TrackInbound, 10, 40, 0, 0, 20000, 200),
	})

	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}

	stations := routes[0].Stations

	// The spread is sqrt(2/3) * 10/40 of the distance out, past four miles
	// at about twenty; the smoothing moves the cut by a station at most.
	if count := len(stations); count < 9 || count > 11 {
		t.Errorf("stations = %d, want the tube cut about twenty miles out", count)
	}

	for index := range stations {
		if stations[index].SpreadNm > routeMaxSpreadNm {
			t.Errorf("station %d: spread %g past the cut", index, stations[index].SpreadNm)
		}
	}

	for index := range trails {
		trail := &trails[index]
		before, after := trail.Loose()

		if !trail.Tubed || len(before) < 2 || len(after) != 1 {
			t.Errorf("member %d: tubed %v, loose %d fixes out and %d at the field; want tubed, loose out, none in",
				index, trail.Tubed, len(before), len(after))
		}

		// An arrival is walked from its last fix, so the tube's far end is
		// TubeFrom, and the fix there is about where the tube stops.
		frame := newFieldFrame(routeField.field)
		eastNm, northNm := frame.offset(&trail.Points[trail.TubeFrom])

		if out := math.Hypot(eastNm, northNm); out > routeStationNm*float64(len(stations))+1 {
			t.Errorf("member %d: tube reaches %g nm out, past its last station", index, out)
		}
	}
}

// TestRouteThatLosesItsTube checks a route whose tracks never agree, three
// arrivals from one direction lost five miles apart, is not emitted, and that
// its members arrive untubed, loose from end to end.
func TestRouteThatLosesItsTube(t *testing.T) {
	t.Parallel()

	var book routeBook

	trails := []Trail{
		routeLine(TrackInbound, 0, 50, 0, 20, 9000, 3000),
		routeLine(TrackInbound, 5, 50, 5, 20, 9000, 3000),
		routeLine(TrackInbound, -5, 50, -5, 20, 9000, 3000),
	}

	routes := book.refresh(trails, 1, routeEpoch, &routeField)
	if book.count != 1 || len(routes) != 0 {
		t.Fatalf("groups %d, routes %d; want one group and no tube", book.count, len(routes))
	}

	for index := range trails {
		before, after := trails[index].Loose()
		if trails[index].Tubed || len(before) != len(trails[index].Points) || after != nil {
			t.Errorf("member %d: tubed %v, loose %d of %d; want all of it loose",
				index, trails[index].Tubed, len(before), len(trails[index].Points))
		}
	}
}

// BenchmarkRouteCompute prices one compute over a full ring: maxGhostTrails
// arrivals of 256 fixes down eight finals fanning out to entry points, so
// every route has a tube, a cut and members to cover.
func BenchmarkRouteCompute(b *testing.B) {
	trails := make([]Trail, 0, maxGhostTrails)

	for index := range maxGhostTrails {
		bearing := float64(index%8)*45 + float64(index%5-2)*3
		radians := bearing * math.Pi / halfCircle
		far := 100.0

		trail := routeLine(TrackInbound, far*math.Sin(radians), far*math.Cos(radians), 0, 0, 30000, 200)
		trail.Points = trail.Points[max(0, len(trail.Points)-256):]
		trail.LostAt = routeEpoch.Add(-time.Duration(index) * time.Second)
		trails = append(trails, trail)
	}

	var book routeBook

	b.ReportAllocs()

	for b.Loop() {
		book.computed = false
		book.refresh(trails, 1, routeEpoch, &routeField)
	}
}

// drawnStrength is the weakest route strength the radar draws a tube at:
// its corridorMinStrength over its tubeLineStrength. It is spelled out here
// rather than imported, because the radar imports this package.
const drawnStrength = 0.015 / 0.75

// flown is count arrivals down one final from the north, one every gap, the
// newest lost at last, ordered oldest first the way the ring keeps them. Each
// is laid a little east or west of the centreline so the station sums have a
// spread to carry.
func flown(count int, gap time.Duration, last time.Time) []Trail {
	trails := make([]Trail, 0, count)

	for index := range count {
		offset := float64(index%5-2) * 0.3
		trail := routeLine(TrackInbound, offset, 30, 0, 0, 9000, 200)
		trail.LostAt = last.Add(-time.Duration(count-1-index) * gap)
		trails = append(trails, trail)
	}

	return trails
}

func TestRouteRecency(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name            string
		quiet, halfLife time.Duration
		want            float64
	}{
		{name: "just flown", quiet: 0, halfLife: time.Hour, want: 1},
		{name: "end of the hold", quiet: 15 * time.Minute, halfLife: time.Hour, want: 1},
		{name: "halfway down", quiet: 37*time.Minute + 30*time.Second, halfLife: time.Hour, want: 0.5},
		{name: "one half-life", quiet: time.Hour, halfLife: time.Hour, want: 0},
		{name: "long gone", quiet: 10 * time.Hour, halfLife: time.Hour, want: 0},
		{name: "scales with the half-life", quiet: 75 * time.Minute, halfLife: 2 * time.Hour, want: 0.5},
		{name: "lost after now", quiet: -time.Hour, halfLife: time.Hour, want: 1},
		{name: "no half-life", quiet: 10 * time.Hour, halfLife: 0, want: 1},
	} {
		if got := routeRecency(testCase.quiet, testCase.halfLife); math.Abs(got-testCase.want) > 1e-12 {
			t.Errorf("%s: recency = %g, want %g", testCase.name, got, testCase.want)
		}
	}
}

// TestRoutesFadeWhenQuiet is the night with nothing flying. Forty arrivals
// down one final used to hold the route at full strength for nearly four
// half-lives after the last of them, because the summed weight is capped at
// three tracks. It now holds for a quarter of a half-life, then falls every
// second it is looked at, the same whether the routes were recomputed that
// second or not, and is under the radar's threshold before one half-life is
// out and at nothing by then. Three tracks fade on the same clock as forty.
func TestRoutesFadeWhenQuiet(t *testing.T) {
	t.Parallel()

	for _, count := range []int{3, 10, 40} {
		trails := flown(count, 5*time.Minute, routeEpoch)

		var book routeBook

		previous := 2.0

		for second := 0; second <= int(time.Hour/time.Second); second++ {
			now := routeEpoch.Add(time.Duration(second) * time.Second)

			routes := book.refresh(trails, 1, now, &routeField)
			if len(routes) != 1 {
				t.Fatalf("%d tracks, %ds: routes = %d, want 1", count, second, len(routes))
			}

			strength := routes[0].Strength
			quiet := time.Duration(second) * time.Second

			switch {
			case quiet <= 15*time.Minute && count >= 10 && strength != 1:
				t.Fatalf("%d tracks, %s quiet: strength %g, want held at 1", count, quiet, strength)
			case quiet > 15*time.Minute && strength >= previous:
				t.Fatalf("%d tracks, %s quiet: strength %g after %g, want falling", count, quiet, strength, previous)
			case previous <= 1 && previous-strength > 0.001:
				t.Fatalf("%d tracks, %s quiet: strength fell %g in a second, want smooth",
					count, quiet, previous-strength)
			case quiet >= 57*time.Minute && strength >= drawnStrength:
				t.Fatalf("%d tracks, %s quiet: strength %g, want under the radar's threshold", count, quiet, strength)
			default:
			}

			previous = strength
		}

		if previous != 0 {
			t.Errorf("%d tracks: strength %g one half-life on, want none", count, previous)
		}
	}
}

// TestQuietRouteKeepsItsShape checks a fading tube does not move. Its members
// all lose weight at the same rate, which leaves every weighted mean and
// spread where it was, so the stations a minute's recompute produces late in
// the fade are the ones it started with, even though the members were lost
// hours apart and the oldest now counts for almost nothing.
func TestQuietRouteKeepsItsShape(t *testing.T) {
	t.Parallel()

	trails := flown(12, 20*time.Minute, routeEpoch)

	var book routeBook

	first := slices.Clone(book.refresh(trails, 1, routeEpoch, &routeField)[0].Stations)
	late := book.refresh(trails, 1, routeEpoch.Add(50*time.Minute), &routeField)[0].Stations

	if book.generation != 2 || len(late) != len(first) {
		t.Fatalf("generation %d with %d stations, want a recompute with %d", book.generation, len(late), len(first))
	}

	for index := range first {
		was, now := first[index], late[index]

		for _, pair := range [][2]float64{
			{was.Latitude, now.Latitude}, {was.Longitude, now.Longitude},
			{was.Altitude, now.Altitude}, {was.SpreadNm, now.SpreadNm}, {was.SpreadFt, now.SpreadFt},
		} {
			// Only the sum-of-squares rounding is left: a millionth of a
			// foot on the altitude spread.
			if math.Abs(pair[0]-pair[1]) > 1e-5 {
				t.Fatalf("station %d moved from %+v to %+v", index, was, now)
			}
		}
	}
}

// TestRouteHeldBetweenFlights checks a route flown every 25 minutes, at the
// default half-life, stays at full strength through every gap rather than
// dimming and coming back with each arrival.
func TestRouteHeldBetweenFlights(t *testing.T) {
	t.Parallel()

	field := routeField
	field.halfLife = DefaultCorridorHalfLife

	trails := flown(15, 25*time.Minute, routeEpoch)

	var book routeBook

	for minute := range 25 {
		routes := book.refresh(trails, 1, routeEpoch.Add(time.Duration(minute)*time.Minute), &field)
		if len(routes) != 1 || routes[0].Strength != 1 {
			t.Fatalf("%d minutes after the last arrival: %d routes, want one at full strength", minute, len(routes))
		}
	}
}

// TestRunwayChange checks the old runway's tube fades over the same span a
// quiet night takes while the new runway's builds up. Arrivals come from the
// north every five minutes until the change and from the south after it.
func TestRunwayChange(t *testing.T) {
	t.Parallel()

	old := flown(24, 5*time.Minute, routeEpoch)

	// Three arrivals on the new runway, the newest just lost, are most of a
	// corridor at once, and the few more that follow make all of one.
	for _, step := range []struct {
		after          time.Duration
		newMin         float64
		oldMin, oldMax float64
	}{
		{after: 15 * time.Minute, newMin: 0.9, oldMin: 1, oldMax: 1},
		{after: 40 * time.Minute, newMin: 1, oldMin: 0.3, oldMax: 0.7},
		{after: time.Hour, newMin: 1},
	} {
		now := routeEpoch.Add(step.after)
		fresh := flown(int(step.after/(5*time.Minute)), 5*time.Minute, now)

		for index := range fresh {
			// The new runway's arrivals come in from the south.
			for fix := range fresh[index].Points {
				point := &fresh[index].Points[fix]
				point.Latitude = 2*routeField.field.Latitude - point.Latitude
			}
		}

		var book routeBook

		trails := append(slices.Clone(old), fresh...)
		routes := book.refresh(trails, 1, now, &routeField)

		if len(routes) != 2 {
			t.Fatalf("%s after the change: routes = %d, want 2", step.after, len(routes))
		}

		// Grouping runs newest first, so the new runway's route is the first.
		newer, older := routes[0].Strength, routes[1].Strength
		if newer < step.newMin || older < step.oldMin || older > step.oldMax {
			t.Errorf("%s after the change: new runway %g, old %g; want %g or more and %g to %g",
				step.after, newer, older, step.newMin, step.oldMin, step.oldMax)
		}
	}
}

// TestRouteRefreshAllocatesNothing checks the path every frame takes between
// computes, which now ages each route's strength, stays free.
//
//nolint:paralleltest // AllocsPerRun panics inside a parallel test.
func TestRouteRefreshAllocatesNothing(t *testing.T) {
	trails := flown(10, 5*time.Minute, routeEpoch)

	var book routeBook

	book.refresh(trails, 1, routeEpoch, &routeField)

	at := routeEpoch

	allocs := testing.AllocsPerRun(100, func() {
		at = at.Add(time.Millisecond)
		book.refresh(trails, 1, at, &routeField)
	})
	if allocs != 0 {
		t.Errorf("refresh between computes allocates %g times, want 0", allocs)
	}
}
