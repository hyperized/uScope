package sprite_test

import (
	"errors"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/sprite"
)

// canvasGrid reports which pixels of a side by side canvas equal col.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func canvasGrid(canv *canvas.Canvas, side int, col color.RGBA) [][]bool {
	grid := make([][]bool, side)

	for y := range grid {
		grid[y] = make([]bool, side)

		for x := range grid[y] {
			grid[y][x] = canv.Image().RGBAAt(x, y) == col
		}
	}

	return grid
}

// mappedBitmapGrid reports, for a side by side destination, which pixels a
// set source pixel in bmp reaches once every (x, y) is passed through
// mapCoord. It is the "rotate the source grid by hand" half of a rotation
// test, kept independent of anything Draw itself computes.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func mappedBitmapGrid(bmp *sprite.Bitmap, side int, mapCoord func(x, y int) (int, int)) [][]bool {
	grid := make([][]bool, side)
	for y := range grid {
		grid[y] = make([]bool, side)
	}

	for y := range side {
		for x := range side {
			if !bmp.At(x, y) {
				continue
			}

			destX, destY := mapCoord(x, y)
			grid[destY][destX] = true
		}
	}

	return grid
}

// assertGridsEqual fails the test at every pixel where got and want differ.
func assertGridsEqual(t *testing.T, got, want [][]bool) {
	t.Helper()

	for y := range want {
		for x := range want[y] {
			if got[y][x] != want[y][x] {
				t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got[y][x], want[y][x])
			}
		}
	}
}

// countPixels returns how many pixels on the canvas equal want.
func countPixels(canv *canvas.Canvas, want color.RGBA) int {
	bounds := canv.Bounds()
	count := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) == want {
				count++
			}
		}
	}

	return count
}

// TestNew covers the four validation errors New can return.
func TestNew(t *testing.T) {
	t.Parallel()

	oversize := sprite.MaxSide + 1
	oversizedRows := make([]string, oversize)

	for idx := range oversizedRows {
		oversizedRows[idx] = strings.Repeat(".", oversize)
	}

	for _, testCase := range []struct {
		name    string
		rows    []string
		wantErr error
	}{
		{name: "nil rows", rows: nil, wantErr: sprite.ErrEmpty},
		{name: "empty rows", rows: []string{}, wantErr: sprite.ErrEmpty},
		{name: "ragged rows", rows: []string{"##", "#"}, wantErr: sprite.ErrRagged},
		{name: "not square", rows: []string{"###", "###"}, wantErr: sprite.ErrNotSquare},
		{name: "too large", rows: oversizedRows, wantErr: sprite.ErrTooLarge},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			bmp, err := sprite.New(testCase.rows)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, testCase.wantErr)
			}

			if bmp != nil {
				t.Errorf("New() bitmap = %v, want nil", bmp)
			}
		})
	}
}

// TestNewValid covers the two clear characters New accepts.
func TestNewValid(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		rows []string
	}{
		{name: "dot as clear", rows: []string{"#.", ".#"}},
		{name: "space as clear", rows: []string{"# ", " #"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			bmp, err := sprite.New(testCase.rows)
			if err != nil {
				t.Fatalf("New(%v): unexpected error: %v", testCase.rows, err)
			}

			const wantSide = 2

			if got := bmp.Side(); got != wantSide {
				t.Errorf("Side() = %d, want %d", got, wantSide)
			}

			if !bmp.At(0, 0) || bmp.At(1, 0) || bmp.At(0, 1) || !bmp.At(1, 1) {
				t.Errorf("At() pattern for %v did not match the rows given", testCase.rows)
			}
		})
	}
}

// TestBitmapSide covers Side on a plain bitmap and on the built-in airplane.
func TestBitmapSide(t *testing.T) {
	t.Parallel()

	bmp, err := sprite.New([]string{"#.#", ".#.", "#.#"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const wantSide = 3

	if got := bmp.Side(); got != wantSide {
		t.Errorf("Side() = %d, want %d", got, wantSide)
	}

	const wantAirplaneSide = 15

	if got := sprite.Airplane().Side(); got != wantAirplaneSide {
		t.Errorf("Airplane().Side() = %d, want %d", got, wantAirplaneSide)
	}
}

// TestBitmapAt covers in-bounds and out-of-bounds coordinates.
func TestBitmapAt(t *testing.T) {
	t.Parallel()

	bmp, err := sprite.New([]string{
		"#.",
		".#",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, testCase := range []struct {
		name string
		x    int
		y    int
		want bool
	}{
		{name: "top-left set", x: 0, y: 0, want: true},
		{name: "top-right clear", x: 1, y: 0, want: false},
		{name: "bottom-left clear", x: 0, y: 1, want: false},
		{name: "bottom-right set", x: 1, y: 1, want: true},
		{name: "negative x", x: -1, y: 0, want: false},
		{name: "negative y", x: 0, y: -1, want: false},
		{name: "x past edge", x: 2, y: 0, want: false},
		{name: "y past edge", x: 0, y: 2, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := bmp.At(testCase.x, testCase.y); got != testCase.want {
				t.Errorf("At(%d, %d) = %v, want %v", testCase.x, testCase.y, got, testCase.want)
			}
		})
	}
}

// TestAirplaneBuildsSuccessfully proves the built-in string art parses,
// which is the only thing that could realistically be wrong with it.
func TestAirplaneBuildsSuccessfully(t *testing.T) {
	t.Parallel()

	bmp := sprite.Airplane()
	if bmp == nil {
		t.Fatal("Airplane() returned nil")
	}

	const wantSide = 15

	if got := bmp.Side(); got != wantSide {
		t.Errorf("Airplane().Side() = %d, want %d", got, wantSide)
	}
}

// TestDrawHeadingZeroIsIdentity draws the airplane onto a canvas exactly its
// own size, centred so canvas coordinates equal bitmap coordinates, and
// checks the two grids match pixel for pixel in both directions.
func TestDrawHeadingZeroIsIdentity(t *testing.T) {
	t.Parallel()

	bmp := sprite.Airplane()
	side := bmp.Side()
	half := (side - 1) / 2

	canv, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	bmp.Draw(canv, half, half, 0, canvas.White)

	got := canvasGrid(canv, side, canvas.White)
	want := mappedBitmapGrid(bmp, side, func(x, y int) (int, int) { return x, y })

	assertGridsEqual(t, got, want)
}

// TestDrawHeading180 checks the point-symmetric prediction (x, y) ->
// (side-1-x, side-1-y) against the whole canvas.
func TestDrawHeading180(t *testing.T) {
	t.Parallel()

	bmp := sprite.Airplane()
	side := bmp.Side()
	half := (side - 1) / 2

	canv, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	const heading180 = 180.0

	bmp.Draw(canv, half, half, heading180, canvas.White)

	got := canvasGrid(canv, side, canvas.White)
	want := mappedBitmapGrid(bmp, side, func(x, y int) (int, int) { return side - 1 - x, side - 1 - y })

	assertGridsEqual(t, got, want)
}

// TestDrawHeading90MatchesHandRotatedCopy uses a small bitmap with a single
// arm sticking up from the centre, not the airplane, so the rotation has
// nothing symmetric about it to hide a sign error. The prediction, (x, y) ->
// (side-1-y, x), is the forward rotation worked out by hand, independent of
// whatever Draw itself computes.
func TestDrawHeading90MatchesHandRotatedCopy(t *testing.T) {
	t.Parallel()

	const side = 5

	//nolint:goconst // ascii art rows repeat on purpose; naming them would hide the shape.
	bmp, err := sprite.New([]string{
		"..#..",
		"..#..",
		"..#..",
		".....",
		".....",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	half := (side - 1) / 2

	canv, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	const heading90 = 90.0

	bmp.Draw(canv, half, half, heading90, canvas.White)

	got := canvasGrid(canv, side, canvas.White)
	want := mappedBitmapGrid(bmp, side, func(x, y int) (int, int) { return side - 1 - y, x })

	assertGridsEqual(t, got, want)
}

// TestDrawHeading90FrontGoesRightRearGoesLeft is the compass-convention
// check: at heading 90 the front of the airframe must swing to the right of
// centre and the rear to the left, not the other way round.
//
// The fuselage runs down the centre column of every row, front and back
// alike, so a point on that column cannot tell a correct rotation from a
// mirrored one: both a real nose and a mirrored tail would light it up. The
// wing shoulder does not have that problem. (frontCol, frontRow) sits on the
// forward shoulder and is set; its point-symmetric twin, (rearCol, rearRow),
// falls behind the wing where the airframe is narrower and is clear. A
// mirrored rotation would swap which of the two destinations lights up, so
// checking both catches the case a same-column check would miss.
func TestDrawHeading90FrontGoesRightRearGoesLeft(t *testing.T) {
	t.Parallel()

	bmp := sprite.Airplane()
	side := bmp.Side()
	half := (side - 1) / 2

	const (
		frontCol, frontRow = 5, 5
		rearCol, rearRow   = 9, 9
	)

	if !bmp.At(frontCol, frontRow) {
		t.Fatalf("test fixture assumption broken: (%d, %d) is not set on the airplane", frontCol, frontRow)
	}

	if bmp.At(rearCol, rearRow) {
		t.Fatalf("test fixture assumption broken: (%d, %d) is set on the airplane", rearCol, rearRow)
	}

	const (
		canvasSide = 30
		centre     = canvasSide / 2
		heading90  = 90.0
	)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	bmp.Draw(canv, centre, centre, heading90, canvas.White)

	frontDestX := centre + (side - 1 - frontRow) - half
	frontDestY := centre + frontCol - half

	rearDestX := centre + (side - 1 - rearRow) - half
	rearDestY := centre + rearCol - half

	if got := canv.Image().RGBAAt(frontDestX, frontDestY); got != canvas.White {
		t.Errorf("front-shoulder point (%d, %d) = %v, want painted", frontDestX, frontDestY, got)
	}

	if got := canv.Image().RGBAAt(rearDestX, rearDestY); got == canvas.White {
		t.Errorf("rear-mirror point (%d, %d) is painted, want clear: sign convention backwards",
			rearDestX, rearDestY)
	}

	if frontDestX <= centre {
		t.Errorf("front-shoulder point landed at x=%d, want right of centre (x>%d)", frontDestX, centre)
	}
}

// TestDrawOutOfBoundsCentres checks that a centre far off the canvas, in
// either direction, and a centre that puts half the sprite off each edge,
// never panics.
func TestDrawOutOfBoundsCentres(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 10
		farOffset  = 100000
	)

	for _, testCase := range []struct {
		name        string
		cx          int
		cy          int
		wantPainted bool
	}{
		{name: "far positive", cx: farOffset, cy: farOffset, wantPainted: false},
		{name: "far negative", cx: -farOffset, cy: -farOffset, wantPainted: false},
		{name: "half off top-left corner", cx: 0, cy: 0, wantPainted: true},
		{name: "half off bottom-right corner", cx: canvasSide - 1, cy: canvasSide - 1, wantPainted: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(canvasSide, canvasSide)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			sprite.Airplane().Draw(canv, testCase.cx, testCase.cy, 0, canvas.White)

			got := countPixels(canv, canvas.White) > 0
			if got != testCase.wantPainted {
				t.Errorf("Draw at (%d, %d) painted = %v, want %v", testCase.cx, testCase.cy, got, testCase.wantPainted)
			}
		})
	}
}

// TestDrawNaNAndInfHeadingActLikeZero checks that an unusable heading falls
// back to the identity rotation instead of drawing nothing or panicking.
func TestDrawNaNAndInfHeadingActLikeZero(t *testing.T) {
	t.Parallel()

	bmp := sprite.Airplane()
	side := bmp.Side()
	half := (side - 1) / 2

	want := mappedBitmapGrid(bmp, side, func(x, y int) (int, int) { return x, y })

	for _, testCase := range []struct {
		name    string
		heading float64
	}{
		{name: "NaN", heading: math.NaN()},
		{name: "positive infinity", heading: math.Inf(1)},
		{name: "negative infinity", heading: math.Inf(-1)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			bmp.Draw(canv, half, half, testCase.heading, canvas.White)

			assertGridsEqual(t, canvasGrid(canv, side, canvas.White), want)
		})
	}
}

// TestDrawNilSafety checks that a nil receiver and a nil destination each
// draw nothing instead of panicking.
func TestDrawNilSafety(t *testing.T) {
	t.Parallel()

	canv, err := canvas.New(1, 1)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	for _, testCase := range []struct {
		name string
		bmp  *sprite.Bitmap
		dst  *canvas.Canvas
	}{
		{name: "nil receiver", bmp: nil, dst: canv},
		{name: "nil destination", bmp: sprite.Airplane(), dst: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testCase.bmp.Draw(testCase.dst, 0, 0, 0, canvas.White)
		})
	}
}

// TestDrawAllocations checks that Draw allocates nothing on the hot path.
//
// It does not call t.Parallel(): testing.AllocsPerRun panics if the test
// runs in parallel, since it needs the allocation count to itself while it
// measures.
//
//nolint:paralleltest // testing.AllocsPerRun panics if called from a parallel test.
func TestDrawAllocations(t *testing.T) {
	const canvasSide = 64

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	bmp := sprite.Airplane()

	const (
		centre  = canvasSide / 2
		heading = 45.0
		runs    = 100
	)

	allocs := testing.AllocsPerRun(runs, func() {
		bmp.Draw(canv, centre, centre, heading, canvas.White)
	})

	if allocs != 0 {
		t.Errorf("Draw allocated %v allocations per run, want 0", allocs)
	}
}

// BenchmarkDraw measures the cost of one rotated stamp onto a 64x64 canvas.
func BenchmarkDraw(b *testing.B) {
	const canvasSide = 64

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		b.Fatalf("canvas.New: %v", err)
	}

	bmp := sprite.Airplane()

	const (
		centre  = canvasSide / 2
		heading = 45.0
	)

	b.ReportAllocs()

	for b.Loop() {
		bmp.Draw(canv, centre, centre, heading, canvas.White)
	}
}
