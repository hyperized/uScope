package source

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hyperized/uAirwaves/pkg/gps"
	"github.com/hyperized/uAirwaves/pkg/location"
)

// gpsHold is how long the last GPS position keeps being reported after gpsd
// stops delivering a fix.
//
// A receiver under a bridge, a gantry or a roof loses lock for a few seconds
// and gets it back. Falling straight through to the self-locate estimate would
// move the scope's centre by tens of nautical miles and then move it back,
// which reads as a fault and loses whatever was being watched at the time.
//
// Thirty seconds is uAirwaves' own TPV watchdog window, so the hold covers
// exactly the gap before its client gives up on a silent socket and
// reconnects. Past that the dropout is not a dropout any more and the estimate
// is the honest answer.
const gpsHold = 30 * time.Second

// gpsWatcher is the part of uAirwaves' *gps.GPS that Live drives.
//
// It is declared here, where it is consumed, so the whole GPS path is tested
// against a fake and nothing in the test path opens a socket. Watch blocks
// until the context is cancelled; with reconnect on it swallows its own dial
// failures and retries behind a backoff.
type gpsWatcher interface {
	Watch(ctx context.Context, loc *location.Location) error
}

// gpsFactory is how Live builds the watcher it starts.
//
// A factory rather than a watcher field, because the fix callback belongs to
// the Live that is being built and cannot be handed to an option before there
// is one. It is also what keeps gps.New off the constructor's path: NewLive
// opens nothing, and neither does building a Live with gpsd configured.
type gpsFactory func(address string, onFix gps.FixCallback) gpsWatcher

// newGPSWatcher is the production factory: uAirwaves' gpsd client, pointed at
// address, reconnecting on its own.
//
// Reconnect is on for the reason the ingest tolerates a missing dongle. gpsd
// on the uConsole is started by systemd alongside uScope and may not have the
// receiver open yet, and a watcher that gave up on the first refused
// connection would leave the scope on the estimate for the rest of the run.
// Repeated failures are held down to one warning by the client's own
// reconnect logger, so a machine with no gpsd says so once.
//
//nolint:ireturn // gpsFactory is the seam, so the factory has to hand back the interface.
func newGPSWatcher(address string, onFix gps.FixCallback) gpsWatcher {
	return gps.New(
		gps.WithGpsAddress(address),
		gps.WithReconnect(true),
		gps.WithFixCallback(onFix),
	)
}

// lastFix is the last position gpsd reported with a fix on it, and when that
// report arrived.
//
// Live keeps its own copy rather than reading the shared location back,
// because the shared location does not hold it for long. gpsd streams TPV at
// 1 Hz whether or not the receiver is locked, a report with no lock carries
// latitude and longitude zero, and uAirwaves' handler writes every report
// through. So by the time anything notices the fix has gone, the position that
// was lost has already been overwritten with the Gulf of Guinea.
//
// It is written on the gpsd goroutine and read on the one that draws, so it
// carries a lock of its own rather than sitting under Live.mu: the draw path
// must not queue behind a self-locate grid search.
type lastFix struct {
	mu                  sync.Mutex
	latitude, longitude float64
	when                time.Time
}

// stamp records a position gpsd delivered with a fix on it.
func (f *lastFix) stamp(latitude, longitude float64, when time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.latitude, f.longitude, f.when = latitude, longitude, when
}

// snapshot reads the last fix back. A zero time means there has never been
// one, which is what an off, absent or still-searching gpsd looks like.
func (f *lastFix) snapshot() (float64, float64, time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.latitude, f.longitude, f.when
}

// startGPS runs the gpsd watcher when there is one to run.
//
// The caller holds mu, the same as the ingest goroutine started beside it, and
// the watcher joins the same wait group so Close waits for both.
func (l *Live) startGPS(ctx context.Context) {
	if l.gpsd == "" {
		return
	}

	watcher := l.newGPS(l.gpsd, l.onFix)

	l.group.Go(func() { l.watchGPS(ctx, watcher) })
}

// watchGPS runs the watcher until the context is cancelled.
//
// A gpsd that is not there does not stop the program, for the reason a dongle
// that is not plugged in does not: the estimate takes over, the header says
// EST, and the reason goes to stderr once. With reconnect on, uAirwaves'
// watcher returns nil on cancellation and nothing else, so the line below is
// reached only by a caller that replaced the watcher.
func (l *Live) watchGPS(ctx context.Context, watcher gpsWatcher) {
	if err := watcher.Watch(ctx, l.loc); err != nil {
		_, _ = fmt.Fprintf(l.stderr, "uScope: gpsd watch stopped: %v\n", err)
	}
}

// onFix stamps the position gpsd has just delivered.
//
// It runs on the gpsd client's own goroutine, which asks for something cheap
// and lock-friendly, so it reads two floats and takes one mutex. The
// coordinates are read back off the shared location rather than handed in
// because gps.FixCallback carries only the time; the client writes the
// position first and calls this after, and the hook only fires for a report
// that carried one.
func (l *Live) onFix(when time.Time) {
	latitude, longitude := l.loc.GetCoordinates()

	l.fix.stamp(latitude, longitude, when)
}

// gpsReceiver answers when gpsd has something to say, and declines when it has
// nothing, which is what lets the self-locate estimate stay exactly as it was.
//
// A live fix wins over everything. When the fix has gone the last one is kept
// for gpsHold and marked FixGPSNoFix, so a few seconds without lock leaves the
// scope where it was instead of throwing it at the estimate and back.
//
// The held coordinates come from the stamp rather than from the shared
// location, because by then the shared location has been written over: see the
// note on lastFix.
func (l *Live) gpsReceiver() (Receiver, bool) {
	heldLat, heldLon, when := l.fix.snapshot()

	if l.loc.HasFix() {
		latitude, longitude := l.loc.GetCoordinates()

		return Receiver{
			Latitude: latitude, Longitude: longitude, HasFix: true,
			Label: LabelGPS, Mode: fixMode(l.loc.Mode()), LastFix: when,
		}, true
	}

	if when.IsZero() || l.now().Sub(when) > gpsHold {
		return Receiver{}, false
	}

	return Receiver{
		Latitude: heldLat, Longitude: heldLon,
		Label: LabelGPS, Mode: FixGPSNoFix, LastFix: when,
	}, true
}
