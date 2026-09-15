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
	"syscall"

	"github.com/hyperized/uScope/internal/app"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/fbdev"
)

const (
	exitOK      = 0
	exitFailure = 1
)

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
	}

	// Warnings go to stderr because a terminal backend is busy writing
	// frames to stdout, and a warning in the middle of a frame is a mess on
	// screen and an unparseable stream in a pipe.
	if err := app.Run(ctx, settings, stdout, app.WithStderr(stderr), app.WithSource(src)); err != nil {
		_, _ = fmt.Fprintln(stderr, explain(err, cfg))

		return exitFailure
	}

	return exitOK
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
