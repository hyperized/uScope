//go:build unix

package statefile_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/statefile"
)

// TestLoadRefusesAFIFO points Load at a named pipe nothing writes to. A
// blocking open would wait for ever; Load has to come back at once with the
// base and one warning.
func TestLoadRefusesAFIFO(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	var warn bytes.Buffer

	loaded := make(chan string, 1)

	go func() { loaded <- statefile.Load(path, base(), &warn).String() }()

	select {
	case got := <-loaded:
		if got != base().String() {
			t.Errorf("Load = %s, want the base", got)
		}

		if !strings.Contains(warn.String(), "not a regular file") {
			t.Errorf("warning %q, want one saying it is not a regular file", warn.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Load blocked on the FIFO")
	}
}
