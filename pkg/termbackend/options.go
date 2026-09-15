package termbackend

import (
	"image"
	"io"
	"os"

	"github.com/hyperized/uScope/pkg/winsize"
)

// Option changes one thing about a Terminal. Defaults live in Open, so an
// option that is handed a value it cannot use leaves the default alone
// rather than half-configuring the backend.
type Option func(*Terminal)

// WithWriter sends the frames somewhere other than os.Stdout. A nil writer
// is ignored.
func WithWriter(dst io.Writer) Option {
	return func(t *Terminal) {
		if dst != nil {
			t.out = dst
		}
	}
}

// WithDescriptor is the file descriptor the window size is measured on. It
// is separate from the writer because a test writes to a buffer while
// measuring a fake, and because stdout can be redirected while the terminal
// is still there.
func WithDescriptor(fd uintptr) Option {
	return func(t *Terminal) { t.fd = fd }
}

// WithMode picks the encoder. An unknown mode is ignored, so a caller that
// computes one cannot silently land somewhere between the two.
func WithMode(mode Mode) Option {
	return func(t *Terminal) {
		if mode == Blocks || mode == Kitty {
			t.mode = mode
		}
	}
}

// WithCanvas sets the pixel size kitty mode renders at. It has no effect in
// blocks mode, where the cell grid decides the size. A size with a
// non-positive side is ignored.
func WithCanvas(size image.Point) Option {
	return func(t *Terminal) {
		if size.X > 0 && size.Y > 0 {
			t.canvas = size
		}
	}
}

// WithAltScreen turns the alternate screen off, which leaves a single frame
// on the terminal the way --test-pattern leaves one on the framebuffer.
func WithAltScreen(enabled bool) Option {
	return func(t *Terminal) { t.wantAlt = enabled }
}

// WithMeasure replaces the window-size lookup, which is how the sizing is
// tested without a terminal.
func WithMeasure(measure func(fd uintptr) (winsize.Size, error)) Option {
	return func(t *Terminal) {
		if measure != nil {
			t.measure = measure
		}
	}
}

// WithSignals replaces the SIGWINCH subscription, so a test can deliver a
// resize itself instead of asking the operating system for one.
func WithSignals(notify, stop func(ch chan<- os.Signal)) Option {
	return func(t *Terminal) {
		if notify != nil && stop != nil {
			t.notify, t.stop = notify, stop
		}
	}
}
