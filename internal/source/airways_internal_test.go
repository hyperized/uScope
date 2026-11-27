package source

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
)

//nolint:gochecknoglobals // read-only test fixture.
var airwayTestNow = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

// airwayCruise is the level the fixtures fly at, well above the floor.
const airwayCruise = 35000.0

func airwayTestField() corridors {
	return corridors{
		field:    Airfield{ICAO: "EHAM", Latitude: 52.3, Longitude: 4.76},
		placed:   true,
		halfLife: DefaultCorridorHalfLife,
	}
}

// airwayTrack is a straight transit track on a course through a point
// offsetNm to the right of the field, 120 miles long, at one altitude.
func airwayTrack(field Airfield, course, offsetNm, altitude float64, lostAt time.Time) Trail {
	frame := newFieldFrame(field)
	sin, cos := math.Sincos(course * math.Pi / halfCircle)
	rightX, rightY := cos, -sin

	points := make([]airplane.PositionEntry, 0, 101)

	for step := range 101 {
		along := -60 + 1.2*float64(step)
		station := frame.place(&tubeStation{x: sin*along + rightX*offsetNm, y: cos*along + rightY*offsetNm})
		points = append(points, airplane.PositionEntry{
			Latitude: station.Latitude, Longitude: station.Longitude, Altitude: altitude,
		})
	}

	return Trail{ICAO: "ABCDEF", Points: points, LostAt: lostAt, Class: TrackTransit}
}

// airwayTracks is one airwayTrack per course, offset and altitude triple.
func airwayTracks(field Airfield, lost time.Time, tracks ...[3]float64) []Trail {
	trails := make([]Trail, 0, len(tracks))
	for _, track := range tracks {
		trails = append(trails, airwayTrack(field, track[0], track[1], track[2], lost))
	}

	return trails
}

func bundle(t *testing.T, trails []Trail) []Airway {
	t.Helper()

	field := airwayTestField()

	var book airwayBook

	return book.refresh(trails, 1, airwayTestNow, &field)
}

func TestAirwayBundling(t *testing.T) {
	t.Parallel()

	field := airwayTestField().field
	lost := airwayTestNow.Add(-10 * time.Minute)

	for _, testCase := range []struct {
		name   string
		tracks [][3]float64 // course, offset, altitude
		want   []int        // tracks per airway, in order
	}{
		{
			name:   "parallel tracks bundle",
			tracks: [][3]float64{{90, 0, 35000}, {90, 1, 35000}, {90, -1, 36000}, {91, 0.5, 34000}},
			want:   []int{4},
		},
		{
			name:   "a stray does not join",
			tracks: [][3]float64{{90, 0, 35000}, {90, 1, 35000}, {90, -1, 35000}, {135, 0, 35000}},
			want:   []int{3},
		},
		{
			name:   "two tracks are not an airway",
			tracks: [][3]float64{{90, 0, 35000}, {90, 1, 35000}},
		},
		{
			name: "opposite directions stay apart",
			tracks: [][3]float64{
				{90, 0, 35000}, {90, 1, 35000}, {90, -1, 35000},
				{270, 0, 36000}, {270, 1, 36000}, {270, -1, 36000},
			},
			want: []int{3, 3},
		},
		{
			name:   "under the floor is not an airway",
			tracks: [][3]float64{{90, 0, 24000}, {90, 1, 24000}, {90, -1, 24000}},
		},
		{
			name:   "at the floor is",
			tracks: [][3]float64{{90, 0, AirwayFloorFt}, {90, 1, AirwayFloorFt}, {90, -1, AirwayFloorFt}},
			want:   []int{3},
		},
		{
			name: "an off-centre first track does not split the airway",
			tracks: [][3]float64{
				{90, -1, 35000}, {90, 0, 35000}, {90, 1, 35000}, {90, 0.5, 35000}, {90, 3.5, 35000},
			},
			want: []int{5},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := bundle(t, airwayTracks(field, lost, testCase.tracks...))
			if len(got) != len(testCase.want) {
				t.Fatalf("got %d airways, want %d: %+v", len(got), len(testCase.want), got)
			}

			for index, want := range testCase.want {
				if got[index].Tracks != want {
					t.Errorf("airway %d has %d tracks, want %d", index, got[index].Tracks, want)
				}
			}
		})
	}
}

func TestAirwayDirectionAndAltitude(t *testing.T) {
	t.Parallel()

	field := airwayTestField().field
	trails := airwayTracks(field, airwayTestNow.Add(-10*time.Minute), [3]float64{90, 0, 35000},
		[3]float64{90, 1, 37000}, [3]float64{90, -1, 36000})

	got := bundle(t, trails)
	if len(got) != 1 {
		t.Fatalf("got %d airways, want 1", len(got))
	}

	airway := got[0]
	if airway.FromLongitude >= airway.ToLongitude {
		t.Errorf("eastbound airway runs from %.3f to %.3f east; want west to east",
			airway.FromLongitude, airway.ToLongitude)
	}

	if math.Abs(airway.Altitude-36000) > 1 {
		t.Errorf("altitude %.0f, want 36000", airway.Altitude)
	}

	for index := range trails {
		trail := &trails[index]
		if !trail.Airway || trail.AirwayFrom != 0 || trail.AirwayTo != len(trail.Points)-1 {
			t.Errorf("trail %d: airway %v %d..%d, want the whole track", index,
				trail.Airway, trail.AirwayFrom, trail.AirwayTo)
		}
	}
}

func TestAirwayCap(t *testing.T) {
	t.Parallel()

	field := airwayTestField().field
	trails := make([]Trail, 0, 120)

	for line := range 40 {
		for offset := range 3 {
			trails = append(trails, airwayTracks(field, airwayTestNow.Add(-10*time.Minute),
				[3]float64{float64(line) * 9, float64(offset) * 0.5, airwayCruise})...)
		}
	}

	if got := bundle(t, trails); len(got) != airwayMax {
		t.Errorf("got %d airways, want the cap of %d", len(got), airwayMax)
	}
}

// TestAirwayFades ages one book through its cases in order, so they do not
// run in parallel.
//
//nolint:paralleltest // see above.
func TestAirwayFades(t *testing.T) {
	field := airwayTestField()
	trails := airwayTracks(field.field, airwayTestNow,
		[3]float64{90, 0, airwayCruise}, [3]float64{90, 1, airwayCruise}, [3]float64{90, -1, airwayCruise})

	var book airwayBook

	for _, testCase := range []struct {
		name     string
		after    time.Duration
		min, max float64
	}{
		{"fresh", 0, 1, 1},
		{"held, only the weights halving", 25 * time.Minute, 0.85, 0.9},
		{"fading", time.Hour, 0.05, 0.95},
		{"gone", 2 * time.Hour, 0, 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := book.refresh(trails, 1, airwayTestNow.Add(testCase.after), &field)
			if len(got) != 1 {
				t.Fatalf("got %d airways, want 1", len(got))
			}

			if got[0].Strength < testCase.min || got[0].Strength > testCase.max {
				t.Errorf("strength %.3f, want %.2f to %.2f", got[0].Strength, testCase.min, testCase.max)
			}
		})
	}
}

// TestAirwayFollowsTheRoutes checks the book recomputes when the routes'
// generation moves and only then: a trail added under the same generation
// is not seen until the next one.
func TestAirwayFollowsTheRoutes(t *testing.T) {
	t.Parallel()

	field := airwayTestField()
	lost := airwayTestNow.Add(-10 * time.Minute)
	trails := airwayTracks(field.field, lost,
		[3]float64{90, 0, airwayCruise}, [3]float64{90, 1, airwayCruise}, [3]float64{90, -1, airwayCruise})

	var book airwayBook

	book.refresh(trails, 1, airwayTestNow, &field)

	trails = append(trails, airwayTrack(field.field, 90, 0.5, airwayCruise, lost))

	if got := book.refresh(trails, 1, airwayTestNow, &field); got[0].Tracks != 3 {
		t.Errorf("same generation: %d tracks, want the 3 it was computed from", got[0].Tracks)
	}

	if got := book.refresh(trails, 2, airwayTestNow, &field); got[0].Tracks != 4 {
		t.Errorf("next generation: %d tracks, want 4", got[0].Tracks)
	}
}

// TestAirwayRefreshAllocatesNothing does not call t.Parallel:
// testing.AllocsPerRun panics when it is.
//
//nolint:paralleltest // see above.
func TestAirwayRefreshAllocatesNothing(t *testing.T) {
	field := airwayTestField()
	trails := airwayTracks(field.field, airwayTestNow.Add(-10*time.Minute),
		[3]float64{90, 0, airwayCruise}, [3]float64{90, 1, airwayCruise}, [3]float64{90, -1, airwayCruise},
		[3]float64{0, 5, 38000})

	var book airwayBook

	generation := uint64(1)
	book.refresh(trails, generation, airwayTestNow, &field)

	recompute := func() {
		generation++
		book.refresh(trails, generation, airwayTestNow, &field)
	}

	if allocs := testing.AllocsPerRun(20, recompute); allocs != 0 {
		t.Errorf("refresh allocates %.0f times, want 0", allocs)
	}
}

func TestDemoAirwaysOption(t *testing.T) {
	t.Parallel()

	clock := func() time.Time { return airwayTestNow }

	plain, err := NewDemo(WithDemoClock(clock))
	if err != nil {
		t.Fatal(err)
	}

	extra, err := NewDemo(WithDemoClock(clock), WithDemoAirways())
	if err != nil {
		t.Fatal(err)
	}

	before, after := plain.Frame(), extra.Frame()

	if len(after.Ghosts) != len(before.Ghosts)+36 {
		t.Errorf("WithDemoAirways adds %d tracks, want 36", len(after.Ghosts)-len(before.Ghosts))
	}

	if len(after.Airways) != 7 {
		t.Errorf("the demo with airways yields %d airways, want 7", len(after.Airways))
	}
}

// TestAirwayDogleg checks a track that turns is cut at the turn, and each
// straight leg joins the airway it flew: three tracks with the same thirty
// degree bend make two airways of three.
func TestAirwayDogleg(t *testing.T) {
	t.Parallel()

	field := airwayTestField().field
	lost := airwayTestNow.Add(-10 * time.Minute)
	trails := make([]Trail, 0, 3)

	for _, offset := range []float64{-1, 0, 1} {
		first := airwayTrack(field, 90, offset, airwayCruise, lost)
		second := airwayTrack(field, 120, offset, airwayCruise, lost)

		// The first track's western half, then the second's eastern half:
		// they cross at the field, so the join is a bend there.
		points := slices.Concat(first.Points[:51], second.Points[51:])
		trails = append(trails, Trail{ICAO: "ABCDEF", Points: points, LostAt: lost, Class: TrackTransit})
	}

	got := bundle(t, trails)
	if len(got) != 2 || got[0].Tracks != 3 || got[1].Tracks != 3 {
		t.Fatalf("got %+v, want two airways of three tracks", got)
	}
}

// TestAirwayShortLegs checks a leg under airwayMinLegNm counts for nothing,
// and a fix under the floor ends a leg: a track that dips under it every
// few fixes never flies a leg long enough.
func TestAirwayShortLegs(t *testing.T) {
	t.Parallel()

	field := airwayTestField().field
	trails := airwayTracks(field, airwayTestNow.Add(-10*time.Minute),
		[3]float64{90, -1, airwayCruise}, [3]float64{90, 0, airwayCruise}, [3]float64{90, 1, airwayCruise})

	for _, trail := range trails {
		for index := range trail.Points {
			if index%10 == 0 {
				trail.Points[index].Altitude = AirwayFloorFt - 1000
			}
		}
	}

	if got := bundle(t, trails); len(got) != 0 {
		t.Errorf("got %d airways from legs of 12 nm, want none", len(got))
	}
}

// TestAirwayBookFull checks a compute that runs out of bundles drops the
// legs that would have opened one, rather than overwriting a bundle it has.
// Forty bearings nine degrees apart at four offsets ten miles apart open
// 160 lines, past the cap of 128.
func TestAirwayBookFull(t *testing.T) {
	t.Parallel()

	field := airwayTestField().field
	trails := make([]Trail, 0, 160)

	for line := range 40 {
		for offset := range 4 {
			trails = append(trails, airwayTracks(field, airwayTestNow.Add(-10*time.Minute),
				[3]float64{float64(line) * 9, float64(offset) * 10, airwayCruise})...)
		}
	}

	if got := bundle(t, trails); len(got) != 0 {
		t.Errorf("got %d airways from one track each, want none", len(got))
	}
}

// TestAirwayHeadEmpty checks the fitted line of a bundle no leg joined stays
// on the bundle's own line rather than dividing by zero.
func TestAirwayHeadEmpty(t *testing.T) {
	t.Parallel()

	head := airwayHead{originX: 3, originY: 4, dirX: 1, bearing: 90}

	if slope, intercept := head.fit(); slope != 0 || intercept != 0 {
		t.Errorf("fit = %v, %v, want 0, 0", slope, intercept)
	}

	line := head.line()
	if line.originX != 3 || line.originY != 4 || line.dirX != 1 || line.bearing != 90 || line.lastTrail != -1 {
		t.Errorf("line = %+v, want the head's own line with no last trail", line)
	}
}

// TestAirwayFrameWithoutField checks the legs are measured from the newest
// trail's first fix when there is no field, and that nothing is measured
// with no fix at all.
func TestAirwayFrameWithoutField(t *testing.T) {
	t.Parallel()

	field := airwayTestField()
	trails := airwayTracks(field.field, airwayTestNow.Add(-10*time.Minute),
		[3]float64{90, 0, airwayCruise}, [3]float64{90, 1, airwayCruise}, [3]float64{90, -1, airwayCruise})
	trails = append(trails, Trail{ICAO: "EMPTY0", LostAt: airwayTestNow, Class: TrackTransit})

	unplaced := field
	unplaced.placed = false

	var book, empty airwayBook

	if got := book.refresh(trails, 1, airwayTestNow, &unplaced); len(got) != 1 || got[0].Tracks != 3 {
		t.Errorf("without a field got %+v, want one airway of three", got)
	}

	if got := empty.refresh(trails[3:], 1, airwayTestNow, &unplaced); len(got) != 0 {
		t.Errorf("with no fix to measure from got %d airways, want none", len(got))
	}
}
