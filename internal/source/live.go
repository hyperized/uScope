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

	"github.com/hyperized/rtl2832u"
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

	// BiasTeeState is the cached bias-tee bit. uAirwaves keeps it behind a
	// lock next to the controller and refreshes it when the receiver opens
	// and on every successful write, so reading it costs no USB transfer.
	// That is the whole reason it is here rather than BiasTeeEnabled, which
	// does one control transfer per call: Frame runs once per drawn frame.
	BiasTeeState() (supported, enabled bool)

	// SetBiasTee drives the GPIO. It is the only method here that talks to
	// the device on demand, which is why Live never calls it from Frame.
	SetBiasTee(enable bool) error

	// Sweeping reports whether the gain auto-sweep is running.
	Sweeping() bool
}

// dongleOpener is how the bias-tee receiver factory opens the radio.
//
// Its default is rtl2832u.Open itself rather than a wrapper around it, so
// there is no adapter carrying a success branch no machine without a dongle
// could ever run. It names the concrete receiver for the same reason: the
// factory widens it to adsb.Receiver after the error check, which is the one
// order that cannot hand the ingest a non-nil interface holding a nil pointer.
type dongleOpener func(opts ...rtl2832u.Option) (*rtl2832u.Receiver, error)

// Live is the real thing: aircraft decoded from radio, a BEAST feed or a
// replayed capture.
//
// Live is safe for concurrent use. Everything it reads from uAirwaves is
// already locked there, and its own mutable state sits behind mu.
//
// The Ghosts in a Frame alias the tracker's own slice and are only valid until
// the next Frame call, the same caveat Demo carries about its Planes. Frame is
// called once per drawn frame from the run loop, so there is one reader; two
// goroutines drawing from one Live would need a copy each, and nothing here
// makes one.
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

	// gpsd is where the gpsd daemon listens, or the empty string for a run
	// with no GPS to watch. newGPS builds the watcher Start runs, and fix
	// holds the last position that watcher reported with a lock on it.
	gpsd   string
	newGPS gpsFactory
	fix    lastFix

	// biasTee powers an LNA over the coax from the moment the dongle opens,
	// and autoSweep walks the gain grid before the first frame. Both only
	// mean something on the local-SDR path: a BEAST feed's gain belongs to
	// whoever runs the remote demodulator, and a capture has no gain at all.
	//
	// Order matters between the two. The bias-tee goes high at chip-config
	// time, inside Open, so the LNA is already powered when the sweep starts
	// measuring. A sweep run against an unpowered LNA picks the wrong gain
	// cell and the receiver then sits there deaf, which is the failure this
	// pairing exists to avoid.
	biasTee   bool
	autoSweep bool

	// openDongle is the seam the bias-tee factory opens the radio through.
	openDongle dongleOpener

	now              func() time.Time
	stderr           io.Writer
	estimateInterval time.Duration

	mu           sync.Mutex
	cancel       context.CancelFunc
	group        sync.WaitGroup
	lastEstimate time.Time

	// violated is how many of the self-locator's horizon circles missed the
	// estimate it last handed over. The header turns anything above zero into
	// a question mark after the radius, because a widened radius on its own
	// reads as "far away but sound" rather than as "these circles disagree".
	violated int

	// ghosts keeps the trail of an aircraft the store has pruned, so a lost
	// contact leaves its track behind. It sits under mu with the rest of the
	// mutable state, and it runs on every session: see the note on the type
	// for why it is not something to be switched on when it is wanted.
	ghosts ghosts

	// coverage accumulates where the antenna has heard an aircraft, which is
	// what the 3D view's measured envelope is drawn from. It has a lock of its
	// own rather than sitting under mu, because it is written on the ingest
	// goroutine and read on the one that draws, and mu is already held across
	// work neither of those should wait for.
	coverage *coverageCache
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

// WithGPSD watches a gpsd daemon at address for the receiver's own position.
//
// The empty string, which is the default, watches nothing and leaves the
// self-locate estimate to do the work on its own. A position from
// WithManualLocation turns this off: see NewLive.
//
// The daemon is not contacted here. Start launches the watcher, and it
// reconnects on its own, so a gpsd that is not up yet is not a failure.
func WithGPSD(address string) LiveOption {
	return func(l *Live) { l.gpsd = address }
}

// withGPSFactory replaces how the gpsd watcher is built.
//
// Unexported on purpose, the same way withDongleOpener is: it is a test seam
// and not API. A nil factory leaves the real gpsd client in place.
func withGPSFactory(build gpsFactory) LiveOption {
	return func(l *Live) {
		if build != nil {
			l.newGPS = build
		}
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

// WithBiasTee powers an LNA over the coax, from the moment the dongle opens.
//
// It is ignored by every source but the local SDR, which is what uAirwaves
// does with the same flag: a BEAST feed's gain stage is somebody else's and a
// replayed capture has none. The flag layer says so on stderr rather than
// leaving the operator to wonder why nothing lit up.
//
// Powering at open rather than after it is deliberate. rtl2832u pulls the bias
// pin high during chip config, so by the time WithAutoSweep starts measuring
// the LNA is already running, and the sweep measures the chain that will
// actually be receiving.
func WithBiasTee(on bool) LiveOption {
	return func(l *Live) { l.biasTee = on }
}

// WithAutoSweep walks the gain grid once before the first frame and keeps the
// cell that decoded best.
//
// Local SDR only, for the same reason WithBiasTee is. It costs a few seconds
// of silence at startup, which is why the header says SWEEP while it runs: an
// empty scope with no explanation reads as a broken receiver.
func WithAutoSweep(on bool) LiveOption {
	return func(l *Live) { l.autoSweep = on }
}

// withDongleOpener replaces how the bias-tee factory opens the radio.
//
// Unexported on purpose, the same way the ingest interface is: it is a test
// seam and not API. A nil opener leaves rtl2832u.Open in place.
func withDongleOpener(open dongleOpener) LiveOption {
	return func(l *Live) {
		if open != nil {
			l.openDongle = open
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
		ghosts:           newGhosts(),
		now:              time.Now,
		stderr:           os.Stderr,
		estimateInterval: defaultEstimateInterval,
		coverage:         newCoverage(),
		openDongle:       rtl2832u.Open,
		newGPS:           newGPSWatcher,
	}

	for _, opt := range opts {
		opt(live)
	}

	if err := live.validate(); err != nil {
		return nil, err
	}

	live.loc = live.newLocation()

	// Without a position from the operator there is nothing to centre on, so
	// the self-locator earns its keep. With one, both ways of working a
	// position out are off rather than being further opinions to reconcile:
	// the locator would only ever disagree, and a gpsd watcher would write its
	// own fix over the operator's in the shared location, which is what the
	// decoder resolves CPR frames against.
	if live.manual {
		live.gpsd = ""
	} else {
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
	l.startGPS(streamCtx)
}

// Frame takes one snapshot of the whole ingest.
func (l *Live) Frame() Frame {
	receiver := l.receiver()
	planes := l.planes.Sorted(receiver.Latitude, receiver.Longitude, airplanes.WithTrails())
	now := l.now()

	supported, enabled := l.in.BiasTeeState()
	snapshot, grid := l.coverage.snapshot(now)

	return Frame{
		Planes:   planes,
		Ghosts:   l.observe(planes),
		Receiver: receiver,
		Source:   l.in.Source(),
		Stats:    l.in.Stats(),
		Now:      now,
		BiasTee:  BiasTeeState{Supported: supported, Enabled: enabled},
		Sweeping: l.in.Sweeping(),
		Coverage: snapshot,
		Grid:     grid,
	}
}

// BiasTee reports whether this receiver can power an LNA and whether it is.
//
// It reads uAirwaves' cache rather than the chip, so it is safe to call from
// the goroutine that draws. The cache is seeded when the receiver opens and
// rewritten on every successful SetBiasTee; a flip made outside this process,
// with rtl_biast say, is not seen until one of those happens again.
//
//nolint:nonamedreturns // (supported, enabled) reads clearer named at this signature.
func (l *Live) BiasTee() (supported, enabled bool) { return l.in.BiasTeeState() }

// SetBiasTee flips the LNA power.
//
// This is a USB control transfer and may block for as long as the dongle
// takes to answer, so it belongs on a worker goroutine. internal/app is what
// puts it there; nothing on the draw path calls this.
func (l *Live) SetBiasTee(enable bool) error {
	if err := l.in.SetBiasTee(enable); err != nil {
		return fmt.Errorf("source: setting the bias-tee: %w", err)
	}

	return nil
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

// observe folds the frame's aircraft into the ghost ring.
//
// It takes mu in a section of its own rather than holding it across the whole
// of Frame, because receiver above reaches applyEstimate, which takes the
// same lock on its own way past.
func (l *Live) observe(planes airplanes.List) []Trail {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.ghosts.observe(planes)
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
//
// The position observer is always installed now, where it used to go on only
// when there was a self-locator to feed. The coverage tracker wants every fix
// whether or not the operator typed a position in: with --lat and --lon there
// is nothing to locate and still an antenna pattern to measure.
func (l *Live) adsbOptions() []adsb.Option {
	opts := []adsb.Option{adsb.WithLocation(l.loc), adsb.WithPositionObserver(l.positionObserver())}

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
		return l.sdrOptions(append(opts, adsb.WithSourceLabel("SDR")))
	}
}

// sdrOptions adds the two settings that only mean something when uScope is
// driving the radio itself.
//
// It is split out of adsbOptions rather than inlined into its default branch
// because the switch above is about which source was chosen and this is about
// how one of them is opened. Both flags are off unless asked for, so the
// ordinary run appends nothing here.
func (l *Live) sdrOptions(opts []adsb.Option) []adsb.Option {
	if l.biasTee {
		opts = append(opts, adsb.WithReceiverFactory(biasTeeFactory(l.openDongle)))
	}

	if l.autoSweep {
		opts = append(opts, adsb.WithAutoSweep())
	}

	return opts
}

// biasTeeFactory opens the dongle with the bias pin already high.
//
// uAirwaves' own biasTeeReceiverFactory does exactly this. The factory is
// called once per Stream call rather than once here, because Stream owns the
// receiver's lifetime and closes it when it returns, so a reconnect after an
// unplug powers the LNA again on its own.
func biasTeeFactory(open dongleOpener) adsb.ReceiverFactory {
	return func() (adsb.Receiver, error) {
		rcv, err := open(rtl2832u.WithBiasTee(true))
		if err != nil {
			return nil, fmt.Errorf("source: opening the SDR with the bias-tee on: %w", err)
		}

		return rcv, nil
	}
}

// positionObserver fans one decoded fix out to the self-locator and the
// coverage tracker, which is the same pair uAirwaves' own main.go feeds.
//
// The locator is nil whenever the operator gave a position, because then there
// is nothing to work out. The tracker is never nil and reads the receiver
// position back off the shared location each time rather than closing over it:
// a self-locate estimate lands there part way through a run, and the fixes
// binned after it have to be measured from the position that was actually
// known when they arrived.
//
// It runs on the ingest goroutine, so both halves have to stay cheap and
// allocation-free.
func (l *Live) positionObserver() adsb.PositionObserver {
	return func(latitude, longitude, altitudeFt float64) {
		if l.locator != nil {
			l.locator.Observe(latitude, longitude, altitudeFt)
		}

		receiverLat, receiverLon := l.loc.GetCoordinates()
		l.coverage.observe(receiverLat, receiverLon, latitude, longitude, altitudeFt)
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

	// gpsd first, and only then the estimate. The order is also what keeps the
	// two out of each other's way in the shared location: applyEstimate writes
	// the estimate's coordinates there, so it must not run while a GPS fix, or
	// a fix inside its hold window, is the thing being reported.
	if fix, ok := l.gpsReceiver(); ok {
		return fix
	}

	l.applyEstimate()

	if l.loc.Source() != location.SourceInferred {
		return Receiver{Label: LabelNone, Mode: FixNone}
	}

	latitude, longitude := l.loc.GetCoordinates()

	return Receiver{
		Latitude:     latitude,
		Longitude:    longitude,
		ConfidenceNm: l.loc.ConfidenceRadiusNm(),
		Label:        LabelEstimate,
		Mode:         FixEstimated,
		Violated:     l.violatedCount(),
	}
}

// violatedCount is how many of the self-locator's own observations disagreed
// with the estimate it last handed over.
func (l *Live) violatedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.violated
}

// rememberViolated keeps the disagreement count off the estimate just applied.
//
// It takes mu in a section of its own rather than being folded into
// estimateDue, which holds the same lock on its own way past.
func (l *Live) rememberViolated(count int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.violated = count
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
// uAirwaves runs this on a ticker next to its GPS watcher, and the tick
// decides which of the two wins. uScope pulls the estimate on the frame that
// is about to be drawn instead, which keeps the decision on one goroutine and
// the whole path testable with a clock. receiver is what defers to the GPS: it
// only gets this far with no fix and no held one, so the estimate never writes
// over a GPS position in the shared location.
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

	l.rememberViolated(fix.Violated)

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
