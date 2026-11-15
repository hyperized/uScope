package statefile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hyperized/uScope/internal/control"
)

// DefaultDelay is how long a Saver waits after a change before it writes.
// Holding ] on the phone sends a burst of tilt steps; one write for the whole
// burst is the point, and three quarters of a second is short enough that a
// restart right after the operator lets go still finds the result on disk.
const DefaultDelay = 750 * time.Millisecond

// DefaultMinGap is the least time between two writes after the first. The
// file usually sits on an SD card, and a script that flips a setting back and
// forth would otherwise cost a synced create and rename every second for as
// long as it runs. The flush on shutdown ignores it, so the last change is
// still kept.
const DefaultMinGap = 10 * time.Second

// tempFile is the part of *os.File a write needs. It is an interface so a
// test can fail each step in turn.
type tempFile interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
}

// Saver writes the latest state it was handed to one file, off the caller's
// goroutine, coalescing a burst of changes into one write.
//
// Save is safe to call from any goroutine and never blocks on the disk. Run
// does the writing and must run on exactly one goroutine. A write that fails
// is reported once on the warning writer; the failures after it stay quiet
// until a write succeeds again, so a full volume costs one line, not one per
// change.
type Saver struct {
	path   string
	warn   io.Writer
	delay  time.Duration
	minGap time.Duration

	now        func() time.Time
	after      func(time.Duration) <-chan time.Time
	createTemp func(dir, pattern string) (tempFile, error)
	rename     func(from, to string) error
	remove     func(name string) error

	// wake has room for one signal, so Save never blocks and a burst of
	// calls leaves one wake-up behind rather than one each.
	wake chan struct{}

	mu      sync.Mutex
	pending control.State
	dirty   bool

	// written, failing and lastWrite belong to Run's goroutine alone.
	written   []byte
	failing   bool
	lastWrite time.Time
}

// Option configures a Saver.
type Option func(*Saver)

// WithDelay sets how long Run waits after a change before writing. Zero or
// less leaves DefaultDelay in place.
func WithDelay(delay time.Duration) Option {
	return func(s *Saver) {
		if delay > 0 {
			s.delay = delay
		}
	}
}

// WithMinGap sets the least time between two writes after the first. Zero or
// less leaves DefaultMinGap in place.
func WithMinGap(gap time.Duration) Option {
	return func(s *Saver) {
		if gap > 0 {
			s.minGap = gap
		}
	}
}

// New returns a Saver for the file at path that reports a failed write on
// warn. Nothing is written until Save is called and Run is running.
func New(path string, warn io.Writer, opts ...Option) *Saver {
	saver := &Saver{
		path:       path,
		warn:       warn,
		delay:      DefaultDelay,
		minGap:     DefaultMinGap,
		now:        time.Now,
		after:      time.After,
		createTemp: createTemp,
		rename:     os.Rename,
		remove:     os.Remove,
		wake:       make(chan struct{}, 1),
	}

	for _, opt := range opts {
		opt(saver)
	}

	return saver
}

// createTemp is os.CreateTemp behind the tempFile interface.
//
//nolint:ireturn,wrapcheck // the interface is the seam, and write wraps the error.
func createTemp(dir, pattern string) (tempFile, error) {
	return os.CreateTemp(dir, pattern)
}

// Save records state as the one to write next and returns at once.
func (s *Saver) Save(state control.State) {
	s.mu.Lock()
	s.pending, s.dirty = state, true
	s.mu.Unlock()

	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run writes each change Save records, one write per burst, until ctx is
// done. A change still waiting when ctx ends is written before Run returns,
// which is what keeps the last command before a SIGTERM.
func (s *Saver) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.flush()

			return
		case <-s.wake:
		}

		select {
		case <-ctx.Done():
		case <-s.after(s.delay):
			s.holdOff(ctx)
		}

		s.flush()
	}
}

// holdOff waits out the rest of the minimum gap since the last write, or
// until ctx ends, whichever comes first.
func (s *Saver) holdOff(ctx context.Context) {
	if s.lastWrite.IsZero() {
		return
	}

	wait := s.minGap - s.now().Sub(s.lastWrite)
	if wait <= 0 {
		return
	}

	select {
	case <-ctx.Done():
	case <-s.after(wait):
	}
}

// flush writes the pending state, unless nothing changed since the last
// write or the bytes would be the same as the ones already there.
func (s *Saver) flush() {
	s.mu.Lock()
	state, dirty := s.pending, s.dirty
	s.dirty = false
	s.mu.Unlock()

	if !dirty {
		return
	}

	data := Encode(state)
	if bytes.Equal(data, s.written) {
		return
	}

	// A failed attempt counts towards the gap as well: a full card is no
	// reason to retry it every second.
	s.lastWrite = s.now()

	if err := s.write(data); err != nil {
		if !s.failing {
			warnf(s.warn, "warning: state file %s: %v; uScope carries on, and the change is not kept\n",
				s.path, err)
		}

		s.failing = true

		return
	}

	s.written, s.failing = data, false
}

// write replaces the file with data atomically: a temporary file in the
// same directory, synced, then renamed over the old one. A reader, or the
// next start after a crash, sees the old file or the new one and never half
// of either. The same directory matters because rename is only atomic within
// one filesystem.
func (s *Saver) write(data []byte) error {
	file, err := s.createTemp(filepath.Dir(s.path), "."+filepath.Base(s.path)+".*")
	if err != nil {
		return fmt.Errorf("creating a temporary file: %w", err)
	}

	name := file.Name()

	if err := fill(file, data); err != nil {
		_ = s.remove(name)

		return err
	}

	if err := s.rename(name, s.path); err != nil {
		_ = s.remove(name)

		return fmt.Errorf("replacing the file: %w", err)
	}

	return nil
}

// fill writes data, syncs it to disk and closes the file. The sync comes
// before the rename so the name never points at blocks that are not there
// yet.
func fill(file tempFile, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()

		return fmt.Errorf("writing: %w", err)
	}

	if err := file.Sync(); err != nil {
		_ = file.Close()

		return fmt.Errorf("syncing: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("closing: %w", err)
	}

	return nil
}
