package source_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/coverage"
	"github.com/hyperized/uScope/internal/source"
)

// A fixed instant, used wherever a test needs Frame().Now to hold a known
// value instead of whatever time.Now happens to return.
//
//nolint:gochecknoglobals // read-only test fixture, not runtime state.
var fixedNow = time.Date(2026, time.March, 4, 12, 0, 0, 0, time.UTC)

// Stand-in ingest failures. Static so err113 is satisfied; the text is never
// asserted on, only whether stream reported something at all.
var (
	errFakeIngestFailure   = errors.New("fake ingest: stream failed")
	errFakeIngestDisrupted = errors.New("fake ingest: dongle unplugged")
)

// fakeIngest satisfies the unexported ingest interface Live consumes. A test
// only has to set the fields it cares about; the zero value behaves like a
// source that streams nothing and reports nothing.
type fakeIngest struct {
	mu       sync.Mutex
	streamFn func(ctx context.Context, planes *airplanes.Airplanes) error
	calls    int
	source   adsb.SourceInfo
	stats    adsb.Stats
}

// Stream counts the call and then defers to streamFn, or returns nil when
// none was set.
func (f *fakeIngest) Stream(ctx context.Context, planes *airplanes.Airplanes) error {
	f.mu.Lock()
	f.calls++
	stream := f.streamFn
	f.mu.Unlock()

	if stream == nil {
		return nil
	}

	return stream(ctx, planes)
}

// Source returns whatever SourceInfo the test configured.
func (f *fakeIngest) Source() adsb.SourceInfo { return f.source }

// Stats returns whatever Stats the test configured.
func (f *fakeIngest) Stats() adsb.Stats { return f.stats }

// callCount reports how many times Stream has been called, guarded so a test
// can read it while a Stream call is still running on another goroutine.
func (f *fakeIngest) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// syncBuffer is a goroutine-safe bytes.Buffer. Live's stream error report
// happens on the ingest goroutine while the test reads it back from the main
// one, so a plain bytes.Buffer would be a race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, err := s.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("syncBuffer: %w", err)
	}

	return n, nil
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.String()
}

func TestEmpty(t *testing.T) {
	t.Parallel()

	var empty source.Empty

	frame := empty.Frame()
	if frame.Receiver.Label != source.LabelNone {
		t.Errorf("Frame().Receiver.Label = %q, want %q", frame.Receiver.Label, source.LabelNone)
	}

	if len(frame.Planes) != 0 {
		t.Errorf("Frame().Planes = %v, want none", frame.Planes)
	}

	if err := empty.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

func TestNewLiveDefaults(t *testing.T) {
	t.Parallel()

	live, err := source.NewLive()
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	if got := live.Frame().Receiver.Label; got != source.LabelNone {
		t.Errorf("Receiver.Label = %q, want %q (no position known yet)", got, source.LabelNone)
	}
}

// TestNewLiveManualLocation covers the happy path and all four out-of-range
// corners: one past each side of the lat/lon box.
func TestNewLiveManualLocation(t *testing.T) {
	t.Parallel()

	const (
		validLatitude  = 52.31
		validLongitude = 4.76

		latitudeTooHigh  = 91.0
		latitudeTooLow   = -91.0
		longitudeTooHigh = 181.0
		longitudeTooLow  = -181.0
	)

	for _, testCase := range []struct {
		name      string
		latitude  float64
		longitude float64
		wantErr   bool
	}{
		{name: "in range", latitude: validLatitude, longitude: validLongitude, wantErr: false},
		{name: "latitude above range", latitude: latitudeTooHigh, longitude: 0, wantErr: true},
		{name: "latitude below range", latitude: latitudeTooLow, longitude: 0, wantErr: true},
		{name: "longitude above range", latitude: 0, longitude: longitudeTooHigh, wantErr: true},
		{name: "longitude below range", latitude: 0, longitude: longitudeTooLow, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			live, err := source.NewLive(source.WithManualLocation(testCase.latitude, testCase.longitude))
			if testCase.wantErr {
				assertRejectedCoordinate(t, live, err)

				return
			}

			assertManualReceiver(t, live, err, testCase.latitude, testCase.longitude)
		})
	}
}

// assertRejectedCoordinate checks the out-of-range half of a coordinate
// table: an ErrCoordinate error and a nil result.
func assertRejectedCoordinate[T any](t *testing.T, got *T, err error) {
	t.Helper()

	if !errors.Is(err, source.ErrCoordinate) {
		t.Fatalf("error = %v, want ErrCoordinate", err)
	}

	if got != nil {
		t.Errorf("result = %v, want nil", got)
	}
}

// assertManualReceiver checks the in-range half of TestNewLiveManualLocation:
// no error, and a Receiver reporting the manual label at the given position.
func assertManualReceiver(t *testing.T, live *source.Live, err error, latitude, longitude float64) {
	t.Helper()

	if err != nil {
		t.Fatalf("NewLive() unexpected error: %v", err)
	}

	receiver := live.Frame().Receiver
	if receiver.Label != source.LabelManual || !receiver.HasFix {
		t.Errorf("Receiver = %+v, want Label %q and HasFix true", receiver, source.LabelManual)
	}

	if receiver.Latitude != latitude || receiver.Longitude != longitude {
		t.Errorf("Receiver coordinates = (%v, %v), want (%v, %v)",
			receiver.Latitude, receiver.Longitude, latitude, longitude)
	}
}

// TestLiveOptionNilGuards pins the "a nil or zero argument leaves the default
// in place" contract for each LiveOption that has one. Every case proves it
// by behaviour: what NewLive builds still works, rather than reading the
// field the option would have set.
func TestLiveOptionNilGuards(t *testing.T) {
	t.Parallel()

	t.Run("WithClock nil keeps time.Now", func(t *testing.T) {
		t.Parallel()

		live, err := source.NewLive(source.WithClock(nil))
		if err != nil {
			t.Fatalf("NewLive: %v", err)
		}

		before := time.Now()
		now := live.Frame().Now
		after := time.Now()

		if now.Before(before) || now.After(after) {
			t.Errorf("Frame().Now = %v, want between %v and %v", now, before, after)
		}
	})

	t.Run("WithStderr nil keeps a usable writer", func(t *testing.T) {
		t.Parallel()

		fake := &fakeIngest{streamFn: func(context.Context, *airplanes.Airplanes) error {
			return errFakeIngestFailure
		}}

		live, err := source.NewLive(source.WithIngest(fake), source.WithStderr(nil))
		if err != nil {
			t.Fatalf("NewLive: %v", err)
		}

		live.Start(context.Background())

		// A nil writer would panic inside stream's fmt.Fprintf. Getting to
		// Close without a panic is the proof the default writer stayed in
		// place.
		if err := live.Close(); err != nil {
			t.Errorf("Close() = %v, want nil", err)
		}
	})

	t.Run("WithIngest nil keeps the real ADS-B ingest", func(t *testing.T) {
		t.Parallel()

		live, err := source.NewLive(source.WithIngest(nil))
		if err != nil {
			t.Fatalf("NewLive: %v", err)
		}

		const wantLabel = "SDR"

		if got := live.Frame().Source.Label; got != wantLabel {
			t.Errorf("Source.Label = %q, want %q (the default ingest, not a nil one)", got, wantLabel)
		}
	})
}

// TestLiveStartClose covers Start and Close's lifecycle guarantees: Close
// waits for the ingest goroutine, a second Start is a no-op, and Close is
// safe both without a prior Start and when called twice.
// TestLiveCloseWaitsForStream checks that Close does not return before the
// ingest goroutine has, which matters because that goroutine holds whatever
// device or socket the next Start needs free.
func TestLiveCloseWaitsForStream(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	fake := &fakeIngest{streamFn: func(ctx context.Context, _ *airplanes.Airplanes) error {
		<-ctx.Done()
		close(done)

		return ctx.Err()
	}}

	live, err := source.NewLive(source.WithIngest(fake))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	live.Start(context.Background())

	if err := live.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}

	select {
	case <-done:
	default:
		t.Error("stream goroutine had not signalled exit by the time Close returned")
	}
}

// TestLiveStartTwiceStartsOnce checks that a second Start sees a stream
// already running and leaves it alone.
func TestLiveStartTwiceStartsOnce(t *testing.T) {
	t.Parallel()

	fake := &fakeIngest{streamFn: func(ctx context.Context, _ *airplanes.Airplanes) error {
		<-ctx.Done()

		return nil
	}}

	live, err := source.NewLive(source.WithIngest(fake))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	live.Start(context.Background())
	live.Start(context.Background())

	if err := live.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}

	if got := fake.callCount(); got != 1 {
		t.Errorf("Stream called %d times, want 1", got)
	}
}

func TestLiveCloseWithoutStart(t *testing.T) {
	t.Parallel()

	live, err := source.NewLive(source.WithIngest(&fakeIngest{}))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	if err := live.Close(); err != nil {
		t.Errorf("Close() without Start = %v, want nil", err)
	}
}

func TestLiveCloseTwiceIsFine(t *testing.T) {
	t.Parallel()

	fake := &fakeIngest{streamFn: func(ctx context.Context, _ *airplanes.Airplanes) error {
		<-ctx.Done()

		return nil
	}}

	live, err := source.NewLive(source.WithIngest(fake))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	live.Start(context.Background())

	if err := live.Close(); err != nil {
		t.Errorf("first Close() = %v, want nil", err)
	}

	if err := live.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
}

// TestLiveStreamErrorReporting covers the three shapes stream's error check
// has to tell apart, plus the one that is actually reported.
func TestLiveStreamErrorReporting(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		err       error
		wantWrite bool
	}{
		{name: "nil error reports nothing", err: nil, wantWrite: false},
		{name: "context canceled reports nothing", err: context.Canceled, wantWrite: false},
		{
			name:      "wrapped context canceled reports nothing",
			err:       fmt.Errorf("uAirwaves: %w", context.Canceled),
			wantWrite: false,
		},
		{name: "other errors are reported once", err: errFakeIngestDisrupted, wantWrite: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeIngest{streamFn: func(context.Context, *airplanes.Airplanes) error {
				return testCase.err
			}}

			var stderr syncBuffer

			live, err := source.NewLive(source.WithIngest(fake), source.WithStderr(&stderr))
			if err != nil {
				t.Fatalf("NewLive: %v", err)
			}

			live.Start(context.Background())

			if err := live.Close(); err != nil {
				t.Errorf("Close() = %v, want nil", err)
			}

			got := stderr.String() != ""
			if got != testCase.wantWrite {
				t.Errorf("wrote to stderr = %v (%q), want %v", got, stderr.String(), testCase.wantWrite)
			}
		})
	}
}

// TestLiveFramePassesIngestThrough checks that Frame is a thin wrapper: the
// ingest's own Source and Stats come back untouched, and Planes reflects
// whatever the ingest wrote into the shared *airplanes.Airplanes.
func TestLiveFramePassesIngestThrough(t *testing.T) {
	t.Parallel()

	const (
		icao          = "ABC123"
		wantAltitude  = 1000.0
		wantLatitude  = 52.4
		wantLongitude = 4.8

		wantBytesIn          = 42
		wantTotalFrames      = 7
		wantRecoveredFrames  = 1
		wantCallsignsDecoded = 2
		wantCallsignsApplied = 2
	)

	wantSource := adsb.SourceInfo{Label: "TEST", Connected: true, BytesIn: wantBytesIn}
	wantStats := adsb.Stats{
		TotalFrames:      wantTotalFrames,
		RecoveredFrames:  wantRecoveredFrames,
		CallsignsDecoded: wantCallsignsDecoded,
		CallsignsApplied: wantCallsignsApplied,
	}

	fake := &fakeIngest{
		source: wantSource,
		stats:  wantStats,
		streamFn: func(_ context.Context, planes *airplanes.Airplanes) error {
			planes.Ensure(icao)

			if plane, ok := planes.Get(icao); ok {
				plane.Update(airplane.WithAltitude(wantAltitude), airplane.WithPosition(wantLatitude, wantLongitude))
			}

			return nil
		},
	}

	const receiverLat, receiverLon = 52.0, 4.0

	live, err := source.NewLive(source.WithIngest(fake), source.WithManualLocation(receiverLat, receiverLon))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	live.Start(context.Background())

	if err := live.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}

	frame := live.Frame()

	if frame.Source != wantSource {
		t.Errorf("Frame().Source = %+v, want %+v", frame.Source, wantSource)
	}

	if frame.Stats != wantStats {
		t.Errorf("Frame().Stats = %+v, want %+v", frame.Stats, wantStats)
	}

	if len(frame.Planes) != 1 {
		t.Fatalf("Frame().Planes has %d entries, want 1", len(frame.Planes))
	}

	got := frame.Planes[0]
	wrongIdentity := got.ICAO != icao || got.Altitude != wantAltitude
	wrongPosition := got.Latitude != wantLatitude || got.Longitude != wantLongitude

	if wrongIdentity || wrongPosition {
		t.Errorf("Frame().Planes[0] = %+v, want ICAO %q altitude %v at (%v, %v)",
			got, icao, wantAltitude, wantLatitude, wantLongitude)
	}
}

func TestLiveClockControlsFrameNow(t *testing.T) {
	t.Parallel()

	live, err := source.NewLive(source.WithClock(func() time.Time { return fixedNow }))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	if got := live.Frame().Now; !got.Equal(fixedNow) {
		t.Errorf("Frame().Now = %v, want %v", got, fixedNow)
	}
}

func TestNewDemoBuildsTwelveAircraft(t *testing.T) {
	t.Parallel()

	const wantAircraft = 12

	demo, err := source.NewDemo()
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	if got := len(demo.Frame().Planes); got != wantAircraft {
		t.Errorf("len(Frame().Planes) = %d, want %d", got, wantAircraft)
	}
}

// TestDemoPlanesSortedByDistance checks the fleet comes back the way
// airplanes.Sorted would order it: nearest to the receiver first.
func TestDemoPlanesSortedByDistance(t *testing.T) {
	t.Parallel()

	demo, err := source.NewDemo()
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	frame := demo.Frame()
	receiver := frame.Receiver

	previous := 0.0

	for index, plane := range frame.Planes {
		distance := airplanes.HaversineDistance(receiver.Latitude, receiver.Longitude, plane.Latitude, plane.Longitude)
		if index > 0 && distance < previous {
			t.Errorf("plane %d (%s) at %.2f nm is closer than the previous plane at %.2f nm, want non-decreasing",
				index, plane.ICAO, distance, previous)
		}

		previous = distance
	}
}

// TestDemoFallbackAircraft pins the two deliberately awkward entries in the
// fleet table: exactly one aircraft with no callsign, and exactly one with
// neither heading nor velocity. Both exist so the radar's fallback rendering
// has something to draw, and a "cleanup" that removes them should fail here.
func TestDemoFallbackAircraft(t *testing.T) {
	t.Parallel()

	demo, err := source.NewDemo()
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	var noCallsign, noMotion int

	for _, plane := range demo.Frame().Planes {
		if plane.Callsign == "" {
			noCallsign++
		}

		// The undecoded sentinel is negative, not zero: zero is due north and
		// a genuine standstill, and the fleet carries one aircraft whose
		// velocity message has simply never arrived.
		if plane.Heading < 0 && plane.Velocity < 0 {
			noMotion++
		}
	}

	if noCallsign != 1 {
		t.Errorf("aircraft with an empty callsign = %d, want exactly 1", noCallsign)
	}

	if noMotion != 1 {
		t.Errorf("aircraft with an undecoded heading and velocity = %d, want exactly 1", noMotion)
	}
}

// TestNewDemoIsDeterministicWithSameSeed builds two demos from the same seed
// and requires their first frame to place every aircraft identically. A demo
// that looks different across two runs would be useless for comparing builds.
func TestNewDemoIsDeterministicWithSameSeed(t *testing.T) {
	t.Parallel()

	const seed = 7

	first, err := source.NewDemo(source.WithSeed(seed))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	second, err := source.NewDemo(source.WithSeed(seed))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	firstPlanes := first.Frame().Planes
	secondPlanes := second.Frame().Planes

	if len(firstPlanes) != len(secondPlanes) {
		t.Fatalf("plane counts differ: %d vs %d", len(firstPlanes), len(secondPlanes))
	}

	for index := range firstPlanes {
		if firstPlanes[index].ICAO != secondPlanes[index].ICAO ||
			firstPlanes[index].Latitude != secondPlanes[index].Latitude ||
			firstPlanes[index].Longitude != secondPlanes[index].Longitude {
			t.Errorf("plane %d differs between two NewDemo(WithSeed(%d)) instances: %+v vs %+v",
				index, seed, firstPlanes[index], secondPlanes[index])
		}
	}
}

func TestNewDemoDifferentSeedsDifferentLayout(t *testing.T) {
	t.Parallel()

	const seedA, seedB = 1, 2

	demoA, err := source.NewDemo(source.WithSeed(seedA))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	demoB, err := source.NewDemo(source.WithSeed(seedB))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	planesA := demoA.Frame().Planes
	planesB := demoB.Frame().Planes

	identical := true

	for index := range planesA {
		if planesA[index].Latitude != planesB[index].Latitude || planesA[index].Longitude != planesB[index].Longitude {
			identical = false

			break
		}
	}

	if identical {
		t.Error("two different seeds produced an identical layout, want them to differ")
	}
}

// TestNewDemoLocation covers WithDemoLocation's four out-of-range corners
// plus the happy path, where the new position must show up in Frame().
func TestNewDemoLocation(t *testing.T) {
	t.Parallel()

	const (
		latitudeTooHigh  = 91.0
		latitudeTooLow   = -91.0
		longitudeTooHigh = 181.0
		longitudeTooLow  = -181.0

		validLatitude  = 40.0
		validLongitude = -3.5
	)

	for _, testCase := range []struct {
		name      string
		latitude  float64
		longitude float64
		wantErr   bool
	}{
		{name: "latitude above range", latitude: latitudeTooHigh, longitude: 0, wantErr: true},
		{name: "latitude below range", latitude: latitudeTooLow, longitude: 0, wantErr: true},
		{name: "longitude above range", latitude: 0, longitude: longitudeTooHigh, wantErr: true},
		{name: "longitude below range", latitude: 0, longitude: longitudeTooLow, wantErr: true},
		{name: "in range moves the receiver", latitude: validLatitude, longitude: validLongitude, wantErr: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			demo, err := source.NewDemo(source.WithDemoLocation(testCase.latitude, testCase.longitude))
			if testCase.wantErr {
				assertRejectedCoordinate(t, demo, err)

				return
			}

			if err != nil {
				t.Fatalf("NewDemo() unexpected error: %v", err)
			}

			receiver := demo.Frame().Receiver
			if receiver.Latitude != testCase.latitude || receiver.Longitude != testCase.longitude {
				t.Errorf("Receiver = (%v, %v), want (%v, %v)",
					receiver.Latitude, receiver.Longitude, testCase.latitude, testCase.longitude)
			}
		})
	}
}

// TestNewDemoClockNilGuard checks that a nil clock leaves time.Now in place,
// proven by Frame().Now landing inside a window taken around the call.
func TestNewDemoClockNilGuard(t *testing.T) {
	t.Parallel()

	demo, err := source.NewDemo(source.WithDemoClock(nil))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	before := time.Now()
	now := demo.Frame().Now
	after := time.Now()

	if now.Before(before) || now.After(after) {
		t.Errorf("Frame().Now = %v, want between %v and %v", now, before, after)
	}
}

// TestDemoClockNotAdvancingDoesNotMoveAnything covers a clock that stands
// still and one that goes backwards: both must leave every aircraft exactly
// where the previous frame put it, and neither may panic.
func TestDemoClockStandsStillDoesNotMove(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	demo, err := source.NewDemo(source.WithDemoClock(func() time.Time { return fixed }))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	before := demo.Frame().Planes
	after := demo.Frame().Planes

	assertPlanesUnmoved(t, before, after, "a clock that did not advance")
}

func TestDemoClockGoesBackwardsDoesNotMove(t *testing.T) {
	t.Parallel()

	const step = 10 * time.Second

	later := time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)
	earlier := later.Add(-step)

	ticks := []time.Time{later, earlier}
	index := 0

	clock := func() time.Time {
		tick := ticks[index]
		if index < len(ticks)-1 {
			index++
		}

		return tick
	}

	demo, err := source.NewDemo(source.WithDemoClock(clock))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	before := demo.Frame().Planes
	after := demo.Frame().Planes

	assertPlanesUnmoved(t, before, after, "a clock that went backwards")
}

// assertPlanesUnmoved fails the test if any plane's coordinates differ
// between two snapshots that should be identical.
func assertPlanesUnmoved(t *testing.T, before, after airplanes.List, why string) {
	t.Helper()

	for index := range before {
		if before[index].Latitude != after[index].Latitude || before[index].Longitude != after[index].Longitude {
			t.Errorf("plane %d moved with %s: %+v -> %+v", index, why, before[index], after[index])
		}
	}
}

// TestDemoAdvanceMovesAircraft steps the clock forward and checks that an
// aircraft with velocity keeps flying while the deliberately stationary one
// does not.
func TestDemoAdvanceMovesAircraft(t *testing.T) {
	t.Parallel()

	const step = 10 * time.Second

	current := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		tick := current
		current = current.Add(step)

		return tick
	}

	demo, err := source.NewDemo(source.WithDemoClock(clock))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	type position struct{ latitude, longitude float64 }

	before := map[string]position{}

	var stationaryICAO, movingICAO string

	for _, plane := range demo.Frame().Planes {
		before[plane.ICAO] = position{plane.Latitude, plane.Longitude}

		if plane.Velocity <= 0 && stationaryICAO == "" {
			stationaryICAO = plane.ICAO
		}

		if plane.Velocity > 0 && movingICAO == "" {
			movingICAO = plane.ICAO
		}
	}

	if stationaryICAO == "" || movingICAO == "" {
		t.Fatal("fleet has no stationary/moving pair to compare, fixture assumption broken")
	}

	after := map[string]position{}
	for _, plane := range demo.Frame().Planes {
		after[plane.ICAO] = position{plane.Latitude, plane.Longitude}
	}

	if after[stationaryICAO] != before[stationaryICAO] {
		t.Errorf("stationary aircraft moved: before %+v, after %+v", before[stationaryICAO], after[stationaryICAO])
	}

	if after[movingICAO] == before[movingICAO] {
		t.Error("moving aircraft did not move between frames")
	}
}

// TestDemoAdvanceCapsElapsedTime checks demoMaxStep's cap from outside the
// package: a single 60 second jump must move the fleet by no more than a 5
// second step would, by comparing the two seeded the same way.
func TestDemoAdvanceCapsElapsedTime(t *testing.T) {
	t.Parallel()

	const (
		cappedStep = 5 * time.Second
		bigJump    = 60 * time.Second
		seed       = 11
	)

	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	buildDemo := func(t *testing.T, second time.Time) *source.Demo {
		t.Helper()

		ticks := []time.Time{base, second}
		index := 0

		clock := func() time.Time {
			tick := ticks[index]
			if index < len(ticks)-1 {
				index++
			}

			return tick
		}

		demo, err := source.NewDemo(source.WithSeed(seed), source.WithDemoClock(clock))
		if err != nil {
			t.Fatalf("NewDemo: %v", err)
		}

		return demo
	}

	capped := buildDemo(t, base.Add(bigJump))
	uncapped := buildDemo(t, base.Add(cappedStep))

	capped.Frame()
	uncapped.Frame()

	cappedPlanes := capped.Frame().Planes
	uncappedPlanes := uncapped.Frame().Planes

	for index := range cappedPlanes {
		if cappedPlanes[index].Latitude != uncappedPlanes[index].Latitude ||
			cappedPlanes[index].Longitude != uncappedPlanes[index].Longitude {
			t.Errorf("plane %d: a 60s jump landed at (%v, %v), want the same as a 5s step (%v, %v)",
				index, cappedPlanes[index].Latitude, cappedPlanes[index].Longitude,
				uncappedPlanes[index].Latitude, uncappedPlanes[index].Longitude)
		}
	}
}

// TestDemoHistoryCap drives enough frames to fill every trail past its cap
// and checks that none of them grew beyond it.
func TestDemoHistoryCap(t *testing.T) {
	t.Parallel()

	const (
		step         = 5 * time.Second
		frames       = 500
		wantCapacity = 240
	)

	current := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		tick := current
		current = current.Add(step)

		return tick
	}

	demo, err := source.NewDemo(source.WithDemoClock(clock))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	var frame source.Frame

	for range frames {
		frame = demo.Frame()
	}

	for _, plane := range frame.Planes {
		if got := len(plane.PositionHistory); got != wantCapacity {
			t.Errorf("plane %s: len(PositionHistory) = %d, want %d", plane.ICAO, got, wantCapacity)
		}
	}
}

func TestDemoSourceAndStats(t *testing.T) {
	t.Parallel()

	const wantLabel = "DEMO"

	demo, err := source.NewDemo()
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	first := demo.Frame()

	if first.Source.Label != wantLabel {
		t.Errorf("Source.Label = %q, want %q", first.Source.Label, wantLabel)
	}

	if !first.Source.Connected {
		t.Error("Source.Connected = false, want true")
	}

	if first.Stats.TotalFrames != 1 {
		t.Errorf("Stats.TotalFrames after 1 frame = %d, want 1", first.Stats.TotalFrames)
	}

	second := demo.Frame()
	if second.Stats.TotalFrames != 2 {
		t.Errorf("Stats.TotalFrames after 2 frames = %d, want 2", second.Stats.TotalFrames)
	}
}

// TestDemoSectorPlacesFleetInNorthWestQuadrant checks that every aircraft
// WithDemoSector places ends up north and west of the receiver. It asserts on
// the sign of the lat/lon offset rather than recomputing a bearing from the
// result, since the offset's sign is exactly what "north of" and "west of"
// mean and needs no trigonometry of its own to check.
func TestDemoSectorPlacesFleetInNorthWestQuadrant(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	demo, err := source.NewDemo(source.WithDemoSector(), source.WithDemoClock(func() time.Time { return fixed }))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	frame := demo.Frame()
	receiver := frame.Receiver

	if len(frame.Planes) == 0 {
		t.Fatal("Frame().Planes is empty, want the full fleet")
	}

	for _, plane := range frame.Planes {
		if plane.Latitude <= receiver.Latitude {
			t.Errorf("plane %s latitude = %v, want > receiver latitude %v (north)",
				plane.ICAO, plane.Latitude, receiver.Latitude)
		}

		if plane.Longitude >= receiver.Longitude {
			t.Errorf("plane %s longitude = %v, want < receiver longitude %v (west)",
				plane.ICAO, plane.Longitude, receiver.Longitude)
		}
	}
}

// TestDemoDefaultFleetIsNotConfinedToNorthWest proves the previous test is
// checking something real: without WithDemoSector, at least one aircraft
// falls outside the north-west quadrant.
func TestDemoDefaultFleetIsNotConfinedToNorthWest(t *testing.T) {
	t.Parallel()

	demo, err := source.NewDemo()
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	frame := demo.Frame()
	receiver := frame.Receiver

	outside := false

	for _, plane := range frame.Planes {
		if plane.Latitude <= receiver.Latitude || plane.Longitude >= receiver.Longitude {
			outside = true

			break
		}
	}

	if !outside {
		t.Error("default fleet is entirely north-west of the receiver, want at least one aircraft elsewhere")
	}
}

// TestDemoSectorIsDeterministic builds two demos with the same seed and
// WithDemoSector and requires them to place every aircraft identically,
// matching the guarantee the default fleet already has.
func TestDemoSectorIsDeterministic(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }

	first, err := source.NewDemo(source.WithDemoSector(), source.WithDemoClock(clock))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	second, err := source.NewDemo(source.WithDemoSector(), source.WithDemoClock(clock))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	firstPlanes := first.Frame().Planes
	secondPlanes := second.Frame().Planes

	if len(firstPlanes) != len(secondPlanes) {
		t.Fatalf("plane counts differ: %d vs %d", len(firstPlanes), len(secondPlanes))
	}

	for index := range firstPlanes {
		if firstPlanes[index].ICAO != secondPlanes[index].ICAO ||
			firstPlanes[index].Latitude != secondPlanes[index].Latitude ||
			firstPlanes[index].Longitude != secondPlanes[index].Longitude {
			t.Errorf("plane %d differs between two WithDemoSector() instances: %+v vs %+v",
				index, firstPlanes[index], secondPlanes[index])
		}
	}
}

func TestDemoClose(t *testing.T) {
	t.Parallel()

	demo, err := source.NewDemo()
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	if err := demo.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

// sequentialClock hands back each tick in order and then keeps repeating the
// last one, the pattern the demo clock tests above already use inline. It is
// pulled out here because the ghost tests below need the same shape with a
// third tick.
func sequentialClock(ticks []time.Time) func() time.Time {
	index := 0

	return func() time.Time {
		tick := ticks[index]
		if index < len(ticks)-1 {
			index++
		}

		return tick
	}
}

// startLiveIngest builds a Live wired to a fake ingest that hands back the
// shared *airplanes.Airplanes it was given, so the test can add and remove
// aircraft on its own schedule afterwards instead of only once, from inside
// the one Stream call Start makes.
func startLiveIngest(tb testing.TB, opts ...source.LiveOption) (*source.Live, *airplanes.Airplanes) {
	tb.Helper()

	var store *airplanes.Airplanes

	fake := &fakeIngest{streamFn: func(_ context.Context, planes *airplanes.Airplanes) error {
		store = planes

		return nil
	}}

	live, err := source.NewLive(append([]source.LiveOption{source.WithIngest(fake)}, opts...)...)
	if err != nil {
		tb.Fatalf("NewLive: %v", err)
	}

	live.Start(context.Background())

	if err := live.Close(); err != nil {
		tb.Fatalf("Close: %v", err)
	}

	return live, store
}

// addAircraft ensures one aircraft exists in store and gives it an identity
// and a single position fix, so its trail is never empty.
func addAircraft(
	tb testing.TB, store *airplanes.Airplanes, icao, callsign string, altitude, latitude, longitude float64,
) {
	tb.Helper()

	store.Ensure(icao)

	plane, ok := store.Get(icao)
	if !ok {
		tb.Fatalf("Get(%q) after Ensure = false, want true", icao)
	}

	plane.Update(
		airplane.WithCallsign(callsign),
		airplane.WithAltitude(altitude),
		airplane.WithPosition(latitude, longitude),
	)
}

// TestLiveGhostsOffByDefault pins the "off unless asked for" contract: with
// the option omitted, or passed explicitly as false, Frame().Ghosts stays nil
// even after an aircraft has come and gone. A source that turned ghosts on by
// accident would cost every caller memory it never asked for.
func TestLiveGhostsOffByDefault(t *testing.T) {
	t.Parallel()

	const (
		icao            = "GHOST01"
		callsign        = "TESTCS"
		altitude        = 5000.0
		latitude        = 52.1
		longitude       = 4.2
		caseOmitted     = "option omitted"
		caseExplicitOff = "option explicitly false"
	)

	for _, testCase := range []struct {
		name string
		opts []source.LiveOption
	}{
		{name: caseOmitted, opts: nil},
		{name: caseExplicitOff, opts: []source.LiveOption{source.WithGhosts(false)}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			live, store := startLiveIngest(t, testCase.opts...)

			addAircraft(t, store, icao, callsign, altitude, latitude, longitude)
			live.Frame()

			store.Prune(0)

			frame := live.Frame()
			if frame.Ghosts != nil {
				t.Errorf("Frame().Ghosts = %v, want nil with ghosts off", frame.Ghosts)
			}
		})
	}
}

// TestDemoGhostsOffByDefault is TestLiveGhostsOffByDefault's Demo half: the
// fleet's own quiet aircraft goes through the same disappearance, and
// Frame().Ghosts must still stay nil with the option omitted or off.
func TestDemoGhostsOffByDefault(t *testing.T) {
	t.Parallel()

	const (
		pastQuiet       = 90*time.Second + time.Second
		caseOmitted     = "option omitted"
		caseExplicitOff = "option explicitly false"
	)

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name string
		opt  source.DemoOption
	}{
		{name: caseOmitted, opt: nil},
		{name: caseExplicitOff, opt: source.WithDemoGhosts(false)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clock := sequentialClock([]time.Time{start, start.Add(pastQuiet)})

			opts := []source.DemoOption{source.WithDemoClock(clock)}
			if testCase.opt != nil {
				opts = append(opts, testCase.opt)
			}

			demo, err := source.NewDemo(opts...)
			if err != nil {
				t.Fatalf("NewDemo: %v", err)
			}

			demo.Frame()

			frame := demo.Frame()
			if frame.Ghosts != nil {
				t.Errorf("Frame().Ghosts = %v, want nil with ghosts off", frame.Ghosts)
			}
		})
	}
}

// TestLiveGhostAircraftDisappearingLeavesGhost checks the core contract: an
// aircraft the store has pruned shows up, on the very next frame, as a ghost
// carrying the identity and the trail it last reported.
func TestLiveGhostAircraftDisappearingLeavesGhost(t *testing.T) {
	t.Parallel()

	const (
		icao      = "GHOST02"
		callsign  = "LOSTCS"
		altitude  = 8000.0
		latitude  = 52.3
		longitude = 4.9
	)

	live, store := startLiveIngest(t, source.WithGhosts(true))

	addAircraft(t, store, icao, callsign, altitude, latitude, longitude)

	present := live.Frame()
	if len(present.Planes) != 1 {
		t.Fatalf("Frame().Planes while flying = %d, want 1", len(present.Planes))
	}

	if present.Ghosts != nil {
		t.Errorf("Frame().Ghosts while the aircraft is still flying = %v, want nil", present.Ghosts)
	}

	store.Prune(0)

	gone := live.Frame()
	if len(gone.Planes) != 0 {
		t.Errorf("Frame().Planes after Prune = %d, want 0", len(gone.Planes))
	}

	if len(gone.Ghosts) != 1 {
		t.Fatalf("Frame().Ghosts after Prune = %d, want exactly 1", len(gone.Ghosts))
	}

	ghost := gone.Ghosts[0]

	wrongIdentity := ghost.ICAO != icao || ghost.Callsign != callsign || ghost.Altitude != altitude
	if wrongIdentity {
		t.Errorf("ghost identity = %+v, want ICAO %q callsign %q altitude %v",
			ghost, icao, callsign, altitude)
	}

	if len(ghost.Points) == 0 {
		t.Error("ghost.Points is empty, want the position history the aircraft last showed")
	}
}

// TestLiveGhostAircraftReappearingStopsBeingGhost checks revive: the same
// ICAO showing up again must clear the ghost, and must not leave it drawn
// twice.
func TestLiveGhostAircraftReappearingStopsBeingGhost(t *testing.T) {
	t.Parallel()

	const (
		icao      = "GHOST03"
		callsign  = "BACKCS"
		altitude  = 3000.0
		latitude  = 51.9
		longitude = 4.4
	)

	live, store := startLiveIngest(t, source.WithGhosts(true))

	addAircraft(t, store, icao, callsign, altitude, latitude, longitude)
	live.Frame()

	store.Prune(0)

	gone := live.Frame()
	if len(gone.Ghosts) != 1 {
		t.Fatalf("Frame().Ghosts after Prune = %d, want exactly 1", len(gone.Ghosts))
	}

	addAircraft(t, store, icao, callsign, altitude, latitude, longitude)

	back := live.Frame()
	if len(back.Planes) != 1 {
		t.Errorf("Frame().Planes after the aircraft returns = %d, want 1", len(back.Planes))
	}

	if len(back.Ghosts) != 0 {
		t.Errorf("Frame().Ghosts after the aircraft returns = %v, want none", back.Ghosts)
	}
}

// TestDemoQuietAircraftBecomesGhost is the explicit acceptance item: the
// fleet's one aircraft with no callsign must stop appearing in Frame().Planes
// once ninety seconds of simulated time have passed since the first frame,
// its last trail must show up as the one ghost, and it must not come back on
// a later frame.
func TestDemoQuietAircraftBecomesGhost(t *testing.T) {
	t.Parallel()

	const (
		pastQuiet      = 90*time.Second + time.Second
		wellPastQuiet  = 90*time.Second + 5*time.Second
		fleetSize      = 12
		fleetAfterLoss = 11
		wantICAO       = "4951BA" // the fleet's one aircraft with no callsign.
	)

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := sequentialClock([]time.Time{
		start,
		start.Add(pastQuiet),
		start.Add(wellPastQuiet),
	})

	demo, err := source.NewDemo(source.WithDemoClock(clock), source.WithDemoGhosts(true))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	first := demo.Frame()
	if got := len(first.Planes); got != fleetSize {
		t.Fatalf("first Frame().Planes = %d, want %d", got, fleetSize)
	}

	second := demo.Frame()

	if got := len(second.Planes); got != fleetAfterLoss {
		t.Errorf("Frame().Planes after 90s = %d, want %d", got, fleetAfterLoss)
	}

	for _, plane := range second.Planes {
		if plane.ICAO == wantICAO {
			t.Errorf("aircraft %q is still in Frame().Planes after going quiet", wantICAO)
		}
	}

	if got := len(second.Ghosts); got != 1 {
		t.Fatalf("Frame().Ghosts after 90s = %d, want exactly 1", got)
	}

	ghost := second.Ghosts[0]
	if ghost.ICAO != wantICAO {
		t.Errorf("ghost ICAO = %q, want %q", ghost.ICAO, wantICAO)
	}

	if len(ghost.Points) == 0 {
		t.Error("ghost.Points is empty, want the aircraft's last trail")
	}

	third := demo.Frame()

	if got := len(third.Planes); got != fleetAfterLoss {
		t.Errorf("Frame().Planes on a later frame = %d, want %d (it must not come back)", got, fleetAfterLoss)
	}

	if got := len(third.Ghosts); got != 1 {
		t.Errorf("Frame().Ghosts on a later frame = %d, want exactly 1 (not duplicated)", got)
	}
}

// TestDemoFleetWholeBeforeQuietBoundary pins the boundary rather than just
// the end state: at eighty-five seconds the fleet must still be all twelve,
// with no ghost yet, so the drop the previous test checks is known to land on
// the ninety second mark and not sooner.
func TestDemoFleetWholeBeforeQuietBoundary(t *testing.T) {
	t.Parallel()

	const (
		beforeQuiet = 85 * time.Second
		fleetSize   = 12
	)

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := sequentialClock([]time.Time{start, start.Add(beforeQuiet)})

	demo, err := source.NewDemo(source.WithDemoClock(clock), source.WithDemoGhosts(true))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	demo.Frame()

	frame := demo.Frame()
	if got := len(frame.Planes); got != fleetSize {
		t.Errorf("Frame().Planes at 85s = %d, want %d", got, fleetSize)
	}

	if len(frame.Ghosts) != 0 {
		t.Errorf("Frame().Ghosts at 85s = %v, want none", frame.Ghosts)
	}
}

// TestFrameGhostsSurvivesBeingRead checks that the ghosts returned in one
// Frame are safe to read more than once: nothing about reading Ghosts should
// clear or mutate it. It stops short of holding the slice across a second
// Frame call, since Frame explicitly does not guarantee that.
func TestFrameGhostsSurvivesBeingRead(t *testing.T) {
	t.Parallel()

	const pastQuiet = 90*time.Second + time.Second

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := sequentialClock([]time.Time{start, start.Add(pastQuiet)})

	demo, err := source.NewDemo(source.WithDemoClock(clock), source.WithDemoGhosts(true))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	demo.Frame()

	frame := demo.Frame()
	if len(frame.Ghosts) != 1 {
		t.Fatalf("Frame().Ghosts = %d entries, want 1", len(frame.Ghosts))
	}

	first := frame.Ghosts[0]
	second := frame.Ghosts[0]

	wrongIdentity := first.ICAO != second.ICAO || first.Callsign != second.Callsign || first.Altitude != second.Altitude
	if wrongIdentity {
		t.Errorf("reading Ghosts[0] twice gave different identity fields: %+v vs %+v", first, second)
	}

	if len(first.Points) != len(second.Points) {
		t.Errorf("reading Ghosts[0].Points twice gave different lengths: %d vs %d",
			len(first.Points), len(second.Points))
	}

	for index, trail := range frame.Ghosts {
		if trail.ICAO == "" {
			t.Errorf("Ghosts[%d].ICAO is empty on a second read", index)
		}
	}
}

// TestDemoFrameCoverageOnFirstFrame checks that advance's first-frame branch
// folds the fleet into coverage immediately: the very first Frame already
// carries a measured envelope rather than an empty one waiting for a second
// tick.
func TestDemoFrameCoverageOnFirstFrame(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	demo, err := source.NewDemo(source.WithDemoClock(func() time.Time { return fixed }))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	if got := demo.Frame().Coverage.MaxRangeNm; got <= 0 {
		t.Errorf("first Frame().Coverage.MaxRangeNm = %v, want > 0", got)
	}
}

// TestDemoFrameCoverageSurvivesPastQuiet advances the clock past both
// demoQuietAfter and the coverage snapshot interval, and checks that dropping
// the fleet's one quiet aircraft did not erase the envelope the other eleven
// built up.
func TestDemoFrameCoverageSurvivesPastQuiet(t *testing.T) {
	t.Parallel()

	const pastQuietAndInterval = 90*time.Second + 2*time.Second

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := sequentialClock([]time.Time{start, start.Add(pastQuietAndInterval)})

	demo, err := source.NewDemo(source.WithDemoClock(clock))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	demo.Frame()

	if got := demo.Frame().Coverage.MaxRangeNm; got <= 0 {
		t.Errorf("Coverage.MaxRangeNm past the quiet boundary = %v, want > 0", got)
	}
}

// TestDemoSectorCoverageIsLopsided checks that WithDemoSector's crowded fleet
// fills fewer of the sixteen bearing sectors than the default scattered
// fleet does, which is the shape a directional antenna's coverage picture is
// meant to have.
func TestDemoSectorCoverageIsLopsided(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }

	sector, err := source.NewDemo(source.WithDemoSector(), source.WithDemoClock(clock))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	scattered, err := source.NewDemo(source.WithDemoClock(clock))
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	sectorFilled := countFilledSectors(sector.Frame().Coverage.Sectors)
	scatteredFilled := countFilledSectors(scattered.Frame().Coverage.Sectors)

	if sectorFilled >= scatteredFilled {
		t.Errorf("WithDemoSector filled %d of the sixteen bearing sectors, want fewer than the scattered fleet's %d",
			sectorFilled, scatteredFilled)
	}
}

// countFilledSectors counts how many of Coverage.Sectors' bearing sectors
// have seen at least one fix.
func countFilledSectors(sectors [coverage.BearingSectorCount]float64) int {
	count := 0

	for _, distance := range sectors {
		if distance > 0 {
			count++
		}
	}

	return count
}
