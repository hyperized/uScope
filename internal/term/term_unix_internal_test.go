//go:build linux || darwin

package term

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// Errno case names, shared across the classify and rawMode failure tables
// below (goconst).
const (
	nameENOTTY = "ENOTTY"
	nameENODEV = "ENODEV"
	nameEIO    = "EIO"
)

// cookedTermios returns a termios with every flag applyRaw clears set, plus
// one flag per field it does not touch, so one fixture covers both the
// clearing and the preservation assertions.
func cookedTermios() syscall.Termios {
	var attr syscall.Termios

	attr.Lflag |= syscall.ISIG | syscall.ICANON | syscall.ECHO | syscall.IEXTEN | syscall.TOSTOP
	attr.Iflag |= syscall.IXON | syscall.ICRNL | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP | syscall.IGNPAR
	attr.Oflag |= syscall.OPOST | syscall.ONLCR
	attr.Cflag |= syscall.CSIZE | syscall.PARENB | syscall.CREAD
	attr.Cc[syscall.VMIN] = 1
	attr.Cc[syscall.VTIME] = 0

	return attr
}

// assertRawFlags checks the properties applyRaw promises: every cooking flag
// clear, 8-bit characters with no parity, and a non-blocking read with a
// short timeout.
func assertRawFlags(t *testing.T, attr *syscall.Termios) {
	t.Helper()

	if attr.Lflag&(syscall.ISIG|syscall.ICANON|syscall.ECHO|syscall.IEXTEN) != 0 {
		t.Fatalf("Lflag = %#x, want ISIG|ICANON|ECHO|IEXTEN clear", attr.Lflag)
	}

	if attr.Iflag&(syscall.IXON|syscall.ICRNL|syscall.BRKINT|syscall.INPCK|syscall.ISTRIP) != 0 {
		t.Fatalf("Iflag = %#x, want IXON|ICRNL|BRKINT|INPCK|ISTRIP clear", attr.Iflag)
	}

	if attr.Oflag&syscall.OPOST != 0 {
		t.Fatalf("Oflag = %#x, want OPOST clear", attr.Oflag)
	}

	if attr.Cflag&syscall.PARENB != 0 {
		t.Fatalf("Cflag = %#x, want PARENB clear", attr.Cflag)
	}

	// CS8's bits are the CSIZE field's own "8 bits" value, so a clear CSIZE
	// and a set CS8 are the same state, not two separate ones: the character
	// size field must read exactly CS8, with no old size bits left over.
	if attr.Cflag&syscall.CSIZE != syscall.CS8 {
		t.Fatalf("Cflag&CSIZE = %#x, want CS8 (%#x)", attr.Cflag&syscall.CSIZE, syscall.CS8)
	}

	if attr.Cc[syscall.VMIN] != 0 {
		t.Fatalf("Cc[VMIN] = %d, want 0", attr.Cc[syscall.VMIN])
	}

	if attr.Cc[syscall.VTIME] != 1 {
		t.Fatalf("Cc[VTIME] = %d, want 1", attr.Cc[syscall.VTIME])
	}
}

// assertUnrelatedBitsSurvive checks that applyRaw clears only the bits it
// documents, using one flag per field that is not among them.
func assertUnrelatedBitsSurvive(t *testing.T, attr *syscall.Termios) {
	t.Helper()

	if attr.Lflag&syscall.TOSTOP == 0 {
		t.Fatal("TOSTOP was cleared, want it left alone")
	}

	if attr.Iflag&syscall.IGNPAR == 0 {
		t.Fatal("IGNPAR was cleared, want it left alone")
	}

	if attr.Oflag&syscall.ONLCR == 0 {
		t.Fatal("ONLCR was cleared, want it left alone")
	}

	if attr.Cflag&syscall.CREAD == 0 {
		t.Fatal("CREAD was cleared, want it left alone")
	}
}

func TestApplyRaw(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		seed syscall.Termios
	}{
		{name: "cooked", seed: cookedTermios()},
		{name: "zero value", seed: syscall.Termios{}},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			attr := tcase.seed
			applyRaw(&attr)

			assertRawFlags(t, &attr)
		})
	}
}

func TestApplyRawPreservesUnrelatedBits(t *testing.T) {
	t.Parallel()

	attr := cookedTermios()
	applyRaw(&attr)

	assertUnrelatedBitsSurvive(t, &attr)
}

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		errno           error
		wantNotTerminal bool
	}{
		{name: nameENOTTY, errno: syscall.ENOTTY, wantNotTerminal: true},
		{name: nameENODEV, errno: syscall.ENODEV, wantNotTerminal: true},
		{name: nameEIO, errno: syscall.EIO, wantNotTerminal: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			err := classify(tcase.errno, "TEST")
			if !errors.Is(err, tcase.errno) {
				t.Fatalf("classify() error = %v, want wrapping %v", err, tcase.errno)
			}

			if got := errors.Is(err, ErrNotTerminal); got != tcase.wantNotTerminal {
				t.Fatalf("errors.Is(err, ErrNotTerminal) = %v, want %v", got, tcase.wantNotTerminal)
			}
		})
	}
}

// fakeSys is the sys seam for tests: a controllable stand-in for the kernel,
// since a unit test has no real terminal to drive.
type fakeSys struct {
	original  syscall.Termios
	getErr    error
	setErr    error
	failSetOn int // 1-indexed set call to fail on; 0 means never.
	setCalls  int
	recorded  []syscall.Termios
}

func (f *fakeSys) ioctl(_ uintptr, req uintptr, arg unsafe.Pointer) error {
	switch req {
	case ioctlGetAttr:
		if f.getErr != nil {
			return f.getErr
		}

		//nolint:gosec // G103: the fake plays the kernel's role of filling this struct.
		*(*syscall.Termios)(arg) = f.original

		return nil
	case ioctlSetAttr:
		f.setCalls++

		//nolint:gosec // G103: as above, reading back what this package wrote.
		f.recorded = append(f.recorded, *(*syscall.Termios)(arg))

		if f.failSetOn != 0 && f.setCalls == f.failSetOn {
			return f.setErr
		}

		return nil
	default:
		return nil
	}
}

func TestRawModeHappyPath(t *testing.T) {
	t.Parallel()

	original := cookedTermios()
	seam := &fakeSys{original: original}

	restore, err := rawMode(0, seam)
	if err != nil {
		t.Fatalf("rawMode() error = %v, want nil", err)
	}

	if len(seam.recorded) != 1 {
		t.Fatalf("got %d set calls, want 1", len(seam.recorded))
	}

	assertRawFlags(t, &seam.recorded[0])
	assertUnrelatedBitsSurvive(t, &seam.recorded[0])

	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v, want nil", err)
	}

	if len(seam.recorded) != 2 {
		t.Fatalf("got %d set calls, want 2", len(seam.recorded))
	}

	// Restore must write back the struct exactly as first read, not the raw
	// one this package built. Get that wrong and a user's shell comes back
	// with no echo, which reads as a hung machine rather than a bug.
	if seam.recorded[1] != original {
		t.Fatalf("restore wrote %+v, want the original %+v", seam.recorded[1], original)
	}
}

func TestRawModeGetFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		errno           error
		wantNotTerminal bool
	}{
		{name: nameENOTTY, errno: syscall.ENOTTY, wantNotTerminal: true},
		{name: nameENODEV, errno: syscall.ENODEV, wantNotTerminal: true},
		{name: nameEIO, errno: syscall.EIO, wantNotTerminal: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			seam := &fakeSys{getErr: tcase.errno}

			restore, err := rawMode(0, seam)
			if restore != nil {
				t.Fatal("rawMode() restore is non-nil, want nil")
			}

			if !errors.Is(err, tcase.errno) {
				t.Fatalf("rawMode() error = %v, want wrapping %v", err, tcase.errno)
			}

			if got := errors.Is(err, ErrNotTerminal); got != tcase.wantNotTerminal {
				t.Fatalf("errors.Is(err, ErrNotTerminal) = %v, want %v", got, tcase.wantNotTerminal)
			}
		})
	}
}

func TestRawModeSetFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		errno           error
		wantNotTerminal bool
	}{
		{name: nameENOTTY, errno: syscall.ENOTTY, wantNotTerminal: true},
		{name: nameENODEV, errno: syscall.ENODEV, wantNotTerminal: true},
		{name: nameEIO, errno: syscall.EIO, wantNotTerminal: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			seam := &fakeSys{original: cookedTermios(), failSetOn: 1, setErr: tcase.errno}

			restore, err := rawMode(0, seam)
			if restore != nil {
				t.Fatal("rawMode() restore is non-nil, want nil")
			}

			if !errors.Is(err, tcase.errno) {
				t.Fatalf("rawMode() error = %v, want wrapping %v", err, tcase.errno)
			}

			if got := errors.Is(err, ErrNotTerminal); got != tcase.wantNotTerminal {
				t.Fatalf("errors.Is(err, ErrNotTerminal) = %v, want %v", got, tcase.wantNotTerminal)
			}
		})
	}
}

func TestRawModeRestoreFails(t *testing.T) {
	t.Parallel()

	wantErr := syscall.EIO
	seam := &fakeSys{original: cookedTermios(), failSetOn: 2, setErr: wantErr}

	restore, err := rawMode(0, seam)
	if err != nil {
		t.Fatalf("rawMode() error = %v, want nil", err)
	}

	if err := restore(); !errors.Is(err, wantErr) {
		t.Fatalf("restore() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestMakeRaw(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}

	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	_, err = MakeRaw(file.Fd())
	if err == nil {
		t.Fatal("MakeRaw() error = nil, want non-nil")
	}

	// A regular file fails the get-attributes ioctl with ENOTTY, which
	// classify maps to ErrNotTerminal. Confirmed against the real ioctl.
	if !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("MakeRaw() error = %v, want ErrNotTerminal", err)
	}
}
