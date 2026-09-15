package app

import (
	"io"
	"os"
	"runtime"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/term"
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
	loadScenes    func() ([]Drawer, error)

	// source is where the radar scene gets its aircraft. It is built in main
	// from the flags, because that is where the choice between a radio, a
	// feed, a capture and the demo fleet is made. It defaults to an empty one
	// so a caller that only wants the pattern scene does not have to supply
	// a receiver it will never read.
	source source.Source

	// scopeRange is the display range the +, - and a keys drive. It lives
	// here rather than inside the scene so a future second view could share
	// one range control.
	scopeRange *scope.Scope

	// battery is what the radar's header indicator reads. It is nil by
	// default, which is a machine with no battery rather than a failure, and
	// main fills it in when the platform has one.
	battery radar.BatteryReader
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
		stdin:         term.NewReader(os.Stdin.Fd()),
		stderr:        os.Stderr,
		source:        source.Empty{},
		scopeRange:    scope.New(),
	}

	// Bound after the struct exists, because the production scene set reads
	// the source and the range control back off the runner, and both may
	// still be replaced by an option below.
	run.loadScenes = run.defaultScenes

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

// WithScenes replaces the scenes the loop can draw, in SceneKind order. This
// is how a test runs the loop without loading a font.
func WithScenes(scenes ...Drawer) Option {
	return func(r *runner) {
		r.loadScenes = func() ([]Drawer, error) { return scenes, nil }
	}
}

// WithSceneLoader replaces how the scene set is built, which is the seam the
// font loading sits behind.
func WithSceneLoader(load func() ([]Drawer, error)) Option {
	return func(r *runner) { r.loadScenes = load }
}

// WithSource supplies the aircraft the radar scene draws. main builds it from
// the flags and owns its lifetime: it starts the ingest before the loop and
// closes it after, so the run loop never has to know which source it is.
func WithSource(src source.Source) Option {
	return func(r *runner) {
		if src != nil {
			r.source = src
		}
	}
}

// WithBattery supplies what the header's battery indicator reads. main owns
// the poller behind it and starts it before the loop; the scene only reads the
// two numbers off it.
func WithBattery(reader radar.BatteryReader) Option {
	return func(r *runner) {
		if reader != nil {
			r.battery = reader
		}
	}
}

// WithScopeRange replaces the display-range control, which is what a test
// uses to start the radar at a known range.
func WithScopeRange(scopeRange *scope.Scope) Option {
	return func(r *runner) {
		if scopeRange != nil {
			r.scopeRange = scopeRange
		}
	}
}
