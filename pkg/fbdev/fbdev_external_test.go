package fbdev_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/hyperized/uScope/pkg/fbdev"
)

// TestSentinelErrors checks the four errors the package promises callers can
// tell apart actually exist and are non-nil. This file has no build tag, so
// it runs on both Linux and macOS.
func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "ErrUnsupported", err: fbdev.ErrUnsupported},
		{name: "ErrSize", err: fbdev.ErrSize},
		{name: "ErrDepth", err: fbdev.ErrDepth},
		{name: "ErrClosed", err: fbdev.ErrClosed},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			if tcase.err == nil {
				t.Fatalf("%s is nil", tcase.name)
			}

			if !errors.Is(tcase.err, tcase.err) {
				t.Errorf("%s does not match itself via errors.Is", tcase.name)
			}
		})
	}
}

// TestSentinelErrors_Distinct checks the four sentinels are genuinely
// separate errors, so a caller's errors.Is check can tell them apart.
func TestSentinelErrors_Distinct(t *testing.T) {
	t.Parallel()

	all := []error{fbdev.ErrUnsupported, fbdev.ErrSize, fbdev.ErrDepth, fbdev.ErrClosed}

	for outer := range all {
		for inner := range all {
			if outer == inner {
				continue
			}

			if errors.Is(all[outer], all[inner]) {
				t.Errorf("sentinel %d unexpectedly matches sentinel %d", outer, inner)
			}
		}
	}
}

// TestOpen_ErrorsOnUnusablePath checks Open fails on a path with no usable
// framebuffer behind it, without asserting which sentinel comes back: on
// Linux it is a plain open() failure, on the stub it is ErrUnsupported.
func TestOpen_ErrorsOnUnusablePath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "no-such-framebuffer")

	_, err := fbdev.Open(path)
	if err == nil {
		t.Fatal("Open() error = nil, want an error on a path with no usable framebuffer")
	}
}
