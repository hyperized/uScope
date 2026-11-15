package source_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
)

// Schiphol's own receiver, which is also where the demo fleet flies.
const (
	schipholLat = 52.3105
	schipholLon = 4.7683

	// demoHistory is how many tracks the demo invents for the corridors.
	demoHistory = 70
)

func TestParseCorridorHalfLife(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		text    string
		want    time.Duration
		wantErr bool
	}{
		{text: "2h", want: 2 * time.Hour},
		{text: "90m", want: 90 * time.Minute},
		{text: "10m", want: 10 * time.Minute},
		{text: "24h", want: 24 * time.Hour},
		{text: "0", wantErr: true},
		{text: "9m59s", wantErr: true},
		{text: "24h1s", wantErr: true},
		{text: "-2h", wantErr: true},
		{text: "two hours", wantErr: true},
		{text: "", wantErr: true},
	} {
		t.Run(testCase.text, func(t *testing.T) {
			t.Parallel()

			got, err := source.ParseCorridorHalfLife(testCase.text)
			if testCase.wantErr {
				if !errors.Is(err, source.ErrCorridorHalfLife) {
					t.Errorf("err = %v, want ErrCorridorHalfLife", err)
				}

				return
			}

			if err != nil || got != testCase.want {
				t.Errorf("ParseCorridorHalfLife(%q) = %s, %v, want %s", testCase.text, got, err, testCase.want)
			}
		})
	}
}

func TestCheckCorridorHalfLife(t *testing.T) {
	t.Parallel()

	if err := source.CheckCorridorHalfLife(source.DefaultCorridorHalfLife); err != nil {
		t.Errorf("CheckCorridorHalfLife(default) = %v, want nil", err)
	}

	if err := source.CheckCorridorHalfLife(time.Minute); !errors.Is(err, source.ErrCorridorHalfLife) {
		t.Errorf("CheckCorridorHalfLife(1m) = %v, want ErrCorridorHalfLife", err)
	}
}

func TestUnknownCorridorFieldIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := source.NewLive(source.WithIngest(&fakeIngest{}), source.WithCorridorField("ZZZZ")); !errors.Is(
		err, source.ErrUnknownAirfield) {
		t.Errorf("NewLive err = %v, want ErrUnknownAirfield", err)
	}

	if _, err := source.NewDemo(source.WithDemoCorridorField("eham")); !errors.Is(err, source.ErrUnknownAirfield) {
		t.Errorf("NewDemo err = %v, want ErrUnknownAirfield", err)
	}
}

func TestEmptyCarriesNoCorridors(t *testing.T) {
	t.Parallel()

	frame := (source.Empty{}).Frame()
	if frame.Corridors.Placed() || frame.Ghosts != nil {
		t.Errorf("Empty frame carries %+v and %d ghosts, want neither", frame.Corridors, len(frame.Ghosts))
	}
}

// classCount is how many of each class a set of trails holds.
func classCount(trails []source.Trail) map[source.TrackClass]int {
	counts := make(map[source.TrackClass]int)

	for _, trail := range trails {
		counts[trail.Class]++
	}

	return counts
}

// TestDemoSeedsTheExposure checks the first frame carries the invented
// history: forty tracks lost over the last two hours, oldest first, of all
// three classes, around EHAM.
func TestDemoSeedsTheExposure(t *testing.T) {
	t.Parallel()

	demo, err := source.NewDemo(source.WithDemoClock(func() time.Time { return fixedNow }))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	frame := demo.Frame()
	if frame.Corridors.Field.ICAO != classifyFieldICAO || frame.Corridors.HalfLife != source.DefaultCorridorHalfLife {
		t.Errorf("Corridors = %+v, want EHAM at the default half-life", frame.Corridors)
	}

	if len(frame.Ghosts) != demoHistory || frame.GhostGeneration == 0 {
		t.Fatalf("ghosts = %d generation %d, want %d and a moved generation",
			len(frame.Ghosts), frame.GhostGeneration, demoHistory)
	}

	oldest := fixedNow.Add(-2 * time.Hour)

	for index, trail := range frame.Ghosts {
		if trail.LostAt.Before(oldest) || !trail.LostAt.Before(fixedNow) {
			t.Errorf("ghost %d lost at %v, want inside the last two hours", index, trail.LostAt)
		}

		if index > 0 && trail.LostAt.Before(frame.Ghosts[index-1].LostAt) {
			t.Errorf("ghost %d was lost before the one ahead of it", index)
		}
	}

	counts := classCount(frame.Ghosts)
	if counts[source.TrackInbound] == 0 || counts[source.TrackOutbound] == 0 || counts[source.TrackTransit] == 0 {
		t.Errorf("classes = %v, want all three", counts)
	}
}

// TestDemoExposureSlopes checks the 3D picture has something to slope: an
// arrival never climbs and a departure never descends.
func TestDemoExposureSlopes(t *testing.T) {
	t.Parallel()

	demo, err := source.NewDemo(source.WithDemoClock(func() time.Time { return fixedNow }))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	for _, trail := range demo.Frame().Ghosts {
		points := trail.Points
		for index := 1; index < len(points); index++ {
			climb := points[index].Altitude - points[index-1].Altitude

			if trail.Class == source.TrackInbound && climb > 0 || trail.Class == source.TrackOutbound && climb < 0 {
				t.Fatalf("%s (class %d) moves %g ft at fix %d", trail.ICAO, trail.Class, climb, index)
			}
		}

		if first, last := points[0].Altitude, points[len(points)-1].Altitude; first <= 0 || last <= 0 {
			t.Errorf("%s ends at %g and %g ft, want both decoded", trail.ICAO, first, last)
		}
	}
}

// TestDemoExposureIsDeterministic checks two demos on one seed invent the
// same history, and a different seed a different one.
func TestDemoExposureIsDeterministic(t *testing.T) {
	t.Parallel()

	first := demoGhosts(t)
	again := demoGhosts(t)
	other := demoGhosts(t, source.WithSeed(1))

	for index := range first {
		if first[index].Callsign != again[index].Callsign || !first[index].LostAt.Equal(again[index].LostAt) ||
			first[index].Points[0] != again[index].Points[0] {
			t.Fatalf("ghost %d differs between two runs on one seed", index)
		}
	}

	if first[0].Points[0] == other[0].Points[0] {
		t.Error("a different seed invented the same first track")
	}
}

// demoGhosts is the first frame's ghosts from a demo on fixedNow.
func demoGhosts(tb testing.TB, opts ...source.DemoOption) []source.Trail {
	tb.Helper()

	demo, err := source.NewDemo(append([]source.DemoOption{
		source.WithDemoClock(func() time.Time { return fixedNow }),
	}, opts...)...)
	if err != nil {
		tb.Fatalf("NewDemo: %v", err)
	}

	return demo.Frame().Ghosts
}

func TestDemoExposurePinnedAndUnplaced(t *testing.T) {
	t.Parallel()

	pinned, err := source.NewDemo(source.WithDemoCorridorField("EGLL"))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	frame := pinned.Frame()
	if frame.Corridors.Field.ICAO != "EGLL" || len(frame.Ghosts) != demoHistory {
		t.Errorf("pinned demo = %+v with %d ghosts, want EGLL with %d",
			frame.Corridors, len(frame.Ghosts), demoHistory)
	}

	nowhere, err := source.NewDemo(source.WithDemoLocation(0, -150))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	if frame := nowhere.Frame(); frame.Corridors.Placed() || len(frame.Ghosts) != 0 {
		t.Errorf("a demo in mid-Pacific = %+v with %d ghosts, want no field and none",
			frame.Corridors, len(frame.Ghosts))
	}
}

// TestDemoExposureAgesOut checks the half-life bounds the ring: at the
// shortest one, six half-lives is an hour, and the half of the history older
// than that is gone from the first frame.
func TestDemoExposureAgesOut(t *testing.T) {
	t.Parallel()

	ghosts := demoGhosts(t, source.WithDemoCorridorHalfLife(10*time.Minute))
	if len(ghosts) == 0 || len(ghosts) >= demoHistory {
		t.Fatalf("ghosts = %d, want some of the %d aged out", len(ghosts), demoHistory)
	}

	for _, trail := range ghosts {
		if trail.LostAt.Before(fixedNow.Add(-time.Hour)) {
			t.Errorf("%s lost at %v survived an hour's limit", trail.ICAO, trail.LostAt)
		}
	}
}

func TestLiveCorridorsWaitForAFix(t *testing.T) {
	t.Parallel()

	live, _ := startLiveIngest(t)

	if live.Frame().Corridors.Placed() {
		t.Error("a live source with no position has an airfield")
	}
}

// TestLiveClassifiesALostArrival checks a track that ends three miles from
// Schiphol comes back as an inbound ghost stamped with the frame's clock.
func TestLiveClassifiesALostArrival(t *testing.T) {
	t.Parallel()

	live, store := startLiveIngest(t,
		source.WithManualLocation(schipholLat, schipholLon), source.WithClock(func() time.Time { return fixedNow }))

	addAircraft(t, store, "484AC1", "KLM123", 1500, schipholLat+0.05, schipholLon)
	live.Frame()
	store.Prune(0)

	frame := live.Frame()
	if frame.Corridors.Field.ICAO != classifyFieldICAO {
		t.Errorf("Field = %q, want EHAM", frame.Corridors.Field.ICAO)
	}

	if len(frame.Ghosts) != 1 || frame.Ghosts[0].Class != source.TrackInbound ||
		!frame.Ghosts[0].LostAt.Equal(fixedNow) {
		t.Errorf("ghosts = %+v, want one inbound lost at %v", frame.Ghosts, fixedNow)
	}
}

func TestLiveCorridorFieldPin(t *testing.T) {
	t.Parallel()

	live, _ := startLiveIngest(t, source.WithCorridorField("EGLL"))

	if got := live.Frame().Corridors; got.Field.ICAO != "EGLL" {
		t.Errorf("pinned live corridors = %+v, want EGLL before any fix", got)
	}
}

// TestAltitudeBandOf checks both sides of each band boundary, and that an
// altitude nobody decoded is in no band.
func TestAltitudeBandOf(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		altitude float64
		want     int
		ok       bool
	}{
		{name: "undecoded", altitude: 0},
		{name: "negative", altitude: -100},
		{name: "NaN", altitude: math.NaN()},
		{name: "a foot up", altitude: 1, want: source.BandLow, ok: true},
		{name: "under the low ceiling", altitude: source.LowCeilingFt - 1, want: source.BandLow, ok: true},
		{name: "at the low ceiling", altitude: source.LowCeilingFt, want: source.BandMid, ok: true},
		{name: "under the mid ceiling", altitude: source.MidCeilingFt - 1, want: source.BandMid, ok: true},
		{name: "at the mid ceiling", altitude: source.MidCeilingFt, want: source.BandHigh, ok: true},
		{name: "cruise", altitude: 41000, want: source.BandHigh, ok: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			band, ok := source.AltitudeBandOf(testCase.altitude)
			if band != testCase.want || ok != testCase.ok {
				t.Errorf("AltitudeBandOf(%g) = %d, %t, want %d, %t",
					testCase.altitude, band, ok, testCase.want, testCase.ok)
			}
		})
	}
}

// classifyField is Schiphol, where the classifier tests stand.
const (
	classifyFieldICAO = "EHAM"
	classifyFieldLat  = 52.3086
	classifyFieldLon  = 4.7639

	// thirtyNmDeg is thirty nautical miles of latitude in degrees.
	thirtyNmDeg = 0.5
)

func classifyCorridors() source.Corridors {
	return source.Corridors{Field: source.Airfield{
		ICAO: classifyFieldICAO, Latitude: classifyFieldLat, Longitude: classifyFieldLon,
	}}
}

// southOfField is a snapshot thirty miles south of the field, so the field
// lies due north of it.
func southOfField(heading, altitude, vertRate float64) airplane.Snapshot {
	return airplane.Snapshot{
		Latitude: classifyFieldLat - thirtyNmDeg, Longitude: classifyFieldLon,
		Heading: heading, Altitude: altitude, VertRate: vertRate,
	}
}

// beyondTerminal is southOfField moved ten miles further out, past the
// terminal-area radius.
func beyondTerminal(heading, altitude, vertRate float64) airplane.Snapshot {
	plane := southOfField(heading, altitude, vertRate)
	plane.Latitude -= thirtyNmDeg / 3

	return plane
}

func TestCorridorsClassify(t *testing.T) {
	t.Parallel()

	near := airplane.PositionEntry{Latitude: classifyFieldLat + 0.01, Longitude: classifyFieldLon}
	far := airplane.PositionEntry{Latitude: classifyFieldLat + thirtyNmDeg, Longitude: classifyFieldLon}

	// A bearing of 350 from a spot east of the line north to the field.
	eastOffset := thirtyNmDeg * math.Tan(10*math.Pi/180) * 60 /
		(60 * math.Cos(classifyFieldLat*math.Pi/180))
	wrapped := southOfField(10, 3000, 0)
	wrapped.Longitude = classifyFieldLon + eastOffset

	withHistory := func(plane airplane.Snapshot, history ...airplane.PositionEntry) airplane.Snapshot {
		plane.PositionHistory = history

		return plane
	}

	for _, testCase := range []struct {
		name   string
		placed bool
		plane  airplane.Snapshot
		want   source.TrackClass
	}{
		{"not placed", false, southOfField(0, 3000, 0), source.TrackTransit},
		{"no position", true, airplane.Snapshot{Heading: 0, Altitude: 3000}, source.TrackTransit},
		{"undecoded heading", true, southOfField(-1, 3000, 0), source.TrackTransit},
		{"outbound by history", true, withHistory(southOfField(180, 9000, 0), near, far), source.TrackOutbound},
		{"history near at both ends falls through", true,
			withHistory(southOfField(0, 3000, 0), near, near), source.TrackInbound},
		{"history starting far falls through", true,
			withHistory(southOfField(90, 20000, 0), far, far), source.TrackTransit},
		{"inbound low and pointed at the field", true, southOfField(0, 3000, 0), source.TrackInbound},
		{"inbound high but descending", true, southOfField(0, 20000, -300), source.TrackInbound},
		{"high and level towards the field", true, southOfField(0, 20000, 0), source.TrackTransit},
		{"pointing away and climbing", true, southOfField(180, 20000, 300), source.TrackOutbound},
		{"pointing away but level", true, southOfField(180, 20000, 0), source.TrackTransit},
		{"crossing", true, southOfField(90, 3000, 0), source.TrackTransit},
		{"heading wraps round north", true, wrapped, source.TrackInbound},
		{"terminal descent on a crossing heading", true, southOfField(90, 3000, -300), source.TrackInbound},
		{"terminal descent pointing away", true, southOfField(180, 3000, -1200), source.TrackInbound},
		{"terminal climb on a crossing heading", true, southOfField(90, 3000, 300), source.TrackOutbound},
		{"terminal climb pointing at the field", true, southOfField(0, 3000, 1500), source.TrackOutbound},
		{"terminal level falls through to the cones", true, southOfField(0, 3000, 299), source.TrackInbound},
		{"terminal level crossing is transit", true, southOfField(90, 3000, -299), source.TrackTransit},
		{"descent outside the terminal radius", true, beyondTerminal(90, 3000, -1200), source.TrackTransit},
		{"descent at the terminal ceiling", true, southOfField(90, 10000, -1200), source.TrackTransit},
		{"history beats the terminal rule", true,
			withHistory(southOfField(0, 3000, -1200), near, far), source.TrackOutbound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			corridors := classifyCorridors()
			if !testCase.placed {
				corridors = source.Corridors{}
			}

			if got := corridors.Classify(testCase.plane); got != testCase.want {
				t.Fatalf("Classify = %v, want %v", got, testCase.want)
			}
		})
	}
}

//nolint:paralleltest // AllocsPerRun panics inside a parallel test.
func TestCorridorsClassifyAllocatesNothing(t *testing.T) {
	corridors := classifyCorridors()
	plane := southOfField(0, 3000, 0)
	plane.PositionHistory = []airplane.PositionEntry{
		{Latitude: classifyFieldLat + 0.01, Longitude: classifyFieldLon},
		{Latitude: classifyFieldLat + thirtyNmDeg, Longitude: classifyFieldLon},
	}

	allocs := testing.AllocsPerRun(100, func() { _ = corridors.Classify(plane) })
	if allocs != 0 {
		t.Fatalf("Classify allocated %v times, want 0", allocs)
	}
}
