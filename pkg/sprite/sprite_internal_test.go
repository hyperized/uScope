package sprite

import "testing"

// TestInvRotate checks the inverse-rotation arithmetic directly, at the four
// axis-aligned angles, with sin and cos passed in as exact values rather than
// computed from degrees. That keeps the case away from any floating-point
// noise degrees-to-radians conversion would add, so it tests only the
// rotation itself.
func TestInvRotate(t *testing.T) {
	t.Parallel()

	const (
		offsetX = 3
		offsetY = 1
	)

	for _, testCase := range []struct {
		name   string
		sin    float64
		cos    float64
		wantSX int
		wantSY int
	}{
		{name: "0 degrees", sin: 0, cos: 1, wantSX: offsetX, wantSY: offsetY},
		{name: "90 degrees", sin: 1, cos: 0, wantSX: offsetY, wantSY: -offsetX},
		{name: "180 degrees", sin: 0, cos: -1, wantSX: -offsetX, wantSY: -offsetY},
		{name: "270 degrees", sin: -1, cos: 0, wantSX: -offsetY, wantSY: offsetX},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotSX, gotSY := invRotate(offsetX, offsetY, testCase.sin, testCase.cos)
			if gotSX != testCase.wantSX || gotSY != testCase.wantSY {
				t.Errorf("invRotate(%d, %d, %v, %v) = (%d, %d), want (%d, %d)",
					offsetX, offsetY, testCase.sin, testCase.cos, gotSX, gotSY, testCase.wantSX, testCase.wantSY)
			}
		})
	}
}

// TestMustBitmapPanicsOnInvalidRows proves the panic path a bad literal in
// this file would take is reachable and actually fires, rather than being
// dead code that happens to never run.
func TestMustBitmapPanicsOnInvalidRows(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("mustBitmap did not panic on ragged rows")
		}
	}()

	mustBitmap([]string{"##", "#"})
}

// TestAirplaneSingleton checks that the sync.OnceValue memoization actually
// memoizes: two calls must return the same pointer, not two separately built
// bitmaps.
func TestAirplaneSingleton(t *testing.T) {
	t.Parallel()

	first := airplane()
	second := airplane()

	if first != second {
		t.Error("airplane() returned different pointers across calls")
	}
}

// TestArrowSingleton checks that the sync.OnceValue memoization actually
// memoizes: two calls must return the same pointer, not two separately built
// bitmaps.
func TestArrowSingleton(t *testing.T) {
	t.Parallel()

	first := arrow()
	second := arrow()

	if first != second {
		t.Error("arrow() returned different pointers across calls")
	}
}

// TestNewPixelLayout checks the packed pixel slice directly against a known
// small pattern, white-box, rather than only through At.
func TestNewPixelLayout(t *testing.T) {
	t.Parallel()

	bmp, err := New([]string{
		"#.",
		".#",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const wantSide = 2

	if bmp.side != wantSide {
		t.Fatalf("side = %d, want %d", bmp.side, wantSide)
	}

	want := []bool{true, false, false, true}

	if len(bmp.pixels) != len(want) {
		t.Fatalf("pixels length = %d, want %d", len(bmp.pixels), len(want))
	}

	for idx, wantSet := range want {
		if bmp.pixels[idx] != wantSet {
			t.Errorf("pixels[%d] = %v, want %v", idx, bmp.pixels[idx], wantSet)
		}
	}
}
