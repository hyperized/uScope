// Package app is the run loop and the wiring around it.
//
// Three modes share one entry point. --png renders a still frame to a file
// and never touches a device, which is how the scene gets checked on a
// laptop. --test-pattern paints one frame on the framebuffer and exits
// without changing console or terminal state, so it is safe over ssh and the
// frame stays up until something else repaints. Everything else is the live
// loop.
//
// Every device this package talks to arrives through a function field, so
// the whole of Run is exercised on a Mac with fakes.
package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"sync"
	"time"

	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/pattern"
	"github.com/hyperized/uScope/internal/term"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/fbdev"
	"github.com/hyperized/uScope/pkg/rotate"
	"github.com/hyperized/uScope/pkg/vt"
)

const (
	// Default PNG size, matching the uConsole's landscape logical screen.
	defaultWidth  = 1280
	defaultHeight = 720

	// defaultFPS is used when a caller hands us a nonsense frame rate. The
	// flag layer already range-checks, so this is belt and braces.
	defaultFPS = 30

	// keyBuffer stops a burst of key repeats from blocking the reader
	// between frames. At 30 fps the loop drains it 30 times a second.
	keyBuffer = 16
)

// Blitter is the part of a framebuffer the run loop uses. It is declared
// here, where it is consumed, so fbdev does not have to know about it.
type Blitter interface {
	Blit(img *image.RGBA, rot rotate.Rotation) error
	Close() error
	Width() int
	Height() int
	BitsPerPixel() int
	Stride() int
	String() string
}

// Drawer paints one frame. pattern.Scene is the only implementation in
// slice 1; the radar will be the second.
type Drawer interface {
	Draw(dst *canvas.Canvas, elapsed time.Duration)
}

// Config is the parsed intent of the command line.
type Config struct {
	FBPath      string
	Rotation    rotate.Rotation
	AutoRotate  bool
	FPS         int
	TestPattern bool
	PNG         string
	Size        image.Point
}

// session groups the three things every frame needs, so the loop signature
// stays readable.
type session struct {
	dev  Blitter
	canv *canvas.Canvas
	rot  rotate.Rotation
}

// Run executes one of the three modes and returns when it is done.
//
// It returns nil for a clean quit, including a cancelled context: the user
// pressing q and the user pressing Ctrl-C are the same outcome.
func Run(ctx context.Context, cfg Config, stdout io.Writer, opts ...Option) error {
	run := newRunner(opts...)

	if cfg.PNG != "" {
		return run.renderPNG(cfg, stdout)
	}

	return run.renderDevice(ctx, cfg, stdout)
}

// sayf writes a line to the console.
//
// The error is dropped on purpose. If stdout has gone away there is nothing
// useful left to do about it, and failing a render because a status line did
// not print would be worse than the missing line.
func sayf(dst io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(dst, format, args...)
}

// enter performs a mode switch that is allowed to fail.
//
// Both the console switch and raw mode are refused in ordinary situations:
// over ssh there is no VT to switch, and with stdin redirected there is no
// terminal to put into raw mode. Those are listed in degrade and turn into a
// warning plus a restore function that does nothing, so the caller never has
// to nil-check.
//
// The bool reports whether the switch actually applied, which is how the
// caller tells "running without a keyboard" from "running with one".
func enter(
	stdout io.Writer,
	what string,
	switchMode func() (func() error, error),
	degrade ...error,
) (func() error, bool, error) {
	restore, err := switchMode()
	if err == nil {
		return restore, true, nil
	}

	for _, tolerated := range degrade {
		if errors.Is(err, tolerated) {
			sayf(stdout, "warning: %s unavailable: %v\n", what, err)

			return func() error { return nil }, false, nil
		}
	}

	return nil, false, fmt.Errorf("app: %s: %w", what, err)
}

// quits reports whether a key ends the run.
func quits(key input.Key) bool {
	switch key.Kind {
	case input.Esc, input.CtrlC:
		return true
	case input.Rune:
		return key.Rune == 'q' || key.Rune == 'Q'
	case input.Up, input.Down, input.Left, input.Right, input.Enter:
		fallthrough
	default:
		return false
	}
}

// renderPNG draws one frame to a file. No device is opened, so this is the
// path that works on a Mac.
func (r *runner) renderPNG(cfg Config, stdout io.Writer) error {
	// Only an entirely unset size falls back. Half a size, say a width with
	// a negative height, is a caller mistake, and canvas.New says so rather
	// than quietly rendering something nobody asked for.
	size := cfg.Size
	if size == (image.Point{}) {
		size = image.Pt(defaultWidth, defaultHeight)
	}

	canv, err := canvas.New(size.X, size.Y)
	if err != nil {
		return fmt.Errorf("app: png canvas: %w", err)
	}

	r.scene.Draw(canv, 0)

	file, err := r.createPNG(cfg.PNG)
	if err != nil {
		return fmt.Errorf("app: create %s: %w", cfg.PNG, err)
	}

	if err := png.Encode(file, canv.Image()); err != nil {
		_ = file.Close()

		return fmt.Errorf("app: encode %s: %w", cfg.PNG, err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("app: close %s: %w", cfg.PNG, err)
	}

	sayf(stdout, "wrote %s %dx%d\n", cfg.PNG, size.X, size.Y)

	return nil
}

// renderDevice opens the framebuffer, sizes a canvas for it, and hands off
// to the one-shot or the live path.
func (r *runner) renderDevice(ctx context.Context, cfg Config, stdout io.Writer) error {
	dev, err := r.openFB(cfg.FBPath)
	if err != nil {
		return fmt.Errorf("app: framebuffer: %w", err)
	}

	defer func() { _ = dev.Close() }()

	rot := r.resolveRotation(cfg, stdout)

	width, height := rot.Logical(dev.Width(), dev.Height())

	canv, err := canvas.New(width, height)
	if err != nil {
		return fmt.Errorf("app: canvas for %s: %w", dev, err)
	}

	ses := session{dev: dev, canv: canv, rot: rot}

	if cfg.TestPattern {
		return r.once(ses, cfg.FBPath, stdout)
	}

	return r.live(ctx, cfg, ses, stdout)
}

// resolveRotation picks the rotation, preferring what the operator asked for
// over what fbcon says.
//
// An unreadable sysfs file is not fatal. On a machine where fbcon is not
// loaded there is simply no rotation to inherit, and upright is the right
// guess; --rotate is there for when it is the wrong one.
func (r *runner) resolveRotation(cfg Config, stdout io.Writer) rotate.Rotation {
	if !cfg.AutoRotate {
		return cfg.Rotation
	}

	rot, err := r.readRotation(rotate.SysfsPath)
	if err != nil {
		sayf(stdout, "warning: cannot read %s: %v; assuming rotate=0\n", rotate.SysfsPath, err)

		return rotate.None
	}

	return rot
}

// once paints a single frame and reports the geometry it used.
//
// It deliberately does not touch console or terminal state. That is what
// makes it usable over ssh, and it leaves the pattern on screen until the
// console repaints over it.
func (r *runner) once(ses session, path string, stdout io.Writer) error {
	r.scene.Draw(ses.canv, 0)

	if err := ses.dev.Blit(ses.canv.Image(), ses.rot); err != nil {
		return fmt.Errorf("app: blit: %w", err)
	}

	bounds := ses.canv.Bounds()
	sayf(stdout, "fb=%s %s rotate=%s logical=%dx%d\n", path, ses.dev, ses.rot, bounds.Dx(), bounds.Dy())

	return nil
}

// live runs the interactive loop.
//
// The deferred calls unwind in exactly the order the console needs: stop the
// ticker, cancel the reader and wait for it, restore the terminal, restore
// the console mode, close the device. Deferred calls also run while a panic
// unwinds, so there is no recover here: a crash still hands back a console
// in text mode with echo on.
func (r *runner) live(ctx context.Context, cfg Config, ses session, stdout io.Writer) error {
	restoreVT, _, err := enter(stdout, "console graphics mode", r.enterGraphics, vt.ErrNotConsole)
	if err != nil {
		return err
	}

	defer func() { _ = restoreVT() }()

	restoreTerm, raw, err := enter(stdout, "raw keyboard mode", r.makeRawStdin,
		term.ErrNotTerminal, term.ErrUnsupported)
	if err != nil {
		return err
	}

	defer func() { _ = restoreTerm() }()

	keys := make(chan input.Key, keyBuffer)
	readCtx, cancelRead := context.WithCancel(ctx)

	var group sync.WaitGroup

	defer group.Wait()
	defer cancelRead()

	// Only read when raw mode actually applied. Without it stdin is not a
	// terminal, so there are no keys coming, and a read on a pipe nobody
	// closes would block this goroutine past shutdown and hang group.Wait.
	// Quitting is then down to the context, which is what the warning said.
	//
	// The reader's error is dropped: it is always either a cancelled context
	// or a closed stdin, and both mean the same thing here.
	if raw {
		group.Go(func() {
			_ = input.Read(readCtx, r.stdin, keys)
		})
	}

	return r.loop(ctx, cfg, ses, keys)
}

// loop draws a frame per tick until something asks it to stop.
func (r *runner) loop(ctx context.Context, cfg Config, ses session, keys <-chan input.Key) error {
	rate := cfg.FPS
	if rate <= 0 {
		rate = defaultFPS
	}

	tick, stopTick := r.newTicker(time.Second / time.Duration(rate))
	defer stopTick()

	start := r.now()

	for {
		select {
		case <-ctx.Done():
			return nil
		case key := <-keys:
			if quits(key) {
				return nil
			}
		case now := <-tick:
			r.scene.Draw(ses.canv, now.Sub(start))

			if err := ses.dev.Blit(ses.canv.Image(), ses.rot); err != nil {
				return fmt.Errorf("app: blit: %w", err)
			}
		}
	}
}

// openDevice is the production framebuffer opener.
//
// It returns the interface rather than *fbdev.Device on purpose: this
// function is the seam's default value, so its type is the seam's type.
//
// Coverage note: the success return needs a real framebuffer, so a unit test
// on any developer machine can only reach the failure path. pkg/fbdev's own
// integration test covers the layer underneath. The same applies to
// enterConsoleGraphics and rawStdin below, and those three are the whole of
// this package's coverage shortfall.
//
// The staticcheck suppression is for the non-Linux build, where fbdev.Open
// is a stub that always fails and the error check therefore reads as
// redundant. On Linux it is the only thing catching a missing device.
//
//nolint:ireturn,staticcheck // seam returns the interface; SA4023 is a build-tag artefact.
func openDevice(path string) (Blitter, error) {
	dev, err := fbdev.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	return dev, nil
}

// enterConsoleGraphics opens the controlling terminal and switches it.
//
// /dev/tty rather than stdin, because the ioctl needs the session's own
// terminal and stdin may have been redirected.
//
// Coverage note: a test runner and a CI job both lack a controlling terminal,
// so the open fails and nothing past it runs. pkg/vt's integration test is
// what exercises the rest, on a machine that has a console.
//
//nolint:staticcheck // SA4023 is a build-tag artefact: vt.Graphics is a stub off Linux.
func enterConsoleGraphics() (func() error, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: opening /dev/tty: %w", vt.ErrNotConsole, err)
	}

	restore, err := vt.Graphics(tty)
	if err != nil {
		_ = tty.Close()

		return nil, fmt.Errorf("switching %s to graphics: %w", tty.Name(), err)
	}

	return func() error {
		restoreErr := restore()
		_ = tty.Close()

		return restoreErr
	}, nil
}

// rawStdin puts the real stdin into raw mode.
//
// Coverage note: `go test` gives the binary a pipe for stdin, not a terminal,
// so only the ErrNotTerminal path runs here. internal/term's integration test
// covers the success path against a real tty.
func rawStdin() (func() error, error) {
	restore, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		// Wrapped, not replaced: the caller degrades on term.ErrNotTerminal
		// and errors.Is has to keep finding it.
		return nil, fmt.Errorf("raw mode on stdin: %w", err)
	}

	return restore, nil
}

// realTicker is the production clock.
func realTicker(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

// createFile is the production PNG sink.
//
//nolint:ireturn // the seam is io.WriteCloser so a test can supply one.
func createFile(path string) (io.WriteCloser, error) {
	// The path is whatever the operator passed to --png. Picking it is the
	// point of the flag.
	file, err := os.Create(path) //nolint:gosec // operator-supplied output path.
	if err != nil {
		return nil, fmt.Errorf("creating %s: %w", path, err)
	}

	return file, nil
}

// newScene is the production drawer.
//
//nolint:ireturn // the seam is Drawer so the radar can replace the pattern.
func newScene() Drawer {
	return pattern.New()
}
