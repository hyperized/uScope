package app_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/app"
	"github.com/hyperized/uScope/internal/term"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/rotate"
	"github.com/hyperized/uScope/pkg/vt"
)

// testTimeout bounds every blocking wait in this file, so a regression that
// hangs the run loop fails the test instead of the CI job.
const testTimeout = 3 * time.Second

// unusedPNGPath and fbPath are fixture values for tests that never actually
// touch the filesystem or a device: the seam under test is faked out before
// either path would be used for real.
const (
	unusedPNGPath = "unused.png"
	fbPath        = "/dev/fb0"
)

// recvOrTimeout receives one value from ch, failing the test if none arrives
// within timeout.
//
//nolint:ireturn // generic helper: T is whatever the caller's channel carries, interface or not.
func recvOrTimeout[T any](t *testing.T, ch <-chan T, timeout time.Duration, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", what)
	}

	var zero T

	return zero
}

// runAsync starts app.Run in its own goroutine and returns a channel that
// carries its result, so the test can wait on it with a bound.
func runAsync(ctx context.Context, cfg app.Config, stdout io.Writer, opts ...app.Option) <-chan error {
	done := make(chan error, 1)

	go func() {
		done <- app.Run(ctx, cfg, stdout, opts...)
	}()

	return done
}

// openFBSpy records whether the framebuffer opener was invoked, for the PNG
// path's "nothing else is touched" guarantee.
type openFBSpy struct {
	called bool
}

//nolint:ireturn // the seam under test returns the interface; the spy must match it.
func (s *openFBSpy) open(string) (app.Blitter, error) {
	s.called = true

	return nil, errStub
}

var errStub = errors.New("stub")

// --- PNG mode -----------------------------------------------------------

func TestRunPNGHappyPath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "out.png")
	cfg := app.Config{PNG: path, Size: image.Pt(64, 48)}

	var buf bytes.Buffer

	spy := &openFBSpy{}

	if err := app.Run(t.Context(), cfg, &buf, app.WithFramebuffer(spy.open)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if spy.called {
		t.Error("the framebuffer opener was called in PNG mode, want it untouched")
	}

	file, err := os.Open(path) //nolint:gosec // path is a t.TempDir() fixture, not user input.
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	img, err := png.Decode(file)
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}

	if got := img.Bounds(); got.Dx() != 64 || got.Dy() != 48 {
		t.Errorf("decoded size = %dx%d, want 64x48", got.Dx(), got.Dy())
	}

	wantLine := fmt.Sprintf("wrote %s %dx%d\n", path, 64, 48)
	if got := buf.String(); got != wantLine {
		t.Errorf("stdout = %q, want %q", got, wantLine)
	}
}

func TestRunPNGDefaultSize(t *testing.T) {
	t.Parallel()

	const (
		defaultWidth  = 1280
		defaultHeight = 720
	)

	for _, testCase := range []struct {
		name string
		size image.Point
	}{
		{name: "zero size", size: image.Point{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "out.png")
			cfg := app.Config{PNG: path, Size: testCase.size}

			var buf bytes.Buffer

			if err := app.Run(t.Context(), cfg, &buf); err != nil {
				t.Fatalf("Run: %v", err)
			}

			file, err := os.Open(path) //nolint:gosec // path is a t.TempDir() fixture.
			if err != nil {
				t.Fatalf("open %s: %v", path, err)
			}
			defer func() { _ = file.Close() }()

			img, err := png.Decode(file)
			if err != nil {
				t.Fatalf("png.Decode: %v", err)
			}

			got := img.Bounds()
			if got.Dx() != defaultWidth || got.Dy() != defaultHeight {
				t.Errorf("decoded size = %dx%d, want %dx%d", got.Dx(), got.Dy(), defaultWidth, defaultHeight)
			}
		})
	}
}

// A size with only one axis set is not a size. It reaches canvas.New, which
// is the one place that decides what a usable canvas is.
func TestRunPNGBadSize(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		size image.Point
	}{
		{name: "negative width", size: image.Pt(-5, 100)},
		{name: "negative height", size: image.Pt(100, -5)},
		{name: "zero width only", size: image.Pt(0, 100)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "out.png")
			cfg := app.Config{PNG: path, Size: testCase.size}

			var buf bytes.Buffer

			err := app.Run(t.Context(), cfg, &buf)
			if !errors.Is(err, canvas.ErrSize) {
				t.Fatalf("Run error = %v, want canvas.ErrSize", err)
			}
		})
	}
}

func TestRunPNGCreatorError(t *testing.T) {
	t.Parallel()

	cfg := app.Config{PNG: unusedPNGPath}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf, app.WithPNGCreator(func(string) (io.WriteCloser, error) {
		return nil, errStub
	}))

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

// limitedWriter accepts writes only up to limit bytes total, then fails.
// It never returns a short write, so it never breaks the io.Writer
// contract: a call either fully succeeds or is fully rejected.
type limitedWriter struct {
	limit   int
	written int
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if w.written+len(data) > w.limit {
		return 0, errStub
	}

	w.written += len(data)

	return len(data), nil
}

func (*limitedWriter) Close() error { return nil }

func TestRunPNGEncodeError(t *testing.T) {
	t.Parallel()

	const writeLimit = 8

	cfg := app.Config{PNG: unusedPNGPath, Size: image.Pt(64, 48)}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf, app.WithPNGCreator(func(string) (io.WriteCloser, error) {
		return &limitedWriter{limit: writeLimit}, nil
	}))

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

// closeFailWriter succeeds on every Write but fails on Close.
type closeFailWriter struct {
	bytes.Buffer
}

func (*closeFailWriter) Close() error { return errStub }

func TestRunPNGCloseError(t *testing.T) {
	t.Parallel()

	cfg := app.Config{PNG: unusedPNGPath, Size: image.Pt(64, 48)}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf, app.WithPNGCreator(func(string) (io.WriteCloser, error) {
		return &closeFailWriter{}, nil
	}))

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

// --- shared live/test-pattern fakes --------------------------------------

// blitCall records one Blit invocation.
type blitCall struct {
	bounds image.Rectangle
	rot    rotate.Rotation
}

// fakeBlitter is the Blitter double used by both test-pattern and live-mode
// tests. Calls are delivered on a channel so a concurrently running loop can
// be observed without a lock.
type fakeBlitter struct {
	width, height, bpp, stride int
	text                       string
	blitErr                    error
	closeErr                   error
	calls                      chan blitCall
	closed                     chan struct{}
}

func newFakeBlitter(width, height, bpp, stride int, text string) *fakeBlitter {
	return &fakeBlitter{
		width: width, height: height, bpp: bpp, stride: stride, text: text,
		calls:  make(chan blitCall, 8),
		closed: make(chan struct{}),
	}
}

func (f *fakeBlitter) Blit(img *image.RGBA, rot rotate.Rotation) error {
	f.calls <- blitCall{bounds: img.Bounds(), rot: rot}

	return f.blitErr
}

func (f *fakeBlitter) Close() error {
	close(f.closed)

	return f.closeErr
}

func (f *fakeBlitter) Width() int        { return f.width }
func (f *fakeBlitter) Height() int       { return f.height }
func (f *fakeBlitter) BitsPerPixel() int { return f.bpp }
func (f *fakeBlitter) Stride() int       { return f.stride }
func (f *fakeBlitter) String() string    { return f.text }

// drawCall records one Draw invocation.
type drawCall struct {
	bounds  image.Rectangle
	elapsed time.Duration
}

// fakeDrawer is the Drawer double.
type fakeDrawer struct {
	calls chan drawCall
}

func newFakeDrawer() *fakeDrawer {
	return &fakeDrawer{calls: make(chan drawCall, 8)}
}

func (f *fakeDrawer) Draw(dst *canvas.Canvas, elapsed time.Duration) {
	f.calls <- drawCall{bounds: dst.Bounds(), elapsed: elapsed}
}

// --- test-pattern mode ----------------------------------------------------

func TestRunTestPatternHappyPath(t *testing.T) {
	t.Parallel()

	const (
		physWidth  = 720
		physHeight = 1280
		bpp        = 16
		stride     = 1440
	)

	blitter := newFakeBlitter(physWidth, physHeight, bpp, stride, "720x1280 16bpp stride=1440")
	drawer := newFakeDrawer()

	consoleSwitch := &switchSpy{}
	rawSwitch := &switchSpy{}

	cfg := app.Config{
		FBPath:      fbPath,
		TestPattern: true,
		Rotation:    rotate.Clockwise,
	}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScene(drawer),
		app.WithConsoleSwitch(consoleSwitch.switchMode),
		app.WithRawMode(rawSwitch.switchMode),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	call := recvOrTimeout(t, blitter.calls, testTimeout, "Blit call")

	if call.bounds.Dx() != defaultWidth || call.bounds.Dy() != defaultHeight {
		t.Errorf("canvas handed to Blit is %dx%d, want %dx%d",
			call.bounds.Dx(), call.bounds.Dy(), defaultWidth, defaultHeight)
	}

	if call.rot != rotate.Clockwise {
		t.Errorf("Blit rotation = %v, want %v", call.rot, rotate.Clockwise)
	}

	select {
	case <-blitter.closed:
	default:
		t.Error("Close was not called")
	}

	const want = "fb=/dev/fb0 720x1280 16bpp stride=1440 rotate=1 logical=1280x720\n"
	if got := buf.String(); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}

	// Test-pattern mode must never touch console or terminal state: that is
	// what makes it safe to run over ssh, where there is no VT to switch and
	// often no terminal to put in raw mode.
	if consoleSwitch.called {
		t.Error("console switch was called in test-pattern mode, want it untouched")
	}

	if rawSwitch.called {
		t.Error("raw mode switch was called in test-pattern mode, want it untouched")
	}
}

const (
	defaultWidth  = 1280
	defaultHeight = 720
)

func TestRunTestPatternFramebufferError(t *testing.T) {
	t.Parallel()

	cfg := app.Config{FBPath: fbPath, TestPattern: true}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf, app.WithFramebuffer(func(string) (app.Blitter, error) {
		return nil, errStub
	}))

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

func TestRunTestPatternBlitError(t *testing.T) {
	t.Parallel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	blitter.blitErr = errStub

	cfg := app.Config{FBPath: fbPath, TestPattern: true}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
	)

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}

	select {
	case <-blitter.closed:
	default:
		t.Error("Close was not called after a Blit error")
	}
}

func TestRunTestPatternZeroSizeCanvasError(t *testing.T) {
	t.Parallel()

	blitter := newFakeBlitter(0, 0, 0, 0, "empty")

	cfg := app.Config{FBPath: fbPath, TestPattern: true}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
	)

	if !errors.Is(err, canvas.ErrSize) {
		t.Errorf("err = %v, want wrapping %v", err, canvas.ErrSize)
	}
}

// --- live mode helpers ------------------------------------------------

// switchSpy is a WithConsoleSwitch/WithRawMode double that always succeeds
// and records that it was invoked.
type switchSpy struct {
	called bool
}

func (s *switchSpy) switchMode() (func() error, error) {
	s.called = true

	return func() error { return nil }, nil
}

// orderedRestore is a restore function that appends a label to order when
// called, optionally returning an error, for pinning restore sequencing.
func orderedRestore(order *[]string, label string, restoreErr error) func() (func() error, error) {
	return func() (func() error, error) {
		return func() error {
			*order = append(*order, label)

			return restoreErr
		}, nil
	}
}

// degradeSwitch always fails with an error wrapping sentinel.
func degradeSwitch(sentinel error) func() (func() error, error) {
	return func() (func() error, error) {
		return nil, fmt.Errorf("wrap: %w", sentinel)
	}
}

// abortSwitch always fails with err, unwrapped by any tolerated sentinel.
func abortSwitch(err error) func() (func() error, error) {
	return func() (func() error, error) {
		return nil, err
	}
}

// fakeTicker is the WithTicker double. new is called exactly once per Run,
// so interval and stopCalls need no synchronization beyond the
// happens-before edge that receiving Run's result already provides.
type fakeTicker struct {
	ch        chan time.Time
	interval  time.Duration
	stopCalls int
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time)}
}

func (f *fakeTicker) new(interval time.Duration) (<-chan time.Time, func()) {
	f.interval = interval

	return f.ch, func() { f.stopCalls++ }
}

// countingReader records how many times Read was called, for asserting the
// reader goroutine never ran when raw mode degraded.
type countingReader struct {
	calls int
}

func (r *countingReader) Read([]byte) (int, error) {
	r.calls++

	return 0, io.EOF
}

// idleReader always reports "nothing yet, no error", the shape of a
// raw-mode read that timed out. It never blocks, so it stays responsive to
// context cancellation the same way the real reader is.
type idleReader struct{}

func (idleReader) Read([]byte) (int, error) { return 0, nil }

// onceReader hands back data on the first Read and reports a clean EOF on
// every call after that.
type onceReader struct {
	data []byte
	done bool
}

func (r *onceReader) Read(buf []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}

	r.done = true

	return copy(buf, r.data), nil
}

// stagedReader hands back stage1 on the first Read, then waits for advance
// to be closed before handing back stage2 on the second Read. Every Read
// after that reports a clean EOF.
type stagedReader struct {
	stage1  []byte
	stage2  []byte
	advance chan struct{}
	step    int
}

func (r *stagedReader) Read(buf []byte) (int, error) {
	switch r.step {
	case 0:
		r.step++

		return copy(buf, r.stage1), nil
	case 1:
		<-r.advance

		r.step++

		return copy(buf, r.stage2), nil
	default:
		return 0, io.EOF
	}
}

// liveConfig is the shared Config for live-mode tests: a square canvas
// keeps rotation out of the way of tests that are not about geometry.
func liveConfig(fps int) app.Config {
	return app.Config{FBPath: fbPath, FPS: fps}
}

// --- live mode: quit keys -------------------------------------------------

func TestRunLiveQuitKeys(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		data []byte
	}{
		{name: "lowercase q", data: []byte{'q'}},
		{name: "uppercase Q", data: []byte{'Q'}},
		{name: "ctrl-c", data: []byte{0x03}},
		{name: "esc", data: []byte{0x1b}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
			defer cancel()

			blitter := newFakeBlitter(100, 100, 16, 200, "fake")
			drawer := newFakeDrawer()
			ticker := newFakeTicker()

			done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
				app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
				app.WithScene(drawer),
				app.WithConsoleSwitch((&switchSpy{}).switchMode),
				app.WithRawMode((&switchSpy{}).switchMode),
				app.WithInput(&onceReader{data: testCase.data}),
				app.WithTicker(ticker.new),
			)

			err := recvOrTimeout(t, done, testTimeout, "Run to return after a quit key")
			if err != nil {
				t.Errorf("Run: %v, want nil", err)
			}

			if ticker.stopCalls != 1 {
				t.Errorf("ticker stop calls = %d, want 1", ticker.stopCalls)
			}
		})
	}
}

// TestRunLiveNonQuitKeyContinues checks that an ordinary key does not end
// the run: a frame must still be drawn after it, and the run only ends once
// a quit key follows.
func TestRunLiveNonQuitKeyContinues(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	reader := &stagedReader{stage1: []byte{'x'}, stage2: []byte{'q'}, advance: make(chan struct{})}

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScene(drawer),
		app.WithConsoleSwitch((&switchSpy{}).switchMode),
		app.WithRawMode((&switchSpy{}).switchMode),
		app.WithInput(reader),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call after the non-quit key")
	recvOrTimeout(t, blitter.calls, testTimeout, "a Blit call after the non-quit key")

	close(reader.advance)

	err := recvOrTimeout(t, done, testTimeout, "Run to return after the quit key")
	if err != nil {
		t.Errorf("Run: %v, want nil", err)
	}
}

// --- live mode: context cancellation, ticks, blit errors, FPS -------------

func TestRunLiveContextCancellationEndsRun(t *testing.T) {
	t.Parallel()

	watchdog, cancelWatchdog := context.WithTimeout(t.Context(), testTimeout)
	defer cancelWatchdog()

	ctx, cancel := context.WithCancel(watchdog)

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScene(drawer),
		app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
		app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call before cancellation")

	cancel()

	err := recvOrTimeout(t, done, testTimeout, "Run to return after context cancellation")
	if err != nil {
		t.Errorf("Run: %v, want nil", err)
	}

	if ticker.stopCalls != 1 {
		t.Errorf("ticker stop calls = %d, want 1", ticker.stopCalls)
	}
}

func TestRunLiveTicksDrawFrames(t *testing.T) {
	t.Parallel()

	const (
		ticksToSend = 3
		fps         = 10
	)

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	done := runAsync(runCtx, liveConfig(fps), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScene(drawer),
		app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
		app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
		app.WithTicker(ticker.new),
		app.WithClock(func() time.Time { return start }),
	)

	for tick := 1; tick <= ticksToSend; tick++ {
		offset := time.Duration(tick) * time.Second
		ticker.ch <- start.Add(offset)

		draw := recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call")
		if draw.elapsed != offset {
			t.Errorf("tick %d: Draw elapsed = %v, want %v", tick, draw.elapsed, offset)
		}

		recvOrTimeout(t, blitter.calls, testTimeout, "a Blit call")
	}

	cancelRun()

	err := recvOrTimeout(t, done, testTimeout, "Run to return")
	if err != nil {
		t.Errorf("Run: %v, want nil", err)
	}

	if ticker.interval != time.Second/fps {
		t.Errorf("ticker interval = %v, want %v", ticker.interval, time.Second/fps)
	}

	if ticker.stopCalls != 1 {
		t.Errorf("ticker stop calls = %d, want 1", ticker.stopCalls)
	}
}

func TestRunLiveBlitError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	blitter.blitErr = errStub
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScene(drawer),
		app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
		app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	err := recvOrTimeout(t, done, testTimeout, "Run to return after a Blit error")
	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}

	if ticker.stopCalls != 1 {
		t.Errorf("ticker stop calls = %d, want 1", ticker.stopCalls)
	}

	select {
	case <-blitter.closed:
	default:
		t.Error("Close was not called after a Blit error")
	}
}

func TestRunLiveFPSGuard(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		fps  int
	}{
		{name: "zero FPS falls back to the default", fps: 0},
		{name: "negative FPS falls back to the default", fps: -1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const defaultFPS = 30

			watchdog, cancelWatchdog := context.WithTimeout(t.Context(), testTimeout)
			defer cancelWatchdog()

			ctx, cancel := context.WithCancel(watchdog)
			cancel()

			blitter := newFakeBlitter(100, 100, 16, 200, "fake")
			ticker := newFakeTicker()

			done := runAsync(ctx, liveConfig(testCase.fps), &bytes.Buffer{},
				app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
				app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
				app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
				app.WithTicker(ticker.new),
			)

			err := recvOrTimeout(t, done, testTimeout, "Run to return on an already-cancelled context")
			if err != nil {
				t.Errorf("Run: %v, want nil", err)
			}

			if ticker.interval != time.Second/defaultFPS {
				t.Errorf("ticker interval = %v, want %v", ticker.interval, time.Second/defaultFPS)
			}
		})
	}
}

// --- live mode: degrading vs aborting --------------------------------

func TestRunLiveConsoleSwitchDegrades(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	var buf bytes.Buffer

	done := runAsync(runCtx, liveConfig(30), &buf,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScene(drawer),
		app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
		app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call despite the degraded console switch")

	cancelRun()

	err := recvOrTimeout(t, done, testTimeout, "Run to return")
	if err != nil {
		t.Errorf("Run: %v, want nil", err)
	}

	got := buf.String()
	if !strings.Contains(got, "warning: console graphics mode unavailable") {
		t.Errorf("stdout = %q, want a console graphics mode warning", got)
	}
}

func TestRunLiveRawModeDegrades(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		sentinel error
	}{
		{name: "not a terminal", sentinel: term.ErrNotTerminal},
		{name: "unsupported platform", sentinel: term.ErrUnsupported},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
			defer cancel()

			runCtx, cancelRun := context.WithCancel(ctx)
			defer cancelRun()

			blitter := newFakeBlitter(100, 100, 16, 200, "fake")
			drawer := newFakeDrawer()
			ticker := newFakeTicker()
			reader := &countingReader{}

			var buf bytes.Buffer

			done := runAsync(runCtx, liveConfig(30), &buf,
				app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
				app.WithScene(drawer),
				app.WithConsoleSwitch((&switchSpy{}).switchMode),
				app.WithRawMode(degradeSwitch(testCase.sentinel)),
				app.WithInput(reader),
				app.WithTicker(ticker.new),
			)

			ticker.ch <- time.Now()

			recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call despite the degraded raw mode")

			// With no working keyboard, cancelling the context is the only
			// way out: this is the Ctrl-C-from-the-shell path taking over
			// for the keys a raw terminal would otherwise deliver.
			cancelRun()

			err := recvOrTimeout(t, done, testTimeout, "Run to return")
			if err != nil {
				t.Errorf("Run: %v, want nil", err)
			}

			if reader.calls != 0 {
				t.Errorf("reader was called %d times, want 0: raw mode never applied", reader.calls)
			}

			got := buf.String()
			if !strings.Contains(got, "warning: raw keyboard mode unavailable") {
				t.Errorf("stdout = %q, want a raw keyboard mode warning", got)
			}
		})
	}
}

func TestRunLiveConsoleSwitchAborts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")

	err := app.Run(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithConsoleSwitch(abortSwitch(errStub)),
	)

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

func TestRunLiveRawModeAborts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")

	err := app.Run(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithConsoleSwitch((&switchSpy{}).switchMode),
		app.WithRawMode(abortSwitch(errStub)),
	)

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

// TestRunLiveRestoreOrder pins the unwind order documented on live: the
// terminal is restored before the console mode, and a failing restore does
// not change Run's result.
func TestRunLiveRestoreOrder(t *testing.T) {
	t.Parallel()

	watchdog, cancelWatchdog := context.WithTimeout(t.Context(), testTimeout)
	defer cancelWatchdog()

	ctx, cancel := context.WithCancel(watchdog)
	cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")

	var order []string

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithConsoleSwitch(orderedRestore(&order, "vt", nil)),
		app.WithRawMode(orderedRestore(&order, "term", errStub)),
		app.WithInput(idleReader{}),
	)

	err := recvOrTimeout(t, done, testTimeout, "Run to return on an already-cancelled context")
	if err != nil {
		t.Errorf("Run: %v, want nil: a failing restore must not change the result", err)
	}

	want := []string{"term", "vt"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Errorf("restore order = %v, want %v", order, want)
	}
}
