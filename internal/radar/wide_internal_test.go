package radar

import (
	"image"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
)

// columnCanvas is a canvas wide enough for the split to lay out a column, so
// hiding one is a change rather than a no-op.
//
// It borrows the sizes TestLayoutSplit already measures against rather than
// naming a second pair: wideCanvasWidth is above minColumnWidth for exactly
// this reason.
func columnCanvas(tb testing.TB) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(wideCanvasWidth, wideCanvasHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	return canv
}

// TestToggleWide checks that w flips the column away and back, and that the
// two bare views refuse it.
//
// A bare view has no column to hide and no key bar to say what happened, so
// the key falls through to the run loop there rather than changing a flag
// nothing on screen could show. That is the camera keys' rule, applied to a
// key that works in the two views with chrome instead of the two with a
// camera.
func TestToggleWide(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		view    View
		presses []rune
		want    bool
		taken   bool
	}{
		{name: "the scope takes w", view: ViewScope, presses: []rune{'w'}, want: true, taken: true},
		{name: "the scope takes the shifted twin", view: ViewScope, presses: []rune{'W'}, want: true, taken: true},
		{name: "a second press puts the column back", view: ViewScope, presses: []rune{'w', 'W'}, taken: true},
		{name: "the 3D view takes w", view: View3D, presses: []rune{'w'}, want: true, taken: true},
		{name: "minimal refuses it", view: ViewMinimal, presses: []rune{'w'}},
		{name: "the bare 3D view refuses it", view: ViewMinimal3D, presses: []rune{'w'}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: testCase.view}

			taken := false
			for _, key := range testCase.presses {
				taken = scene.handleRune(key)
			}

			if taken != testCase.taken {
				t.Errorf("the last press was taken = %v, want %v", taken, testCase.taken)
			}

			if scene.wide != testCase.want {
				t.Errorf("wide = %v, want %v", scene.wide, testCase.want)
			}

			if got := scene.capEngaged(capWide); got != testCase.want {
				t.Errorf("capEngaged(capWide) = %v, want it to follow the flag at %v", got, testCase.want)
			}
		})
	}
}

// TestWideSplitTakesTheWholeBox checks the carving itself: with the column
// hidden the scope box is everything the header and the key bar left, and
// there is no column rectangle at all.
//
// The two are one claim rather than two. A box that grew while the column
// stayed would draw the picture over the flight list.
func TestWideSplitTakesTheWholeBox(t *testing.T) {
	t.Parallel()

	canv := columnCanvas(t)
	scene := &Scene{}

	narrow := scene.newLayout(canv)
	narrow.split()

	if narrow.column.Empty() {
		t.Fatal("the ordinary layout left no column, so this comparison proves nothing")
	}

	scene.wide = true

	wide := scene.newLayout(canv)
	wide.split()

	if !wide.column.Empty() {
		t.Errorf("the wide layout kept a column at %v, want none", wide.column)
	}

	want := image.Rect(wide.left, wide.top, wide.right, wide.bottom)
	if wide.scope != want {
		t.Errorf("the wide scope box = %v, want the whole box at %v", wide.scope, want)
	}

	if wide.scope.Dx() <= narrow.scope.Dx() {
		t.Errorf("the wide scope box is %d wide against %d, want it wider", wide.scope.Dx(), narrow.scope.Dx())
	}
}

// TestWideRedrawsTheBackgroundLayer checks that w is in the layer key.
//
// It has to be. The rings are drawn on the layer and the aircraft over it, and
// hiding the column moves the box the rings are centred in; a stale layer
// would leave the traffic sitting beside its own rings until something else in
// the key moved.
func TestWideRedrawsTheBackgroundLayer(t *testing.T) {
	t.Parallel()

	scene, canv, _ := layerScene(t)
	scene.Draw(canv, 0)

	if !scene.handleRune('w') {
		t.Fatal("the w key was not handled, want the scope to take it")
	}

	scene.Draw(canv, 0)

	if scene.layerRuns != 2 {
		t.Errorf("layerRuns after hiding the column = %d, want 2", scene.layerRuns)
	}
}

// TestWide3DReachesPastTheColumn checks the camera rather than the picture: a
// point one range radius due east of the receiver has to land further right
// with the column hidden than with it there.
//
// The camera's lens is the box's own width, so the framing is unchanged and
// the outer ring still spans ringSpan of whatever box it is given. That is
// exactly why the ground grows: the same fraction of a wider box is more
// pixels.
func TestWide3DReachesPastTheColumn(t *testing.T) {
	t.Parallel()

	canv := columnCanvas(t)
	frame := source.Frame{
		Receiver: source.Receiver{
			Latitude: layerBaseLat, Longitude: layerBaseLon, HasFix: true, Mode: source.FixManual,
		},
	}

	scene := wide3DScene()

	narrow, narrowBox := eastEdge3(t, scene, canv, frame)

	scene.wide = true

	wide, wideBox := eastEdge3(t, scene, canv, frame)

	if wide <= narrow {
		t.Errorf("east landed at x=%d wide against x=%d narrow, want it further right", wide, narrow)
	}

	if wide <= narrowBox.Max.X {
		t.Errorf("east landed at x=%d, want it past the old scope box at %d", wide, narrowBox.Max.X)
	}

	if wideBox.Dx() <= narrowBox.Dx() {
		t.Errorf("the wide viewport is %d across against %d, want it wider", wideBox.Dx(), narrowBox.Dx())
	}
}

// wide3DScene is a scene in the 3D view with a still camera, so the azimuth
// the case above measures against is the one the scene starts on and not one
// an orbit has wound forward.
func wide3DScene() *Scene {
	return &Scene{
		shown:      View3D,
		scopeRange: scope.New(scope.WithCurrent(layerRangeNm)),
		elevation:  defaultElevation,
		exaggerate: DefaultExaggerate,
	}
}

// eastEdge3 measures where a point one range radius due east of the receiver
// lands, and hands back the scope box it was measured in.
//
// The layout is carved the way Draw carves it. The header and the key bar both
// come to nothing without faces to set them in, which is what this fixture
// wants: the only thing left moving the box is the split.
func eastEdge3(tb testing.TB, scene *Scene, canv *canvas.Canvas, frame source.Frame) (int, image.Rectangle) {
	tb.Helper()

	lay := scene.newLayout(canv)
	lay.bottom -= scene.keyBarHeight(&lay)
	lay.top += scene.headerHeight(&lay)
	lay.split()

	view, drawable := scene.measure3D(&lay, frame, 0)
	if !drawable {
		tb.Fatal("measure3D refused a panel-sized box, want a camera")
	}

	east, _, visible := view.cam.at(point3{east: view.scopeNm})
	if !visible {
		tb.Fatal("the camera could not see a point on the outer ring, want it in frame")
	}

	return east, lay.scope
}
