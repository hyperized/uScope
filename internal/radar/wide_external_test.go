package radar_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// Where the blocks the w key hides sit at the panel's resolution.
//
// columnBox is the whole right-hand column, from the gap past the scope square
// to the right margin. columnGapBox is that gap itself, the sixteen pixels of
// blockGap between the two, which nothing draws in while the column is there.
// wideCapBox is the W cap in the bottom bar: it sits right after the eleven
// caps that were in keyCaps before it, so its left edge is where the bar used
// to end, and its width is one small glyph plus the cap's padding either side.
//
//nolint:gochecknoglobals // a rectangle is data, and image.Rectangle cannot be const.
var (
	columnBox    = image.Rect(625, 77, 1264, 670)
	columnGapBox = image.Rect(609, 77, 625, 670)
	wideCapBox   = image.Rect(780, 686, 796, 704)

	// columnTypeBox is the column with its first seventy-five pixels left out,
	// which is where the cases below count the reading colour.
	//
	// The home marker is set in Ink as well, and once the scope takes the
	// whole width it sits at the middle of the frame, a few pixels inside the
	// column's left edge. The card and the rows set type all the way across
	// the column, so starting past the marker costs nothing and keeps the
	// count about them.
	columnTypeBox = image.Rect(700, 77, 1264, 670)
)

// TestWideHidesTheColumn checks the whole point of the key: with it on, the
// card, the rows and the legend are gone and what they stood on is field.
//
// The count is of the reading colour rather than of painted pixels. With the
// column hidden the scope takes the whole width and its rings run through
// where the card used to be, so "nothing is drawn there" is not true and was
// never the claim. Ink is: the column's three blocks are the only thing in the
// frame that sets type in it, so an Ink pixel inside the column box is a block
// that did not go away.
func TestWideHidesTheColumn(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(radar.Settings{RangeNm: sceneRangeNm})
	scene.Draw(canv, 0)

	before := countColour(canv, columnTypeBox, theme.Night.Ink)
	if before == 0 {
		t.Fatal("the column set no type before w, so this comparison proves nothing")
	}

	// A pixel the card actually painted, so the check below is about that
	// pixel rather than about a corner nothing ever reached.
	card, found := firstPainted(canv, columnBox)
	if !found {
		t.Fatal("the column drew nothing at all before w")
	}

	if !press(scene, 'w') {
		t.Fatal("the w key was not handled, want the scope to take it")
	}

	scene.Draw(canv, 0)

	if got := countColour(canv, columnTypeBox, theme.Night.Ink); got != 0 {
		t.Errorf("the column set %d ink pixels after w, want none", got)
	}

	if got := canv.Image().RGBAAt(card.X, card.Y); got != theme.Night.Field {
		t.Errorf("the pixel at %v where the card was = %v, want the field %v", card, got, theme.Night.Field)
	}
}

// TestWideCentresTheScope checks that the ring moves to the middle of the
// frame rather than staying where the column left it.
//
// It is measured off the picture itself: the boundary ring's own two crossings
// of the row it is centred on. A circle is symmetric about its centre whatever
// the rounding, which a glyph is not, so the midpoint of those two is the
// centre to the pixel. The airfield markers are turned off first because they
// are drawn in the same colour and one of them falling on that row would be
// counted as part of the ring.
func TestWideCentresTheScope(t *testing.T) {
	t.Parallel()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame())
	scene.Apply(radar.Settings{RangeNm: sceneRangeNm})

	if !press(scene, 'a') {
		t.Fatal("the a key was not handled, want the airfield toggle to take it")
	}

	scene.Draw(canv, 0)

	narrow, found := ringMiddle(canv)
	if !found {
		t.Fatal("nothing was drawn on the ring's own row, so there is no centre to measure")
	}

	if !press(scene, 'w') {
		t.Fatal("the w key was not handled, want the scope to take it")
	}

	scene.Draw(canv, 0)

	wide, found := ringMiddle(canv)
	if !found {
		t.Fatal("the wide scope drew nothing on the ring's own row")
	}

	middle := canv.Bounds().Min.X + canv.Bounds().Dx()/2
	if abs(wide-middle) > 1 {
		t.Errorf("the wide ring is centred on x=%d, want the canvas centre at %d", wide, middle)
	}

	if abs(narrow-middle) <= 1 {
		t.Errorf("the ordinary ring was already centred on x=%d, so this proves nothing", narrow)
	}
}

// ringRow is the canvas row the ring's own centre falls on at the panel's
// resolution: the scope box runs from just under the header to just above the
// key bar, and the rings are centred in it. The box keeps that height whether
// or not there is a column beside it, so the row is the same in both.
const ringRow = (77 + 670) / 2

// ringMiddle is the midpoint of the boundary ring's two crossings of its own
// centre row, which is the ring's centre.
//
// It reads the rule colour rather than every painted pixel. The cardinal
// letters sit further out on the same row and their ink does not fill a glyph
// cell evenly, so a plain extent would be a few pixels off centre for a reason
// that has nothing to do with where the ring is.
func ringMiddle(canv *canvas.Canvas) (int, bool) {
	row := image.Rect(canv.Bounds().Min.X, ringRow, canv.Bounds().Max.X, ringRow+1)

	first, last := colourExtent(canv, row, theme.Night.Rule)
	if first < 0 {
		return 0, false
	}

	return (first + last) / 2, true
}

// colourExtent is paintedExtent for one exact colour: the leftmost and
// rightmost columns of box holding a pixel of col, or -1 for neither.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func colourExtent(canv *canvas.Canvas, box image.Rectangle, col color.RGBA) (int, int) {
	area := box.Intersect(canv.Bounds())
	first, last := -1, -1

	for x := area.Min.X; x < area.Max.X; x++ {
		for y := area.Min.Y; y < area.Max.Y; y++ {
			if canv.Image().RGBAAt(x, y) != col {
				continue
			}

			if first < 0 {
				first = x
			}

			last = x

			break
		}
	}

	return first, last
}

// firstPainted is the first pixel in box, read left to right and then down,
// that is not the field.
func firstPainted(canv *canvas.Canvas, box image.Rectangle) (image.Point, bool) {
	area := box.Intersect(canv.Bounds())

	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) != theme.Night.Field {
				return image.Pt(x, y), true
			}
		}
	}

	return image.Point{}, false
}

// abs is the absolute value of an int, which the standard library has for
// floats and not for these.
func abs(value int) int {
	if value < 0 {
		return -value
	}

	return value
}

// TestWide3DCrossesTheColumnBoundary checks that the perspective viewport
// grows with the box rather than staying the square the column left.
//
// The gap is the evidence. blockGap is the sixteen pixels between the scope
// square and the column, and nothing draws in it while the column is there; a
// ground ring running through it is the picture on the far side of a boundary
// that is no longer a boundary.
func TestWide3DCrossesTheColumnBoundary(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(view3DSettings())
	scene.Draw(canv, 0)

	if got := painted(canv, columnGapBox); got != 0 {
		t.Fatalf("the ordinary 3D view drew %d pixels in the column gap, want none", got)
	}

	if countColour(canv, columnTypeBox, theme.Night.Ink) == 0 {
		t.Fatal("the column set no type in the 3D view, so this comparison proves nothing")
	}

	if !press(scene, 'w') {
		t.Fatal("the w key was not handled, want the 3D view to take it")
	}

	scene.Draw(canv, 0)

	if painted(canv, columnGapBox) == 0 {
		t.Error("the wide 3D view drew nothing in the column gap, want the ground running through it")
	}

	if got := countColour(canv, columnTypeBox, theme.Night.Ink); got != 0 {
		t.Errorf("the column set %d ink pixels in the wide 3D view, want none", got)
	}
}

// TestWideLeavesTheBareViewsAlone checks that w is refused where there is no
// column, and that refusing it really does leave the picture alone.
//
// Both halves matter. A key that changed a flag without changing anything on
// screen would be a key whose effect turned up the next time v came round to a
// view with a column.
func TestWideLeavesTheBareViewsAlone(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		presses int
	}{
		{name: "minimal mode", presses: pressViewMinimal},
		{name: "the bare 3D view", presses: pressViewMinimal3D},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
			scene.Apply(radar.Settings{RangeNm: sceneRangeNm})
			pressView(t, scene, testCase.presses)
			scene.Draw(canv, 0)

			before, err := canvas.New(panelWidth, panelHeight)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			scene.Draw(before, 0)

			if press(scene, 'w') {
				t.Error("the w key was taken in a bare view, want it to fall through to the run loop")
			}

			scene.Draw(canv, 0)

			if !identical(canv, before) {
				t.Error("w changed the picture in a bare view, want it left exactly as it was")
			}
		})
	}
}

// TestWideCapFollowsTheKey checks that the bar says which state the column is
// in, the way every other toggle's cap does: filled while the scope is wide
// and a hollow outline while the column is there.
func TestWideCapFollowsTheKey(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(radar.Settings{RangeNm: sceneRangeNm})
	scene.Draw(canv, 0)

	hollowInk := countColour(canv, wideCapBox, theme.Night.Ink)
	hollowField := countColour(canv, wideCapBox, theme.Night.Field)

	if hollowInk >= hollowField {
		t.Errorf("column shown: %d ink and %d field pixels, want a hollow cap", hollowInk, hollowField)
	}

	if !press(scene, 'w') {
		t.Fatal("the w key was not handled, want the scope to take it")
	}

	scene.Draw(canv, 0)

	filledInk := countColour(canv, wideCapBox, theme.Night.Ink)
	filledField := countColour(canv, wideCapBox, theme.Night.Field)

	if filledInk <= filledField {
		t.Errorf("column hidden: %d ink and %d field pixels, want a filled cap", filledInk, filledField)
	}
}

// TestWideRenderPNG writes the demo fleet with the column hidden, flat and in
// perspective, to a directory an operator names.
//
// It is skipped unless USCOPE_PNG_DIR is set, for the reason
// TestFilterRenderPNG is: nothing else in the suite touches disk, and a CI run
// has no directory worth writing these into. What it is for is the half a
// pixel count cannot check, which for this key is whether a scope with the
// whole frame to itself actually looks better than one with a column beside
// it.
func TestWideRenderPNG(t *testing.T) {
	t.Parallel()

	dir := os.Getenv("USCOPE_PNG_DIR")
	if dir == "" {
		t.Skip("USCOPE_PNG_DIR is not set")
	}

	writeViewPNG(t, dir, "wide-scope.png", radar.Settings{RangeNm: sceneRangeNm}, 'w')
	writeViewPNG(t, dir, "wide-3d.png", view3DSettings(), 'w')
}

// writeViewPNG draws one frame of the demo fleet under a settings block, with
// any keys in presses sent first, and writes the result to name inside dir.
//
// The keys go through Handle rather than through a setter, because some of
// what these pictures show is reachable from the keyboard and nowhere else.
// A first frame is drawn before them for the reason writeFilterPNG draws one:
// a key that reads the legend needs a legend to have been counted.
func writeViewPNG(tb testing.TB, dir, name string, set radar.Settings, presses ...rune) {
	tb.Helper()

	demo, err := source.NewDemo()
	if err != nil {
		tb.Fatalf("source.NewDemo: %v", err)
	}

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene := radar.New(testFaces(tb), demo, scope.New())
	scene.Apply(set)
	scene.Draw(canv, 0)

	for _, key := range presses {
		if !press(scene, key) {
			tb.Fatalf("the %c key was not handled while drawing %s", key, name)
		}
	}

	scene.Draw(canv, 0)
	savePNG(tb, dir, name, canv)
}

// savePNG writes a canvas out as a PNG under an operator-named directory.
func savePNG(tb testing.TB, dir, name string, canv *canvas.Canvas) {
	tb.Helper()

	path := filepath.Join(dir, name)

	file, err := os.Create(path) //nolint:gosec // an operator-named directory, the trust boundary --png also runs at.
	if err != nil {
		tb.Fatalf("os.Create(%s): %v", path, err)
	}

	defer func() { _ = file.Close() }()

	if err := png.Encode(file, canv.Image()); err != nil {
		tb.Fatalf("png.Encode(%s): %v", path, err)
	}
}
