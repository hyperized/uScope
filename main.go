// Command uScope draws straight into the Linux framebuffer on a ClockworkPi
// uConsole. It owns every pixel rather than every character cell, but it
// still behaves like a console program: one binary, started from tty1,
// keyboard driven, q to get back to the shell.
//
// Slice 1 is the shell around that idea. It opens the framebuffer, works out
// which way the panel is turned, paints a test pattern, and puts the console
// back the way it found it. The radar comes later.
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
		TestPattern: cfg.testPattern,
		PNG:         cfg.pngPath,
		Size:        cfg.size,
	}

	if err := app.Run(ctx, settings, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, explain(err, cfg))

		return exitFailure
	}

	return exitOK
}

// explain turns "no framebuffer on this platform" into advice, because that
// is what someone running uScope on their laptop has just hit.
func explain(err error, cfg config) string {
	if errors.Is(err, fbdev.ErrUnsupported) && cfg.pngPath == "" {
		return "no framebuffer backend on " + runtime.GOOS +
			"; use --png to render the pattern to a file"
	}

	return err.Error()
}
