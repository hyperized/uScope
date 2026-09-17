// Package app is the run loop and the wiring around it.
//
// Three modes share one entry point. --png renders a still frame to a file
// and never opens a device, which is how the scene gets checked on a laptop.
// --test-pattern paints one frame and exits without taking the screen over,
// so it is safe over ssh and the frame stays up until something else
// repaints. Everything else is the live loop.
//
// Slice 2 put a backend.Backend between the loop and the screen. The loop
// asks how big a canvas the backend wants, draws that, and hands it over,
// which is the same code whether the frame ends up in /dev/fb0 on the
// uConsole, in Kitty graphics escape sequences over ssh, or in half-block
// characters. Every device this package talks to arrives through a function
// field, so the whole of Run is exercised on a Mac with fakes.
//
// Slice 3 made the scene a set rather than a single drawer. --scene picks
// which one starts, and that is the one the whole run draws: nothing cycles
// between them. Building the set loads the embedded fonts, which is why a
// font that will not parse stops the program before it opens a device.
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
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/term"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/backend"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/fbdev"
	"github.com/hyperized/uScope/pkg/rotate"
	"github.com/hyperized/uScope/pkg/termbackend"
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

	// linuxGOOS is the only platform with a framebuffer, so it is the only
	// one where auto mode bothers trying to open one.
	linuxGOOS = "linux"
)

// Blitter is the part of a framebuffer the fb backend uses. It is declared
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

// Drawer paints one frame. Both scenes implement it.
type Drawer interface {
	Draw(dst *canvas.Canvas, elapsed time.Duration)
}

// KeyHandler is the optional other half of the scene contract. A scene that
// binds keys of its own implements it and gets first refusal on every key;
// anything it does not take falls through to the loop, which is what keeps q
// working whichever scene is on screen.
//
// It is declared here, where it is consumed, so a scene does not have to
// import internal/app to be one.
type KeyHandler interface {
	Handle(key input.Key) bool
}

// offerKey hands a key to the scene and reports whether the scene took it.
func offerKey(scene Drawer, key input.Key) bool {
	binder, ok := scene.(KeyHandler)

	return ok && binder.Handle(key)
}

// Themed is the optional other half of a scene's colour contract. A scene
// that carries a theme.Palette implements it, and the l and k keys change the
// colours by calling SetPalette on every scene that does, not only the one on
// screen, so switching scenes later still shows the palette that was chosen.
//
// It is declared here, where it is consumed, for the same reason KeyHandler
// is: a scene does not have to import internal/app to satisfy it.
type Themed interface {
	SetPalette(pal theme.Palette)
}

// applyPalette hands the palette to every scene that implements Themed. A
// scene without one, such as the orientation pattern, is left exactly as it
// was built.
func applyPalette(scenes []Drawer, pal theme.Palette) {
	for _, scene := range scenes {
		if themed, ok := scene.(Themed); ok {
			themed.SetPalette(pal)
		}
	}
}

// Configured is the optional half of a scene's settings contract, the sibling
// of Themed. Only the radar has settings, and they reach it here rather than
// through the scene loader, because that loader is a seam with no config in
// scope and widening it would cost every test that replaces it.
//
// It carries the whole block rather than one setter per flag. The radar keeps
// gaining settings, and an interface per flag would end up as a shelf of
// one-method interfaces that all mean "the command line said so".
type Configured interface {
	Apply(set radar.Settings)
}

// applySettings hands the settings to every scene that takes them. A scene
// without any, such as the orientation pattern, is left exactly as it was
// built.
func applySettings(scenes []Drawer, set radar.Settings) {
	for _, scene := range scenes {
		if configured, ok := scene.(Configured); ok {
			configured.Apply(set)
		}
	}
}

// Config is the parsed intent of the command line.
type Config struct {
	FBPath      string
	Rotation    rotate.Rotation
	AutoRotate  bool
	FPS         int
	Frames      int
	TestPattern bool
	PNG         string
	Size        image.Point
	Backend     backend.Kind
	Scene       SceneKind

	// Theme is the colour theme to start on. The zero value reads as
	// theme.KindNight, which is the default on a backlit handheld.
	Theme theme.Kind

	// Look is the palette look to start on, the other half of the pair Theme
	// picks from. The zero value reads as theme.LookGlass, the cockpit
	// palette.
	Look theme.Look

	// Radar is what the flags picked for the radar scene. Every field's zero
	// value is that setting's default, for the same reason Theme's is.
	Radar radar.Settings
}

// session is one backend plus the canvas that fits it, and the label that
// describes the pair in the startup line.
type session struct {
	back  backend.Backend
	canv  *canvas.Canvas
	label string

	// scenes is the whole set in SceneKind order and active is the one
	// --scene picked at startup. Nothing changes it once the run has begun.
	scenes []Drawer
	active int

	// themeKind and look are the two halves of the palette currently applied:
	// l cycles night and day, k cycles glass, phosphor and mono, and the pair
	// names one of the six. Both start at whatever the flags asked for.
	themeKind theme.Kind
	look      theme.Look

	// console is true only for the framebuffer. It is the framebuffer that
	// needs the VT switched into graphics mode; doing that to a terminal
	// backend would blank the very screen it is drawing on.
	console bool
}

// Run executes one of the modes and returns when it is done.
//
// It returns nil for a clean quit, including a cancelled context: the user
// pressing q and the user pressing Ctrl-C are the same outcome.
func Run(ctx context.Context, cfg Config, stdout io.Writer, opts ...Option) error {
	run := newRunner(opts...)

	// The b key's bias-tee flip runs on a worker, and the source it talks to
	// is closed by main the moment Run returns. Waiting here is what stops a
	// control transfer still being in the air when the dongle is let go.
	defer run.biasTee.wait()

	scenes, err := run.loadScenes()
	if err != nil {
		return err
	}

	applyPalette(scenes, cfg.Look.Palette(cfg.Theme))
	applySettings(scenes, cfg.Radar)

	active := int(cfg.Scene)
	if active >= len(scenes) {
		return fmt.Errorf("%w: %s", ErrNoScene, cfg.Scene)
	}

	if cfg.PNG != "" || cfg.Backend == backend.PNG {
		return run.renderPNG(cfg, scenes[active], stdout)
	}

	return run.renderBackend(ctx, cfg, scenes, active, stdout)
}

// scene is the one currently on screen.
//
//nolint:ireturn // a scene is a Drawer; that is the whole point of the seam.
func (s *session) scene() Drawer { return s.scenes[s.active] }

// cycleTheme steps to the other colour theme, which is what the l key is bound
// to. The look it is worn in does not move.
func (s *session) cycleTheme() {
	s.themeKind = s.themeKind.Next()
	s.repaint()
}

// cycleLook steps to the next look, glass to phosphor to mono and back, which
// is what the k key is bound to. Night or day does not move, so a look picked
// at three in the morning arrives in the dark palette it was reached for in.
func (s *session) cycleLook() {
	s.look = s.look.Next()
	s.repaint()
}

// repaint hands the palette the two cycles now name to every scene that takes
// one, not only the one on screen, so switching scenes later still shows the
// colours that were picked.
func (s *session) repaint() {
	applyPalette(s.scenes, s.look.Palette(s.themeKind))
}

// sayf writes a line to the console.
//
// The error is dropped on purpose. If the writer has gone away there is
// nothing useful left to do about it, and failing a render because a status
// line did not print would be worse than the missing line.
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
	status io.Writer,
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
			sayf(status, "warning: %s unavailable: %v\n", what, err)

			return func() error { return nil }, false, nil
		}
	}

	return nil, false, fmt.Errorf("app: %s: %w", what, err)
}

// command is what a keypress asks the live loop to do.
type command uint8

const (
	// cmdNone is every key nothing is bound to at this level. The arrows and
	// the radar's own keys are taken by the scene before the loop sees them.
	//
	// m is deliberately not bound anywhere yet. It is reserved for the shore
	// overlay, and binding it to something else in the meantime would be a
	// key to unlearn later.
	cmdNone command = iota
	cmdQuit
	cmdNextTheme
	cmdNextLook
)

// classify maps a key onto a command.
func classify(key input.Key) command {
	switch key.Kind {
	case input.Esc, input.CtrlC:
		return cmdQuit
	case input.Rune:
		return runeCommand(key.Rune)
	case input.Up, input.Down, input.Left, input.Right, input.Enter:
		fallthrough
	default:
		return cmdNone
	}
}

// runeCommand maps a printable key onto a command. Both cases are bound so
// the keys keep working with caps lock on, which is easy to hit by accident
// on the uConsole's small keyboard.
//
// v is not among them. It used to cycle which scene was on screen; now it
// toggles the radar's own minimal view, so Scene.Handle claims it before the
// loop ever gets to classify it.
func runeCommand(value rune) command {
	switch value {
	case 'q', 'Q':
		return cmdQuit
	case 'l', 'L':
		return cmdNextTheme
	case 'k', 'K':
		return cmdNextLook
	default:
		return cmdNone
	}
}

// dispatch applies a classified command to the session and reports whether
// the loop should stop. It exists as its own function, rather than a run of
// ifs inline in loop, to keep loop's own branching within the cognitive
// complexity limit.
func dispatch(ses *session, action command) bool {
	switch action {
	case cmdQuit:
		return true
	case cmdNextTheme:
		ses.cycleTheme()
	case cmdNextLook:
		ses.cycleLook()
	case cmdNone:
		fallthrough
	default:
		// Every key the loop binds nothing to arrives here and changes
		// nothing. The scene was offered it first and did not want it either,
		// so there is genuinely nothing left to do with the press.
	}

	return false
}

// renderPNG draws one frame to a file. No device is opened, so this is the
// path that works anywhere.
func (r *runner) renderPNG(cfg Config, scene Drawer, stdout io.Writer) error {
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

	scene.Draw(canv, 0)

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

// renderBackend opens a backend, sizes a canvas for it, and hands off to the
// one-shot or the live path.
func (r *runner) renderBackend(ctx context.Context, cfg Config, scenes []Drawer, active int, stdout io.Writer) error {
	ses, err := r.selectBackend(cfg, stdout)
	if err != nil {
		return err
	}

	defer func() { _ = ses.back.Close() }()

	ses.scenes, ses.active = scenes, active
	ses.themeKind, ses.look = cfg.Theme, cfg.Look

	width, height := ses.back.Size()

	canv, err := canvas.New(width, height)
	if err != nil {
		return fmt.Errorf("app: canvas for %s: %w", ses.label, err)
	}

	ses.canv = canv

	// A terminal backend is writing frames to stdout, so the startup line
	// and any warning have to go somewhere else or they land in the middle
	// of the picture.
	status := stdout
	if !ses.console {
		status = r.stderr
	}

	if cfg.TestPattern {
		return r.once(ses, status)
	}

	return r.live(ctx, cfg, ses, status)
}

// selectBackend builds the backend the operator asked for, or works one out.
func (r *runner) selectBackend(cfg Config, stdout io.Writer) (*session, error) {
	switch cfg.Backend {
	case backend.Framebuffer:
		return r.framebuffer(cfg, stdout)
	case backend.Kitty, backend.Blocks:
		return r.terminal(cfg, cfg.Backend)
	case backend.Auto, backend.PNG:
		fallthrough
	default:
		return r.autoBackend(cfg, stdout)
	}
}

// autoBackend picks a backend without being told.
//
// The device's own screen wins when there is one, because that is the point
// of the program. Failing to open it is not an error here: a Linux box with
// no framebuffer, or an ssh session onto one where the device is busy, still
// has a terminal, and falling through to it is more useful than refusing to
// start.
func (r *runner) autoBackend(cfg Config, stdout io.Writer) (*session, error) {
	if r.goos == linuxGOOS {
		if ses, err := r.framebuffer(cfg, stdout); err == nil {
			return ses, nil
		}
	}

	if backend.KittyCapable(r.getenv) {
		return r.terminal(cfg, backend.Kitty)
	}

	return r.terminal(cfg, backend.Blocks)
}

// framebuffer opens the device and wraps it with the rotation, which is the
// only thing standing between fbdev's API and the Backend one.
func (r *runner) framebuffer(cfg Config, stdout io.Writer) (*session, error) {
	dev, err := r.openFB(cfg.FBPath)
	if err != nil {
		return nil, fmt.Errorf("app: framebuffer: %w", err)
	}

	rot := r.resolveRotation(cfg, stdout)

	return &session{
		back:    &fbBackend{dev: dev, rot: rot},
		label:   fmt.Sprintf("fb=%s %s rotate=%s", cfg.FBPath, dev, rot),
		console: true,
	}, nil
}

// terminal opens one of the two terminal backends.
func (r *runner) terminal(cfg Config, kind backend.Kind) (*session, error) {
	back, err := r.openTerm(cfg, kind)
	if err != nil {
		return nil, fmt.Errorf("app: terminal: %w", err)
	}

	width, height := back.Size()

	return &session{back: back, label: fmt.Sprintf("%s %dx%d", kind, width, height)}, nil
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
// It deliberately does not switch the console or the terminal into anything.
// That is what makes it usable over ssh, and it leaves the frame on screen
// until something else repaints.
func (*runner) once(ses *session, status io.Writer) error {
	if err := ses.frame(ses.scene(), 0); err != nil {
		return err
	}

	bounds := ses.canv.Bounds()
	sayf(status, "%s logical=%dx%d\n", ses.label, bounds.Dx(), bounds.Dy())

	return nil
}

// live runs the interactive loop.
//
// The deferred calls unwind in exactly the order the console needs: stop the
// ticker, cancel the reader and wait for it, restore the terminal, restore
// the console mode, close the backend. Deferred calls also run while a panic
// unwinds, so there is no recover here: a crash still hands back a console
// in text mode with echo on.
func (r *runner) live(ctx context.Context, cfg Config, ses *session, status io.Writer) error {
	restoreVT, err := r.enterConsole(ses, status)
	if err != nil {
		return err
	}

	defer func() { _ = restoreVT() }()

	restoreTerm, raw, err := enter(status, "raw keyboard mode", r.makeRawStdin,
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

// enterConsole switches the VT into graphics mode, but only for the
// framebuffer. A terminal backend draws through the console rather than
// underneath it, so switching would blank its own output.
func (r *runner) enterConsole(ses *session, status io.Writer) (func() error, error) {
	if !ses.console {
		return func() error { return nil }, nil
	}

	restore, _, err := enter(status, "console graphics mode", r.enterGraphics, vt.ErrNotConsole)

	return restore, err
}

// loop draws a frame per tick until something asks it to stop.
func (r *runner) loop(ctx context.Context, cfg Config, ses *session, keys <-chan input.Key) error {
	rate := cfg.FPS
	if rate <= 0 {
		rate = defaultFPS
	}

	tick, stopTick := r.newTicker(time.Second / time.Duration(rate))
	defer stopTick()

	start := r.now()
	drawn := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case key := <-keys:
			if offerKey(ses.scene(), key) {
				continue
			}

			if dispatch(ses, classify(key)) {
				return nil
			}
		case now := <-tick:
			if err := ses.frame(ses.scene(), now.Sub(start)); err != nil {
				return err
			}

			drawn++
			if cfg.Frames > 0 && drawn >= cfg.Frames {
				return nil
			}
		}
	}
}

// frame resizes the canvas if it has to, draws, and blits.
func (s *session) frame(scene Drawer, elapsed time.Duration) error {
	if err := s.resize(); err != nil {
		return err
	}

	scene.Draw(s.canv, elapsed)

	if err := s.back.Blit(s.canv.Image()); err != nil {
		return fmt.Errorf("app: blit: %w", err)
	}

	return nil
}

// resize rebuilds the canvas when the backend changes its mind about how big
// it should be, which from here is what someone dragging the corner of a
// terminal window looks like.
//
// The canvas is thrown away rather than reshaped because image.RGBA owns a
// flat slice whose stride is baked in. Allocating one is cheap next to the
// frames drawn between two resizes.
func (s *session) resize() error {
	width, height := s.back.Size()

	if bounds := s.canv.Bounds(); bounds.Dx() == width && bounds.Dy() == height {
		return nil
	}

	canv, err := canvas.New(width, height)
	if err != nil {
		return fmt.Errorf("app: canvas for %s: %w", s.label, err)
	}

	s.canv = canv

	return nil
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

// openTerminal is the production terminal backend builder.
//
// The alternate screen is skipped for --test-pattern so a single frame stays
// on the terminal after uScope exits, which is the same promise the
// framebuffer path makes.
//
//nolint:ireturn // seam returns the interface, so its default has to.
func openTerminal(cfg Config, kind backend.Kind) (backend.Backend, error) {
	mode := termbackend.Blocks
	if kind == backend.Kitty {
		mode = termbackend.Kitty
	}

	size := cfg.Size
	if size == (image.Point{}) {
		size = image.Pt(defaultWidth, defaultHeight)
	}

	dev, err := termbackend.Open(
		termbackend.WithMode(mode),
		termbackend.WithCanvas(size),
		termbackend.WithAltScreen(!cfg.TestPattern),
	)
	if err != nil {
		return nil, fmt.Errorf("opening terminal: %w", err)
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
