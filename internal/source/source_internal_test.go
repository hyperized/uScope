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

	"github.com/hyperized/rtl2832u"
	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/coverage"
	"github.com/hyperized/uAirwaves/pkg/gps"
	"github.com/hyperized/uAirwaves/pkg/location"
)

// errStreamFailure stands in for whatever an ingest might fail with. Static
// so err113 is satisfied; stream's test only checks whether something was
// reported, never the text.
var errStreamFailure = errors.New("stub ingest: stream failed") //nolint:gochecknoglobals // error sentinel, not state.

// errDongleOpenFailure and errDongleOpenPreset stand in for whatever the
// dongle might fail to open with. Static so err113 is satisfied; the
// dongle-opener tests below only care which one comes back, never its text.
var (
	//nolint:gochecknoglobals // error sentinel, not state.
	errDongleOpenFailure = errors.New("test dongle: open failed")
	//nolint:gochecknoglobals // error sentinel, not state.
	errDongleOpenPreset = errors.New("test dongle: preset opener called")
)

// stubIngest is a minimal ingest implementation for calling stream directly,
// without the goroutine Start adds. Every call happens on the test's own
// goroutine, so unlike the Start/Close tests in the external file this needs
// no synchronisation.
type stubIngest struct {
	err error

	// The bias-tee half. biasErr is what SetBiasTee reports; the two bools
	// are what the cached read hands back, and sweeping what the header asks.
	biasSupported bool
	biasEnabled   bool
	sweeping      bool
	biasErr       error
}

func (s stubIngest) Stream(context.Context, *airplanes.Airplanes) error { return s.err }

func (stubIngest) Source() adsb.SourceInfo { return adsb.SourceInfo{} }

func (stubIngest) Stats() adsb.Stats { return adsb.Stats{} }

//nolint:nonamedreturns // mirrors the interface it satisfies.
func (s stubIngest) BiasTeeState() (supported, enabled bool) { return s.biasSupported, s.biasEnabled }

func (s stubIngest) SetBiasTee(bool) error { return s.biasErr }

func (s stubIngest) Sweeping() bool { return s.sweeping }

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

	tracker := newGhosts()
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

	tracker := newGhosts()
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

	tracker := newGhosts()

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

	tracker := newGhosts()

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
		tracker := newGhosts()

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

	tracker := newGhosts()

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

// TestCoverageCacheObserve checks observe's success path and its four drop
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

	t.Run("a low but positive altitude lands too", func(t *testing.T) {
		t.Parallel()

		const lowAltitudeFt = 1200.0

		cache := newCoverage()
		cache.observe(receiverLat, receiverLon, aircraftLat, aircraftLon, lowAltitudeFt)

		if snapshot := cache.tracker.Snapshot(); snapshot.MaxRangeNm <= 0 {
			t.Errorf("MaxRangeNm at %g ft = %v, want > 0", lowAltitudeFt, snapshot.MaxRangeNm)
		}
	})

	for _, testCase := range []struct {
		name                     string
		receiverLat, receiverLon float64
		lat, lon                 float64
		altitudeFt               float64
	}{
		{
			name:        "receiver at (0, 0) is dropped",
			receiverLat: 0, receiverLon: 0, lat: aircraftLat, lon: aircraftLon, altitudeFt: altitudeFt,
		},
		{
			name:        "aircraft at (0, 0) is dropped",
			receiverLat: receiverLat, receiverLon: receiverLon, lat: 0, lon: 0, altitudeFt: altitudeFt,
		},
		{
			name:        "a NaN coordinate is dropped",
			receiverLat: receiverLat, receiverLon: receiverLon,
			lat: math.NaN(), lon: aircraftLon, altitudeFt: altitudeFt,
		},
		{
			name:        "an undecoded altitude is dropped",
			receiverLat: receiverLat, receiverLon: receiverLon,
			lat: aircraftLat, lon: aircraftLon, altitudeFt: 0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cache := newCoverage()
			cache.observe(testCase.receiverLat, testCase.receiverLon, testCase.lat, testCase.lon, testCase.altitudeFt)

			if snapshot := cache.tracker.Snapshot(); snapshot != (coverage.Snapshot{}) {
				t.Errorf("Snapshot() = %+v, want the zero value (fix dropped)", snapshot)
			}

			if sum := sumGridCells(cache.grid); sum != 0 {
				t.Errorf("sum of grid cells = %d, want 0 (fix dropped)", sum)
			}
		})
	}
}

// TestCoverageCacheObserveFillsGrid checks that an ordinary fix lands in the
// grid as well as the tracker, in a test function of its own rather than a
// subtest of TestCoverageCacheObserve, which is already at the cognitive
// complexity limit revive enforces for one function.
func TestCoverageCacheObserveFillsGrid(t *testing.T) {
	t.Parallel()

	const (
		receiverLat = 52.0
		receiverLon = 4.0
		aircraftLat = 52.5
		aircraftLon = 4.5
		altitudeFt  = 10000.0

		wantCount = 1
	)

	cache := newCoverage()
	cache.observe(receiverLat, receiverLon, aircraftLat, aircraftLon, altitudeFt)

	distanceNm := airplanes.HaversineDistance(receiverLat, receiverLon, aircraftLat, aircraftLon)
	bearingDeg := bearingOf(receiverLat, receiverLon, aircraftLat, aircraftLon)
	wantSector, wantBand, wantBin := bearingSector(bearingDeg), altitudeBand(altitudeFt), distanceBin(distanceNm)

	if got := cache.grid.Cells[wantSector][wantBand][wantBin]; got != wantCount {
		t.Errorf("grid.Cells[%d][%d][%d] = %d, want %d", wantSector, wantBand, wantBin, got, wantCount)
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

	first, _ := cache.snapshot(zero)
	if first.MaxRangeNm <= 0 {
		t.Fatalf("first snapshot() at the zero time returned MaxRangeNm = %v, want > 0", first.MaxRangeNm)
	}

	cache.observe(receiverLat, receiverLon, secondLat, secondLon, altitudeFt)

	stillWithin, _ := cache.snapshot(zero.Add(coverageInterval - time.Nanosecond))
	if stillWithin != first {
		t.Errorf("snapshot() inside coverageInterval = %+v, want the cached %+v", stillWithin, first)
	}

	pastInterval, _ := cache.snapshot(zero.Add(coverageInterval))
	if pastInterval == first {
		t.Error("snapshot() at coverageInterval returned the stale copy, want the refreshed one")
	}
}

// TestCoverageCacheSnapshotThrottlesGrid checks that the grid half of
// snapshot's return is throttled by the same coverageInterval as the
// tracker's Snapshot, rather than being refreshed on every call regardless of
// the clock.
func TestCoverageCacheSnapshotThrottlesGrid(t *testing.T) {
	t.Parallel()

	const (
		receiverLat = 52.0
		receiverLon = 4.0
		firstLat    = 52.5
		firstLon    = 4.5
		secondLat   = 53.5
		secondLon   = 5.5
		altitudeFt  = 10000.0

		wantAfterFirst = 1
		wantAfterBoth  = 2
	)

	var zero time.Time

	cache := newCoverage()
	cache.observe(receiverLat, receiverLon, firstLat, firstLon, altitudeFt)

	_, firstGrid := cache.snapshot(zero)
	if sum := sumGridCells(firstGrid); sum != wantAfterFirst {
		t.Fatalf("first snapshot() grid sum = %d, want %d", sum, wantAfterFirst)
	}

	cache.observe(receiverLat, receiverLon, secondLat, secondLon, altitudeFt)

	_, stillWithin := cache.snapshot(zero.Add(coverageInterval - time.Nanosecond))
	if sum := sumGridCells(stillWithin); sum != wantAfterFirst {
		t.Errorf("snapshot() grid sum inside coverageInterval = %d, want the cached %d", sum, wantAfterFirst)
	}

	_, pastInterval := cache.snapshot(zero.Add(coverageInterval))
	if sum := sumGridCells(pastInterval); sum != wantAfterBoth {
		t.Errorf("snapshot() grid sum at coverageInterval = %d, want the refreshed %d", sum, wantAfterBoth)
	}
}

// TestBearingSector checks bearingSector's bin boundaries, plus the one
// clamp case bearingOf can actually produce: a bearing of exactly 360
// degrees, which is what a value a hair under zero rounds up to once the
// circle is added back in.
func TestBearingSector(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		bearingDeg float64
		want       int
	}{
		{name: "0 degrees is sector 0", bearingDeg: 0.0, want: 0},
		{name: "just under the sector width stays in sector 0", bearingDeg: 22.4, want: 0},
		{name: "at the sector width moves into sector 1", bearingDeg: 22.5, want: 1},
		{name: "just under a full circle is the last sector", bearingDeg: 359.9, want: 15},
		{name: "a full circle clamps into the last sector", bearingDeg: 360.0, want: 15},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := bearingSector(testCase.bearingDeg); got != testCase.want {
				t.Errorf("bearingSector(%v) = %d, want %d", testCase.bearingDeg, got, testCase.want)
			}
		})
	}
}

// TestAltitudeBand checks altitudeBand's bin boundaries, and that anything at
// or above the top of the grid clamps into the last band instead of indexing
// past it.
func TestAltitudeBand(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		altitudeFt float64
		want       int
	}{
		{name: "1 foot is band 0", altitudeFt: 1.0, want: 0},
		{name: "just under the band width stays in band 0", altitudeFt: 4999.0, want: 0},
		{name: "at the band width moves into band 1", altitudeFt: 5000.0, want: 1},
		{name: "just under the top of the grid is the last band", altitudeFt: 49999.0, want: 9},
		{name: "the top of the grid clamps into the last band", altitudeFt: 50000.0, want: 9},
		{name: "well past the top of the grid clamps the same way", altitudeFt: 250000.0, want: 9},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := altitudeBand(testCase.altitudeFt); got != testCase.want {
				t.Errorf("altitudeBand(%v) = %d, want %d", testCase.altitudeFt, got, testCase.want)
			}
		})
	}
}

// TestDistanceBin checks distanceBin's bin boundaries, and that anything at
// or beyond the outer edge of the grid clamps into the last bin instead of
// indexing past it.
func TestDistanceBin(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		distanceNm float64
		want       int
	}{
		{name: "0 nm is bin 0", distanceNm: 0.0, want: 0},
		{name: "just under the bin width stays in bin 0", distanceNm: 9.9, want: 0},
		{name: "at the bin width moves into bin 1", distanceNm: 10.0, want: 1},
		{name: "just under the outer edge is the last bin", distanceNm: 249.9, want: 24},
		{name: "the outer edge clamps into the last bin", distanceNm: 250.0, want: 24},
		{name: "well past the outer edge clamps the same way", distanceNm: 5000.0, want: 24},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := distanceBin(testCase.distanceNm); got != testCase.want {
				t.Errorf("distanceBin(%v) = %d, want %d", testCase.distanceNm, got, testCase.want)
			}
		})
	}
}

// TestSaturatingInc checks the counter's two states: an ordinary count still
// has room to grow, and one already at math.MaxUint32 must not wrap back to
// zero, which is the one thing that would erase the strongest evidence a
// cell holds after a session left running long enough to reach it.
func TestSaturatingInc(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		counter uint32
		want    uint32
	}{
		{name: "below the ceiling increments", counter: 41, want: 42},
		{name: "at the ceiling stays put", counter: math.MaxUint32, want: math.MaxUint32},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			counter := testCase.counter
			saturatingInc(&counter)

			if counter != testCase.want {
				t.Errorf("saturatingInc() = %d, want %d", counter, testCase.want)
			}
		})
	}
}

// TestCoverageGridObserve checks that one fix lands in exactly the cell its
// three indices name, with every other cell in the grid left at zero: a
// bearing sector, altitude band and distance bin the fix did not fall into
// must not gain a count from it.
func TestCoverageGridObserve(t *testing.T) {
	t.Parallel()

	const (
		distanceNm = 15.0
		bearingDeg = 40.0
		altitudeFt = 12000.0

		wantCount = 1
	)

	var grid CoverageGrid
	grid.observe(distanceNm, bearingDeg, altitudeFt)

	wantSector, wantBand, wantBin := bearingSector(bearingDeg), altitudeBand(altitudeFt), distanceBin(distanceNm)

	if got := grid.Cells[wantSector][wantBand][wantBin]; got != wantCount {
		t.Errorf("Cells[%d][%d][%d] = %d, want %d", wantSector, wantBand, wantBin, got, wantCount)
	}

	if sum := sumGridCells(grid); sum != wantCount {
		t.Errorf("sum of every cell = %d, want %d (only the named cell should hold a count)", sum, wantCount)
	}
}

// sumGridCells adds every count held by a grid, so a test can check "nothing
// anywhere else" as one comparison instead of a triple loop over Cells.
func sumGridCells(grid CoverageGrid) uint64 {
	var sum uint64

	for _, sectors := range grid.Cells {
		for _, bands := range sectors {
			for _, count := range bands {
				sum += uint64(count)
			}
		}
	}

	return sum
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

// TestLiveWithBiasTeeOption checks that WithBiasTee sets the field plainly,
// on or off. Unlike WithClock or WithStderr there is no nil or zero value to
// guard against: false is as deliberate a choice as true, since it is what an
// operator without an LNA wants.
func TestLiveWithBiasTeeOption(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		on   bool
	}{
		{name: "on", on: true},
		{name: "off", on: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			live := &Live{}
			WithBiasTee(testCase.on)(live)

			if live.biasTee != testCase.on {
				t.Errorf("biasTee after WithBiasTee(%v) = %v, want %v", testCase.on, live.biasTee, testCase.on)
			}
		})
	}
}

// TestLiveWithAutoSweepOption mirrors TestLiveWithBiasTeeOption for the other
// half of the pairing: WithAutoSweep sets autoSweep plainly, on or off.
func TestLiveWithAutoSweepOption(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		on   bool
	}{
		{name: "on", on: true},
		{name: "off", on: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			live := &Live{}
			WithAutoSweep(testCase.on)(live)

			if live.autoSweep != testCase.on {
				t.Errorf("autoSweep after WithAutoSweep(%v) = %v, want %v", testCase.on, live.autoSweep, testCase.on)
			}
		})
	}
}

// TestWithDongleOpenerOption checks that withDongleOpener replaces the seam
// the bias-tee factory opens the radio through, and that a nil opener leaves
// whatever was already there in place, the same nil-guard contract every
// other option with a default carries.
func TestWithDongleOpenerOption(t *testing.T) {
	t.Parallel()

	t.Run("replaces the opener", func(t *testing.T) {
		t.Parallel()

		custom := func(...rtl2832u.Option) (*rtl2832u.Receiver, error) { return nil, errDongleOpenFailure }

		live := &Live{}
		withDongleOpener(custom)(live)

		if _, err := live.openDongle(); !errors.Is(err, errDongleOpenFailure) {
			t.Errorf("openDongle() error = %v, want %v", err, errDongleOpenFailure)
		}
	})

	t.Run("nil leaves the opener unchanged", func(t *testing.T) {
		t.Parallel()

		preset := func(...rtl2832u.Option) (*rtl2832u.Receiver, error) { return nil, errDongleOpenPreset }

		live := &Live{openDongle: preset}
		withDongleOpener(nil)(live)

		if _, err := live.openDongle(); !errors.Is(err, errDongleOpenPreset) {
			t.Errorf("openDongle() error = %v, want %v (unchanged)", err, errDongleOpenPreset)
		}
	})
}

// TestLiveSdrOptions covers all four combinations of biasTee and autoSweep:
// each flag appends exactly one adsb.Option when it is on and nothing when it
// is off, and the two are independent of each other. The options themselves
// cannot be compared, so this only asserts on how many came back.
func TestLiveSdrOptions(t *testing.T) {
	t.Parallel()

	const baseLen = 1 // whatever adsbOptions had already built up before this ran.

	for _, testCase := range []struct {
		name      string
		biasTee   bool
		autoSweep bool
		wantAdded int
	}{
		{name: "neither on", biasTee: false, autoSweep: false, wantAdded: 0},
		{name: "bias-tee only", biasTee: true, autoSweep: false, wantAdded: 1},
		{name: "auto-sweep only", biasTee: false, autoSweep: true, wantAdded: 1},
		{name: "both on", biasTee: true, autoSweep: true, wantAdded: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			live := &Live{biasTee: testCase.biasTee, autoSweep: testCase.autoSweep}

			got := live.sdrOptions(make([]adsb.Option, baseLen))

			if len(got) != baseLen+testCase.wantAdded {
				t.Errorf("len(sdrOptions()) = %d, want %d", len(got), baseLen+testCase.wantAdded)
			}
		})
	}
}

// TestBiasTeeFactory covers both branches: a dongle that opens hands back a
// non-nil adsb.Receiver, and one that fails wraps the error so a caller's
// errors.Is still finds it underneath.
func TestBiasTeeFactory(t *testing.T) {
	t.Parallel()

	t.Run("open succeeds", func(t *testing.T) {
		t.Parallel()

		open := func(...rtl2832u.Option) (*rtl2832u.Receiver, error) { return &rtl2832u.Receiver{}, nil }

		rcv, err := biasTeeFactory(open)()
		if err != nil {
			t.Fatalf("biasTeeFactory()() error = %v, want nil", err)
		}

		if rcv == nil {
			t.Error("biasTeeFactory()() receiver = nil, want the opened dongle")
		}
	})

	t.Run("open fails", func(t *testing.T) {
		t.Parallel()

		open := func(...rtl2832u.Option) (*rtl2832u.Receiver, error) { return nil, errDongleOpenFailure }

		_, err := biasTeeFactory(open)()
		if !errors.Is(err, errDongleOpenFailure) {
			t.Errorf("biasTeeFactory()() error = %v, want it to wrap %v", err, errDongleOpenFailure)
		}
	})
}

// gpsdTestAddress is a syntactically valid gpsd address shared by the gpsd
// tests below. None of them dial it: every gpsWatcher here is a fake reached
// through the factory seam, and the one test that touches the real factory
// only builds a watcher, never calls Watch on it.
const gpsdTestAddress = "127.0.0.1:2947"

// errGPSWatchFailure stands in for whatever a gpsd watcher might fail with.
// Static so err113 is satisfied; watchGPS's test only checks whether
// something was reported, never the text.
//
//nolint:gochecknoglobals // error sentinel, not state.
var errGPSWatchFailure = errors.New("test gpsd watcher: watch failed")

// stubGPSWatcher is a gpsWatcher that returns immediately with whatever
// error it was built with, which is enough to drive startGPS and watchGPS
// without leaving a goroutine running past the test.
type stubGPSWatcher struct {
	err error
}

func (s stubGPSWatcher) Watch(context.Context, *location.Location) error { return s.err }

// markedWatcher is a gpsWatcher carrying a name, so a test can tell which
// factory built the watcher live.newGPS hands back without comparing the
// factories themselves, which Go does not allow.
type markedWatcher struct {
	mark string
}

func (markedWatcher) Watch(context.Context, *location.Location) error { return nil }

// blockingGPSWatcher blocks until its context is cancelled and then closes
// done, so a test can prove Close waited for it rather than returning as
// soon as the ingest half finished.
type blockingGPSWatcher struct {
	done chan struct{}
}

func (b blockingGPSWatcher) Watch(ctx context.Context, _ *location.Location) error {
	<-ctx.Done()
	close(b.done)

	return nil
}

// seedLocatorEstimate feeds a Live's self-locator enough real observations
// to clear Estimate's readiness gates: uAirwaves' selflocate package wants
// at least 30, with one of them under its altitude ceiling. The geometry
// does not matter here the way it does in TestLiveSelfLocateEstimate: these
// tests only need Estimate to succeed, never to converge on a particular
// position.
func seedLocatorEstimate(live *Live) {
	const (
		observations = 40
		altitudeFt   = 5000.0
		step         = 0.01
		baseLat      = 52.0
		baseLon      = 4.0
	)

	for i := range observations {
		live.locator.Observe(baseLat+float64(i)*step, baseLon+float64(i)*step, altitudeFt)
	}
}

// TestLastFixSnapshot covers lastFix's zero value and the round trip through
// stamp.
func TestLastFixSnapshot(t *testing.T) {
	t.Parallel()

	var fix lastFix

	lat, lon, when := fix.snapshot()
	if lat != 0 || lon != 0 || !when.IsZero() {
		t.Fatalf("snapshot() of a zero lastFix = (%v, %v, %v), want (0, 0, zero time)", lat, lon, when)
	}

	const wantLat, wantLon = 51.5, -0.1

	wantWhen := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	fix.stamp(wantLat, wantLon, wantWhen)

	gotLat, gotLon, gotWhen := fix.snapshot()
	if gotLat != wantLat || gotLon != wantLon || !gotWhen.Equal(wantWhen) {
		t.Errorf("snapshot() after stamp = (%v, %v, %v), want (%v, %v, %v)",
			gotLat, gotLon, gotWhen, wantLat, wantLon, wantWhen)
	}
}

// TestWithGPSDOption checks that WithGPSD sets the address plainly.
func TestWithGPSDOption(t *testing.T) {
	t.Parallel()

	live := &Live{}
	WithGPSD(gpsdTestAddress)(live)

	if live.gpsd != gpsdTestAddress {
		t.Errorf("gpsd after WithGPSD(%q) = %q, want %q", gpsdTestAddress, live.gpsd, gpsdTestAddress)
	}
}

// TestWithGPSFactoryOption covers withGPSFactory's nil guard: a non-nil
// factory replaces whatever built the watcher, a nil one leaves it alone.
// Functions cannot be compared, so each case proves which factory is in
// place by the mark on the watcher it hands back.
func TestWithGPSFactoryOption(t *testing.T) {
	t.Parallel()

	t.Run("a non-nil factory replaces the default", func(t *testing.T) {
		t.Parallel()

		//nolint:ireturn // gpsFactory is the seam under test, so it has to hand back the interface.
		custom := func(string, gps.FixCallback) gpsWatcher { return markedWatcher{mark: "custom"} }

		live := &Live{}
		withGPSFactory(custom)(live)

		watcher, ok := live.newGPS(gpsdTestAddress, nil).(markedWatcher)
		if !ok || watcher.mark != "custom" {
			t.Errorf("newGPS() = %#v, want the custom factory's watcher", watcher)
		}
	})

	t.Run("nil leaves the existing factory in place", func(t *testing.T) {
		t.Parallel()

		//nolint:ireturn // gpsFactory is the seam under test, so it has to hand back the interface.
		preset := func(string, gps.FixCallback) gpsWatcher { return markedWatcher{mark: "preset"} }

		live := &Live{newGPS: preset}
		withGPSFactory(nil)(live)

		watcher, ok := live.newGPS(gpsdTestAddress, nil).(markedWatcher)
		if !ok || watcher.mark != "preset" {
			t.Errorf("newGPS() after withGPSFactory(nil) = %#v, want the preset factory's watcher", watcher)
		}
	})
}

// TestNewLiveGPSDManualClears covers NewLive's rule that a manual position
// turns gpsd off entirely, and that gpsd alone keeps the address and builds
// a locator.
func TestNewLiveGPSDManualClears(t *testing.T) {
	t.Parallel()

	const manualLat, manualLon = 52.0, 4.0

	t.Run("a manual location clears gpsd and the locator", func(t *testing.T) {
		t.Parallel()

		live := newTestLive(t, WithManualLocation(manualLat, manualLon), WithGPSD(gpsdTestAddress))

		if live.gpsd != "" {
			t.Errorf("gpsd = %q, want empty (manual location wins)", live.gpsd)
		}

		if live.locator != nil {
			t.Error("locator = non-nil, want nil (manual location wins)")
		}
	})

	t.Run("gpsd alone keeps the address and builds a locator", func(t *testing.T) {
		t.Parallel()

		live := newTestLive(t, WithGPSD(gpsdTestAddress))

		if live.gpsd != gpsdTestAddress {
			t.Errorf("gpsd = %q, want %q", live.gpsd, gpsdTestAddress)
		}

		if live.locator == nil {
			t.Error("locator = nil, want a self-locator built")
		}
	})
}

// TestNewLiveDefaultGPSFactoryIsReal checks that a Live built without
// withGPSFactory keeps newGPSWatcher: calling it builds a real watcher and
// opens nothing, since gps.New only assembles a struct. Watch is never
// called on what comes back, so this dials no socket.
func TestNewLiveDefaultGPSFactoryIsReal(t *testing.T) {
	t.Parallel()

	live := newTestLive(t)

	if watcher := live.newGPS(gpsdTestAddress, nil); watcher == nil {
		t.Error("newGPS() = nil, want the real gpsd watcher")
	}
}

// TestLiveStartGPS covers both of startGPS's branches: no address never
// calls the factory, and an address calls it exactly once with that address
// and a non-nil callback.
func TestLiveStartGPS(t *testing.T) {
	t.Parallel()

	t.Run("no address means the factory is never called", func(t *testing.T) {
		t.Parallel()

		called := false
		//nolint:ireturn // gpsFactory is the seam under test, so it has to hand back the interface.
		factory := func(string, gps.FixCallback) gpsWatcher {
			called = true

			return stubGPSWatcher{}
		}

		live := &Live{newGPS: factory}
		live.startGPS(context.Background())

		if called {
			t.Error("factory was called with no gpsd address configured")
		}
	})

	t.Run("an address calls the factory once with the address and a callback", func(t *testing.T) {
		t.Parallel()

		var (
			calls      int
			gotAddress string
			gotFix     gps.FixCallback
		)

		//nolint:ireturn // gpsFactory is the seam under test, so it has to hand back the interface.
		factory := func(addr string, onFix gps.FixCallback) gpsWatcher {
			calls++
			gotAddress = addr
			gotFix = onFix

			return stubGPSWatcher{}
		}

		live := &Live{gpsd: gpsdTestAddress, newGPS: factory}
		live.startGPS(context.Background())
		live.group.Wait()

		if calls != 1 {
			t.Errorf("factory called %d times, want 1", calls)
		}

		if gotAddress != gpsdTestAddress {
			t.Errorf("factory address = %q, want %q", gotAddress, gpsdTestAddress)
		}

		if gotFix == nil {
			t.Error("factory callback = nil, want onFix")
		}
	})
}

// TestLiveWatchGPS covers watchGPS's two outcomes: an error from the watcher
// puts one line on stderr, a nil error writes nothing.
func TestLiveWatchGPS(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		err       error
		wantWrite bool
	}{
		{name: "nil error writes nothing", err: nil, wantWrite: false},
		{name: "an error writes one line", err: errGPSWatchFailure, wantWrite: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer

			live := &Live{stderr: &stderr}
			live.watchGPS(context.Background(), stubGPSWatcher{err: testCase.err})

			got := stderr.String() != ""
			if got != testCase.wantWrite {
				t.Errorf("watchGPS() wrote %q, want write=%v", stderr.String(), testCase.wantWrite)
			}
		})
	}
}

// TestLiveOnFix checks that onFix reads the shared location's coordinates
// and stamps them alongside the time it is given.
func TestLiveOnFix(t *testing.T) {
	t.Parallel()

	const wantLat, wantLon = 48.85, 2.35

	live := &Live{loc: location.New(location.WithLatitude(wantLat), location.WithLongitude(wantLon))}
	wantWhen := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	live.onFix(wantWhen)

	gotLat, gotLon, gotWhen := live.fix.snapshot()
	if gotLat != wantLat || gotLon != wantLon || !gotWhen.Equal(wantWhen) {
		t.Errorf("fix.snapshot() after onFix(%v) = (%v, %v, %v), want (%v, %v, %v)",
			wantWhen, gotLat, gotLon, gotWhen, wantLat, wantLon, wantWhen)
	}
}

// TestLiveGPSReceiverLiveFix covers gpsReceiver's live-fix branch: a live fix
// wins outright, reporting the shared location's own coordinates and mode
// rather than anything held from an earlier stamp.
func TestLiveGPSReceiverLiveFix(t *testing.T) {
	t.Parallel()

	const (
		liveLat    = 51.5
		liveLon    = -0.1
		heldLat    = 52.3
		heldLon    = 4.9
		twoDMode   = 2
		threeDMode = 3
	)

	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name     string
		mode     int
		wantMode FixMode
	}{
		{name: "a live 3D fix", mode: threeDMode, wantMode: FixGPS3D},
		{name: "a live 2D fix", mode: twoDMode, wantMode: FixGPS2D},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			loc := location.New(
				location.WithLatitude(liveLat), location.WithLongitude(liveLon), location.WithMode(testCase.mode))
			live := &Live{loc: loc, now: func() time.Time { return now }}
			live.fix.stamp(heldLat, heldLon, now.Add(-time.Second))

			receiver, ok := live.gpsReceiver()
			if !ok {
				t.Fatal("gpsReceiver() ok = false, want true for a live fix")
			}

			wrongState := receiver.Label != LabelGPS || receiver.Mode != testCase.wantMode || !receiver.HasFix
			if wrongState {
				t.Errorf("Receiver = %+v, want Label %q Mode %d HasFix true", receiver, LabelGPS, testCase.wantMode)
			}

			if receiver.Latitude != liveLat || receiver.Longitude != liveLon {
				t.Errorf("Receiver coordinates = (%v, %v), want the live fix (%v, %v)",
					receiver.Latitude, receiver.Longitude, liveLat, liveLon)
			}

			if want := now.Add(-time.Second); !receiver.LastFix.Equal(want) {
				t.Errorf("Receiver.LastFix = %v, want %v", receiver.LastFix, want)
			}
		})
	}
}

// TestLiveGPSReceiverHeldFix covers gpsReceiver's hold window: a lost fix
// keeps reporting the position it was last stamped with, right up to and
// including the gpsHold boundary itself.
func TestLiveGPSReceiverHeldFix(t *testing.T) {
	t.Parallel()

	const heldLat, heldLon = 52.3, 4.9

	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name string
		age  time.Duration
	}{
		{name: "a stamp inside the hold window", age: gpsHold - time.Second},
		{name: "a stamp exactly at the hold boundary", age: gpsHold},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			when := now.Add(-testCase.age)

			live := &Live{loc: location.New(), now: func() time.Time { return now }}
			live.fix.stamp(heldLat, heldLon, when)

			receiver, ok := live.gpsReceiver()
			if !ok {
				t.Fatal("gpsReceiver() ok = false, want true (within the hold window)")
			}

			wrongState := receiver.Label != LabelGPS || receiver.Mode != FixGPSNoFix || receiver.HasFix
			if wrongState {
				t.Errorf("Receiver = %+v, want Label %q Mode %d HasFix false", receiver, LabelGPS, FixGPSNoFix)
			}

			if receiver.Latitude != heldLat || receiver.Longitude != heldLon {
				t.Errorf("Receiver coordinates = (%v, %v), want the held fix (%v, %v)",
					receiver.Latitude, receiver.Longitude, heldLat, heldLon)
			}

			if !receiver.LastFix.Equal(when) {
				t.Errorf("Receiver.LastFix = %v, want %v", receiver.LastFix, when)
			}
		})
	}
}

// TestLiveGPSReceiverDeclines covers gpsReceiver's two ways of saying no: a
// stamp older than gpsHold, and a Live that was never stamped at all. Either
// way the caller is expected to fall back to the self-locate estimate.
func TestLiveGPSReceiverDeclines(t *testing.T) {
	t.Parallel()

	const heldLat, heldLon = 52.3, 4.9

	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	t.Run("a stamp older than the hold", func(t *testing.T) {
		t.Parallel()

		live := &Live{loc: location.New(), now: clock}
		live.fix.stamp(heldLat, heldLon, now.Add(-gpsHold-time.Second))

		if _, ok := live.gpsReceiver(); ok {
			t.Error("gpsReceiver() ok = true, want false (past the hold window)")
		}
	})

	t.Run("never stamped", func(t *testing.T) {
		t.Parallel()

		live := &Live{loc: location.New(), now: clock}

		if _, ok := live.gpsReceiver(); ok {
			t.Error("gpsReceiver() ok = true, want false (no fix has ever been stamped)")
		}
	})
}

// TestLiveReceiverGPSPrecedenceWins covers the two cases where gpsReceiver
// wins the argument with the self-locate estimate outright: a live fix, and
// a fix lost inside the hold window, which must report the position gpsd
// last confirmed rather than the estimate or the zeroes gpsd is still
// streaming into the shared location.
func TestLiveReceiverGPSPrecedenceWins(t *testing.T) {
	t.Parallel()

	const (
		gpsLat     = 51.5
		gpsLon     = -0.1
		heldLat    = 52.3
		heldLon    = 4.9
		staleLat   = 0.0
		staleLon   = 0.0
		threeDMode = 3
		noFixMode  = 1
		insideHold = gpsHold - time.Second
	)

	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	t.Run("a live GPS fix beats an available estimate", func(t *testing.T) {
		t.Parallel()

		live := newTestLive(t, WithClock(clock))
		seedLocatorEstimate(live)
		live.loc.Update(
			location.WithLatitude(gpsLat), location.WithLongitude(gpsLon), location.WithMode(threeDMode))

		receiver := live.receiver()
		if receiver.Label != LabelGPS || receiver.Mode != FixGPS3D {
			t.Errorf("Receiver = %+v, want Label %q Mode %d", receiver, LabelGPS, FixGPS3D)
		}

		if receiver.Latitude != gpsLat || receiver.Longitude != gpsLon {
			t.Errorf("Receiver coordinates = (%v, %v), want the live fix (%v, %v)",
				receiver.Latitude, receiver.Longitude, gpsLat, gpsLon)
		}
	})

	t.Run("a fix lost inside the hold reports the held position", func(t *testing.T) {
		t.Parallel()

		live := newTestLive(t, WithClock(clock))
		seedLocatorEstimate(live)
		live.fix.stamp(heldLat, heldLon, now.Add(-insideHold))
		// gpsd keeps streaming after the fix is lost, and a no-fix TPV report
		// carries zero coordinates: this is what the shared location looks
		// like by the time receiver is asked. The held fix, not this stale
		// pair and not the estimate, is what must come back.
		live.loc.Update(
			location.WithLatitude(staleLat), location.WithLongitude(staleLon), location.WithMode(noFixMode))

		receiver := live.receiver()
		if receiver.Label != LabelGPS || receiver.Mode != FixGPSNoFix {
			t.Errorf("Receiver = %+v, want Label %q Mode %d", receiver, LabelGPS, FixGPSNoFix)
		}

		if receiver.Latitude != heldLat || receiver.Longitude != heldLon {
			t.Errorf("Receiver coordinates = (%v, %v), want the held fix (%v, %v)",
				receiver.Latitude, receiver.Longitude, heldLat, heldLon)
		}
	})
}

// TestLiveReceiverGPSPrecedenceFallsThrough covers the two cases where
// gpsReceiver has nothing to report: a fix lost more than gpsHold ago, and no
// GPS activity at all. Both must leave the self-locate estimate, or the lack
// of one, as the answer.
func TestLiveReceiverGPSPrecedenceFallsThrough(t *testing.T) {
	t.Parallel()

	const (
		heldLat     = 52.3
		heldLon     = 4.9
		staleLat    = 0.0
		staleLon    = 0.0
		noFixMode   = 1
		outsideHold = gpsHold + time.Second
	)

	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	t.Run("a fix lost past the hold window falls through to the estimate", func(t *testing.T) {
		t.Parallel()

		live := newTestLive(t, WithClock(clock))
		seedLocatorEstimate(live)
		live.fix.stamp(heldLat, heldLon, now.Add(-outsideHold))
		live.loc.Update(
			location.WithLatitude(staleLat), location.WithLongitude(staleLon), location.WithMode(noFixMode))

		receiver := live.receiver()
		if receiver.Label != LabelEstimate || receiver.Mode != FixEstimated {
			t.Errorf("Receiver = %+v, want Label %q Mode %d", receiver, LabelEstimate, FixEstimated)
		}
	})

	t.Run("neither GPS nor an estimate leaves the receiver unknown", func(t *testing.T) {
		t.Parallel()

		receiver := newTestLive(t, WithClock(clock)).receiver()
		if receiver.Label != LabelNone || receiver.Mode != FixNone {
			t.Errorf("Receiver = %+v, want Label %q Mode %d", receiver, LabelNone, FixNone)
		}
	})
}

// TestLiveReceiverViolatedCount checks that Violated only ever reaches the
// Receiver through the estimate branch: a manual position and a GPS fix
// both report zero even with a stale non-zero count sitting behind them.
func TestLiveReceiverViolatedCount(t *testing.T) {
	t.Parallel()

	const (
		estimateLat  = 52.5
		estimateLon  = 4.5
		violatedWant = 2
		manualLat    = 51.0
		manualLon    = 3.0
		gpsLat       = 50.0
		gpsLon       = 2.0
		threeDMode   = 3
	)

	t.Run("an estimate with violated circles reports the count", func(t *testing.T) {
		t.Parallel()

		loc := location.New(
			location.WithLatitude(estimateLat), location.WithLongitude(estimateLon),
			location.WithSource(location.SourceInferred))
		live := &Live{loc: loc}
		live.rememberViolated(violatedWant)

		receiver := live.receiver()
		if receiver.Label != LabelEstimate || receiver.Violated != violatedWant {
			t.Errorf("Receiver = %+v, want Label %q Violated %d", receiver, LabelEstimate, violatedWant)
		}
	})

	t.Run("a manual position reports zero regardless of a stale violated count", func(t *testing.T) {
		t.Parallel()

		loc := location.New(location.WithLatitude(manualLat), location.WithLongitude(manualLon))
		live := &Live{manual: true, manualLat: manualLat, manualLon: manualLon, loc: loc}
		live.rememberViolated(violatedWant)

		if got := live.receiver().Violated; got != 0 {
			t.Errorf("Receiver.Violated for a manual position = %d, want 0", got)
		}
	})

	t.Run("a GPS fix reports zero regardless of a stale violated count", func(t *testing.T) {
		t.Parallel()

		loc := location.New(
			location.WithLatitude(gpsLat), location.WithLongitude(gpsLon), location.WithMode(threeDMode))
		live := &Live{loc: loc}
		live.rememberViolated(violatedWant)

		if got := live.receiver().Violated; got != 0 {
			t.Errorf("Receiver.Violated for a GPS fix = %d, want 0", got)
		}
	})
}

// TestLiveCloseWaitsForGPSWatcher checks that Close waits for the gpsd
// watcher's goroutine, the same guarantee it already gives the ingest one.
func TestLiveCloseWaitsForGPSWatcher(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	//nolint:ireturn // gpsFactory is the seam under test, so it has to hand back the interface.
	factory := func(string, gps.FixCallback) gpsWatcher { return blockingGPSWatcher{done: done} }

	live, err := NewLive(WithIngest(stubIngest{}), WithGPSD(gpsdTestAddress), withGPSFactory(factory))
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
		t.Error("GPS watcher goroutine had not signalled exit by the time Close returned")
	}
}
