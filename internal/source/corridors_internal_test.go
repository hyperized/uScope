package source

import (
	"math"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
)

// schiphol is the field the demo receiver resolves to.
const schiphol = "EHAM"

// equatorField sits on the equator so a degree of longitude is exactly sixty
// nautical miles and the distances in these tests can be worked out by eye.
//
//nolint:gochecknoglobals // read-only test fixture.
var equatorField = Airfield{ICAO: "TEST", Latitude: 0, Longitude: 10}

//nolint:gochecknoglobals // read-only test fixture.
var corridorEpoch = time.Date(2026, time.November, 2, 9, 0, 0, 0, time.UTC)

// placedCorridors is an exposure classified against equatorField.
func placedCorridors(halfLife time.Duration) corridors {
	return corridors{field: equatorField, placed: true, halfLife: halfLife}
}

// fixAt is a fix eastNm and northNm from equatorField.
func fixAt(eastNm, northNm, altitudeFt float64) airplane.PositionEntry {
	return airplane.PositionEntry{
		Latitude: northNm / nmPerDegree, Longitude: equatorField.Longitude + eastNm/nmPerDegree, Altitude: altitudeFt,
	}
}

func TestCorridorClassify(t *testing.T) {
	t.Parallel()

	near, edge, far := fixAt(3, 0, 1000), fixAt(0, corridorFieldNm, 1000), fixAt(40, 0, 30000)

	for _, testCase := range []struct {
		name   string
		placed bool
		points []airplane.PositionEntry
		want   TrackClass
	}{
		{name: "no field yet", points: []airplane.PositionEntry{far, near}, want: TrackTransit},
		{name: "no fixes", placed: true, want: TrackTransit},
		{name: "ends at the field", placed: true, points: []airplane.PositionEntry{far, near}, want: TrackInbound},
		{name: "ends on the edge", placed: true, points: []airplane.PositionEntry{far, edge}, want: TrackInbound},
		{name: "starts at the field", placed: true, points: []airplane.PositionEntry{near, far}, want: TrackOutbound},
		{name: "a circuit", placed: true, points: []airplane.PositionEntry{near, far, near}, want: TrackInbound},
		{name: "passes by", placed: true, points: []airplane.PositionEntry{far, fixAt(-40, 0, 30000)}},
		{name: "one fix at the field", placed: true, points: []airplane.PositionEntry{near}, want: TrackInbound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			exposure := placedCorridors(time.Hour)
			exposure.placed = testCase.placed

			if got := exposure.classify(testCase.points); got != testCase.want {
				t.Errorf("classify = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestCorridorResolve(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		receiver Receiver
		want     string
		ok       bool
	}{
		{name: "no fix", receiver: Receiver{Latitude: demoLatitude, Longitude: demoLongitude}},
		{
			name:     "loose estimate",
			receiver: Receiver{Latitude: demoLatitude, Longitude: demoLongitude, Mode: FixEstimated, ConfidenceNm: 97},
		},
		{
			name:     "estimate with no radius",
			receiver: Receiver{Latitude: demoLatitude, Longitude: demoLongitude, Mode: FixEstimated},
		},
		{
			name: "held GPS fix inside the radius",
			receiver: Receiver{
				Latitude: demoLatitude, Longitude: demoLongitude, Mode: FixGPSNoFix, ConfidenceNm: 4,
			},
		},
		{
			name: "settled estimate",
			receiver: Receiver{
				Latitude: demoLatitude, Longitude: demoLongitude, Mode: FixEstimated, ConfidenceNm: 97, SpreadNm: 4,
			},
			want: schiphol, ok: true,
		},
		{
			name: "estimate on the radius",
			receiver: Receiver{
				Latitude: demoLatitude, Longitude: demoLongitude, Mode: FixEstimated,
				ConfidenceNm: corridorEstimateDoubtNm,
			},
			want: schiphol, ok: true,
		},
		{name: "far from anything", receiver: Receiver{Latitude: 0, Longitude: -150, HasFix: true}},
		{
			name: "Schiphol", receiver: Receiver{Latitude: demoLatitude, Longitude: demoLongitude, HasFix: true},
			want: schiphol, ok: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			exposure, err := newCorridors(DefaultCorridorHalfLife, "")
			if err != nil {
				t.Fatalf("newCorridors: %v", err)
			}

			if ok := exposure.resolve(testCase.receiver); ok != testCase.ok || exposure.field.ICAO != testCase.want {
				t.Errorf("resolve = %v field %q, want %v field %q", ok, exposure.field.ICAO, testCase.ok, testCase.want)
			}
		})
	}
}

func TestCorridorResolveKeepsThePinnedField(t *testing.T) {
	t.Parallel()

	exposure, err := newCorridors(DefaultCorridorHalfLife, "EGLL")
	if err != nil {
		t.Fatalf("newCorridors: %v", err)
	}

	if !exposure.resolve(Receiver{Latitude: demoLatitude, Longitude: demoLongitude, HasFix: true}) {
		t.Fatal("resolve = false on a pinned field")
	}

	if got := exposure.public(); got.Field.ICAO != "EGLL" || got.HalfLife != DefaultCorridorHalfLife {
		t.Errorf("public = %+v, want EGLL at the default half-life", got)
	}
}

func TestLookupAirfield(t *testing.T) {
	t.Parallel()

	if field, ok := lookupAirfield(schiphol); !ok || field.ICAO != schiphol {
		t.Errorf("lookupAirfield(EHAM) = %+v, %v", field, ok)
	}

	if _, ok := lookupAirfield("eham"); ok {
		t.Error("lookupAirfield(eham) = true, want an exact match only")
	}
}

func TestCorridorOptionsBounds(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		give time.Duration
		want time.Duration
	}{
		{name: "zero", give: 0, want: DefaultCorridorHalfLife},
		{name: "too short", give: corridorMinHalfLife - time.Second, want: DefaultCorridorHalfLife},
		{name: "too long", give: corridorMaxHalfLife + time.Second, want: DefaultCorridorHalfLife},
		{name: "shortest", give: corridorMinHalfLife, want: corridorMinHalfLife},
		{name: "longest", give: corridorMaxHalfLife, want: corridorMaxHalfLife},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			live, err := NewLive(WithIngest(stubIngest{}), WithCorridorHalfLife(testCase.give))
			if err != nil {
				t.Fatalf("NewLive: %v", err)
			}

			if live.ghosts.corridors.halfLife != testCase.want {
				t.Errorf("Live half-life = %s, want %s", live.ghosts.corridors.halfLife, testCase.want)
			}

			demo, err := NewDemo(WithDemoCorridorHalfLife(testCase.give))
			if err != nil {
				t.Fatalf("NewDemo: %v", err)
			}

			if demo.ghosts.corridors.halfLife != testCase.want {
				t.Errorf("Demo half-life = %s, want %s", demo.ghosts.corridors.halfLife, testCase.want)
			}
		})
	}
}

// arrivalSnapshot is an aircraft whose track ends three miles from
// equatorField, which is an arrival once it is lost.
func arrivalSnapshot(icao string) airplane.Snapshot {
	return airplane.Snapshot{
		ICAO:            icao,
		PositionHistory: []airplane.PositionEntry{fixAt(30, 0, 9000), fixAt(3, 0, 1000)},
	}
}

// TestGhostsStampALostTrack checks a trail leaves the sky with the frame time
// it was lost on and a class decided from its ends.
func TestGhostsStampALostTrack(t *testing.T) {
	t.Parallel()

	tracker := newGhosts()
	tracker.corridors = placedCorridors(time.Hour)

	tracker.observe(airplanes.List{arrivalSnapshot("E00001")}, Receiver{}, corridorEpoch)

	lostAt := corridorEpoch.Add(time.Second)

	lost := tracker.observe(nil, Receiver{}, lostAt)
	if len(lost) != 1 {
		t.Fatalf("ghosts = %d, want 1", len(lost))
	}

	if !lost[0].LostAt.Equal(lostAt) || lost[0].Class != TrackInbound {
		t.Errorf("ghost = %v %d, want %v inbound", lost[0].LostAt, lost[0].Class, lostAt)
	}
}

// TestGhostsResolveBeforeTheyClassify checks the field found on a frame is
// the one the trails lost on that same frame are classified against.
func TestGhostsResolveBeforeTheyClassify(t *testing.T) {
	t.Parallel()

	tracker := newGhosts()

	field, ok := lookupAirfield(schiphol)
	if !ok {
		t.Fatal("EHAM missing from the airport list")
	}

	arrival := airplane.Snapshot{
		ICAO:            "E00002",
		PositionHistory: []airplane.PositionEntry{{Latitude: field.Latitude + 0.05, Longitude: field.Longitude}},
	}

	tracker.observe(airplanes.List{arrival}, Receiver{}, corridorEpoch)

	frame := tracker.frame(nil, Receiver{Latitude: demoLatitude, Longitude: demoLongitude, HasFix: true}, corridorEpoch)
	if len(frame.trails) != 1 || frame.trails[0].Class != TrackInbound {
		t.Fatalf("trails = %+v, want one inbound", frame.trails)
	}

	if frame.corridors.Field.ICAO != schiphol || frame.generation == 0 {
		t.Errorf("frame = %+v generation %d, want EHAM and a moved generation", frame.corridors, frame.generation)
	}
}

// pushAged puts a trail into the ring that was lost age before corridorEpoch.
func pushAged(tracker *ghosts, icao string, age time.Duration) {
	tracker.push(Trail{
		ICAO: icao, Points: []airplane.PositionEntry{{Latitude: 1}}, LostAt: corridorEpoch.Add(-age),
	})
}

func TestGhostsExpireByAge(t *testing.T) {
	t.Parallel()

	const halfLife = 10 * time.Minute

	life := corridorExposureLives * halfLife

	tracker := newGhosts()
	tracker.corridors = placedCorridors(halfLife)

	pushAged(&tracker, "F00001", life+time.Minute)
	pushAged(&tracker, "F00002", life+time.Second)
	pushAged(&tracker, "F00003", life)
	pushAged(&tracker, "F00004", time.Minute)

	before := tracker.version

	got := ghostICAOs(tracker.observe(nil, Receiver{}, corridorEpoch))
	if want := []string{"F00003", "F00004"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ghosts after expiry = %v, want %v", got, want)
	}

	if tracker.version == before {
		t.Error("expiry left the version where it was")
	}

	// Inside the cadence nothing is looked at, so F00003 survives a check it
	// would fail.
	got = ghostICAOs(tracker.observe(nil, Receiver{}, corridorEpoch.Add(corridorExpireEvery-time.Second)))
	if len(got) != 2 {
		t.Errorf("ghosts inside the cadence = %v, want both still there", got)
	}

	got = ghostICAOs(tracker.observe(nil, Receiver{}, corridorEpoch.Add(corridorExpireEvery)))
	if len(got) != 1 || got[0] != "F00004" {
		t.Errorf("ghosts a minute on = %v, want [F00004]", got)
	}
}

// TestGhostsExpireWhenTheClockGoesBack checks a clock stepped backwards runs
// the check rather than waiting for the old reading to come round again.
func TestGhostsExpireWhenTheClockGoesBack(t *testing.T) {
	t.Parallel()

	const halfLife = 10 * time.Minute

	tracker := newGhosts()
	tracker.corridors = placedCorridors(halfLife)
	tracker.lastExpire = corridorEpoch.Add(time.Hour)

	pushAged(&tracker, "F00011", corridorExposureLives*halfLife+time.Minute)

	if got := tracker.observe(nil, Receiver{}, corridorEpoch); len(got) != 0 {
		t.Errorf("ghosts = %v, want the old one expired", ghostICAOs(got))
	}
}

// TestGhostsExpireStepsOverHoles checks a revival's hole at the old end is
// evicted without moving the version, and that the walk carries on past it.
func TestGhostsExpireStepsOverHoles(t *testing.T) {
	t.Parallel()

	const halfLife = 10 * time.Minute

	tracker := newGhosts()
	tracker.corridors = placedCorridors(halfLife)

	pushAged(&tracker, "F00021", time.Minute)
	pushAged(&tracker, "F00022", time.Minute)
	tracker.revive("F00021")

	before := tracker.version
	tracker.expire(corridorEpoch)

	if tracker.evicted != 1 || tracker.version != before {
		t.Errorf("evicted %d version %d, want the hole gone and the version at %d",
			tracker.evicted, tracker.version, before)
	}
}

// TestGhostsVersionFollowsTheRing checks the version moves on a loss and a
// revival, and stays put on a frame that changes nothing.
func TestGhostsVersionFollowsTheRing(t *testing.T) {
	t.Parallel()

	tracker := newGhosts()

	start := tracker.version
	loseGhost(&tracker, "G00001", 2)

	lost := tracker.version
	if lost == start {
		t.Fatal("losing a trail left the version where it was")
	}

	tracker.observe(nil, Receiver{}, time.Time{})

	if tracker.version != lost {
		t.Error("a frame that changed nothing moved the version")
	}

	tracker.observe(airplanes.List{snapshotWithHistory("G00001", 2)}, Receiver{}, time.Time{})

	if tracker.version == lost {
		t.Error("a revival left the version where it was")
	}
}

func TestAngleDiffDeg(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		a, b float64
		want float64
	}{
		{"same", 90, 90, 0},
		{"wraps north", 350, 10, 20},
		{"wraps the other way", 10, 350, 20},
		{"opposite", 0, 180, 180},
		{"negative bearing", -10, 10, 20},
		{"past a full turn", 400, 10, 30},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := angleDiffDeg(testCase.a, testCase.b); got != testCase.want {
				t.Fatalf("angleDiffDeg(%v, %v) = %v, want %v", testCase.a, testCase.b, got, testCase.want)
			}
		})
	}
}

func TestDistanceAgreesWithNear(t *testing.T) {
	t.Parallel()

	exposure := placedCorridors(time.Hour)
	// Sixty miles per degree on the equator: nine and eleven miles out.
	for _, testCase := range []struct {
		eastNm float64
		want   bool
	}{{9, true}, {11, false}} {
		fix := fixAt(testCase.eastNm, 0, 0)
		if got := exposure.near(fix); got != testCase.want {
			t.Fatalf("near at %v nm = %v, want %v", testCase.eastNm, got, testCase.want)
		}

		got := equatorField.distanceNm(fix.Latitude, fix.Longitude)
		if math.Abs(got-testCase.eastNm) > 0.001 {
			t.Fatalf("distanceNm = %v, want %v", got, testCase.eastNm)
		}
	}
}
