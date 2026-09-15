package app

import (
	"io"
	"os"
	"runtime"
	"time"

	"github.com/hyperized/uScope/pkg/backend"
	"github.com/hyperized/uScope/pkg/rotate"
)

// runner holds the seams. Every one of them has a production default, so
// Run(ctx, cfg, stdout) with no options is the real program; tests replace
// only the piece they are interested in.
type runner struct {
	openFB        func(path string) (Blitter, error)
	openTerm      func(cfg Config, kind backend.Kind) (backend.Backend, error)
	enterGraphics func() (func() error, error)
	makeRawStdin  func() (func() error, error)
	readRotation  func(path string) (rotate.Rotation, error)
	newTicker     func(interval time.Duration) (<-chan time.Time, func())
	createPNG     func(path string) (io.WriteCloser, error)
	now           func() time.Time
	getenv        func(name string) string
	goos          string
	stdin         io.Reader
	stderr        io.Writer
	scene         Drawer
}

// newRunner builds the production wiring and then applies the overrides.
func newRunner(opts ...Option) *runner {
	run := &runner{
		openFB:        openDevice,
		openTerm:      openTerminal,
		enterGraphics: enterConsoleGraphics,
		makeRawStdin:  rawStdin,
		readRotation:  rotate.FromSysfs,
		newTicker:     realTicker,
		createPNG:     createFile,
		now:           time.Now,
		getenv:        os.Getenv,
		goos:          runtime.GOOS,
		stdin:         os.Stdin,
		stderr:        os.Stderr,
		scene:         newScene(),
	}

	for _, opt := range opts {
		opt(run)
	}

	return run
}

// Option replaces one of the run loop's dependencies.
type Option func(*runner)

// WithFramebuffer replaces the framebuffer opener.
func WithFramebuffer(open func(path string) (Blitter, error)) Option {
	return func(r *runner) { r.openFB = open }
}

// WithTerminal replaces the terminal backend builder, which is how the
// terminal paths are tested without a terminal.
func WithTerminal(open func(cfg Config, kind backend.Kind) (backend.Backend, error)) Option {
	return func(r *runner) { r.openTerm = open }
}

// WithConsoleSwitch replaces the console graphics-mode switch.
func WithConsoleSwitch(switchMode func() (func() error, error)) Option {
	return func(r *runner) { r.enterGraphics = switchMode }
}

// WithRawMode replaces the terminal raw-mode switch.
func WithRawMode(switchMode func() (func() error, error)) Option {
	return func(r *runner) { r.makeRawStdin = switchMode }
}

// WithRotationReader replaces the fbcon sysfs lookup.
func WithRotationReader(read func(path string) (rotate.Rotation, error)) Option {
	return func(r *runner) { r.readRotation = read }
}

// WithTicker replaces the frame clock, which is how a test drives an exact
// number of frames without waiting for real time to pass.
func WithTicker(newTicker func(interval time.Duration) (<-chan time.Time, func())) Option {
	return func(r *runner) { r.newTicker = newTicker }
}

// WithPNGCreator replaces the PNG output sink.
func WithPNGCreator(create func(path string) (io.WriteCloser, error)) Option {
	return func(r *runner) { r.createPNG = create }
}

// WithClock replaces the start-of-run timestamp.
func WithClock(now func() time.Time) Option {
	return func(r *runner) { r.now = now }
}

// WithEnv replaces the environment lookup used to recognise the terminal.
func WithEnv(getenv func(name string) string) Option {
	return func(r *runner) { r.getenv = getenv }
}

// WithGOOS replaces the platform name auto mode tests against, so the Linux
// branch of the decision is reachable from a Mac.
func WithGOOS(goos string) Option {
	return func(r *runner) { r.goos = goos }
}

// WithInput replaces the keyboard source.
func WithInput(src io.Reader) Option {
	return func(r *runner) { r.stdin = src }
}

// WithStderr replaces where warnings and the startup line go when a terminal
// backend has taken over stdout.
func WithStderr(dst io.Writer) Option {
	return func(r *runner) { r.stderr = dst }
}

// WithScene replaces the thing being drawn.
func WithScene(scene Drawer) Option {
	return func(r *runner) { r.scene = scene }
}
