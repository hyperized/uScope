package radar_test

import (
	"os"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// keyShore is the cap that turns the coastline and its fill on and off.
const keyShore = 'm'

// waterFadeAlpha is how far the tint sits off the field, written out again
// here rather than read from the scene. Two independent copies of the same
// number disagree when one of them changes, which is the point.
const waterFadeAlpha = 0.08

// waterTint is the sea's colour on the palette every test in this file draws
// with.
//
//nolint:gochecknoglobals // color.RGBA is a struct, so this cannot be const.
var waterTint = fadeInto(theme.Night.Field, theme.Night.Data, waterFadeAlpha)

// waterFleetRangeNm is the range the toggle tests draw at. It is pinned rather
// than left on auto so the picture before a key press and the picture after it
// differ in one thing only.
const waterFleetRangeNm = 80

// TestWaterFillFollowsTheShoreKey checks that m governs the fill and the
// outlines together.
//
// They are one picture rather than two overlays. A coastline with no fill
// behind it says where a line is; the fill is what says which side of it is
// sea, and a key that took one away and left the other would leave the scope
// saying half of something.
func TestWaterFillFollowsTheShoreKey(t *testing.T) {
	t.Parallel()

	scene, canv := waterFleetScene(t)
	scene.Draw(canv, 0)

	tintOn := countColour(canv, scopeBox, waterTint)
	shoreOn := countColour(canv, scopeBox, theme.Night.Shore)

	if tintOn == 0 || shoreOn == 0 {
		t.Fatalf("the fixture drew %d tint and %d shore pixels, want both above 0", tintOn, shoreOn)
	}

	if !press(scene, keyShore) {
		t.Fatal("Handle('m') = false, want the scene to take it")
	}

	scene.Draw(canv, 0)

	if got := countColour(canv, scopeBox, waterTint); got != 0 {
		t.Errorf("after m the scope has %d water pixels, want 0", got)
	}

	if got := countColour(canv, scopeBox, theme.Night.Shore); got != 0 {
		t.Errorf("after m the scope has %d shore pixels, want 0", got)
	}

	if !press(scene, keyShore) {
		t.Fatal("Handle('m') = false on the way back, want the scene to take it")
	}

	scene.Draw(canv, 0)

	if got := countColour(canv, scopeBox, waterTint); got != tintOn {
		t.Errorf("after m twice the scope has %d water pixels, want the %d it started with", got, tintOn)
	}
}

// TestWaterFillOffBySetting checks that --shore off starts without the fill,
// not merely without the outlines.
func TestWaterFillOffBySetting(t *testing.T) {
	t.Parallel()

	scene, canv := waterFleetScene(t)
	scene.Apply(radar.Settings{Shore: radar.ToggleOff, RangeNm: waterFleetRangeNm})
	scene.Draw(canv, 0)

	if got := countColour(canv, scopeBox, waterTint); got != 0 {
		t.Errorf("Draw with --shore off = %d water pixels, want 0", got)
	}
}

// TestPerspectiveKeepsOutlinesOnly pins the decision DESIGN.md's open list
// carries: the tilted view draws the coastline and nothing under it.
//
// In perspective the ground is a trapezium running to a horizon rather than a
// disc, so there is no flooded shape for the land to be taken back out of.
// Filling it needs its own answer to where the world stops, and that is a
// separate piece of work from this one.
func TestPerspectiveKeepsOutlinesOnly(t *testing.T) {
	t.Parallel()

	scene, canv := waterFleetScene(t)
	scene.Apply(radar.Settings{View: radar.View3D, RangeNm: waterFleetRangeNm})
	scene.Draw(canv, 0)

	bounds := canv.Bounds()

	if got := countColour(canv, bounds, theme.Night.Shore); got == 0 {
		t.Error("the 3D view drew no coastline, so this test proves nothing about the fill")
	}

	if got := countColour(canv, bounds, waterTint); got != 0 {
		t.Errorf("the 3D view drew %d water pixels, want 0: it keeps outlines only", got)
	}
}

// TestBareViewFillsEdgeToEdge checks the bare flat view's own ground. It has
// no range ring, so the sea is the whole canvas rather than a disc, and the
// corners have to carry the tint the way the middle does.
//
// The set here is outlines with no land half at all, which is what Decode
// hands back and what a caller that only wants coastlines would be holding.
// That is what makes the assertion exact: with nothing to take the water back
// out, a flood that reached the corners leaves all four of them tinted, and a
// disc-shaped one leaves them on the bare field. Over the real data the
// corners are Germany, Belgium and the North Sea, and the two answers would be
// indistinguishable.
func TestBareViewFillsEdgeToEdge(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame,
		radar.WithShore(syntheticShoreSet(t, shoreLineThroughReceiver())))
	scene.Apply(radar.Settings{View: radar.ViewMinimal, RangeNm: waterFleetRangeNm})

	// The bare views start with both overlays off, which is what they are for,
	// so the coastline has to be asked for before there is anything to fill.
	if !press(scene, keyShore) {
		t.Fatal("Handle('m') = false in the bare view, want the scene to take it")
	}

	scene.Draw(canv, 0)

	bounds := canv.Bounds()

	if got := countColour(canv, bounds, waterTint); got == 0 {
		t.Fatal("the bare view drew no water at all")
	}

	corners := []struct {
		name string
		x    int
		y    int
	}{
		{name: "top left", x: bounds.Min.X, y: bounds.Min.Y},
		{name: "top right", x: bounds.Max.X - 1, y: bounds.Min.Y},
		{name: "bottom left", x: bounds.Min.X, y: bounds.Max.Y - 1},
		{name: "bottom right", x: bounds.Max.X - 1, y: bounds.Max.Y - 1},
	}

	for _, corner := range corners {
		if got := canv.Image().RGBAAt(corner.x, corner.y); got != waterTint {
			t.Errorf("the %s corner is %v, want the water tint %v painted edge to edge",
				corner.name, got, waterTint)
		}
	}
}

// TestWaterRenderPNG writes a picture of the fill in each look a decision was
// made about.
//
// It is skipped unless USCOPE_PNG_DIR is set, the same as the other render
// tests here: nothing else in the suite touches disk. What it is for is the
// half a pixel count cannot check. A tint can be measured; whether the sea
// still reads as quieter than the range rings, and whether a coastline drawn
// in the quietest colour in the palette still stands off the water behind it,
// is something you look at.
//
// The five are the five decisions: the tint at a range where the coast is the
// picture and at one where it is a continent, the same thing in phosphor where
// green is the ground rather than a reading, the day page where the fade runs
// towards the paper instead of away from it, and the bare view where the sea
// goes edge to edge with no ring around it.
func TestWaterRenderPNG(t *testing.T) {
	t.Parallel()

	dir := os.Getenv("USCOPE_PNG_DIR")
	if dir == "" {
		t.Skip("USCOPE_PNG_DIR is not set")
	}

	night := []radar.Option{radar.WithPalette(theme.Night)}
	phosphor := []radar.Option{radar.WithPalette(theme.PhosphorNight)}
	monoDay := []radar.Option{radar.WithPalette(theme.MonoDay)}

	const (
		nearNm = 40
		farNm  = 200
	)

	writeWaterPNG(t, dir, "water-glass-40.png", radar.Settings{RangeNm: nearNm}, night, nil)
	writeWaterPNG(t, dir, "water-glass-200.png", radar.Settings{RangeNm: farNm}, night, nil)
	writeWaterPNG(t, dir, "water-phosphor-40.png", radar.Settings{RangeNm: nearNm}, phosphor, nil)
	writeWaterPNG(t, dir, "water-mono-day-40.png", radar.Settings{RangeNm: nearNm}, monoDay, nil)

	// The bare view opens with its overlays off, so the coastline has to be
	// asked for before the picture has anything in it.
	writeWaterPNG(t, dir, "water-minimal.png",
		radar.Settings{RangeNm: nearNm, View: radar.ViewMinimal}, night, []rune{keyShore})
}

// writeWaterPNG draws one frame of the demo fleet over the real coastline and
// writes it.
//
// It loads the embedded data rather than a synthetic set, because the point of
// these pictures is what the fill looks like over a real coast: a rectangle of
// invented land would prove the renderer works and show nothing about whether
// the result reads.
func writeWaterPNG(
	tb testing.TB, dir, name string, set radar.Settings, opts []radar.Option, presses []rune,
) {
	tb.Helper()

	data, err := shore.Load()
	if err != nil {
		tb.Fatalf("shore.Load: %v", err)
	}

	demo, err := source.NewDemo()
	if err != nil {
		tb.Fatalf("source.NewDemo: %v", err)
	}

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene := radar.New(testFaces(tb), demo, scope.New(), append(opts, radar.WithShore(data))...)
	scene.Apply(set)
	drawWithKeys(tb, scene, canv, name, presses)
	savePNG(tb, dir, name, canv)
}

// waterFleetScene is a scene over the real coastline with one aircraft on it,
// at a pinned range, which is what the toggle tests compare pictures of.
func waterFleetScene(tb testing.TB) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	data, err := shore.Load()
	if err != nil {
		tb.Fatalf("shore.Load: %v", err)
	}

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene := radar.New(testFaces(tb), &fakeSource{frame: frame},
		scope.New(scope.WithCurrent(waterFleetRangeNm)), radar.WithShore(data))
	scene.Apply(radar.Settings{RangeNm: waterFleetRangeNm})

	return scene, canv
}
