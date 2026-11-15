package radar

import (
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/pkg/canvas"
)

// barLine is the bar's design height before scaling: one 16-pixel line of the
// bold Terminus face, the header's six pixels over and under it, and the rule.
const barLine = 16 + 2*headerPadY + ruleHeight

func barScene(tb testing.TB, shown View, width, height int) (*Scene, *canvas.Canvas) {
	tb.Helper()

	canv, err := canvas.New(width, height)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	frame := layerFrame(layerBaseLat)
	frame.Now = time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC)

	scene := New(layerTestFaces(tb), &stubSource{frame: frame}, scope.New(scope.WithCurrent(waterProbeRangeNm)))
	scene.shown = shown
	scene.minimalBar = true

	return scene, canv
}

func TestBarHeight(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name          string
		view          View
		width, height int
		off, noFaces  bool
		want          int
	}{
		{name: "minimal at the panel's size", view: ViewMinimal, width: 1280, height: 720, want: barLine},
		{name: "bare 3d at 1080p", view: ViewMinimal3D, width: 1920, height: 1080, want: barLine},
		{
			name: "bare 3d at 4K scales with the layout", view: ViewMinimal3D,
			width: 3840, height: 2160, want: 2 * barLine,
		},
		{name: "switched off", view: ViewMinimal3D, width: 1280, height: 720, off: true},
		{name: "the scope has a header instead", view: ViewScope, width: 1280, height: 720},
		{name: "the 3d view has a header instead", view: View3D, width: 1280, height: 720},
		{name: "a canvas too short to give it up", view: ViewMinimal, width: 200, height: barLine},
		{name: "no faces", view: ViewMinimal, width: 1280, height: 720, noFaces: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv := barScene(t, testCase.view, testCase.width, testCase.height)
			scene.minimalBar = !testCase.off

			if testCase.noFaces {
				scene.faces = Faces{}
			}

			lay := scene.newLayout(canv)
			if got := scene.barHeight(&lay); got != testCase.want {
				t.Errorf("barHeight = %d, want %d", got, testCase.want)
			}
		})
	}
}

// TestBarMovesTheProjectionDown checks the flat bare view centres its
// projection in what the bar leaves rather than on the whole canvas, so the
// picture moves down by half the bar instead of being painted over.
func TestBarMovesTheProjectionDown(t *testing.T) {
	t.Parallel()

	scene, canv := barScene(t, ViewMinimal, 1280, 720)
	lay := scene.newLayout(canv)
	bar := scene.barHeight(&lay)
	lay.scope = belowBar(canv.Bounds(), bar)

	view, drawable := scene.measureScope(&lay, layerFrame(layerBaseLat).Receiver)
	if !drawable {
		t.Fatal("measureScope found no room")
	}

	if want := bar + (720-bar)/2; view.geom.centerY != want {
		t.Errorf("centerY = %d, want %d", view.geom.centerY, want)
	}

	if box := belowBar(canv.Bounds(), 0); box != canv.Bounds() {
		t.Errorf("belowBar with no bar = %v, want the canvas %v", box, canv.Bounds())
	}
}

// TestBarRebuildsTheLayer checks h in the flat bare view with its shore on
// rebuilds the background, since the shore has to move with the centre.
func TestBarRebuildsTheLayer(t *testing.T) {
	t.Parallel()

	scene, canv := barScene(t, ViewMinimal, 1280, 720)
	scene.minimalShore = true
	scene.minimalBar = false

	scene.Draw(canv, 0)
	scene.Draw(canv, 0)

	before := scene.layerRuns

	if !scene.toggleBar() {
		t.Fatal("toggleBar refused in minimal")
	}

	scene.Draw(canv, 0)

	if scene.layerRuns != before+1 {
		t.Errorf("layerRuns = %d after h, want %d", scene.layerRuns, before+1)
	}
}

func TestDrawBarWithNoHeightDrawsNothing(t *testing.T) {
	t.Parallel()

	scene, canv := barScene(t, ViewMinimal, 320, 240)
	canv.Clear(scene.pal.Field)

	lay := scene.newLayout(canv)
	scene.drawBar(&lay, layerFrame(layerBaseLat), 0)

	if got := paintedIn(canv, canv.Bounds()); got != 0 {
		t.Errorf("drawBar(0) painted %d pixels, want none", got)
	}
}

// TestBarRectsStayInsideTheCanvas is the bar's own bounds check: the fill and
// the rule end exactly where the scope box begins.
func TestBarRectsStayInsideTheCanvas(t *testing.T) {
	t.Parallel()

	scene, canv := barScene(t, ViewMinimal3D, 1920, 1080)
	scene.Draw(canv, 0)

	lay := scene.newLayout(canv)
	bar := scene.barHeight(&lay)

	if got := canv.Image().RGBAAt(5, bar-1); got != scene.pal.Rule {
		t.Errorf("row %d = %v, want the rule %v", bar-1, got, scene.pal.Rule)
	}

	if box := belowBar(canv.Bounds(), bar); box.Min.Y != bar || box.Max != canv.Bounds().Max {
		t.Errorf("scope box = %v, want it to start under the rule", box)
	}
}

// TestBarKeyOnlyInBareViews checks h flips the bar in the two bare views and
// falls through in the two with chrome, leaving the setting alone there.
func TestBarKeyOnlyInBareViews(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		view  View
		taken bool
	}{
		{ViewScope, false},
		{View3D, false},
		{ViewMinimal, true},
		{ViewMinimal3D, true},
	} {
		t.Run(string(testCase.view), func(t *testing.T) {
			t.Parallel()

			scene, _ := barScene(t, testCase.view, 1280, 720)
			scene.minimalBar = false

			if got := scene.Handle(input.Key{Kind: input.Rune, Rune: 'h'}); got != testCase.taken {
				t.Fatalf("h taken = %v, want %v", got, testCase.taken)
			}

			if scene.minimalBar != testCase.taken {
				t.Errorf("minimalBar after h = %v, want %v", scene.minimalBar, testCase.taken)
			}

			if testCase.taken && (!scene.Handle(input.Key{Kind: input.Rune, Rune: 'H'}) || scene.minimalBar) {
				t.Error("H did not put the bar away again")
			}
		})
	}
}
