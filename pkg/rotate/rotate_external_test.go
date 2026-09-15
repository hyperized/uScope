package rotate_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperized/uScope/pkg/rotate"
)

// Rotation and corner names, shared across several tables below (goconst).
const (
	nameNone             = "none"
	nameClockwise        = "clockwise"
	nameUpsideDown       = "upside down"
	nameCounterClockwise = "counter clockwise"

	nameTopLeft     = "top left"
	nameTopRight    = "top right"
	nameBottomLeft  = "bottom left"
	nameBottomRight = "bottom right"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    rotate.Rotation
		wantErr bool
	}{
		{name: "zero", input: "0", want: rotate.None},
		{name: "one", input: "1", want: rotate.Clockwise},
		{name: "two", input: "2", want: rotate.UpsideDown},
		{name: "three", input: "3", want: rotate.CounterClockwise},
		{name: "empty", input: "", wantErr: true},
		{name: "four", input: "4", wantErr: true},
		{name: "negative", input: "-1", wantErr: true},
		{name: "word", input: "auto", wantErr: true},
		{name: "leading space", input: " 1", wantErr: true},
		{name: "zero padded", input: "01", wantErr: true},
		{name: "spelled out", input: "one", wantErr: true},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			got, err := rotate.Parse(tcase.input)
			if tcase.wantErr {
				if !errors.Is(err, rotate.ErrInvalid) {
					t.Fatalf("Parse(%q) error = %v, want ErrInvalid", tcase.input, err)
				}

				if got != rotate.None {
					t.Fatalf("Parse(%q) = %v, want None on error", tcase.input, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tcase.input, err)
			}

			if got != tcase.want {
				t.Fatalf("Parse(%q) = %v, want %v", tcase.input, got, tcase.want)
			}
		})
	}
}

func TestFromSysfs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    rotate.Rotation
		wantErr error
	}{
		{name: "kernel newline", content: "1\n", want: rotate.Clockwise},
		{name: "no newline", content: "0", want: rotate.None},
		{name: "surrounding whitespace", content: " 2 \n", want: rotate.UpsideDown},
		{name: "out of range", content: "7\n", wantErr: rotate.ErrInvalid},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			checkFromSysfs(t, tcase.content, tcase.want, tcase.wantErr)
		})
	}
}

// checkFromSysfs writes content to a fresh temp file and asserts the result.
// Split out of TestFromSysfs to keep cognitive complexity down.
func checkFromSysfs(t *testing.T, content string, want rotate.Rotation, wantErr error) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "rotate")

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := rotate.FromSysfs(path)
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("FromSysfs() error = %v, want %v", err, wantErr)
		}

		if got != rotate.None {
			t.Fatalf("FromSysfs() = %v, want None on error", got)
		}

		return
	}

	if err != nil {
		t.Fatalf("FromSysfs() unexpected error: %v", err)
	}

	if got != want {
		t.Fatalf("FromSysfs() = %v, want %v", got, want)
	}
}

func TestFromSysfsMissingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "missing")

	got, err := rotate.FromSysfs(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FromSysfs() error = %v, want os.ErrNotExist", err)
	}

	if got != rotate.None {
		t.Fatalf("FromSysfs() = %v, want None on error", got)
	}
}

func TestSysfsPath(t *testing.T) {
	t.Parallel()

	const want = "/sys/class/graphics/fbcon/rotate"

	if rotate.SysfsPath != want {
		t.Fatalf("SysfsPath = %q, want %q", rotate.SysfsPath, want)
	}
}

func TestRotationConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rot  rotate.Rotation
		want rotate.Rotation
	}{
		{name: nameNone, rot: rotate.None, want: 0},
		{name: nameClockwise, rot: rotate.Clockwise, want: 1},
		{name: nameUpsideDown, rot: rotate.UpsideDown, want: 2},
		{name: nameCounterClockwise, rot: rotate.CounterClockwise, want: 3},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			if tcase.rot != tcase.want {
				t.Fatalf("%s = %v, want %v", tcase.name, tcase.rot, tcase.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rot  rotate.Rotation
		want string
	}{
		{name: nameNone, rot: rotate.None, want: "0"},
		{name: nameClockwise, rot: rotate.Clockwise, want: "1"},
		{name: nameUpsideDown, rot: rotate.UpsideDown, want: "2"},
		{name: nameCounterClockwise, rot: rotate.CounterClockwise, want: "3"},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			if got := tcase.rot.String(); got != tcase.want {
				t.Fatalf("String() = %q, want %q", got, tcase.want)
			}
		})
	}
}

func TestLogical(t *testing.T) {
	t.Parallel()

	const (
		physW      = 720
		physH      = 1280
		squareSide = 100
	)

	tests := []struct {
		name  string
		rot   rotate.Rotation
		physW int
		physH int
		wantW int
		wantH int
	}{
		{name: "clockwise swaps axes", rot: rotate.Clockwise, physW: physW, physH: physH, wantW: physH, wantH: physW},
		{
			name: "counter clockwise swaps axes", rot: rotate.CounterClockwise,
			physW: physW, physH: physH, wantW: physH, wantH: physW,
		},
		{name: "none keeps axes", rot: rotate.None, physW: physW, physH: physH, wantW: physW, wantH: physH},
		{
			name: "upside down keeps axes", rot: rotate.UpsideDown,
			physW: physW, physH: physH, wantW: physW, wantH: physH,
		},
		{
			name: "square buffer", rot: rotate.Clockwise,
			physW: squareSide, physH: squareSide, wantW: squareSide, wantH: squareSide,
		},
		{name: "one pixel buffer", rot: rotate.Clockwise, physW: 1, physH: 1, wantW: 1, wantH: 1},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			gotW, gotH := tcase.rot.Logical(tcase.physW, tcase.physH)
			if gotW != tcase.wantW || gotH != tcase.wantH {
				t.Fatalf("Logical(%d, %d) = (%d, %d), want (%d, %d)",
					tcase.physW, tcase.physH, gotW, gotH, tcase.wantW, tcase.wantH)
			}
		})
	}
}

// mapCase is a single logical-to-physical corner assertion, shared by the
// per-rotation Map tests below. The physical buffer is always 720x1280,
// the real uConsole geometry, so runMapCases hardcodes it.
type mapCase struct {
	name  string
	x     int
	y     int
	wantX int
	wantY int
}

func runMapCases(t *testing.T, rot rotate.Rotation, tests []mapCase) {
	t.Helper()

	const physW, physH = 720, 1280

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			gotX, gotY := rot.Map(tcase.x, tcase.y, physW, physH)
			if gotX != tcase.wantX || gotY != tcase.wantY {
				t.Fatalf("Map(%d, %d, %d, %d) = (%d, %d), want (%d, %d)",
					tcase.x, tcase.y, physW, physH, gotX, gotY, tcase.wantX, tcase.wantY)
			}
		})
	}
}

func TestMapNone(t *testing.T) {
	t.Parallel()

	const physW, physH = 720, 1280

	runMapCases(t, rotate.None, []mapCase{
		{name: nameTopLeft, x: 0, y: 0, wantX: 0, wantY: 0},
		{name: nameTopRight, x: physW - 1, y: 0, wantX: physW - 1, wantY: 0},
		{name: nameBottomLeft, x: 0, y: physH - 1, wantX: 0, wantY: physH - 1},
		{name: nameBottomRight, x: physW - 1, y: physH - 1, wantX: physW - 1, wantY: physH - 1},
	})
}

func TestMapClockwise(t *testing.T) {
	t.Parallel()

	// Logical canvas is 1280x720 here: a quarter turn swaps the axes.
	const physW, physH = 720, 1280

	runMapCases(t, rotate.Clockwise, []mapCase{
		{name: nameTopLeft, x: 0, y: 0, wantX: physW - 1, wantY: 0},
		{name: nameTopRight, x: physH - 1, y: 0, wantX: physW - 1, wantY: physH - 1},
		{name: nameBottomLeft, x: 0, y: physW - 1, wantX: 0, wantY: 0},
		{name: nameBottomRight, x: physH - 1, y: physW - 1, wantX: 0, wantY: physH - 1},
	})
}

func TestMapUpsideDown(t *testing.T) {
	t.Parallel()

	const physW, physH = 720, 1280

	runMapCases(t, rotate.UpsideDown, []mapCase{
		{name: nameTopLeft, x: 0, y: 0, wantX: physW - 1, wantY: physH - 1},
		{name: nameTopRight, x: physW - 1, y: 0, wantX: 0, wantY: physH - 1},
		{name: nameBottomLeft, x: 0, y: physH - 1, wantX: physW - 1, wantY: 0},
		{name: nameBottomRight, x: physW - 1, y: physH - 1, wantX: 0, wantY: 0},
	})
}

func TestMapCounterClockwise(t *testing.T) {
	t.Parallel()

	// Logical canvas is 1280x720 here too, mirrored against Clockwise.
	const physW, physH = 720, 1280

	runMapCases(t, rotate.CounterClockwise, []mapCase{
		{name: nameTopLeft, x: 0, y: 0, wantX: 0, wantY: physH - 1},
		{name: nameTopRight, x: physH - 1, y: 0, wantX: 0, wantY: 0},
		{name: nameBottomLeft, x: 0, y: physW - 1, wantX: physW - 1, wantY: physH - 1},
		{name: nameBottomRight, x: physH - 1, y: physW - 1, wantX: physW - 1, wantY: 0},
	})
}

// TestMapBijection proves the mapping is a bijection: every logical pixel of
// the canvas lands on its own physical pixel, all of them land inside the
// buffer, and together they cover it exactly. If that were not true, part of
// the frame would be dropped on the device instead of merely misplaced.
func TestMapBijection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rot  rotate.Rotation
	}{
		{name: nameNone, rot: rotate.None},
		{name: nameClockwise, rot: rotate.Clockwise},
		{name: nameUpsideDown, rot: rotate.UpsideDown},
		{name: nameCounterClockwise, rot: rotate.CounterClockwise},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			checkBijection(t, tcase.rot)
		})
	}
}

// checkBijection walks every logical pixel of a small buffer and checks the
// physical pixels it lands on are in range and never repeated. Split out of
// TestMapBijection to keep cognitive complexity down.
func checkBijection(t *testing.T, rot rotate.Rotation) {
	t.Helper()

	const pw, ph = 4, 6 //nolint:varnamelen // pw, ph mirror the source's own parameter names.

	logicalW, logicalH := rot.Logical(pw, ph)
	seen := make(map[[2]int]bool, pw*ph)

	for logicalX := range logicalW {
		for logicalY := range logicalH {
			physX, physY := rot.Map(logicalX, logicalY, pw, ph)
			assertInBounds(t, physX, physY, pw, ph)

			key := [2]int{physX, physY}
			if seen[key] {
				t.Fatalf("Map(%d, %d) = (%d, %d), duplicate physical pixel", logicalX, logicalY, physX, physY)
			}

			seen[key] = true
		}
	}

	if len(seen) != pw*ph {
		t.Fatalf("mapped %d distinct pixels, want %d", len(seen), pw*ph)
	}
}

//nolint:varnamelen // x, y, pw, ph is the universal idiom for a coordinate map, matching the source.
func assertInBounds(t *testing.T, x, y, pw, ph int) {
	t.Helper()

	if x < 0 || x >= pw || y < 0 || y >= ph {
		t.Fatalf("(%d, %d) out of bounds for %dx%d", x, y, pw, ph)
	}
}
