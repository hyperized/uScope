package termbackend_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"os"
	"strings"
	"testing"

	"github.com/hyperized/uScope/pkg/termbackend"
	"github.com/hyperized/uScope/pkg/winsize"
)

// errStub is a sentinel used wherever a test only needs some non-nil
// error, matching the convention in internal/app's test files.
var errStub = errors.New("stub")

// Terminal control sequences, spelled out here rather than imported since
// this file is a separate package and cannot see termbackend's unexported
// constants. Values are copied from the brief termbackend was built
// against.
const (
	enterAlt    = "\x1b[?1049h"
	leaveAlt    = "\x1b[?1049l"
	hideCursor  = "\x1b[?25l"
	showCursor  = "\x1b[?25h"
	clearScreen = "\x1b[2J"
	cursorHome  = "\x1b[H"
	resetSGR    = "\x1b[0m"
	newline     = "\r\n"

	kittyMarker = "\x1b_G"
)

// Geometry constants shared across the tests below, named so mnd does not
// flag them.
const (
	pipeCols = 80
	pipeRows = 24

	defaultCanvasWidth  = 1280
	defaultCanvasHeight = 720

	// A terminal geometry distinct from the pipe-mode defaults, so a test
	// failure cannot be confused with termbackend silently falling back to
	// pipe mode.
	termCols = 100
	termRows = 40

	// A second, different geometry used by the resize tests, so "the size
	// changed" is unambiguous.
	resizedCols = 132
	resizedRows = 50

	// A small custom canvas for kitty mode, distinct from the default so a
	// test proves WithCanvas actually took effect.
	customCanvasWidth  = 640
	customCanvasHeight = 360

	// A file descriptor value no real terminal would have, so a test that
	// asserts on it cannot be confused with the fd Open defaults to.
	distinctDescriptor = 7
)

// measureStub stands in for winsize.Get. A test points it at a fixed size
// or error, and the resize tests repoint it mid-test to whatever the next
// measurement should report.
type measureStub struct {
	size winsize.Size
	err  error
}

func (m *measureStub) get(uintptr) (winsize.Size, error) {
	return m.size, m.err
}

// signalCapture stands in for the real SIGWINCH subscription. Open hands
// it the channel a run loop would read resizes from, which lets a test
// deliver one itself instead of waiting on the kernel.
type signalCapture struct {
	ch    chan<- os.Signal
	stops int
}

func (s *signalCapture) notify(ch chan<- os.Signal) { s.ch = ch }
func (s *signalCapture) stop(chan<- os.Signal)      { s.stops++ }

// fakeSignal is the smallest possible os.Signal: just enough to put
// something on the resize channel without depending on a real signal
// number, which the fake channel never needed in the first place.
type fakeSignal struct{}

func (fakeSignal) String() string { return "fake resize" }
func (fakeSignal) Signal()        {}

// afterNWriter succeeds for its first ok calls to Write and fails every
// call after that with errStub. It lets a test fail one specific write in
// a sequence (Open's enter sequence, a Blit, a Close) without having to
// know its exact byte offset.
type afterNWriter struct {
	ok    int
	calls int
}

func (w *afterNWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.calls > w.ok {
		return 0, errStub
	}

	return len(data), nil
}

// wrapStub wraps errStub with some context, the way a real measure or
// writer error would arrive.
func wrapStub(context string) error {
	return fmt.Errorf("%s: %w", context, errStub)
}

// --- pipe mode -----------------------------------------------------------

// TestOpen_PipeMode covers both reasons a real Open ends up in pipe mode:
// stdout is not a terminal, or the platform has no way to ask at all. Both
// must behave identically, so the rest of the smoke test lives in the same
// table.
func TestOpen_PipeMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "not a terminal", err: fmt.Errorf("measuring: %w", winsize.ErrNotTerminal)},
		{name: "unsupported platform", err: fmt.Errorf("measuring: %w", winsize.ErrUnsupported)},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			checkPipeModeSmokeTest(t, tcase.err)
		})
	}
}

// checkPipeModeSmokeTest runs Open, Size, Blit and Close against a measure
// that fails with measureErr, and checks the whole round trip behaves as
// pipe mode: nothing written by Open, the 80x24 grid, a successful Blit at
// that size, and a Close that writes only the reset. Split out of
// TestOpen_PipeMode to keep that test under the cognitive-complexity limit.
func checkPipeModeSmokeTest(t *testing.T, measureErr error) {
	t.Helper()

	var buf bytes.Buffer

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMeasure((&measureStub{err: measureErr}).get),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if buf.Len() != 0 {
		t.Fatalf("Open wrote %d bytes in pipe mode, want none", buf.Len())
	}

	gotWidth, gotHeight := term.Size()
	if gotWidth != pipeCols || gotHeight != pipeRows*2 {
		t.Fatalf("Size() = (%d, %d), want (%d, %d)", gotWidth, gotHeight, pipeCols, pipeRows*2)
	}

	img := image.NewRGBA(image.Rect(0, 0, gotWidth, gotHeight))
	if err := term.Blit(img); err != nil {
		t.Fatalf("Blit: %v", err)
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := buf.String()
	if !strings.HasSuffix(got, resetSGR+newline) {
		t.Errorf("stream ends with %q, want it to end with %q", got, resetSGR+newline)
	}

	if strings.Contains(got, enterAlt) {
		t.Errorf("piped stream contains the alternate-screen sequence: %q", got)
	}
}

func TestOpen_MeasurementFailure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMeasure((&measureStub{err: wrapStub("ioctl")}).get),
	)
	if !errors.Is(err, errStub) {
		t.Errorf("Open() error = %v, want wrapping %v", err, errStub)
	}

	if term != nil {
		t.Error("Open() returned a non-nil Terminal alongside an error")
	}
}

// --- terminal mode, blocks -------------------------------------------------

func TestOpen_TerminalMode(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	wantEnter := enterAlt + hideCursor + clearScreen
	if got := buf.String(); got != wantEnter {
		t.Fatalf("Open wrote %q, want exactly %q", got, wantEnter)
	}

	gotWidth, gotHeight := term.Size()
	if gotWidth != termCols || gotHeight != termRows*2 {
		t.Fatalf("Size() = (%d, %d), want (%d, %d)", gotWidth, gotHeight, termCols, termRows*2)
	}

	buf.Reset()

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	wantClose := showCursor + leaveAlt + resetSGR
	if got := buf.String(); got != wantClose {
		t.Errorf("Close wrote %q, want exactly %q", got, wantClose)
	}
}

func TestOpen_AltScreenDisabled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
		termbackend.WithAltScreen(false),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if buf.Len() != 0 {
		t.Fatalf("Open wrote %d bytes with the alternate screen disabled, want none", buf.Len())
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	wantClose := resetSGR + newline
	if got := buf.String(); got != wantClose {
		t.Errorf("Close wrote %q, want exactly %q", got, wantClose)
	}
}

// --- kitty mode ------------------------------------------------------------

func TestKitty_Size(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       []termbackend.Option
		wantWidth  int
		wantHeight int
	}{
		{
			name:       "default canvas",
			opts:       nil,
			wantWidth:  defaultCanvasWidth,
			wantHeight: defaultCanvasHeight,
		},
		{
			name:       "canvas set explicitly",
			opts:       []termbackend.Option{termbackend.WithCanvas(image.Pt(customCanvasWidth, customCanvasHeight))},
			wantWidth:  customCanvasWidth,
			wantHeight: customCanvasHeight,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			opts := append([]termbackend.Option{
				termbackend.WithWriter(&buf),
				termbackend.WithMode(termbackend.Kitty),
				termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
			}, tcase.opts...)

			term, err := termbackend.Open(opts...)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			gotWidth, gotHeight := term.Size()
			if gotWidth != tcase.wantWidth || gotHeight != tcase.wantHeight {
				t.Errorf("Size() = (%d, %d), want (%d, %d)", gotWidth, gotHeight, tcase.wantWidth, tcase.wantHeight)
			}
		})
	}
}

func TestKitty_Blit(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMode(termbackend.Kitty),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	buf.Reset()

	width, height := term.Size()

	if err := term.Blit(image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("Blit: %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, cursorHome) {
		t.Fatalf("Blit wrote %q, want it to start with the cursor-home sequence %q", got, cursorHome)
	}

	afterHome := got[len(cursorHome):]
	if !strings.HasPrefix(afterHome, kittyMarker) {
		t.Errorf("after cursor-home, Blit wrote %q, want it to continue with a kitty block %q", afterHome, kittyMarker)
	}
}

// TestKitty_CloseDeletesBeforeRestore checks the order Close uses to undo
// a kitty session: the terminal has to be told to forget the images before
// the alternate screen goes away, or the delete would land on whatever
// screen the shell switched back to.
func TestKitty_CloseDeletesBeforeRestore(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMode(termbackend.Kitty),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	width, height := term.Size()
	if err := term.Blit(image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("Blit: %v", err)
	}

	buf.Reset()

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := buf.String()

	deleteIdx := strings.Index(got, kittyMarker)
	restoreIdx := strings.Index(got, leaveAlt)

	if deleteIdx < 0 {
		t.Fatalf("Close wrote %q, want it to contain a kitty block", got)
	}

	if restoreIdx < 0 {
		t.Fatalf("Close wrote %q, want it to contain the restore sequence", got)
	}

	if deleteIdx > restoreIdx {
		t.Errorf("Close wrote the restore sequence before the kitty delete block: %q", got)
	}
}

// --- Blit and Close edge cases, both modes ---------------------------------

func openFor(t *testing.T, buf *bytes.Buffer, mode termbackend.Mode) *termbackend.Terminal {
	t.Helper()

	term, err := termbackend.Open(
		termbackend.WithWriter(buf),
		termbackend.WithMode(mode),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return term
}

func TestBlit_SizeMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mode   termbackend.Mode
		width  int
		height int
	}{
		{name: "blocks: too small", mode: termbackend.Blocks, width: termCols - 1, height: termRows * 2},
		{name: "blocks: too large", mode: termbackend.Blocks, width: termCols + 1, height: termRows * 2},
		{name: "kitty: too small", mode: termbackend.Kitty, width: defaultCanvasWidth - 1, height: defaultCanvasHeight},
		{name: "kitty: too large", mode: termbackend.Kitty, width: defaultCanvasWidth, height: defaultCanvasHeight + 1},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			term := openFor(t, &buf, tcase.mode)

			img := image.NewRGBA(image.Rect(0, 0, tcase.width, tcase.height))

			err := term.Blit(img)
			if !errors.Is(err, termbackend.ErrSize) {
				t.Errorf("Blit() error = %v, want ErrSize", err)
			}
		})
	}
}

func TestBlit_AfterClose(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	term := openFor(t, &buf, termbackend.Blocks)

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	width, height := term.Size()

	err := term.Blit(image.NewRGBA(image.Rect(0, 0, width, height)))
	if !errors.Is(err, termbackend.ErrClosed) {
		t.Errorf("Blit() after Close error = %v, want ErrClosed", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	term := openFor(t, &buf, termbackend.Blocks)

	if err := term.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	written := buf.Len()

	if err := term.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if buf.Len() != written {
		t.Errorf("second Close wrote %d more bytes, want none", buf.Len()-written)
	}
}

// --- resize ------------------------------------------------------------

func TestResize_PicksUpNewSize(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	measure := &measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}
	signals := &signalCapture{}

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMeasure(measure.get),
		termbackend.WithSignals(signals.notify, signals.stop),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	width, height := term.Size()
	if width != termCols || height != termRows*2 {
		t.Fatalf("Size() before resize = (%d, %d), want (%d, %d)", width, height, termCols, termRows*2)
	}

	measure.size = winsize.Size{Cols: resizedCols, Rows: resizedRows}

	signals.ch <- fakeSignal{}

	width, height = term.Size()
	if width != resizedCols || height != resizedRows*2 {
		t.Fatalf("Size() after resize = (%d, %d), want (%d, %d)", width, height, resizedCols, resizedRows*2)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	if err := term.Blit(img); err != nil {
		t.Errorf("Blit at the new size: %v", err)
	}
}

// TestResize_MeasurementFailureKeepsOldSize covers the case a window being
// dragged hits: the re-measurement after a SIGWINCH can fail an ioctl for a
// moment, and the last known size has to survive that rather than the
// program erroring out or panicking.
func TestResize_MeasurementFailureKeepsOldSize(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	measure := &measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}
	signals := &signalCapture{}

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMeasure(measure.get),
		termbackend.WithSignals(signals.notify, signals.stop),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	measure.err = errStub

	signals.ch <- fakeSignal{}

	width, height := term.Size()
	if width != termCols || height != termRows*2 {
		t.Errorf("Size() after a failed re-measurement = (%d, %d), want the old (%d, %d)",
			width, height, termCols, termRows*2)
	}
}

// --- options ----------------------------------------------------------

// TestOption_NilValuesAreIgnored applies a working option, then a nil one
// meant to override it, and checks the working value is still in effect.
// A nil writer or measure that actually took hold would panic on the next
// call, and a nil signal pair would leave a resize with nowhere to land;
// none of that happens, which is the proof each nil was ignored.
func TestOption_NilValuesAreIgnored(t *testing.T) {
	t.Parallel()

	t.Run("nil writer", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		_, err := termbackend.Open(
			termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
			termbackend.WithWriter(&buf),
			termbackend.WithWriter(nil),
		)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		if buf.Len() == 0 {
			t.Error("nothing was written to the buffer set before the nil writer, want the buffer still in use")
		}
	})

	t.Run("nil measure", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		term, err := termbackend.Open(
			termbackend.WithWriter(&buf),
			termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
			termbackend.WithMeasure(nil),
		)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		width, height := term.Size()
		if width != termCols || height != termRows*2 {
			t.Errorf("Size() = (%d, %d), want (%d, %d) from the measure set before the nil one",
				width, height, termCols, termRows*2)
		}
	})

	t.Run("nil signal pair", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		measure := &measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}
		signals := &signalCapture{}

		term, err := termbackend.Open(
			termbackend.WithWriter(&buf),
			termbackend.WithMeasure(measure.get),
			termbackend.WithSignals(signals.notify, signals.stop),
			termbackend.WithSignals(nil, nil),
		)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		measure.size = winsize.Size{Cols: resizedCols, Rows: resizedRows}

		signals.ch <- fakeSignal{}

		width, height := term.Size()
		if width != resizedCols || height != resizedRows*2 {
			t.Errorf("Size() after resize = (%d, %d), want (%d, %d); the fake signal pair was not still wired up",
				width, height, resizedCols, resizedRows*2)
		}
	})
}

// TestOption_InvalidModeKeepsDefault sets Kitty and then hands WithMode an
// out-of-range value. If the invalid value took hold, cells would no
// longer equal Kitty, and Size would report the blocks-mode cell grid
// instead of the fixed canvas.
func TestOption_InvalidModeKeepsDefault(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	const outOfRangeMode = 99

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
		termbackend.WithMode(termbackend.Kitty),
		termbackend.WithMode(termbackend.Mode(outOfRangeMode)),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	width, height := term.Size()
	if width != defaultCanvasWidth || height != defaultCanvasHeight {
		t.Errorf("Size() = (%d, %d), want the kitty canvas (%d, %d); an invalid Mode value was accepted",
			width, height, defaultCanvasWidth, defaultCanvasHeight)
	}
}

func TestOption_InvalidCanvasKeepsDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		bad  image.Point
	}{
		{name: "zero width", bad: image.Pt(0, customCanvasHeight)},
		{name: "zero height", bad: image.Pt(customCanvasWidth, 0)},
		{name: "negative width", bad: image.Pt(-customCanvasWidth, customCanvasHeight)},
		{name: "negative height", bad: image.Pt(customCanvasWidth, -customCanvasHeight)},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			term, err := termbackend.Open(
				termbackend.WithWriter(&buf),
				termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
				termbackend.WithMode(termbackend.Kitty),
				termbackend.WithCanvas(tcase.bad),
			)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			width, height := term.Size()
			if width != defaultCanvasWidth || height != defaultCanvasHeight {
				t.Errorf("Size() = (%d, %d), want the untouched default (%d, %d)",
					width, height, defaultCanvasWidth, defaultCanvasHeight)
			}
		})
	}
}

// --- write failures ------------------------------------------------------

func TestOpen_WriterFailsImmediately(t *testing.T) {
	t.Parallel()

	term, err := termbackend.Open(
		termbackend.WithWriter(&afterNWriter{ok: 0}),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
	)
	if !errors.Is(err, errStub) {
		t.Errorf("Open() error = %v, want wrapping %v", err, errStub)
	}

	if term != nil {
		t.Error("Open() returned a non-nil Terminal alongside an error")
	}
}

func TestBlit_WriterFails(t *testing.T) {
	t.Parallel()

	writer := &afterNWriter{ok: 1}

	term, err := termbackend.Open(
		termbackend.WithWriter(writer),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	width, height := term.Size()

	err = term.Blit(image.NewRGBA(image.Rect(0, 0, width, height)))
	if !errors.Is(err, errStub) {
		t.Errorf("Blit() error = %v, want wrapping %v", err, errStub)
	}
}

func TestClose_WriterFails(t *testing.T) {
	t.Parallel()

	writer := &afterNWriter{ok: 1}

	term, err := termbackend.Open(
		termbackend.WithWriter(writer),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	err = term.Close()
	if !errors.Is(err, errStub) {
		t.Errorf("Close() error = %v, want wrapping %v", err, errStub)
	}
}

// Blit in kitty mode writes the cursor-home sequence and then the image
// block as two separate writes, so a failing writer can catch either one
// independently. TestKitty_Blit_CursorHomeWriteFails fails the first,
// TestKitty_Blit_FrameWriteFails the second.
func TestKitty_Blit_CursorHomeWriteFails(t *testing.T) {
	t.Parallel()

	writer := &afterNWriter{ok: 1}

	term, err := termbackend.Open(
		termbackend.WithWriter(writer),
		termbackend.WithMode(termbackend.Kitty),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	width, height := term.Size()

	err = term.Blit(image.NewRGBA(image.Rect(0, 0, width, height)))
	if !errors.Is(err, errStub) {
		t.Errorf("Blit() error = %v, want wrapping %v", err, errStub)
	}
}

func TestKitty_Blit_FrameWriteFails(t *testing.T) {
	t.Parallel()

	writer := &afterNWriter{ok: 2}

	term, err := termbackend.Open(
		termbackend.WithWriter(writer),
		termbackend.WithMode(termbackend.Kitty),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	width, height := term.Size()

	err = term.Blit(image.NewRGBA(image.Rect(0, 0, width, height)))
	if !errors.Is(err, errStub) {
		t.Errorf("Blit() error = %v, want wrapping %v", err, errStub)
	}
}

// TestKitty_Blit_SkipsCursorHomeWithoutAltScreen covers the other side of
// the cursor-home write: off the alternate screen a frame is meant to land
// wherever the shell already left the cursor, so Blit must not home first.
func TestKitty_Blit_SkipsCursorHomeWithoutAltScreen(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	term, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMode(termbackend.Kitty),
		termbackend.WithMeasure((&measureStub{size: winsize.Size{Cols: termCols, Rows: termRows}}).get),
		termbackend.WithAltScreen(false),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	width, height := term.Size()
	if err := term.Blit(image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("Blit: %v", err)
	}

	if got := buf.String(); !strings.HasPrefix(got, kittyMarker) {
		t.Errorf("Blit wrote %q, want it to start directly with a kitty block, no cursor-home", got)
	}
}

// --- descriptor option ---------------------------------------------------

// TestOption_WithDescriptor checks that the fd Open measures on is the one
// WithDescriptor set, not the default. The measure seam is the only thing
// that ever sees it, so a stub that records its argument is the only way
// to observe it.
func TestOption_WithDescriptor(t *testing.T) {
	t.Parallel()

	var gotFD uintptr

	measure := func(fd uintptr) (winsize.Size, error) {
		gotFD = fd

		return winsize.Size{Cols: termCols, Rows: termRows}, nil
	}

	var buf bytes.Buffer

	_, err := termbackend.Open(
		termbackend.WithWriter(&buf),
		termbackend.WithMeasure(measure),
		termbackend.WithDescriptor(distinctDescriptor),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if gotFD != distinctDescriptor {
		t.Errorf("measure was called with fd %d, want %d", gotFD, distinctDescriptor)
	}
}
