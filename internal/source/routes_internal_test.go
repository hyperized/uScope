package source

import (
	"math"
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
			name: "three overflights on one airway are one route",
			trails: []Trail{
				routeLine(TrackTransit, -40, 10, 40, 10, 30000, 30000),
				routeLine(TrackTransit, -40, 11, 40, 11, 32000, 32000),
				routeLine(TrackTransit, -40, 9, 40, 9, 34000, 34000),
			},
			groups: 1, routes: 1, tubed: 3,
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
			name: "the two directions of one airway are two routes",
			trails: []Trail{
				routeLine(TrackTransit, -40, 10, 40, 10, 30000, 30000),
				routeLine(TrackTransit, 40, 10, -40, 10, 30000, 30000),
			},
			groups: 2,
		},
		{
			name: "two parallel airways are two routes",
			trails: []Trail{
				routeLine(TrackTransit, -40, 10, 40, 10, 30000, 30000),
				routeLine(TrackTransit, -40, 30, 40, 30, 30000, 30000),
			},
			groups: 2,
		},
		{
			name:   "a track of one fix is in no route",
			trails: []Trail{{ICAO: "R1", Points: []airplane.PositionEntry{routeFix(1, 1, 1000)}}},
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

// TestRouteBookFull checks a track that would open one route past the cap is
// left out rather than pushing another route out.
func TestRouteBookFull(t *testing.T) {
	t.Parallel()

	trails := make([]Trail, 0, routeMaxRoutes+1)

	for index := range routeMaxRoutes + 1 {
		// One airway every ten miles north of the field, all flown east, so
		// no two pass the field close enough to share a route.
		northNm := float64(index) * 10
		trails = append(trails, routeLine(TrackTransit, -10, northNm, 10, northNm, 30000, 30000))
	}

	var book routeBook

	book.refresh(trails, 1, routeEpoch, &routeField)

	if book.count != routeMaxRoutes || book.member[routeMaxRoutes] != -1 {
		t.Errorf("routes %d, last member %d; want %d and none", book.count, book.member[routeMaxRoutes], routeMaxRoutes)
	}

	// A second compute reuses the sums it zeroes, which is the path a
	// steady ring takes once a minute.
	book.refresh(trails, 2, routeEpoch, &routeField)

	if book.count != routeMaxRoutes {
		t.Errorf("recomputed routes = %d, want %d", book.count, routeMaxRoutes)
	}
}

// TestStationAlongEdges checks the overflight walk's three odd cases: a track
// that doubles back keeps its first pass, a track that starts and ends on one
// spot still has an axis, and a track entirely outside the station window has
// no stations.
func TestStationAlongEdges(t *testing.T) {
	t.Parallel()

	frame := newFieldFrame(routeField.field)

	var book routeBook

	back := []airplane.PositionEntry{
		routeFix(-10, 5, 30000), routeFix(10, 5, 30000), routeFix(0, 5, 30000), routeFix(20, 5, 30000),
	}
	if _, ok := book.stationAlong(back, frame); !ok {
		t.Fatal("a track that doubles back has no stations")
	}

	for index := 1; index < len(book.track); index++ {
		if book.track[index].k <= book.track[index-1].k {
			t.Fatalf("station %d repeats or goes back: %d after %d", index, book.track[index].k, book.track[index-1].k)
		}
	}

	loop := []airplane.PositionEntry{routeFix(0, 5, 30000), routeFix(3, 5, 30000), routeFix(0, 5, 30000)}
	book.track = book.track[:0]

	if _, ok := book.stationAlong(loop, frame); !ok {
		t.Error("a track that ends where it began has no stations")
	}

	far := []airplane.PositionEntry{routeFix(400, 0, 30000), routeFix(410, 0, 30000)}
	book.track = book.track[:0]

	if _, ok := book.stationAlong(far, frame); ok {
		t.Error("a track past the window has stations")
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
func handSums(first int, members []int, spreads []float64) *[routeMaxStations]stationSum {
	var sums [routeMaxStations]stationSum

	for index, count := range members {
		eastNm := float64(index)
		sums[first+index] = stationSum{
			w: 1, wx: eastNm, wxx: eastNm*eastNm + spreads[index]*spreads[index],
			wa: 1, wal: 30000, waa: 30000 * 30000, n: count,
		}
	}

	return &sums
}

// TestTubeRules checks the three rules in their order on hand-built sums: the
// members floor stops the run, the spread cut stops it again from the same
// anchor after smoothing, and a run left under two stations is no tube.
func TestTubeRules(t *testing.T) {
	t.Parallel()

	base := routeMaxStations / 2

	for _, testCase := range []struct {
		name    string
		class   TrackClass
		first   int
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
		{
			name:  "an overflight grows both ways from abeam",
			class: TrackTransit, first: base - 3, members: []int{2, 3, 3, 3, 3, 3, 2},
			spreads: []float64{0, 0, 0, 0, 0, 0, 0},
			want:    routeSpan{from: base - 2, to: base + 3},
		},
		{
			name:  "an overflight never abeam anchors on the nearest station flown",
			class: TrackTransit, first: base + 4, members: []int{3, 3, 3},
			spreads: []float64{0, 0, 0},
			want:    routeSpan{from: base + 4, to: base + 7},
		},
		{
			name:  "an overflight thin abeam is no tube, however busy further on",
			class: TrackTransit, first: base, members: []int{2, 5, 5, 5},
			spreads: []float64{0, 0, 0, 0},
		},
		{
			name:  "the spread cuts an overflight both ways from abeam",
			class: TrackTransit, first: base - 4, members: []int{3, 3, 3, 3, 3, 3, 3, 3, 3},
			spreads: []float64{9, 9, 0, 0, 0, 0, 0, 9, 9},
			// Smoothed from base-4: 6, 4.5, 3.6, 1.8, 0, 1.8, 3.6, 4.5, 6.
			want: routeSpan{from: base - 2, to: base + 3},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var book routeBook

			book.count = 1
			book.heads[0].class = testCase.class
			book.sums[0] = handSums(testCase.first, testCase.members, testCase.spreads)

			if got := book.tube(0); got != testCase.want {
				t.Errorf("tube = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// TestAbeam checks the anchor search: the slot abeam when flown, the earlier
// of two equally near, and the middle slot when nothing was flown at all.
func TestAbeam(t *testing.T) {
	t.Parallel()

	base := routeMaxStations / 2

	for _, testCase := range []struct {
		name  string
		flown []int
		want  int
	}{
		{name: "abeam", flown: []int{base, base + 1}, want: base},
		{name: "a tie takes the earlier", flown: []int{base - 2, base + 2}, want: base - 2},
		{name: "only the last slot", flown: []int{routeMaxStations - 1}, want: routeMaxStations - 1},
		{name: "only the first slot", flown: []int{0}, want: 0},
		{name: "nothing flown", want: base},
	} {
		var sums [routeMaxStations]stationSum
		for _, slot := range testCase.flown {
			sums[slot].n = 1
		}

		if got := abeam(&sums); got != testCase.want {
			t.Errorf("%s: abeam = %d, want %d", testCase.name, got, testCase.want)
		}
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
// overflights the full eight miles apart abeam the field, is not emitted, and
// that its members arrive untubed, loose from end to end.
func TestRouteThatLosesItsTube(t *testing.T) {
	t.Parallel()

	var book routeBook

	trails := []Trail{
		routeLine(TrackTransit, -40, 0, 40, 0, 30000, 30000),
		routeLine(TrackTransit, -40, 7, 40, 7, 30000, 30000),
		routeLine(TrackTransit, -40, -7, 40, -7, 30000, 30000),
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

// TestRouteMemberOutsideItsTube checks an overflight that joined a route but
// was lost before it reached the stretch the tube covers is left untubed, and
// that a member covering only part of it is loose either side.
func TestRouteMemberOutsideItsTube(t *testing.T) {
	t.Parallel()

	_, trails := computeRoutes([]Trail{
		routeLine(TrackTransit, -20, 5, 20, 5, 30000, 30000),
		routeLine(TrackTransit, -20, 5, 20, 5, 30000, 30000),
		routeLine(TrackTransit, -20, 5, 20, 5, 30000, 30000),
		routeLine(TrackTransit, -60, 5, -40, 5, 30000, 30000),
		routeLine(TrackTransit, -40, 5, 40, 5, 30000, 30000),
	})

	if trails[3].Tubed {
		t.Error("a member lost before the tube began is tubed")
	}

	before, after := trails[4].Loose()
	if !trails[4].Tubed || len(before) < 2 || len(after) < 2 {
		t.Errorf("a member longer than the tube: tubed %v, loose %d before and %d after; want both ends loose",
			trails[4].Tubed, len(before), len(after))
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
