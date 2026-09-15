// Package input turns the bytes a raw terminal hands us into key events.
//
// A raw tty delivers an arrow key as three bytes, and a read can land in the
// middle of them, so the decoder is a small state machine that carries a
// partial sequence between calls. It has no notion of a terminal and touches
// no syscalls, which is what keeps it testable off-device.
package input

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// readBufSize is the read buffer for the reader goroutine. A keyboard burst
// is a handful of bytes, so 64 covers a fast key repeat with room to spare.
const readBufSize = 64

// Control bytes we care about. The rest of C0 is passed through as a rune.
const (
	byteCtrlC = 0x03
	byteEsc   = 0x1B
	byteLF    = 0x0A
	byteCR    = 0x0D
)

// Kind labels a key event. Rune means "look at the Rune field"; everything
// else is a named key and carries a zero Rune.
type Kind uint8

// The key kinds uScope recognises. Slice 1 needs quit and the arrows; the
// rest arrive as Rune.
//
//nolint:varnamelen // Up and Esc are the names for these; padding them helps nobody.
const (
	Rune Kind = iota
	Up
	Down
	Left
	Right
	Enter
	Esc
	CtrlC
)

// Key is one decoded key press.
type Key struct {
	Kind Kind
	Rune rune
}

// state is where the decoder is in an escape sequence.
type state uint8

const (
	ground     state = iota // not in a sequence
	afterEsc                // saw ESC
	afterIntro              // saw ESC [ or ESC O
)

// Decoder converts a byte stream into keys. It is stateful and not safe for
// concurrent use: one Decoder per reader.
//
// The zero value is ready to use.
type Decoder struct {
	state state
	keys  []Key
}

// Feed decodes the next chunk of input.
//
// The returned slice is reused on the next call, so consume it before
// feeding again. Callers send the keys straight down a channel, and one
// shared slice keeps the input path free of per-read allocations.
//
// An empty chunk is meaningful rather than a no-op: a raw tty configured
// with VTIME returns zero bytes on a read timeout, and that timeout is what
// tells a pending lone ESC that no sequence is coming after all.
func (d *Decoder) Feed(chunk []byte) []Key {
	d.keys = d.keys[:0]

	if len(chunk) == 0 {
		return d.flush()
	}

	for _, char := range chunk {
		d.step(char)
	}

	return d.keys
}

// flush resolves a half-finished sequence as a bare Esc.
func (d *Decoder) flush() []Key {
	if d.state != ground {
		d.state = ground
		d.keys = append(d.keys, Key{Kind: Esc})
	}

	return d.keys
}

// step advances the state machine by one byte.
func (d *Decoder) step(char byte) {
	switch d.state {
	case afterEsc:
		d.stepAfterEsc(char)
	case afterIntro:
		d.stepAfterIntro(char)
	case ground:
		fallthrough
	default:
		d.stepGround(char)
	}
}

// stepGround handles a byte outside any escape sequence.
func (d *Decoder) stepGround(char byte) {
	switch char {
	case byteEsc:
		d.state = afterEsc
	case byteCtrlC:
		d.keys = append(d.keys, Key{Kind: CtrlC})
	case byteCR, byteLF:
		d.keys = append(d.keys, Key{Kind: Enter})
	default:
		// Bytes above 0x7F are handed back one per rune rather than decoded
		// as UTF-8. Every key uScope binds is ASCII, and multi-byte input
		// can wait until there is something that needs it.
		d.keys = append(d.keys, Key{Kind: Rune, Rune: rune(char)})
	}
}

// stepAfterEsc handles the byte following an ESC.
func (d *Decoder) stepAfterEsc(char byte) {
	if char == '[' || char == 'O' {
		d.state = afterIntro

		return
	}

	// Not an introducer, so the ESC stood alone. Emit it and let this byte
	// start again from the top, which is how ESC followed by a letter reads
	// as two separate presses.
	d.state = ground
	d.keys = append(d.keys, Key{Kind: Esc})
	d.step(char)
}

// stepAfterIntro handles the final byte of a CSI or SS3 sequence.
func (d *Decoder) stepAfterIntro(char byte) {
	d.state = ground

	if kind, ok := arrow(char); ok {
		d.keys = append(d.keys, Key{Kind: kind})

		return
	}

	// Something we do not model, a function key for instance. Report the ESC
	// and drop the rest of the sequence rather than spraying its bytes at the
	// application as if the user had typed them.
	d.keys = append(d.keys, Key{Kind: Esc})
}

// arrow maps the final byte of an arrow sequence to its kind.
func arrow(char byte) (Kind, bool) {
	switch char {
	case 'A':
		return Up, true
	case 'B':
		return Down, true
	case 'C':
		return Right, true
	case 'D':
		return Left, true
	default:
		return Rune, false
	}
}

// Read decodes src until ctx is cancelled or src fails, sending every key to
// out. It is meant to run in its own goroutine.
//
// It never blocks on out for longer than cancellation takes, so the caller
// can drop the channel on the floor at shutdown. On a raw tty with VMIN=0
// and VTIME=1 a read returns at least every 100 ms, which bounds how long
// cancellation takes to be noticed. io.EOF is a clean stop, not an error;
// the app therefore feeds it term.Reader, which never reports a timed-out
// read as EOF the way os.File does.
func Read(ctx context.Context, src io.Reader, out chan<- Key) error {
	var dec Decoder

	buf := make([]byte, readBufSize)

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("input: read cancelled: %w", err)
		}

		count, readErr := src.Read(buf)

		if err := send(ctx, dec.Feed(buf[:count]), out); err != nil {
			return err
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}

			return fmt.Errorf("input: read: %w", readErr)
		}
	}
}

// send pushes keys to out, giving up if the context is cancelled first.
func send(ctx context.Context, keys []Key, out chan<- Key) error {
	for _, key := range keys {
		select {
		case out <- key:
		case <-ctx.Done():
			return fmt.Errorf("input: send cancelled: %w", ctx.Err())
		}
	}

	return nil
}
