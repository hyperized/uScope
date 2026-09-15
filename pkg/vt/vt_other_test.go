//go:build !linux

package vt //nolint:testpackage // white-box to mirror the linux internal test; nothing here needs an unexported seam.

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

// TestGraphicsStub covers the non-Linux stub: it always reports
// ErrNotConsole and names the running platform in the message.
func TestGraphicsStub(t *testing.T) {
	t.Parallel()

	restore, err := Graphics(nil)
	if restore != nil {
		t.Fatal("Graphics() restore is non-nil, want nil")
	}

	if !errors.Is(err, ErrNotConsole) {
		t.Fatalf("Graphics() error = %v, want ErrNotConsole", err)
	}

	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Fatalf("Graphics() error = %q, want it to name %q", err.Error(), runtime.GOOS)
	}
}
