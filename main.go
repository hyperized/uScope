// Command uScope draws straight into the Linux framebuffer on a ClockworkPi
// uConsole. It owns every pixel rather than every character cell, but it
// still behaves like a console program: one binary, started from tty1,
// keyboard driven, q to get back to the shell.
//
// It also draws in a terminal, over ssh or on a laptop, with the same canvas
// code: Kitty graphics where the terminal has them, half-block characters
// where it does not. There is still no radar in it, only a test pattern.
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
	}

	// Warnings go to stderr because a terminal backend is busy writing
	// frames to stdout, and a warning in the middle of a frame is a mess on
	// screen and an unparseable stream in a pipe.
	if err := app.Run(ctx, settings, stdout, app.WithStderr(stderr)); err != nil {
		_, _ = fmt.Fprintln(stderr, explain(err, cfg))

		return exitFailure
	}

	return exitOK
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
