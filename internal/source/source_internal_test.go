package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/coverage"
	"github.com/hyperized/uAirwaves/pkg/location"
)

// errStreamFailure stands in for whatever an ingest might fail with. Static
// so err113 is satisfied; stream's test only checks whether something was
// reported, never the text.
var errStreamFailure = errors.New("stub ingest: stream failed") //nolint:gochecknoglobals // error sentinel, not state.

// stubIngest is a minimal ingest implementation for calling stream directly,
// without the goroutine Start adds. Every call happens on the test's own
// goroutine, so unlike the Start/Close tests in the external file this needs
// no synchronisation.
type stubIngest struct {
	err error
}

func (s stubIngest) Stream(context.Context, *airplanes.Airplanes) error { return s.err }

func (stubIngest) Source() adsb.SourceInfo { return adsb.SourceInfo{} }

func (stubIngest) Stats() adsb.Stats { return adsb.Stats{} }

// almostEqual compares two float64 values within a tolerance, since the
// offset/latitude/longitude arithmetic below is never exact.
func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

// TestLiveNewLocation covers both branches: no manual position builds an
// empty Location, a manual one carries the operator's coordinates.
func TestLiveNewLocation(t *testing.T) {
	t.Parallel()

	t.Run("no manual location builds an empty location", func(t *testing.T) {
		t.Parallel()

		live := &Live{}

		latitude, longitude := live.newLocation().GetCoordinates()
		if latitude != 0 || longitude != 0 {
			t.Errorf("GetCoordinates() = (%v, %v), want (0, 0)", latitude, longitude)
		}
	})

	t.Run("manual location carries the coordinates", func(t *testing.T) {
		t.Parallel()

		const wantLat, wantLon = 52.1, 4.3

		live := &Live{manual: true, manualLat: wantLat, manualLon: wantLon}

		gotLat, gotLon := live.newLocation().GetCoordinates()
		if gotLat != wantLat || gotLon != wantLon {
			t.Errorf("GetCoordinates() = (%v, %v), want (%v, %v)", gotLat, gotLon, wantLat, wantLon)
		}
	})
}

// TestLiveValidate covers validate's early return for a non-manual Live and
// both range checks for a manual one.
func TestLiveValidate(t *testing.T) {
	t.Parallel()

	const (
		outOfRangeLat = 1000.0
		inRange       = 10.0
		badLatitude   = 91.0
		badLongitude  = 181.0
	)

	for _, testCase := range []struct {
		name    string
		live    *Live
		wantErr bool
	}{
		{name: "not manual skips the range check entirely", live: &Live{manualLat: outOfRangeLat}, wantErr: false},
		{name: "manual in range", live: &Live{manual: true, manualLat: inRange, manualLon: inRange}, wantErr: false},
		{
			name:    "manual latitude out of range",
			live:    &Live{manual: true, manualLat: badLatitude, manualLon: 0},
			wantErr: true,
		},
		{
			name:    "manual longitude out of range",
			live:    &Live{manual: true, manualLat: 0, manualLon: badLongitude},
			wantErr: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.live.validate()

			if testCase.wantErr {
				if !errors.Is(err, ErrCoordinate) {
					t.Fatalf("validate() error = %v, want ErrCoordinate", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("validate() error = %v, want nil", err)
			}
		})
	}
}

// TestLiveAdsbOptions covers all three source branches plus replay winning
// when both a replay path and a beast address are set.
func TestLiveAdsbOptions(t *testing.T) {
	t.Parallel()

	const (
		beastAddress = "127.0.0.1:30005"
		replayName   = "capture.iq"

		// location + position observer, on every branch: the observer feeds the
		// coverage tracker as well as the self-locator, so it goes on whether
		// or not a manual location made the locator unnecessary.
		baseOptionCount = 2
	)

	replayPath := filepath.Join(t.TempDir(), replayName)
	if err := os.WriteFile(replayPath, []byte{0x01, 0x02, 0x03}, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	for _, testCase := range []struct {
		name      string
		opts      []LiveOption
		wantLen   int
		wantLabel string
	}{
		{name: "neither replay nor beast", opts: nil, wantLen: baseOptionCount + 1, wantLabel: "SDR"},
		{
			name:      "beast set",
			opts:      []LiveOption{WithBeast(beastAddress)},
			wantLen:   baseOptionCount + 2, //nolint:mnd // beast address plus source label.
			wantLabel: "BEAST " + beastAddress,
		},
		{
			name:      "replay set",
			opts:      []LiveOption{WithReplay(replayPath)},
			wantLen:   baseOptionCount + 2, //nolint:mnd // receiver factory plus source label.
			wantLabel: "REPLAY " + replayName,
		},
		{
			name:      "replay wins over beast",
			opts:      []LiveOption{WithReplay(replayPath), WithBeast(beastAddress)},
			wantLen:   baseOptionCount + 2, //nolint:mnd // receiver factory plus source label.
			wantLabel: "REPLAY " + replayName,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			live, err := NewLive(testCase.opts...)
			if err != nil {
				t.Fatalf("NewLive: %v", err)
			}

			opts := live.adsbOptions()
			if len(opts) != testCase.wantLen {
				t.Errorf("adsbOptions() length = %d, want %d", len(opts), testCase.wantLen)
			}

			probe := adsb.New(opts...)
			if got := probe.Source().Label; got != testCase.wantLabel {
				t.Errorf("Source().Label = %q, want %q", got, testCase.wantLabel)
			}
		})
	}
}

// TestReplayFactory covers opening a real file and naming the path when the
// file does not exist.
func TestReplayFactory(t *testing.T) {
	t.Parallel()

	t.Run("existing file opens", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "capture.iq")
		if err := os.WriteFile(path, []byte{0xAA, 0xBB}, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		rcv, err := replayFactory(path)()
		if err != nil {
			t.Fatalf("replayFactory(%q)() error = %v, want nil", path, err)
		}

		if err := rcv.Close(); err != nil {
			t.Errorf("Close() = %v, want nil", err)
		}
	})

	t.Run("missing file names the path in the error", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "missing.iq")

		_, err := replayFactory(missing)()
		if err == nil {
			t.Fatal("replayFactory(missing)() error = nil, want an error naming the path")
		}

		if !strings.Contains(err.Error(), missing) {
			t.Errorf("error %q does not name path %q", err.Error(), missing)
		}
	})
}

// TestObserverFeedsLocator checks that the closure positionObserver returns
// forwards straight into the locator's own Observe.
func TestObserverFeedsLocator(t *testing.T) {
	t.Parallel()

	const observationAltitudeFt = 5000.0

	live, err := NewLive()
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	live.positionObserver()(52.3, 4.9, observationAltitudeFt)

	if got := live.locator.ObservationCount(); got != 1 {
		t.Errorf("ObservationCount() = %d, want 1", got)
	}
}

// TestLiveStreamDirect calls stream directly, covering the three error
// shapes it has to tell apart: nil, a bare context.Canceled and one wrapping
// it, none of which should reach stderr, against the one that should.
func TestLiveStreamDirect(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		err       error
		wantWrite bool
	}{
		{name: "nil error writes nothing", err: nil, wantWrite: false},
		{name: "context canceled writes nothing", err: context.Canceled, wantWrite: false},
		{
			name:      "wrapped context canceled writes nothing",
			err:       fmt.Errorf("ingest: %w", context.Canceled),
			wantWrite: false,
		},
		{name: "other error writes one line", err: errStreamFailure, wantWrite: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer

			live := &Live{in: stubIngest{err: testCase.err}, stderr: &stderr, planes: airplanes.New()}

			live.stream(context.Background())

			got := stderr.String() != ""
			if got != testCase.wantWrite {
				t.Errorf("stream() wrote %q, want write=%v", stderr.String(), testCase.wantWrite)
			}
		})
	}
}

// TestLiveEstimateDueThrottle covers estimateDue's throttle: the first call
// always goes through, a second call inside the interval is refused, one
// past it is allowed again, and a zero WithEstimateInterval must not have
// shortened the 15 second default.
func TestLiveEstimateDueThrottlesWithinInterval(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	live, err := NewLive(WithClock(func() time.Time { return instant }))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	if !live.estimateDue() {
		t.Fatal("estimateDue() first call = false, want true")
	}

	if live.estimateDue() {
		t.Error("estimateDue() second call at the same instant = true, want false (throttled)")
	}
}

func TestLiveEstimateDueAllowsAfterInterval(t *testing.T) {
	t.Parallel()

	current := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	live, err := NewLive(WithClock(func() time.Time { return current }))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	if !live.estimateDue() {
		t.Fatal("estimateDue() first call = false, want true")
	}

	current = current.Add(defaultEstimateInterval + time.Second)

	if !live.estimateDue() {
		t.Error("estimateDue() after the interval elapsed = false, want true")
	}
}

// TestLiveEstimateDueZeroIntervalKeepsDefault checks that WithEstimateInterval(0)
// left the 15 second default in force, by throttling one second short of it.
func TestLiveEstimateDueZeroIntervalKeepsDefault(t *testing.T) {
	t.Parallel()

	current := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	live, err := NewLive(WithClock(func() time.Time { return current }), WithEstimateInterval(0))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	if !live.estimateDue() {
		t.Fatal("estimateDue() first call = false, want true")
	}

	current = current.Add(defaultEstimateInterval - time.Second)

	if live.estimateDue() {
		t.Error("estimateDue() one second short of the default interval = true, " +
			"want false (0 must not have shortened it)")
	}
}

// TestLiveSelfLocateEstimate drives the locator NewLive builds by default
// straight through observer, the same seam adsbOptions wires onto the real
// ADSB stream, and checks that Frame folds a successful estimate into an
// EST-labelled Receiver. The observations scatter around a point in a ring,
// mirroring how selflocate's own tests get Estimate to converge: a single
// bearing would leave the horizon circles parallel rather than intersecting.
func TestLiveSelfLocateEstimate(t *testing.T) {
	t.Parallel()

	const (
		receiverLat  = 52.0
		receiverLon  = 4.0
		observations = 60

		horizonCoefficient = 1.23
		nmPerDegree        = 60.0
		bearingSteps       = 30
		offsetShare        = 2
		minObservations    = 30
	)

	altitudesFt := [...]float64{1500, 5000, 12000, 25000, 38000}

	live, err := NewLive()
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	if live.locator == nil {
		t.Fatal("NewLive() without a manual location built no self-locator")
	}

	observe := live.positionObserver()

	for i := range observations {
		altitudeFt := altitudesFt[i%len(altitudesFt)]
		horizonNm := horizonCoefficient * math.Sqrt(altitudeFt)

		bearing := float64(i) * (2 * math.Pi / bearingSteps)
		offsetNm := horizonNm / offsetShare

		deltaLat := offsetNm * math.Cos(bearing) / nmPerDegree
		deltaLon := offsetNm * math.Sin(bearing) / (nmPerDegree * math.Cos(receiverLat*math.Pi/180))

		observe(receiverLat+deltaLat, receiverLon+deltaLon, altitudeFt)
	}

	if got := live.locator.ObservationCount(); got < minObservations {
		t.Fatalf("ObservationCount() = %d, want at least %d", got, minObservations)
	}

	receiver := live.Frame().Receiver

	if receiver.Label != LabelEstimate {
		t.Fatalf("Receiver.Label = %q, want %q (ObservationCount = %d)",
			receiver.Label, LabelEstimate, live.locator.ObservationCount())
	}

	if receiver.HasFix {
		t.Error("Receiver.HasFix = true, want false for a self-locate estimate")
	}

	if receiver.ConfidenceNm <= 0 {
		t.Error("Receiver.ConfidenceNm = 0, want > 0 for a self-locate estimate")
	}

	if !almostEqual(receiver.Latitude, receiverLat, 1) || !almostEqual(receiver.Longitude, receiverLon, 1) {
		t.Errorf("Receiver position = (%v, %v), want near (%v, %v)",
			receiver.Latitude, receiver.Longitude, receiverLat, receiverLon)
	}

	// A second Frame call lands well inside the estimate interval, so
	// applyEstimate must take its "not due yet" shortcut and leave the
	// estimate already folded into loc in place rather than recomputing it.
	again := live.Frame().Receiver
	if again.Label != LabelEstimate {
		t.Errorf("second Frame().Receiver.Label = %q, want %q (throttled, previous estimate kept)",
			again.Label, LabelEstimate)
	}
}

// TestLiveApplyEstimateNilLocator covers applyEstimate's guard for a Live
// built without a locator. receiver only ever calls applyEstimate when it
// has one (NewLive builds one whenever the position is not manual), so this
// branch is reached here directly rather than through the public API.
func TestLiveApplyEstimateNilLocator(t *testing.T) {
	t.Parallel()

	live := &Live{}

	live.applyEstimate()
}

// TestLiveReceiverGPSFix covers receiver's GPS branch. Live itself never
// drives loc into a GPS fix (uScope has no gpsd watcher of its own), so this
// pokes the shared *location.Location directly, the same way a GPS-aware
// caller elsewhere in uAirwaves would.
func TestLiveReceiverGPSFix(t *testing.T) {
	t.Parallel()

	const (
		gpsLatitude  = 51.5
		gpsLongitude = -0.1
		threeDFix    = 3
	)

	live, err := NewLive()
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	live.loc.Update(
		location.WithLatitude(gpsLatitude),
		location.WithLongitude(gpsLongitude),
		location.WithMode(threeDFix),
	)

	receiver := live.receiver()

	if receiver.Label != LabelGPS || !receiver.HasFix {
		t.Errorf("Receiver = %+v, want Label %q and HasFix true", receiver, LabelGPS)
	}

	if receiver.Latitude != gpsLatitude || receiver.Longitude != gpsLongitude {
		t.Errorf("Receiver coordinates = (%v, %v), want (%v, %v)",
			receiver.Latitude, receiver.Longitude, gpsLatitude, gpsLongitude)
	}

	if receiver.Mode != FixGPS3D {
		t.Errorf("Receiver.Mode = %d, want FixGPS3D (%d)", receiver.Mode, FixGPS3D)
	}
}

// TestLiveReceiverFixMode pins the mode every branch of receiver reports.
//
// The mode is what the scope colours the home marker by, so a branch that
// reported the wrong one would paint a guess in the colour of a fix. Each case
// drives the same *location.Location a GPS-aware caller would.
func TestLiveReceiverFixMode(t *testing.T) {
	t.Parallel()

	const (
		someLatitude  = 51.5
		someLongitude = -0.1
		twoDFix       = 2
		threeDFix     = 3
		noFix         = 0
	)

	for _, testCase := range []struct {
		name  string
		setUp func(*testing.T) *Live
		want  FixMode
	}{
		{
			name: "nothing known at all",
			setUp: func(t *testing.T) *Live {
				t.Helper()

				return newTestLive(t)
			},
			want: FixNone,
		},
		{
			name: "a position the operator typed in",
			setUp: func(t *testing.T) *Live {
				t.Helper()

				return newTestLive(t, WithManualLocation(someLatitude, someLongitude))
			},
			want: FixManual,
		},
		{
			name: "a two dimensional GPS fix",
			setUp: func(t *testing.T) *Live {
				t.Helper()

				live := newTestLive(t)
				live.loc.Update(
					location.WithLatitude(someLatitude),
					location.WithLongitude(someLongitude),
					location.WithMode(twoDFix),
				)

				return live
			},
			want: FixGPS2D,
		},
		{
			name: "a three dimensional GPS fix",
			setUp: func(t *testing.T) *Live {
				t.Helper()

				live := newTestLive(t)
				live.loc.Update(
					location.WithLatitude(someLatitude),
					location.WithLongitude(someLongitude),
					location.WithMode(threeDFix),
				)

				return live
			},
			want: FixGPS3D,
		},
		{
			name: "a self-locate estimate",
			setUp: func(t *testing.T) *Live {
				t.Helper()

				live := newTestLive(t)
				live.loc.Update(
					location.WithLatitude(someLatitude),
					location.WithLongitude(someLongitude),
					location.WithMode(noFix),
					location.WithSource(location.SourceInferred),
				)

				return live
			},
			want: FixEstimated,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.setUp(t).receiver().Mode; got != testCase.want {
				t.Errorf("Receiver.Mode = %d, want %d", got, testCase.want)
			}
		})
	}
}

// newTestLive builds a Live for the cases above, failing the test rather than
// making every one of them handle a constructor error that cannot happen.
func newTestLive(t *testing.T, opts ...LiveOption) *Live {
	t.Helper()

	live, err := NewLive(opts...)
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	return live
}

// TestLiveWithEstimateIntervalOption covers both branches directly: a
// positive value replaces the field, zero (or less) leaves it alone.
func TestLiveWithEstimateIntervalOption(t *testing.T) {
	t.Parallel()

	const positive = 2 * time.Second

	live := &Live{estimateInterval: defaultEstimateInterval}

	WithEstimateInterval(positive)(live)

	if live.estimateInterval != positive {
		t.Errorf("estimateInterval after WithEstimateInterval(%v) = %v, want %v",
			positive, live.estimateInterval, positive)
	}

	WithEstimateInterval(0)(live)

	if live.estimateInterval != positive {
		t.Errorf("estimateInterval after WithEstimateInterval(0) = %v, want unchanged %v",
			live.estimateInterval, positive)
	}
}

// TestDemoSnapshotTiebreaksByICAO forces two aircraft to the exact same
// distance from the receiver, which is the only way to reach snapshot's
// ICAO tie-break: the demo fleet's own jittered positions never coincide.
func TestDemoSnapshotTiebreaksByICAO(t *testing.T) {
	t.Parallel()

	const sharedLatitude, sharedLongitude = 52.0, 4.1

	demo := &Demo{
		lat: demoLatitude,
		lon: demoLongitude,
		fleet: []craft{
			{spec: craftSpec{icao: "BBBBBB"}, latitude: sharedLatitude, longitude: sharedLongitude},
			{spec: craftSpec{icao: "AAAAAA"}, latitude: sharedLatitude, longitude: sharedLongitude},
		},
	}

	list := demo.snapshot(time.Now())

	if len(list) != len(demo.fleet) {
		t.Fatalf("snapshot returned %d planes, want %d", len(list), len(demo.fleet))
	}

	if list[0].ICAO != "AAAAAA" || list[1].ICAO != "BBBBBB" {
		t.Errorf("tie-break order = [%s, %s], want [AAAAAA, BBBBBB] (ascending ICAO)", list[0].ICAO, list[1].ICAO)
	}
}

// TestOffset covers offset's pole case (cosLat under minCosLatitude), and its
// ordinary case at a moderate latitude.
func TestOffset(t *testing.T) {
	t.Parallel()

	const (
		tolerance   = 1e-6
		moderateLat = 52.0
		moderateLon = 4.0
		eastBearing = 90.0
		poleLon     = 10.0
	)

	for _, testCase := range []struct {
		name          string
		lat, lon      float64
		bearing, dist float64
		wantLat       float64
		wantLon       float64
	}{
		{
			name: "moderate latitude moves both axes", lat: moderateLat, lon: moderateLon,
			bearing: eastBearing, dist: nmPerDegree,
			wantLat: moderateLat, wantLon: moderateLon + 1.0/math.Cos(moderateLat*math.Pi/180),
		},
		{
			// Starting exactly at the pole with no distance to travel puts
			// cosLat itself under minCosLatitude, which is what drives offset
			// into the clamp-and-wrap branch instead of dividing by it.
			name: "pole falls back to clamp and wrap", lat: maxLatitude, lon: poleLon, bearing: 0, dist: 0,
			wantLat: maxLatitude, wantLon: poleLon,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotLat, gotLon := offset(testCase.lat, testCase.lon, testCase.bearing, testCase.dist)

			if !almostEqual(gotLat, testCase.wantLat, tolerance) {
				t.Errorf("offset() latitude = %v, want %v", gotLat, testCase.wantLat)
			}

			if !almostEqual(gotLon, testCase.wantLon, tolerance) {
				t.Errorf("offset() longitude = %v, want %v", gotLon, testCase.wantLon)
			}
		})
	}
}

// TestWrapLongitude covers wrapping past both edges of the valid range, plus
// a value already inside it.
func TestWrapLongitude(t *testing.T) {
	t.Parallel()

	const tolerance = 1e-9

	for _, testCase := range []struct {
		name string
		in   float64
		want float64
	}{
		{name: "already in range", in: 45.0, want: 45.0},
		{name: "wraps past +180", in: 190.0, want: -170.0},
		{name: "wraps past -180", in: -190.0, want: 170.0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := wrapLongitude(testCase.in); !almostEqual(got, testCase.want, tolerance) {
				t.Errorf("wrapLongitude(%v) = %v, want %v", testCase.in, got, testCase.want)
			}
		})
	}
}

// TestClampLatitude covers both poles and a value already inside range.
func TestClampLatitude(t *testing.T) {
	t.Parallel()

	const overshoot = 10.0

	for _, testCase := range []struct {
		name string
		in   float64
		want float64
	}{
		{name: "already in range", in: 45.0, want: 45.0},
		{name: "clamps at the north pole", in: maxLatitude + overshoot, want: maxLatitude},
		{name: "clamps at the south pole", in: minLatitude - overshoot, want: minLatitude},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := clampLatitude(testCase.in); got != testCase.want {
				t.Errorf("clampLatitude(%v) = %v, want %v", testCase.in, got, testCase.want)
			}
		})
	}
}

// TestReciprocal covers the wraparound case (a track past 180 degrees, which
// pulls math.Mod's result back up by a full turn) alongside the plain case.
func TestReciprocal(t *testing.T) {
	t.Parallel()

	const tolerance = 1e-9

	for _, testCase := range []struct {
		name  string
		track float64
		want  float64
	}{
		{name: "plain course", track: 90.0, want: 270.0},
		{name: "wraps back into range", track: 270.0, want: 90.0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := reciprocal(testCase.track); !almostEqual(got, testCase.want, tolerance) {
				t.Errorf("reciprocal(%v) = %v, want %v", testCase.track, got, testCase.want)
			}
		})
	}
}

// snapshotWithHistory builds the one shape ghosts.go reads out of a
// airplane.Snapshot: an ICAO plus a trail of the given length. Nothing else
// on the struct matters to keep, sweep or push, so nothing else is set.
func snapshotWithHistory(icao string, points int) airplane.Snapshot {
	history := make([]airplane.PositionEntry, points)
	for index := range history {
		history[index] = airplane.PositionEntry{Latitude: float64(index)}
	}

	return airplane.Snapshot{ICAO: icao, PositionHistory: history}
}

// ghostICAOs collects the ICAO of every trail in order, so a test can assert
// against a plain slice of strings instead of a slice of Trail values.
func ghostICAOs(trails []Trail) []string {
	icaos := make([]string, len(trails))
	for index, trail := range trails {
		icaos[index] = trail.ICAO
	}

	return icaos
}

// loseGhost is the only way observe's push path is reached: an aircraft has
// to be seen on one frame and gone on the next before sweep will give it up.
// It hands back the ghost list as it stands right after that aircraft is
// lost.
func loseGhost(tracker *ghosts, icao string, points int) []Trail {
	tracker.observe(airplanes.List{snapshotWithHistory(icao, points)})

	return tracker.observe(nil)
}

// TestGhostsEvictByTrailCount drives more lost aircraft through observe than
// maxTrails allows and checks the ring keeps only the newest ones, oldest
// first, and never grows past the cap. maxTrails is a field for exactly this:
// driving eviction needs no fixture anywhere near the production 2000 cap.
func TestGhostsEvictByTrailCount(t *testing.T) {
	t.Parallel()

	const (
		trailCap    = 3
		trailPoints = 2
	)

	tracker := newGhosts(true)
	tracker.maxTrails = trailCap

	var last []Trail

	for _, icao := range []string{"A00001", "A00002", "A00003", "A00004", "A00005"} {
		last = loseGhost(&tracker, icao, trailPoints)
	}

	want := []string{"A00003", "A00004", "A00005"}
	got := ghostICAOs(last)

	if !slices.Equal(got, want) {
		t.Errorf("Ghosts after 5 losses with maxTrails=%d = %v, want %v", trailCap, got, want)
	}

	if len(last) > trailCap {
		t.Errorf("len(Ghosts) = %d, want at most %d", len(last), trailCap)
	}
}

// TestGhostsEvictByPointCount checks the other cap: eviction driven by total
// points rather than trail count, with maxTrails left at its large default.
// Three trails of four points each against a cap of ten force exactly one
// eviction, so the survivors are a known pair rather than just "no more than
// the cap".
func TestGhostsEvictByPointCount(t *testing.T) {
	t.Parallel()

	const (
		pointCap    = 10
		trailPoints = 4
	)

	tracker := newGhosts(true)
	tracker.maxPoints = pointCap

	var last []Trail

	for _, icao := range []string{"B00001", "B00002", "B00003"} {
		last = loseGhost(&tracker, icao, trailPoints)
	}

	want := []string{"B00002", "B00003"}
	got := ghostICAOs(last)

	if !slices.Equal(got, want) {
		t.Errorf("Ghosts after 3 losses of %d points each with maxPoints=%d = %v, want %v",
			trailPoints, pointCap, got, want)
	}
}

// TestGhostsNoPointsNeverPushed checks push's guard: an aircraft that appears
// with an empty history and then vanishes must leave no ghost at all. A
// track of no fixes has nothing to draw, and keeping it would spend a ring
// slot on nothing.
func TestGhostsNoPointsNeverPushed(t *testing.T) {
	t.Parallel()

	const emptyHistory = 0

	tracker := newGhosts(true)

	got := loseGhost(&tracker, "C00001", emptyHistory)

	if len(got) != 0 {
		t.Errorf("Ghosts after an aircraft with no history vanished = %v, want none", got)
	}
}

// TestGhostsEmptyICAOIgnored checks keep's guard: a snapshot with no ICAO is
// never tracked at all, so it neither becomes a ghost when it "vanishes" nor
// spends a slot in the ring on the way.
func TestGhostsEmptyICAOIgnored(t *testing.T) {
	t.Parallel()

	const points = 3

	tracker := newGhosts(true)

	got := loseGhost(&tracker, "", points)

	if len(got) != 0 {
		t.Errorf("Ghosts after an aircraft with no ICAO vanished = %v, want none", got)
	}

	if len(tracker.live) != 0 {
		t.Errorf("live map after an aircraft with no ICAO = %d entries, want 0 (never tracked)", len(tracker.live))
	}
}

// TestGhostsSweepOrderIsSortedAndStable checks that two aircraft lost on the
// same frame always enter the ring in ascending ICAO order, whichever order
// the map sweep happened to walk them in. Map iteration order varies between
// runs even within one process, which is exactly what sweep's sort is meant
// to hide, so this repeats the sequence enough times to have caught a
// regression back to an unsorted sweep.
func TestGhostsSweepOrderIsSortedAndStable(t *testing.T) {
	t.Parallel()

	const (
		points  = 2
		repeats = 25
	)

	for attempt := range repeats {
		tracker := newGhosts(true)

		tracker.observe(airplanes.List{
			snapshotWithHistory("ZULU01", points),
			snapshotWithHistory("ALPHA1", points),
		})

		got := ghostICAOs(tracker.observe(nil))
		want := []string{"ALPHA1", "ZULU01"}

		if !slices.Equal(got, want) {
			t.Fatalf("attempt %d: Ghosts order = %v, want %v (ascending by ICAO)", attempt, got, want)
		}
	}
}

// TestGhostsReviveLeavesHoleWithoutShifting pushes three ghosts, revives the
// middle one, and checks the other two keep the order they were lost in. A
// revival that shuffled the ring instead of leaving a hole would make an
// older ghost look newer than it is.
func TestGhostsReviveLeavesHoleWithoutShifting(t *testing.T) {
	t.Parallel()

	const points = 2

	tracker := newGhosts(true)

	loseGhost(&tracker, "D00001", points)
	loseGhost(&tracker, "D00002", points)
	before := loseGhost(&tracker, "D00003", points)

	wantBefore := []string{"D00001", "D00002", "D00003"}
	if got := ghostICAOs(before); !slices.Equal(got, wantBefore) {
		t.Fatalf("Ghosts before revival = %v, want %v", got, wantBefore)
	}

	// D00002 reappears: keep calls revive first, which empties its ring slot
	// before the trail is recorded as live again.
	afterRevive := tracker.observe(airplanes.List{snapshotWithHistory("D00002", points)})

	wantAfter := []string{"D00001", "D00003"}
	if got := ghostICAOs(afterRevive); !slices.Equal(got, wantAfter) {
		t.Errorf("Ghosts after D00002 revived = %v, want %v", got, wantAfter)
	}
}

// TestBearingOf checks bearingOf against the four cardinal directions and the
// one case that forces its negative-atan2 branch: an aircraft to the
// north-west, which must come out between 270 and 360 degrees rather than as
// a negative number.
func TestBearingOf(t *testing.T) {
	t.Parallel()

	const (
		receiverLat = 52.0
		receiverLon = 4.0
		offset      = 1.0
		tolerance   = 0.5

		north = 0.0
		east  = 90.0
		south = 180.0
		west  = 270.0
	)

	for _, testCase := range []struct {
		name             string
		lat, lon         float64
		wantMin, wantMax float64
	}{
		{
			name: "due north", lat: receiverLat + offset, lon: receiverLon,
			wantMin: north - tolerance, wantMax: north + tolerance,
		},
		{
			name: "due east", lat: receiverLat, lon: receiverLon + offset,
			wantMin: east - tolerance, wantMax: east + tolerance,
		},
		{
			name: "due south", lat: receiverLat - offset, lon: receiverLon,
			wantMin: south - tolerance, wantMax: south + tolerance,
		},
		{
			name: "due west", lat: receiverLat, lon: receiverLon - offset,
			wantMin: west - tolerance, wantMax: west + tolerance,
		},
		{
			name: "north-west lands in the negative-atan2 branch",
			lat:  receiverLat + offset, lon: receiverLon - offset,
			wantMin: west, wantMax: degreesPerCircle,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := bearingOf(receiverLat, receiverLon, testCase.lat, testCase.lon)

			if got < testCase.wantMin || got > testCase.wantMax {
				t.Errorf("bearingOf(%v, %v, %v, %v) = %v, want between %v and %v",
					receiverLat, receiverLon, testCase.lat, testCase.lon, got, testCase.wantMin, testCase.wantMax)
			}
		})
	}
}

// TestCoverageCacheObserve checks observe's success path and its three drop
// guards. A dropped fix must leave the tracker's Snapshot entirely at its
// zero value, since nothing was ever folded into it.
func TestCoverageCacheObserve(t *testing.T) {
	t.Parallel()

	const (
		receiverLat = 52.0
		receiverLon = 4.0
		aircraftLat = 52.5
		aircraftLon = 4.5
		altitudeFt  = 10000.0
	)

	t.Run("an ordinary fix lands in the tracker", func(t *testing.T) {
		t.Parallel()

		cache := newCoverage()
		cache.observe(receiverLat, receiverLon, aircraftLat, aircraftLon, altitudeFt)

		wantDistance := airplanes.HaversineDistance(receiverLat, receiverLon, aircraftLat, aircraftLon)
		snapshot := cache.tracker.Snapshot()

		if snapshot.MaxRangeNm <= 0 {
			t.Fatalf("MaxRangeNm = %v, want > 0", snapshot.MaxRangeNm)
		}

		matched := false

		for _, sector := range snapshot.Sectors {
			if sector == wantDistance {
				matched = true

				break
			}
		}

		if !matched {
			t.Errorf("Sectors = %v, want one entry at %v", snapshot.Sectors, wantDistance)
		}
	})

	for _, testCase := range []struct {
		name                     string
		receiverLat, receiverLon float64
		lat, lon                 float64
	}{
		{name: "receiver at (0, 0) is dropped", receiverLat: 0, receiverLon: 0, lat: aircraftLat, lon: aircraftLon},
		{name: "aircraft at (0, 0) is dropped", receiverLat: receiverLat, receiverLon: receiverLon, lat: 0, lon: 0},
		{
			name:        "a NaN coordinate is dropped",
			receiverLat: receiverLat, receiverLon: receiverLon,
			lat: math.NaN(), lon: aircraftLon,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cache := newCoverage()
			cache.observe(testCase.receiverLat, testCase.receiverLon, testCase.lat, testCase.lon, altitudeFt)

			if snapshot := cache.tracker.Snapshot(); snapshot != (coverage.Snapshot{}) {
				t.Errorf("Snapshot() = %+v, want the zero value (fix dropped)", snapshot)
			}
		})
	}
}

// TestCoverageCacheSnapshot drives the cache's one-second throttle by hand:
// the first call takes a copy even at the zero time, a call inside
// coverageInterval reuses it, and a call at or past coverageInterval picks up
// whatever has been observed since.
func TestCoverageCacheSnapshot(t *testing.T) {
	t.Parallel()

	const (
		receiverLat = 52.0
		receiverLon = 4.0
		firstLat    = 52.5
		firstLon    = 4.5
		secondLat   = 53.5
		secondLon   = 5.5
		altitudeFt  = 10000.0
	)

	var zero time.Time

	cache := newCoverage()
	cache.observe(receiverLat, receiverLon, firstLat, firstLon, altitudeFt)

	first := cache.snapshot(zero)
	if first.MaxRangeNm <= 0 {
		t.Fatalf("first snapshot() at the zero time returned MaxRangeNm = %v, want > 0", first.MaxRangeNm)
	}

	cache.observe(receiverLat, receiverLon, secondLat, secondLon, altitudeFt)

	stillWithin := cache.snapshot(zero.Add(coverageInterval - time.Nanosecond))
	if stillWithin != first {
		t.Errorf("snapshot() inside coverageInterval = %+v, want the cached %+v", stillWithin, first)
	}

	pastInterval := cache.snapshot(zero.Add(coverageInterval))
	if pastInterval == first {
		t.Error("snapshot() at coverageInterval returned the stale copy, want the refreshed one")
	}
}

// TestLivePositionObserver checks positionObserver's two halves. With no
// manual position the receiver is still unknown when the fix arrives, so the
// locator is the half that is provably fed; with a manual position the
// receiver is known up front, so coverage is the half that is provably fed
// and the locator is nil.
func TestLivePositionObserver(t *testing.T) {
	t.Parallel()

	const (
		manualLat   = 52.3
		manualLon   = 4.77
		observedLat = 52.9
		observedLon = 5.1
		altitudeFt  = 15000.0
	)

	t.Run("with a locator present, the locator is fed", func(t *testing.T) {
		t.Parallel()

		live, err := NewLive()
		if err != nil {
			t.Fatalf("NewLive: %v", err)
		}

		if live.locator == nil {
			t.Fatal("NewLive() without a manual location built no self-locator")
		}

		live.positionObserver()(observedLat, observedLon, altitudeFt)

		if got := live.locator.ObservationCount(); got != 1 {
			t.Errorf("ObservationCount() = %d, want 1", got)
		}
	})

	t.Run("with a manual location, the locator is nil and coverage still runs", func(t *testing.T) {
		t.Parallel()

		live, err := NewLive(WithManualLocation(manualLat, manualLon))
		if err != nil {
			t.Fatalf("NewLive: %v", err)
		}

		if live.locator != nil {
			t.Fatal("NewLive() with a manual location built a self-locator, want nil")
		}

		live.positionObserver()(observedLat, observedLon, altitudeFt)

		if got := live.Frame().Coverage.MaxRangeNm; got <= 0 {
			t.Errorf("Frame().Coverage.MaxRangeNm = %v, want > 0", got)
		}
	})
}

// TestDemoObserveFleetSkipsQuiet checks the guard that keeps observeFleet
// from binning a quiet aircraft. Skipping it is what stops a quiet aircraft
// being folded into coverage for ever: once it stops transmitting its last
// position should freeze rather than keep refreshing whichever sector it
// happened to sit in. The quiet aircraft here sits much farther out than the
// flying one, so if the guard were missing it would dominate MaxRangeNm
// instead.
func TestDemoObserveFleetSkipsQuiet(t *testing.T) {
	t.Parallel()

	const (
		nearOffset = 1.0
		farOffset  = 5.0
		altitudeFt = 10000.0
	)

	flyingLat, flyingLon := demoLatitude+nearOffset, demoLongitude+nearOffset
	quietLat, quietLon := demoLatitude-farOffset, demoLongitude-farOffset

	demo := &Demo{
		lat:      demoLatitude,
		lon:      demoLongitude,
		coverage: newCoverage(),
		fleet: []craft{
			{spec: craftSpec{altitude: altitudeFt}, latitude: flyingLat, longitude: flyingLon},
			{spec: craftSpec{altitude: altitudeFt}, latitude: quietLat, longitude: quietLon, quiet: true},
		},
	}

	demo.observeFleet()

	wantDistance := airplanes.HaversineDistance(demoLatitude, demoLongitude, flyingLat, flyingLon)

	if got := demo.coverage.tracker.Snapshot().MaxRangeNm; got != wantDistance {
		t.Errorf("MaxRangeNm = %v, want %v (the farther, quiet aircraft must not have been observed)",
			got, wantDistance)
	}
}
