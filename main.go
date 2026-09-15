// Command uScope draws straight into the Linux framebuffer on a ClockworkPi
// uConsole. It owns every pixel rather than every character cell, but it
// still behaves like a console program: one binary, started from tty1,
// keyboard driven, q to get back to the shell.
//
// It also draws in a terminal, over ssh or on a laptop, with the same canvas
// code: Kitty graphics where the terminal has them, half-block characters
// where it does not.
//
// The radar is the default scene. Where its aircraft come from is settled
// here, from the flags: a captured IQ file, a BEAST feed, the invented demo
// fleet, or the radio on the uConsole. --scene picks which scene starts, and
// in the live loop s steps between the radar, the orientation pattern and the
// type specimen.
//
// Exit status is 0 on a clean quit and 1 on any failure, with the reason on
// stderr.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/hyperized/uAirwaves/pkg/battery"
	"github.com/hyperized/uScope/internal/app"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/fbdev"
)

const (
	exitOK      = 0
	exitFailure = 1

	// unknownCharge is what the battery status starts at, so the header draws
	// no indicator until a reading has actually arrived. Zero would mean a
	// flat battery, which is a different claim.
	unknownCharge = -1

	// batteryInterval is how often the power supply is read. It is the same
	// figure uAirwaves polls at: a battery percentage moves a point every few
	// minutes, and a tighter loop would only wake the CPU for nothing.
	batteryInterval = 30 * time.Second

	// batteryPoll is how often startBattery looks to see whether the first
	// reading has landed yet.
	batteryPoll = 5 * time.Millisecond
)

// batterySettle is how long the first frame waits for the first battery
// reading.
//
// Without it a one-shot render is a race between the picture and the poller,
// and --test-pattern would usually draw a header with no indicator on a laptop
// that plainly has a battery. A machine with no battery does not pay it: the
// watcher returns ErrUnsupported straight away and the wait ends with it.
//
//nolint:gochecknoglobals // test seam; production always holds 300ms.
var batterySettle = 300 * time.Millisecond

// osExit is a seam so a test can call main without ending the test binary.
//
//nolint:gochecknoglobals // test seam; production always holds os.Exit.
var osExit = os.Exit

// newSource is a seam for the same reason.
//
// run's "the source would not build" branch is otherwise unreachable: the
// flag layer range-checks --lat and --lon before sourceFor ever sees them, so
// nothing a user can type gets that far. The branch still has to exist,
// because internal/source validates its own input and may grow a failure mode
// the flags know nothing about.
//
//nolint:gochecknoglobals // test seam; production always holds sourceFor.
var newSource = sourceFor

// watchBattery is a seam for the same reason.
//
// The real one shells out to pmset on macOS and reads /sys on Linux. A unit
// test must do neither, and a test binary that polled the developer's laptop
// battery would be slow as well as wrong.
//
//nolint:gochecknoglobals // test seam; production always holds pollBattery.
var watchBattery = pollBattery

func main() {
	osExit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with the process boundary handed in, which is what makes it
// testable.
//
// SIGINT and SIGTERM cancel the context rather than killing the process, so
// the deferred restores in the run loop still get to put the console back.
// A second signal falls through to the default handler and kills it outright,
// which is the escape hatch if a restore ever wedges.
func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)

		return exitFailure
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	src, err := newSource(cfg, runtime.GOOS, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)

		return exitFailure
	}

	// Closed after the loop rather than by the loop: main owns the source
	// because main built it, and the deferred close waits for the ingest
	// goroutine so the radio is let go before the process exits.
	defer func() { _ = src.Close() }()

	start(ctx, src)

	// The poller gets a context of its own so it stops when run returns, not
	// only when a signal arrives. Without that the deferred wait below would
	// sit on a goroutine that is still politely polling a battery nobody is
	// going to look at again. The two defers unwind in the order that needs:
	// cancel, then wait.
	batteryCtx, stopBattery := context.WithCancel(ctx)
	status, waitBattery := startBattery(batteryCtx, cfg, stderr)

	defer waitBattery()
	defer stopBattery()

	settings := app.Config{
		FBPath:      cfg.fbPath,
		Rotation:    cfg.rotation,
		AutoRotate:  cfg.autoRotate,
		FPS:         cfg.fps,
		Frames:      cfg.frames,
		TestPattern: cfg.testPattern,
		PNG:         cfg.pngPath,
		Size:        cfg.size,
		Backend:     cfg.backend,
		Scene:       cfg.scene,
		Theme:       cfg.theme,
		Radar:       radar.Settings{Colour: cfg.colour, Airports: cfg.airports},
	}

	// Warnings go to stderr because a terminal backend is busy writing
	// frames to stdout, and a warning in the middle of a frame is a mess on
	// screen and an unparseable stream in a pipe.
	if err := app.Run(ctx, settings, stdout,
		app.WithStderr(stderr), app.WithSource(src), app.WithBattery(status)); err != nil {
		_, _ = fmt.Fprintln(stderr, explain(err, cfg))

		return exitFailure
	}

	return exitOK
}

// startBattery begins polling the machine's power supply and hands back the
// status the header reads, plus the function that waits for the poller.
//
// A machine with no battery is the ordinary case on a desktop, so a failure
// here is a line on stderr and nothing more: a radar that refused to start
// because it could not read a battery would be a worse program than one that
// draws the scope without an indicator.
//
// In practice nothing reaches that line today. uAirwaves' watcher logs its own
// failures and returns nil, including the unsupported-platform case, so the
// indicator simply never appears. The branch stays because the watcher's
// signature says it can fail and a later version may start saying so.
func startBattery(ctx context.Context, cfg config, stderr io.Writer) (*battery.Status, func()) {
	status := battery.NewStatus(battery.WithPercentage(unknownCharge))
	done := make(chan struct{})

	var group sync.WaitGroup

	group.Go(func() {
		defer close(done)

		if err := watchBattery(ctx, status, cfg.battery); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: battery unavailable: %v\n", appName, err)
		}
	})

	awaitCharge(status, done)

	return status, group.Wait
}

// awaitCharge waits for the first reading, for the watcher to give up, or for
// the settle window to run out, whichever comes first.
//
// It polls rather than taking a signal from the watcher because the watcher is
// uAirwaves' and offers none. The window is short enough not to be felt at
// startup and long enough for a pmset call or a read of /sys to finish.
func awaitCharge(status *battery.Status, done <-chan struct{}) {
	deadline := time.After(batterySettle)

	for {
		if status.GetPercentage() >= 0 {
			return
		}

		select {
		case <-done:
			return
		case <-deadline:
			return
		case <-time.After(batteryPoll):
		}
	}
}

// pollBattery is the production watcher. The override is only consulted by the
// Linux reader; macOS takes its figures from pmset and ignores it.
//
// The error comes back unwrapped, which is the one place in this program that
// happens. uAirwaves' watcher swallows its own failures by design: an
// unsupported platform and a transient read both end in a log line and a nil
// return, so the only value that can arrive here is nil. Wrapping it would add
// context to something that never happens and two statements no test can
// reach, and the caller's own message already names what failed.
func pollBattery(ctx context.Context, status *battery.Status, path string) error {
	if path != "" {
		//nolint:wrapcheck // see above: the watcher returns nil or nothing at all.
		return battery.WatchWithInterval(ctx, status, batteryInterval, path)
	}

	//nolint:wrapcheck // see above: the watcher returns nil or nothing at all.
	return battery.Watch(ctx, status)
}

// starter is the half of a source that has a goroutine behind it. Demo has
// none, so it does not implement this and nothing has to be started.
type starter interface {
	Start(ctx context.Context)
}

// start runs the ingest if the source has one.
func start(ctx context.Context, src source.Source) {
	if live, ok := src.(starter); ok {
		live.Start(ctx)
	}
}

// sourceFor builds the aircraft source the flags asked for.
//
// With nothing asked for, the answer depends on the machine. Linux is where
// the radio driver works, so that is the local SDR. Anywhere else there is no
// receiver to open, and refusing to start would be a poor answer on the laptop
// the layout is developed on, so the demo fleet takes over and says so once on
// stderr.
//
//nolint:ireturn // the whole point is that the caller cannot tell which one it got.
func sourceFor(cfg config, goos string, stderr io.Writer) (source.Source, error) {
	switch cfg.source {
	case sourceReplay:
		return live(cfg, source.WithReplay(cfg.replay))
	case sourceBeast:
		return live(cfg, source.WithBeast(cfg.beast))
	case sourceDemo:
		return demo(cfg)
	case sourceAuto:
		fallthrough
	default:
		if goos == linuxGOOS {
			return live(cfg)
		}

		_, _ = fmt.Fprintf(stderr,
			"%s: no receiver on %s and no --beast or --replay-iq given; flying the demo fleet\n",
			appName, goos)

		return demo(cfg)
	}
}

// live builds the real ingest, adding the operator's position when there is
// one.
//
//nolint:ireturn // every branch of sourceFor returns the interface.
func live(cfg config, opts ...source.LiveOption) (source.Source, error) {
	if cfg.hasLocation {
		opts = append(opts, source.WithManualLocation(cfg.latitude, cfg.longitude))
	}

	src, err := source.NewLive(opts...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", appName, err)
	}

	return src, nil
}

// demo builds the invented fleet.
//
//nolint:ireturn // every branch of sourceFor returns the interface.
func demo(cfg config) (source.Source, error) {
	var opts []source.DemoOption
	if cfg.hasLocation {
		opts = append(opts, source.WithDemoLocation(cfg.latitude, cfg.longitude))
	}

	src, err := source.NewDemo(opts...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", appName, err)
	}

	return src, nil
}

// explain turns "no framebuffer on this platform" into advice, because that
// is what someone running uScope on their laptop has just hit.
func explain(err error, cfg config) string {
	if errors.Is(err, fbdev.ErrUnsupported) && cfg.pngPath == "" {
		return "no framebuffer on " + runtime.GOOS +
			"; use --backend blocks or --backend kitty to draw in the terminal, or --png for a file"
	}

	return err.Error()
}
