package history

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// bufferSize is the write buffer between the encoder and the file. Gob
// writes one message per call, and a snapshot is one message, so this is
// what turns a 30 MB message into writes of a size the disk likes.
const bufferSize = 64 << 10

// tempFile is the part of *os.File a write needs. It is an interface so a
// test can fail each step in turn.
type tempFile interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
}

// Writer replaces one history file with a snapshot, atomically.
//
// Writer is not safe for concurrent use. internal/source calls it from its
// writer goroutine and, once that has stopped, from Close, which is one
// caller at a time by construction.
//
// A failed write is reported on the warning writer once per distinct cause,
// which is the innermost error rather than the whole message: the message
// names a temporary file whose name is new every time. A full volume costs one line
// rather than one a minute, and a different failure after it still gets its
// own line. A write that succeeds clears the memory, so the same failure
// coming back later is reported again.
type Writer struct {
	path string
	warn io.Writer

	createTemp func(dir, pattern string) (tempFile, error)
	rename     func(from, to string) error
	remove     func(name string) error

	reported string
}

// NewWriter returns a Writer for the file at path that reports failures on
// warn. It touches nothing until Write is called.
func NewWriter(path string, warn io.Writer) *Writer {
	return &Writer{
		path:       path,
		warn:       warn,
		createTemp: createTemp,
		rename:     os.Rename,
		remove:     os.Remove,
	}
}

// createTemp is os.CreateTemp behind the tempFile interface.
//
//nolint:ireturn,wrapcheck // the interface is the seam, and write wraps the error.
func createTemp(dir, pattern string) (tempFile, error) {
	return os.CreateTemp(dir, pattern)
}

// Write replaces the file with snap and reports whether it did. A failure is
// reported on the warning writer, never returned: the history is a cache,
// and nothing a caller could do about a full disk is worth stopping for.
func (w *Writer) Write(snap *Snapshot) bool {
	err := w.replace(snap)
	if err == nil {
		w.reported = ""

		return true
	}

	if cause := innermost(err).Error(); cause != w.reported {
		w.reported = cause
		_, _ = fmt.Fprintf(w.warn,
			"warning: history file %s: %v; the run carries on, and the tracks are not kept\n", w.path, err)
	}

	return false
}

// replace puts snap in a temporary file in the same directory, synced, then
// renames it over the old one. A kill part way through leaves the old file
// whole, and rename is only atomic within one filesystem, which is why the
// temporary file is a sibling.
func (w *Writer) replace(snap *Snapshot) error {
	file, err := w.createTemp(filepath.Dir(w.path), "."+filepath.Base(w.path)+".*")
	if err != nil {
		return fmt.Errorf("creating a temporary file: %w", err)
	}

	name := file.Name()

	if err := fill(file, snap); err != nil {
		_ = w.remove(name)

		return err
	}

	if err := w.rename(name, w.path); err != nil {
		_ = w.remove(name)

		return fmt.Errorf("replacing the file: %w", err)
	}

	return nil
}

// fill encodes snap into the file, syncs it and closes it. The sync comes
// before the rename so the name never points at blocks that are not there
// yet.
func fill(file tempFile, snap *Snapshot) error {
	buffered := bufio.NewWriterSize(file, bufferSize)

	if err := Encode(buffered, snap); err != nil {
		_ = file.Close()

		return err
	}

	if err := buffered.Flush(); err != nil {
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

// innermost unwraps err as far as it goes.
func innermost(err error) error {
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return err
		}

		err = inner
	}
}
