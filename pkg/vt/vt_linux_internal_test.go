//go:build linux

package vt

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// badMode is a value KDGETMODE could report that is neither KD_TEXT nor
// KD_GRAPHICS. enterGraphics clamps it to KD_TEXT before ever handing it
// back to KDSETMODE, which is the branch these tests exist to prove.
const badMode = int32(7)

// Errno case names, shared across the classify and enterGraphics failure
// tables below (goconst).
const (
	nameENOTTY = "ENOTTY"
	nameEINVAL = "EINVAL"
	nameEPERM  = "EPERM"
	nameEIO    = "EIO"
)

// fakeCall records one ioctl the fake received, in order.
type fakeCall struct {
	req uintptr
	arg uintptr
}

// fakeSys is the sys seam for tests: a controllable stand-in for the console
// driver, since a unit test has no controlling terminal of its own.
//
// It records each call in the same (request, argument) shape the real ioctls
// take, so the assertions read like the syscall trace they stand for.
type fakeSys struct {
	calls     []fakeCall
	mode      int32
	getErr    error
	setErr    error
	failSetOn int // 1-indexed KDSETMODE call to fail on; 0 means never.
	setCalls  int
}

// getMode plays KDGETMODE. The seam returns the mode by value, so there is no
// pointer for the fake to write through.
func (f *fakeSys) getMode(_ uintptr) (int32, error) {
	f.calls = append(f.calls, fakeCall{req: kdGetMode, arg: 0})

	if f.getErr != nil {
		return 0, f.getErr
	}

	return f.mode, nil
}

// setMode plays KDSETMODE.
func (f *fakeSys) setMode(_ uintptr, mode int32) error {
	//nolint:gosec // the fake only ever sees the small KD_ constants.
	f.calls = append(f.calls, fakeCall{req: kdSetMode, arg: uintptr(mode)})
	f.setCalls++

	if f.failSetOn != 0 && f.setCalls == f.failSetOn {
		return f.setErr
	}

	return nil
}

// openTestFile returns a plain file enterGraphics can call Fd() on. The fake
// intercepts every ioctl, so the file's actual content and type never matter.
func openTestFile(t *testing.T) *os.File {
	t.Helper()

	path := filepath.Join(t.TempDir(), "vt-test")

	//nolint:gosec // G304: path is built from t.TempDir(), not from any external input.
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create: %v", err)
	}

	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	return file
}

func TestEnterGraphicsHappyPath(t *testing.T) {
	t.Parallel()

	tty := openTestFile(t)
	seam := &fakeSys{mode: kdText}

	restore, err := enterGraphics(tty, seam)
	if err != nil {
		t.Fatalf("enterGraphics() error = %v, want nil", err)
	}

	if len(seam.calls) != 2 {
		t.Fatalf("got %d ioctl calls after enter, want 2", len(seam.calls))
	}

	if seam.calls[0].req != kdGetMode {
		t.Fatalf("call 0 req = %#x, want KDGETMODE", seam.calls[0].req)
	}

	if seam.calls[1].req != kdSetMode || seam.calls[1].arg != uintptr(kdGraphics) {
		t.Fatalf("call 1 = (%#x, %d), want (KDSETMODE, %d)", seam.calls[1].req, seam.calls[1].arg, kdGraphics)
	}

	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v, want nil", err)
	}

	if len(seam.calls) != 3 {
		t.Fatalf("got %d ioctl calls after restore, want 3", len(seam.calls))
	}

	if seam.calls[2].req != kdSetMode || seam.calls[2].arg != uintptr(kdText) {
		t.Fatalf("restore call = (%#x, %d), want (KDSETMODE, %d)", seam.calls[2].req, seam.calls[2].arg, kdText)
	}
}

func TestEnterGraphicsRestoresPreviousMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		reportedMode int32
		wantRestore  uintptr
	}{
		{name: "text mode", reportedMode: kdText, wantRestore: uintptr(kdText)},
		{name: "graphics mode", reportedMode: kdGraphics, wantRestore: uintptr(kdGraphics)},
		// A driver reporting anything else gets clamped to text before
		// restoring, so a bad kernel value never becomes a wild uintptr.
		{name: "out of range mode clamps to text", reportedMode: badMode, wantRestore: uintptr(kdText)},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			tty := openTestFile(t)
			seam := &fakeSys{mode: tcase.reportedMode}

			restore, err := enterGraphics(tty, seam)
			if err != nil {
				t.Fatalf("enterGraphics() error = %v, want nil", err)
			}

			if err := restore(); err != nil {
				t.Fatalf("restore() error = %v, want nil", err)
			}

			last := seam.calls[len(seam.calls)-1]
			if last.req != kdSetMode || last.arg != tcase.wantRestore {
				t.Fatalf("restore call = (%#x, %d), want (KDSETMODE, %d)", last.req, last.arg, tcase.wantRestore)
			}
		})
	}
}

func TestEnterGraphicsNilFile(t *testing.T) {
	t.Parallel()

	seam := &fakeSys{}

	_, err := enterGraphics(nil, seam)
	if !errors.Is(err, ErrNotConsole) {
		t.Fatalf("enterGraphics(nil) error = %v, want ErrNotConsole", err)
	}

	if len(seam.calls) != 0 {
		t.Fatalf("got %d ioctl calls, want 0", len(seam.calls))
	}
}

func TestEnterGraphicsGetModeFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		errno          error
		wantNotConsole bool
	}{
		{name: nameENOTTY, errno: syscall.ENOTTY, wantNotConsole: true},
		{name: nameEINVAL, errno: syscall.EINVAL, wantNotConsole: true},
		{name: nameEPERM, errno: syscall.EPERM, wantNotConsole: true},
		{name: nameEIO, errno: syscall.EIO, wantNotConsole: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			tty := openTestFile(t)
			seam := &fakeSys{getErr: tcase.errno}

			_, err := enterGraphics(tty, seam)
			if !errors.Is(err, tcase.errno) {
				t.Fatalf("enterGraphics() error = %v, want wrapping %v", err, tcase.errno)
			}

			if got := errors.Is(err, ErrNotConsole); got != tcase.wantNotConsole {
				t.Fatalf("errors.Is(err, ErrNotConsole) = %v, want %v", got, tcase.wantNotConsole)
			}
		})
	}
}

func TestEnterGraphicsSetModeFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		errno          error
		wantNotConsole bool
	}{
		{name: nameENOTTY, errno: syscall.ENOTTY, wantNotConsole: true},
		{name: nameEINVAL, errno: syscall.EINVAL, wantNotConsole: true},
		{name: nameEPERM, errno: syscall.EPERM, wantNotConsole: true},
		{name: nameEIO, errno: syscall.EIO, wantNotConsole: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			tty := openTestFile(t)
			seam := &fakeSys{mode: kdText, failSetOn: 1, setErr: tcase.errno}

			restore, err := enterGraphics(tty, seam)
			if restore != nil {
				t.Fatal("enterGraphics() restore is non-nil, want nil")
			}

			if !errors.Is(err, tcase.errno) {
				t.Fatalf("enterGraphics() error = %v, want wrapping %v", err, tcase.errno)
			}

			if got := errors.Is(err, ErrNotConsole); got != tcase.wantNotConsole {
				t.Fatalf("errors.Is(err, ErrNotConsole) = %v, want %v", got, tcase.wantNotConsole)
			}

			if len(seam.calls) != 2 {
				t.Fatalf("got %d ioctl calls, want 2 (get, then the failed set)", len(seam.calls))
			}
		})
	}
}

func TestEnterGraphicsRestoreFails(t *testing.T) {
	t.Parallel()

	tty := openTestFile(t)
	wantErr := syscall.EIO
	seam := &fakeSys{mode: kdText, failSetOn: 2, setErr: wantErr}

	restore, err := enterGraphics(tty, seam)
	if err != nil {
		t.Fatalf("enterGraphics() error = %v, want nil", err)
	}

	if err := restore(); !errors.Is(err, wantErr) {
		t.Fatalf("restore() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		errno          error
		wantNotConsole bool
	}{
		{name: nameENOTTY, errno: syscall.ENOTTY, wantNotConsole: true},
		{name: nameEINVAL, errno: syscall.EINVAL, wantNotConsole: true},
		{name: nameEPERM, errno: syscall.EPERM, wantNotConsole: true},
		{name: nameEIO, errno: syscall.EIO, wantNotConsole: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			err := classify(tcase.errno, "TEST")
			if !errors.Is(err, tcase.errno) {
				t.Fatalf("classify() error = %v, want wrapping %v", err, tcase.errno)
			}

			if got := errors.Is(err, ErrNotConsole); got != tcase.wantNotConsole {
				t.Fatalf("errors.Is(err, ErrNotConsole) = %v, want %v", got, tcase.wantNotConsole)
			}
		})
	}
}

func TestGraphics(t *testing.T) {
	t.Parallel()

	tty := openTestFile(t)

	_, err := Graphics(tty)
	if err == nil {
		t.Fatal("Graphics() error = nil, want non-nil")
	}

	// A regular file fails KDGETMODE with ENOTTY, which classify maps to
	// ErrNotConsole. Confirmed against the real ioctl in the Linux container.
	if !errors.Is(err, ErrNotConsole) {
		t.Fatalf("Graphics() error = %v, want ErrNotConsole", err)
	}
}
