package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/location"
	"github.com/hyperized/uAirwaves/pkg/selflocate"
)

// Coordinate limits. The flag layer checks these too; they are checked again
// here because NewLive is a library entry point and a caller that skipped the
// flags would otherwise centre the scope on a coordinate that does not exist.
const (
	minLatitude, maxLatitude   = -90.0, 90.0
	minLongitude, maxLongitude = -180.0, 180.0
)

// defaultEstimateInterval is how often the self-locator is asked for an
// answer. Estimate runs a grid search over every observation it holds, which
// is far too much work to do at the frame rate, and the answer moves slowly
// anyway: it only changes when aircraft at new bearings come into range.
// uAirwaves ticks the same worker at 15 seconds.
const defaultEstimateInterval = 15 * time.Second

// ErrCoordinate is returned by NewLive for a latitude or longitude outside
// the range that exists.
var ErrCoordinate = errors.New("source: coordinate out of range")

// ingest is the part of *adsb.ADSB that Live uses.
//
// It is declared here, where it is consumed, so the whole of Live is tested
// against a fake and nothing in the test path goes near a radio or a socket.
type ingest interface {
	Stream(ctx context.Context, planes *airplanes.Airplanes) error
	Source() adsb.SourceInfo
	Stats() adsb.Stats
}

// Live is the real thing: aircraft decoded from radio, a BEAST feed or a
// replayed capture.
//
// Live is safe for concurrent use. Everything it reads from uAirwaves is
// already locked there, and its own mutable state sits behind mu.
type Live struct {
	in      ingest
	planes  *airplanes.Airplanes
	loc     *location.Location
	locator *selflocate.Locator

	// The source choice, collected by the options and read once by NewLive.
	// Replay wins over beast, matching uAirwaves, so a developer replaying a
	// capture on a host that also has a feed configured gets the capture.
	beast  string
	replay string

	manualLat, manualLon float64
	manual               bool

	now              func() time.Time
	stderr           io.Writer
	estimateInterval time.Duration

	mu           sync.Mutex
	cancel       context.CancelFunc
	group        sync.WaitGroup
	lastEstimate time.Time
}

// LiveOption configures a Live at construction.
//
// Live and Demo have an option type each rather than sharing one, because a
// beast address means nothing to the demo fleet and a seed means nothing to a
// radio. Two types make that a compile error instead of a surprise.
type LiveOption func(*Live)

// WithBeast consumes Mode S frames from a remote demodulator over TCP instead
// of driving a local radio.
func WithBeast(address string) LiveOption {
	return func(l *Live) { l.beast = address }
}

// WithReplay plays a captured IQ file through the demodulator, which is how
// the decode path is exercised on a machine with no receiver.
func WithReplay(path string) LiveOption {
	return func(l *Live) { l.replay = path }
}

// WithManualLocation pins the receiver position instead of working it out.
//
// A known position also makes the first decode faster: with a reference
// nearby, a single CPR frame resolves to a position, where without one the
// decoder waits for the matching half of the pair.
func WithManualLocation(latitude, longitude float64) LiveOption {
	return func(l *Live) {
		l.manualLat, l.manualLon = latitude, longitude
		l.manual = true
	}
}

// WithClock replaces the frame timestamp and the self-locate throttle. A nil
// function leaves time.Now in place.
func WithClock(now func() time.Time) LiveOption {
	return func(l *Live) {
		if now != nil {
			l.now = now
		}
	}
}

// WithStderr replaces where an ingest failure is reported. A terminal backend
// owns stdout, so this defaults to os.Stderr and not to stdout.
func WithStderr(dst io.Writer) LiveOption {
	return func(l *Live) {
		if dst != nil {
			l.stderr = dst
		}
	}
}

// WithIngest replaces the ADS-B ingest, which is the seam the tests use. The
// interface is unexported, so a caller outside this package can only satisfy
// it, never name it, which is what keeps it a test seam rather than API.
func WithIngest(in ingest) LiveOption {
	return func(l *Live) {
		if in != nil {
			l.in = in
		}
	}
}

// WithEstimateInterval replaces how often the self-locator is consulted. A
// value of zero or less leaves the default alone.
func WithEstimateInterval(interval time.Duration) LiveOption {
	return func(l *Live) {
		if interval > 0 {
			l.estimateInterval = interval
		}
	}
}

// NewLive builds the live source. It opens nothing: the radio, the socket or
// the file is opened by Start, on the goroutine that runs the ingest.
func NewLive(opts ...LiveOption) (*Live, error) {
	live := &Live{
		planes:           airplanes.New(),
		now:              time.Now,
		stderr:           os.Stderr,
		estimateInterval: defaultEstimateInterval,
	}

	for _, opt := range opts {
		opt(live)
	}

	if err := live.validate(); err != nil {
		return nil, err
	}

	live.loc = live.newLocation()

	// Without a position from the operator there is nothing to centre on, so
	// the self-locator earns its keep. With one, it would only ever disagree.
	if !live.manual {
		live.locator = selflocate.New()
	}

	if live.in == nil {
		live.in = adsb.New(live.adsbOptions()...)
	}

	return live, nil
}

// Start runs the ingest on its own goroutine and returns straight away.
//
// A failing ingest does not stop the program. A dongle that is not plugged in,
// or a BEAST host that is down, should leave an empty scope on screen with the
// source marked disconnected, which is more use than a binary that refuses to
// start. The reason goes to stderr once.
//
// Calling Start twice is a no-op: the second call sees a stream already
// running and leaves it alone.
func (l *Live) Start(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.cancel != nil {
		return
	}

	streamCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	l.group.Go(func() { l.stream(streamCtx) })
}

// Frame takes one snapshot of the whole ingest.
func (l *Live) Frame() Frame {
	receiver := l.receiver()

	return Frame{
		Planes:   l.planes.Sorted(receiver.Latitude, receiver.Longitude, airplanes.WithTrails()),
		Receiver: receiver,
		Source:   l.in.Source(),
		Stats:    l.in.Stats(),
		Now:      l.now(),
	}
}

// Close stops the ingest and waits for its goroutine.
//
// Waiting matters: the goroutine holds the radio or the socket, and returning
// before it has let go would leave the device busy for whatever starts next.
func (l *Live) Close() error {
	l.mu.Lock()
	cancel := l.cancel
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	l.group.Wait()

	return nil
}

// validate range-checks what the options collected.
func (l *Live) validate() error {
	if !l.manual {
		return nil
	}

	if l.manualLat < minLatitude || l.manualLat > maxLatitude {
		return fmt.Errorf("%w: latitude %g is outside %g to %g",
			ErrCoordinate, l.manualLat, minLatitude, maxLatitude)
	}

	if l.manualLon < minLongitude || l.manualLon > maxLongitude {
		return fmt.Errorf("%w: longitude %g is outside %g to %g",
			ErrCoordinate, l.manualLon, minLongitude, maxLongitude)
	}

	return nil
}

// newLocation builds the shared receiver position. The decoder holds the same
// pointer, so a self-locate estimate written here also sharpens its CPR
// decoding on the next frame.
func (l *Live) newLocation() *location.Location {
	if !l.manual {
		return location.New()
	}

	return location.New(
		location.WithLatitude(l.manualLat),
		location.WithLongitude(l.manualLon),
	)
}

// adsbOptions turns the source choice into uAirwaves' option list.
func (l *Live) adsbOptions() []adsb.Option {
	opts := []adsb.Option{adsb.WithLocation(l.loc)}

	if l.locator != nil {
		opts = append(opts, adsb.WithPositionObserver(observer(l.locator)))
	}

	switch {
	case l.replay != "":
		return append(opts,
			adsb.WithReceiverFactory(replayFactory(l.replay)),
			adsb.WithSourceLabel("REPLAY "+filepath.Base(l.replay)))
	case l.beast != "":
		return append(opts,
			adsb.WithBeastAddress(l.beast),
			adsb.WithSourceLabel("BEAST "+l.beast))
	default:
		return append(opts, adsb.WithSourceLabel("SDR"))
	}
}

// observer feeds every decoded aircraft position to the self-locator.
//
// uAirwaves fans the same hook out to its coverage tracker as well. uScope has
// no coverage panel, so there is one consumer and no fan-out.
func observer(locator *selflocate.Locator) adsb.PositionObserver {
	return func(latitude, longitude, altitudeFt float64) {
		locator.Observe(latitude, longitude, altitudeFt)
	}
}

// replayFactory opens the capture when the ingest asks for it.
//
// The file is opened per Stream call rather than once here, because Stream
// owns the receiver's lifetime and closes it when it returns.
func replayFactory(path string) adsb.ReceiverFactory {
	return func() (adsb.Receiver, error) {
		rcv, err := adsb.NewFileReceiver(path)
		if err != nil {
			return nil, fmt.Errorf("source: opening replay %s: %w", path, err)
		}

		return rcv, nil
	}
}

// stream is the goroutine body, split out so the error path is testable
// without a goroutine in the test.
func (l *Live) stream(ctx context.Context) {
	err := l.in.Stream(ctx, l.planes)
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}

	_, _ = fmt.Fprintf(l.stderr, "uScope: ADS-B ingest stopped: %v\n", err)
}

// receiver reports where the scope is centred and how that was arrived at.
func (l *Live) receiver() Receiver {
	if l.manual {
		latitude, longitude := l.loc.GetCoordinates()

		return Receiver{
			Latitude: latitude, Longitude: longitude, HasFix: true,
			Label: LabelManual, Mode: FixManual,
		}
	}

	l.applyEstimate()

	latitude, longitude := l.loc.GetCoordinates()

	switch {
	case l.loc.HasFix():
		return Receiver{
			Latitude: latitude, Longitude: longitude, HasFix: true,
			Label: LabelGPS, Mode: fixMode(l.loc.Mode()),
		}
	case l.loc.Source() == location.SourceInferred:
		return Receiver{
			Latitude:     latitude,
			Longitude:    longitude,
			ConfidenceNm: l.loc.ConfidenceRadiusNm(),
			Label:        LabelEstimate,
			Mode:         FixEstimated,
		}
	default:
		return Receiver{Label: LabelNone, Mode: FixNone}
	}
}

// gpsMode2D is uAirwaves' description of a fix without altitude. Its other
// spelling is "3D fix", which is not named here because everything that is not
// this one lands on the same branch.
const gpsMode2D = "2D fix"

// fixMode reads uAirwaves' fix description.
//
// It is only ever called for a location that reports a fix, so anything that
// is not the 2D spelling counts as a full one. A receiver that has a fix and
// cannot describe it still has a fix, and calling that no-fix would have the
// home marker say something worse than the truth.
func fixMode(mode string) FixMode {
	if mode == gpsMode2D {
		return FixGPS2D
	}

	return FixGPS3D
}

// applyEstimate folds the self-locator's answer into the shared location once
// it has one.
//
// uAirwaves runs this on a ticker next to a GPS watcher, and the tick decides
// which of the two wins. uScope has no GPS, so there is nothing to defer to
// and no reason for a second goroutine: pulling the estimate on the frame
// that is about to be drawn keeps the source on one goroutine and makes the
// whole path testable with a clock.
func (l *Live) applyEstimate() {
	if l.locator == nil {
		return
	}

	if !l.estimateDue() {
		return
	}

	fix, ok := l.locator.Estimate()
	if !ok {
		return
	}

	l.loc.Update(
		location.WithSource(location.SourceInferred),
		location.WithConfidenceRadiusNm(fix.ConfidenceRadiusNm),
		location.WithLatitude(fix.Latitude),
		location.WithLongitude(fix.Longitude),
	)
}

// estimateDue reports whether enough time has passed to ask again, and
// records the attempt when it has.
func (l *Live) estimateDue() bool {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.lastEstimate.IsZero() && now.Sub(l.lastEstimate) < l.estimateInterval {
		return false
	}

	l.lastEstimate = now

	return true
}
