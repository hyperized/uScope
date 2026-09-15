//go:build linux || darwin

package winsize

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"
)

// Geometry for the field-mapping cases below. Cols and Rows are always
// different from each other, and so are XPixels and YPixels, so a test
// would fail if Get ever mapped one field to the other's place: that
// transposition is the obvious way to get this wrong.
const (
	cols80  = 80
	rows24  = 24
	cellW8  = 8
	cellH16 = 16
	pixelsX = cols80 * cellW8  // 640
	pixelsY = rows24 * cellH16 // 384

	colsWide  = 132
	rowsShort = 50
)

// fakeSys is the sys seam for tests: a controllable stand-in for the
// kernel, since a unit test has no real terminal to drive.
type fakeSys struct {
	reply winsizeIoctl
	err   error
}

func (f *fakeSys) ioctl(_ uintptr, req uintptr, arg unsafe.Pointer) error {
	if req != ioctlGetWinsize {
		return nil
	}

	if f.err != nil {
		return f.err
	}

	//nolint:gosec // G103: the fake plays the kernel's role of filling this struct.
	*(*winsizeIoctl)(arg) = f.reply

	return nil
}

func TestGetFieldMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		reply winsizeIoctl
		want  Size
	}{
		{
			name:  "all zero",
			reply: winsizeIoctl{},
			want:  Size{},
		},
		{
			name:  "80x24 cells with 8x16 pixel cells",
			reply: winsizeIoctl{Row: rows24, Col: cols80, Xpixel: pixelsX, Ypixel: pixelsY},
			want:  Size{Cols: cols80, Rows: rows24, XPixels: pixelsX, YPixels: pixelsY},
		},
		{
			name:  "cells reported, zero pixels",
			reply: winsizeIoctl{Row: rowsShort, Col: colsWide, Xpixel: 0, Ypixel: 0},
			want:  Size{Cols: colsWide, Rows: rowsShort, XPixels: 0, YPixels: 0},
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			seam := &fakeSys{reply: tcase.reply}

			got, err := get(0, seam)
			if err != nil {
				t.Fatalf("get() error = %v, want nil", err)
			}

			if got != tcase.want {
				t.Fatalf("get() = %+v, want %+v", got, tcase.want)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		errno           error
		wantNotTerminal bool
	}{
		{name: "ENOTTY", errno: syscall.ENOTTY, wantNotTerminal: true},
		{name: "ENODEV", errno: syscall.ENODEV, wantNotTerminal: true},
		{name: "EINVAL", errno: syscall.EINVAL, wantNotTerminal: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			err := classify(tcase.errno)
			if !errors.Is(err, tcase.errno) {
				t.Fatalf("classify() error = %v, want wrapping %v", err, tcase.errno)
			}

			if got := errors.Is(err, ErrNotTerminal); got != tcase.wantNotTerminal {
				t.Fatalf("errors.Is(err, ErrNotTerminal) = %v, want %v", got, tcase.wantNotTerminal)
			}
		})
	}
}

func TestGetIoctlFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		errno           error
		wantNotTerminal bool
	}{
		{name: "ENOTTY", errno: syscall.ENOTTY, wantNotTerminal: true},
		{name: "ENODEV", errno: syscall.ENODEV, wantNotTerminal: true},
		{name: "EINVAL", errno: syscall.EINVAL, wantNotTerminal: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			seam := &fakeSys{err: tcase.errno}

			_, err := get(0, seam)
			if !errors.Is(err, tcase.errno) {
				t.Fatalf("get() error = %v, want wrapping %v", err, tcase.errno)
			}

			if got := errors.Is(err, ErrNotTerminal); got != tcase.wantNotTerminal {
				t.Fatalf("errors.Is(err, ErrNotTerminal) = %v, want %v", got, tcase.wantNotTerminal)
			}
		})
	}
}
