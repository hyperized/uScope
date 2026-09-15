package rotate

import "testing"

// TestValid exercises the whole uint8 range that matters: the four real
// rotations, plus values a caller could hand us that are not among them.
func TestValid(t *testing.T) {
	t.Parallel()

	const (
		outOfRange    = Rotation(4)
		wayOutOfRange = Rotation(255)
	)

	tests := []struct {
		name string
		rot  Rotation
		want bool
	}{
		{name: "none", rot: None, want: true},
		{name: "clockwise", rot: Clockwise, want: true},
		{name: "upside down", rot: UpsideDown, want: true},
		{name: "counter clockwise", rot: CounterClockwise, want: true},
		{name: "four", rot: outOfRange, want: false},
		{name: "max uint8", rot: wayOutOfRange, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.rot.Valid(); got != tc.want {
				t.Fatalf("Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStringInvalid reaches String's default branch. Rotation is a uint8, so
// a value outside the four constants is reachable without any exported
// constructor, and only this in-package test can produce one directly.
func TestStringInvalid(t *testing.T) {
	t.Parallel()

	const (
		invalid = Rotation(4)
		want    = "invalid"
	)

	if got := invalid.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

// TestMapDefault reaches Map's default branch directly. None already falls
// through to the same return, but an out-of-range value should land there
// too rather than mapping pixels off the buffer.
func TestMapDefault(t *testing.T) {
	t.Parallel()

	const (
		invalid  = Rotation(4)
		physW    = 720
		physH    = 1280
		logicalX = 100
		logicalY = 200
	)

	gotX, gotY := invalid.Map(logicalX, logicalY, physW, physH)
	if gotX != logicalX || gotY != logicalY {
		t.Fatalf("Map(%d, %d, %d, %d) = (%d, %d), want identity (%d, %d)",
			logicalX, logicalY, physW, physH, gotX, gotY, logicalX, logicalY)
	}
}
