//go:build linux || darwin

package term

import (
	"errors"
	"fmt"
	"io"
	"syscall"
)

// Reader reads a raw terminal through the read system call rather than
// through os.File.
//
// The distinction matters because of how the two treat an empty read. In raw
// mode with VMIN at 0 and VTIME at 1, the kernel returns zero bytes after
// 100 ms of silence. os.File reports that as io.EOF, and a key reader that
// takes EOF at its word stops for good after the first quiet moment. The
// system call just says zero, which is what happened, so this Reader passes
// that on as (0, nil) and the caller reads again.
type Reader struct {
	fd   int
	read func(fd int, p []byte) (int, error)
}

// NewReader wraps the file descriptor of a terminal that MakeRaw has put in
// raw mode.
func NewReader(fd uintptr) *Reader {
	return &Reader{fd: int(fd), read: syscall.Read}
}

// Read fills p with whatever the terminal has, or returns (0, nil) when the
// read timed out. An interrupted call is retried, every other failure is
// returned wrapped.
func (r *Reader) Read(p []byte) (int, error) {
	for {
		count, err := r.read(r.fd, p)
		if errors.Is(err, syscall.EINTR) {
			continue
		}

		if err != nil {
			return 0, fmt.Errorf("term: read fd %d: %w", r.fd, err)
		}

		return count, nil
	}
}

// Compile-time check that Reader is an io.Reader.
var _ io.Reader = (*Reader)(nil)
