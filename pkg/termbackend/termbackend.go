// Package termbackend shows a uScope frame in a terminal window.
//
// It is the half of slice 2 that owns the terminal: the alternate screen,
// the cursor, how big the window currently is, and which encoder turns the
// canvas into bytes. pkg/kitty and pkg/blocks do the encoding and know
// nothing else; this package decides which of them runs and what size
// canvas they are handed.
//
// Two modes. Kitty sends real pixels to terminals that implement the Kitty
// graphics protocol, Ghostty and WezTerm among them, and it works over ssh
// because the pixels travel as escape sequences like everything else.
// Blocks prints one upper-half-block glyph per cell with a foreground and a
// background colour, so two pixels per cell, and works anywhere 24-bit
// colour does.
//
// When stdout is not a terminal the backend runs in pipe mode: no alternate
// screen, no window size to ask for, an assumed 80x24 grid, and frames
// still written. That is what makes "uScope --backend blocks --frames 3 |
// wc -c" a test rather than a hang.
package termbackend

import (
	"errors"
	"fmt"
	"image"
	"io"
	"os"

	"github.com/hyperized/uScope/pkg/blocks"
	"github.com/hyperized/uScope/pkg/kitty"
	"github.com/hyperized/uScope/pkg/winsize"
)

// Terminal control sequences. They are written by hand rather than looked up
// in terminfo because uScope needs five of them and every terminal it can
// possibly run in has spoken these since the VT100.
const (
	enterAlt    = "\x1b[?1049h"
	leaveAlt    = "\x1b[?1049l"
	hideCursor  = "\x1b[?25l"
	showCursor  = "\x1b[?25h"
	clearScreen = "\x1b[2J"
	cursorHome  = "\x1b[H"
	resetSGR    = "\x1b[0m"
	newline     = "\r\n"
)

const (
	// The canvas kitty mode draws into when the caller does not say. The
	// terminal scales it into the cells, so this is a rendering resolution
	// rather than a window size, and 720p is what the uConsole's own screen
	// is.
	defaultCanvasWidth  = 1280
	defaultCanvasHeight = 720

	// The grid assumed in pipe mode. Nothing can measure a pipe, and 80x24
	// is the size every terminal has defaulted to since the VT100.
	pipeCols = 80
	pipeRows = 24

	// A cell in blocks mode is one pixel wide and two tall, because the
	// glyph splits it in half top and bottom.
	pixelsPerCell = 2

	// Cell shape assumed when the terminal reports its size in cells but not
	// in pixels, which is most of them. A cell is roughly twice as tall as
	// it is wide.
	fallbackCellWidth  = 1
	fallbackCellHeight = 2

	// resizeBuffer is 1 because a burst of SIGWINCH says exactly what a
	// single one says: measure again. Dropping the rest is the point.
	resizeBuffer = 1
)

// Sentinel errors.
var (
	// ErrSize means the image handed to Blit is not the size Size asked for.
	// The run loop rebuilds its canvas when Size changes, so this is a bug
	// in the caller rather than a resize that got away.
	ErrSize = errors.New("termbackend: image size does not match the terminal")

	// ErrClosed means Blit was called after Close.
	ErrClosed = errors.New("termbackend: backend is closed")
)

// Mode is which encoder draws the frame.
type Mode uint8

// The two terminal modes.
const (
	// Blocks is the half-block renderer, which works on any colour terminal.
	Blocks Mode = iota

	// Kitty is the graphics protocol, which draws real pixels.
	Kitty
)

// Terminal is a Backend that draws into a terminal.
//
// It is not safe for concurrent use: it owns the output stream and the
// reused encoder buffers, so the run loop is its only caller.
type Terminal struct {
	out    io.Writer
	fd     uintptr
	mode   Mode
	canvas image.Point

	// wantAlt is what the caller asked for; entered is what actually
	// happened. They differ in pipe mode and for a single still frame,
	// where changing terminal state would be rude.
	wantAlt bool
	entered bool
	piped   bool
	closed  bool

	measure func(fd uintptr) (winsize.Size, error)
	notify  func(ch chan<- os.Signal)
	stop    func(ch chan<- os.Signal)
	resized chan os.Signal

	size    winsize.Size
	encoder *kitty.Encoder
	blocks  *blocks.Renderer
}

// Open takes over the terminal and returns a backend that draws on it.
//
// It writes nothing when stdout is not a terminal, and nothing when the
// caller turns the alternate screen off, so a single frame can be left on
// screen the way --test-pattern leaves one on the framebuffer.
//
// The returned Terminal must be closed, or the shell is left on the
// alternate screen with no cursor.
func Open(opts ...Option) (*Terminal, error) {
	term := &Terminal{
		out:     os.Stdout,
		fd:      os.Stdout.Fd(),
		mode:    Blocks,
		canvas:  image.Pt(defaultCanvasWidth, defaultCanvasHeight),
		wantAlt: true,
		measure: winsize.Get,
		notify:  notifyResize,
		stop:    stopResize,
		encoder: kitty.New(),
		blocks:  blocks.New(),
	}

	for _, opt := range opts {
		opt(term)
	}

	if err := term.firstMeasure(); err != nil {
		return nil, err
	}

	if err := term.enter(); err != nil {
		return nil, err
	}

	term.watch()

	return term, nil
}

// Size reports the canvas this terminal wants, in pixels.
//
// Blocks mode reports the cell grid doubled in height, because the glyph
// stacks two pixels in one cell, so the renderer never has to scale. Kitty
// mode reports the fixed canvas and lets the terminal do the scaling, since
// it can resample better than a nearest-neighbour loop here would.
//
// This is where a pending resize is picked up, so the run loop calling Size
// once a frame is what keeps the canvas in step with the window.
func (t *Terminal) Size() (int, int) {
	t.drainResize()

	return t.canvasSize()
}

// Blit draws one frame.
func (t *Terminal) Blit(img *image.RGBA) error {
	if t.closed {
		return ErrClosed
	}

	width, height := t.canvasSize()
	if bounds := img.Bounds(); bounds.Dx() != width || bounds.Dy() != height {
		return fmt.Errorf("%w: got %dx%d, want %dx%d",
			ErrSize, img.Bounds().Dx(), img.Bounds().Dy(), width, height)
	}

	cols, rows := t.cells()

	if t.mode == Kitty {
		// The image is placed at the cursor, and the terminal leaves the
		// cursor past it, so a running loop has to come home between
		// frames. Off the alternate screen it must not: a single frame
		// belongs where the shell left the cursor, and homing would draw
		// over whatever the user already has on screen.
		if t.entered {
			if err := t.write(cursorHome); err != nil {
				return err
			}
		}

		if err := t.encoder.Frame(t.out, img, cols, rows); err != nil {
			return fmt.Errorf("termbackend: kitty frame: %w", err)
		}

		return nil
	}

	if err := t.blocks.Frame(t.out, img, cols, rows); err != nil {
		return fmt.Errorf("termbackend: blocks frame: %w", err)
	}

	return nil
}

// Close puts the terminal back and is safe to call twice.
//
// What it undoes depends on what Open did. After the alternate screen it
// deletes the kitty images, shows the cursor and hands the shell back its
// own screen. Without it the frame is meant to stay visible, so all it does
// is reset the colours and end the line, which stops the shell prompt
// landing on top of the last row.
func (t *Terminal) Close() error {
	if t.closed {
		return nil
	}

	t.closed = true
	t.unwatch()

	if !t.entered {
		return t.write(resetSGR + newline)
	}

	var err error

	if t.mode == Kitty {
		err = t.encoder.Close(t.out)
	}

	if restoreErr := t.write(showCursor + leaveAlt + resetSGR); err == nil {
		err = restoreErr
	}

	return err
}

// firstMeasure asks the terminal how big it is, and decides whether there is
// a terminal at all.
//
// A descriptor that is not a terminal is the ordinary case rather than a
// failure: it means stdout is a pipe or a file, which is how uScope gets
// tested without a window. Anything else really is a failure.
func (t *Terminal) firstMeasure() error {
	size, err := t.measure(t.fd)
	if err == nil {
		t.size = size

		return nil
	}

	if errors.Is(err, winsize.ErrNotTerminal) || errors.Is(err, winsize.ErrUnsupported) {
		t.piped = true
		t.size = winsize.Size{Cols: pipeCols, Rows: pipeRows}

		return nil
	}

	return fmt.Errorf("termbackend: measuring the terminal: %w", err)
}

// enter switches to the alternate screen, so the shell's scrollback is still
// there when uScope exits.
func (t *Terminal) enter() error {
	if t.piped || !t.wantAlt {
		return nil
	}

	if err := t.write(enterAlt + hideCursor + clearScreen); err != nil {
		return err
	}

	t.entered = true

	return nil
}

// watch subscribes to window-size changes. There is nothing to watch on a
// pipe, and no window either.
func (t *Terminal) watch() {
	if t.piped {
		return
	}

	t.resized = make(chan os.Signal, resizeBuffer)
	t.notify(t.resized)
}

// unwatch stops the subscription so the signal package drops its reference
// to the channel.
func (t *Terminal) unwatch() {
	if t.resized == nil {
		return
	}

	t.stop(t.resized)
	t.resized = nil
}

// drainResize takes every pending SIGWINCH and measures once.
//
// The measurement error is dropped on purpose. A window being resized can
// fail an ioctl for a moment, and the last known size draws a slightly wrong
// frame where returning an error would end the program.
func (t *Terminal) drainResize() {
	pending := false

	for {
		select {
		case <-t.resized:
			pending = true
		default:
			if pending {
				if size, err := t.measure(t.fd); err == nil {
					t.size = size
				}
			}

			return
		}
	}
}

// canvasSize is Size without the resize check, so Blit compares against the
// same numbers the run loop was given rather than a size that arrived in
// between.
func (t *Terminal) canvasSize() (int, int) {
	if t.mode == Kitty {
		return t.canvas.X, t.canvas.Y
	}

	return atLeastOne(t.size.Cols), atLeastOne(t.size.Rows) * pixelsPerCell
}

// cells is the character grid the frame is drawn into.
//
// Kitty mode fits the image into one row less than the window has. After
// drawing, the terminal leaves the cursor below the image, and a cursor
// pushed past the last row scrolls the screen, which would walk the picture
// up by a row on every frame. Blocks mode needs no such margin: it ends the
// last row without a newline, so nothing ever moves.
func (t *Terminal) cells() (int, int) {
	cols, rows := atLeastOne(t.size.Cols), atLeastOne(t.size.Rows)

	if t.mode != Kitty {
		return cols, rows
	}

	cellWidth, cellHeight := t.cellPixels()

	return fitCells(t.canvas.X, t.canvas.Y, cols, atLeastOne(rows-1), cellWidth, cellHeight)
}

// cellPixels is how many pixels a character cell covers.
//
// Most terminals report zero for the pixel fields of TIOCGWINSZ, so the
// fallback is the common case rather than the exception.
func (t *Terminal) cellPixels() (int, int) {
	cols, rows := t.size.Cols, t.size.Rows
	if cols < 1 || rows < 1 || t.size.XPixels < cols || t.size.YPixels < rows {
		return fallbackCellWidth, fallbackCellHeight
	}

	return t.size.XPixels / cols, t.size.YPixels / rows
}

// write sends one string to the terminal.
func (t *Terminal) write(text string) error {
	if _, err := io.WriteString(t.out, text); err != nil {
		return fmt.Errorf("termbackend: writing to the terminal: %w", err)
	}

	return nil
}

// fitCells works out the cell area a canvas should be scaled into so it
// keeps its shape.
//
// Cells are not square, so the aspect ratio that matters is the one in
// pixels: a canvas cols wide covers cols*cellWidth pixels across, and the
// rows are chosen so the height in pixels stays in proportion. When that
// comes out taller than the window, the fit is done the other way round and
// the width follows instead.
func fitCells(canvasWidth, canvasHeight, cols, rows, cellWidth, cellHeight int) (int, int) {
	fitRows := atLeastOne(divRound(cols*cellWidth*canvasHeight, canvasWidth*cellHeight))
	if fitRows <= rows {
		return cols, fitRows
	}

	fitCols := min(atLeastOne(divRound(rows*cellHeight*canvasWidth, canvasHeight*cellWidth)), cols)

	return fitCols, rows
}

// divRound divides and rounds to nearest, so a fit that lands on half a cell
// does not always lose it.
func divRound(numerator, denominator int) int {
	return (numerator + denominator/pixelsPerCell) / denominator
}

// atLeastOne keeps a terminal that reports nonsense from producing a canvas
// with no pixels in it, which canvas.New would reject.
func atLeastOne(value int) int {
	if value < 1 {
		return 1
	}

	return value
}
