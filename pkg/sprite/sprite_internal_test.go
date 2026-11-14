package sprite

import (
	"image"
	"math"
	"strings"
	"testing"

	"github.com/hyperized/uScope/pkg/canvas"
)

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

func TestStamp(t *testing.T) {
	t.Parallel()

	const side = 16

	for _, testCase := range []struct {
		name  string
		scale int
		// block is the half-open rectangle expected to be set.
		block image.Rectangle
	}{
		{name: "scale negative is one pixel", scale: -1, block: image.Rect(9, 7, 10, 8)},
		{name: "scale zero is one pixel", scale: 0, block: image.Rect(9, 7, 10, 8)},
		{name: "scale one is one pixel", scale: 1, block: image.Rect(9, 7, 10, 8)},
		{name: "scale two is a block", scale: 2, block: image.Rect(10, 6, 12, 8)},
		{name: "scale three is a block", scale: 3, block: image.Rect(11, 5, 14, 8)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			Airplane().stamp(canv, 8, 8, 1, -1, testCase.scale, canvas.White)

			for col := range side {
				for row := range side {
					want := image.Pt(col, row).In(testCase.block)

					if got := canv.Image().RGBAAt(col, row) == canvas.White; got != want {
						t.Errorf("pixel (%d, %d): set = %v, want %v", col, row, got, want)
					}
				}
			}
		})
	}
}

// TestLandingIsThePixelDrawSampled checks Landing against Draw's own inverse
// walk, at every whole degree and for every pixel of a filled bitmap: the
// destination it answers with samples the pixel asked about whenever any
// destination does, and is a painted one when none does.
//
//nolint:varnamelen // dx, dy are offsets from the centre, the coordinate idiom used throughout uScope.
func TestLandingIsThePixelDrawSampled(t *testing.T) {
	t.Parallel()

	bmp := squareBitmap('#')
	half := (bmp.side - 1) / 2
	fellBack := false

	for degrees := range 360 {
		turn := TurnTo(float64(degrees))
		sampled := sampledAt(bmp, turn)

		for y := range bmp.side {
			for x := range bmp.side {
				pixel := image.Pt(x, y)
				got := bmp.Landing(pixel, turn)
				srcX, srcY := invRotate(got.X, got.Y, turn.sin, turn.cos)
				hit := image.Pt(half+srcX, half+srcY) == pixel

				if sampled[pixel] && !hit {
					t.Fatalf("at %d degrees pixel %v landed on %v, which samples another pixel", degrees, pixel, got)
				}

				if !hit && !bmp.At(half+srcX, half+srcY) {
					t.Fatalf("at %d degrees unsampled pixel %v landed on %v, which Draw left empty",
						degrees, pixel, got)
				}

				fellBack = fellBack || !hit
			}
		}
	}

	if !fellBack {
		t.Error("no pixel at any angle went unsampled, so the fallback is untested")
	}
}

// TestLandingFallsBackToTheRoundedTurn checks the last resort: on a bitmap
// with nothing painted, a pixel no destination samples lands on the forward
// turn rounded, because there is no painted neighbour to prefer.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func TestLandingFallsBackToTheRoundedTurn(t *testing.T) {
	t.Parallel()

	bmp := squareBitmap('.')
	half := (bmp.side - 1) / 2

	for degrees := range 360 {
		turn := TurnTo(float64(degrees))
		sampled := sampledAt(bmp, turn)

		for y := range bmp.side {
			for x := range bmp.side {
				pixel := image.Pt(x, y)
				if sampled[pixel] {
					continue
				}

				srcX, srcY := float64(x-half), float64(y-half)
				want := image.Pt(
					int(math.Round(srcX*turn.cos-srcY*turn.sin)), int(math.Round(srcX*turn.sin+srcY*turn.cos)),
				)

				if got := bmp.Landing(pixel, turn); got != want {
					t.Fatalf("at %d degrees pixel %v landed on %v, want the rounded turn %v", degrees, pixel, got, want)
				}

				return
			}
		}
	}

	t.Error("no pixel went unsampled at any angle, so the fallback is untested")
}

// squareBitmap is a 15x15 bitmap of one character.
func squareBitmap(char byte) *Bitmap {
	const side = 15

	rows := make([]string, 0, side)
	for range side {
		rows = append(rows, strings.Repeat(string(char), side))
	}

	return mustBitmap(rows)
}

// sampledAt is the set of bitmap pixels Draw samples at least once at turn.
//
//nolint:varnamelen // dx, dy are offsets from the centre, the coordinate idiom used throughout uScope.
func sampledAt(bmp *Bitmap, turn Turn) map[image.Point]bool {
	half := (bmp.side - 1) / 2
	sampled := map[image.Point]bool{}

	for dy := -(half + 1); dy <= half+1; dy++ {
		for dx := -(half + 1); dx <= half+1; dx++ {
			srcX, srcY := invRotate(dx, dy, turn.sin, turn.cos)
			sampled[image.Pt(half+srcX, half+srcY)] = true
		}
	}

	return sampled
}

// TestBitmapLights checks only the built-in silhouette carries lights, and
// that they are its two wingtips, its tail and the centre of its fuselage.
func TestBitmapLights(t *testing.T) {
	t.Parallel()

	got, carried := Airplane().Lights()
	want := Lights{Port: image.Pt(1, 8), Starboard: image.Pt(13, 8), Tail: image.Pt(7, 14), Beacon: image.Pt(7, 7)}

	if !carried || got != want {
		t.Errorf("Airplane().Lights() = %+v, %v, want %+v, true", got, carried, want)
	}

	for name, pixel := range map[string]image.Point{
		"port": got.Port, "starboard": got.Starboard, "tail": got.Tail, "beacon": got.Beacon,
	} {
		if !Airplane().At(pixel.X, pixel.Y) {
			t.Errorf("the %s light at %v is not a pixel of the silhouette", name, pixel)
		}
	}

	if _, carried := mustBitmap([]string{"#"}).Lights(); carried {
		t.Error("a bitmap from New carries lights, want none")
	}
}

// TestTurnToTreatsNonFiniteAsNorth checks TurnTo folds the headings Draw
// folds, so a light and the sprite under it cannot disagree about one.
func TestTurnToTreatsNonFiniteAsNorth(t *testing.T) {
	t.Parallel()

	for _, heading := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := TurnTo(heading); got != (Turn{sin: 0, cos: 1}) {
			t.Errorf("TurnTo(%v) = %+v, want north", heading, got)
		}
	}
}
