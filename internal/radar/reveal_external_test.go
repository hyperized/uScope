package radar_test

import (
	"image"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// revealBand is the height of the band a press reveals at the design size:
// the 16-pixel bold face with three pixels over and under it, inside the
// 16-pixel margin above and below.
const revealBand = 16 + 2*3 + 2*16

// revealSide is one of the two scenes revealPair builds and the canvas it
// draws on.
type revealSide struct {
	scene *radar.Scene
	canv  *canvas.Canvas
}

// draw draws the side's scene at elapsed.
func (r revealSide) draw(elapsed time.Duration) { r.scene.Draw(r.canv, elapsed) }

// revealPair builds two scenes in the same view over the same frame and size,
// one to press keys on and one to leave alone, so what differs between their
// pictures is the reveal and nothing else.
func revealPair(tb testing.TB, view radar.View, width, height int) (revealSide, revealSide) {
	tb.Helper()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	set := radar.Settings{View: view, RangeNm: sceneRangeNm}

	pressed, pressedCanvas, _ := sceneOn(tb, width, height, frame)
	pressed.Apply(set)

	plain, plainCanvas, _ := sceneOn(tb, width, height, frame)
	plain.Apply(set)

	return revealSide{pressed, pressedCanvas}, revealSide{plain, plainCanvas}
}

// pressReveal presses a key nothing binds when a case asks for the key bar,
// which in a bare view brings it up and in a full view changes nothing.
//
//nolint:revive // flag-parameter: reveal is a case's own answer, not a mode to branch deeper on.
func pressReveal(scene *radar.Scene, reveal bool) {
	if reveal {
		pressUnbound(scene)
	}
}

// revealSizes are the uConsole's panel and a 4K canvas, the two sizes the
// bare views run at.
//
//nolint:gochecknoglobals // test fixture.
var revealSizes = []struct {
	name          string
	width, height int
}{
	{"panel", panelWidth, panelHeight},
	{"4K", uhdWidth, uhdHeight},
}

func pressUnbound(scene *radar.Scene) bool {
	return scene.Handle(input.Key{Kind: input.Rune, Rune: 'x'})
}

// TestRevealOverlaysTheBottom presses a key nothing binds in each bare view,
// at both sizes, and checks the frame inside the hold differs from the
// unpressed one only in a band along the bottom, that the band carries the
// softkeys and a full-width rule, and that once the hold is over the two
// frames are the same picture.
func TestRevealOverlaysTheBottom(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewMinimal, radar.ViewMinimal3D} {
		for _, size := range revealSizes {
			t.Run(string(view)+" "+size.name, func(t *testing.T) {
				t.Parallel()

				checkRevealBand(t, view, size.width, size.height)
			})
		}
	}
}

// checkRevealBand is one case of TestRevealOverlaysTheBottom.
func checkRevealBand(t *testing.T, view radar.View, width, height int) {
	t.Helper()

	pressed, plain := revealPair(t, view, width, height)
	edge := height - revealBand*max(1, height/1080)
	rule := image.Rect(0, edge, width, edge+1)

	if pressUnbound(pressed.scene) {
		t.Fatal("x was taken, want it to fall through")
	}

	inside := 2 * time.Second
	pressed.draw(inside)
	plain.draw(inside)

	if !identicalIn(pressed.canv, plain.canv, image.Rect(0, 0, width, edge)) {
		t.Error("the reveal changed the picture above its band, want nothing moved")
	}

	if countColour(pressed.canv, image.Rect(0, edge, width, height), theme.Night.Key) == 0 {
		t.Error("no softkeys in the band inside the hold")
	}

	if got := countColour(pressed.canv, rule, theme.Night.Rule); got != width {
		t.Errorf("the band's top row holds %d rule pixels, want %d", got, width)
	}

	after := inside + 4*time.Second
	pressed.draw(after)
	plain.draw(after)

	if !identical(pressed.canv, plain.canv) {
		t.Error("the frame after the hold differs from one with no press")
	}
}

// TestRevealRestartsOnEachPress checks a second press inside the hold keeps
// the bar up until four seconds after the second press, not the first.
func TestRevealRestartsOnEachPress(t *testing.T) {
	t.Parallel()

	pressed, plain := revealPair(t, radar.ViewMinimal, panelWidth, panelHeight)
	pressed.draw(0)
	pressUnbound(pressed.scene)

	pressed.draw(3 * time.Second)
	pressUnbound(pressed.scene)

	for _, testCase := range []struct {
		name    string
		elapsed time.Duration
		shown   bool
	}{
		{"past the first hold", 5 * time.Second, true},
		{"past the second", 7 * time.Second, false},
	} {
		pressed.draw(testCase.elapsed)
		plain.draw(testCase.elapsed)

		if got := !identical(pressed.canv, plain.canv); got != testCase.shown {
			t.Errorf("%s: bar shown = %v, want %v", testCase.name, got, testCase.shown)
		}
	}
}

// TestRevealLeavesTheFullViewsAlone checks a press in a view with its own key
// bar draws exactly what no press does.
func TestRevealLeavesTheFullViewsAlone(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewScope, radar.View3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			pressed, plain := revealPair(t, view, panelWidth, panelHeight)
			pressUnbound(pressed.scene)
			pressed.draw(0)
			plain.draw(0)

			if !identical(pressed.canv, plain.canv) {
				t.Error("a press changed a full view")
			}
		})
	}
}

// TestRevealKeepsTheQuitKeys checks q and Ctrl-C still fall through to the
// run loop from a bare view, which is what quits.
func TestRevealKeepsTheQuitKeys(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		key  input.Key
	}{
		{"q", input.Key{Kind: input.Rune, Rune: 'q'}},
		{"Q", input.Key{Kind: input.Rune, Rune: 'Q'}},
		{"ctrl-c", input.Key{Kind: input.CtrlC}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, _ := revealPair(t, radar.ViewMinimal3D, panelWidth, panelHeight)
			if scene.scene.Handle(testCase.key) {
				t.Errorf("Handle(%s) = true in a bare view, want it left to the run loop", testCase.name)
			}
		})
	}
}

// TestRevealDrawsWithTheTopBar checks the two bars share a frame: the top bar
// keeps its band and the reveal adds its rule at the bottom.
func TestRevealDrawsWithTheTopBar(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(radar.Settings{View: radar.ViewMinimal, MinimalBar: true})
	pressUnbound(scene)
	scene.Draw(canv, 0)

	if got := canv.Image().RGBAAt(barProbeX, 1); got != theme.Night.Band {
		t.Errorf("top row = %v, want the band %v", got, theme.Night.Band)
	}

	edge := panelHeight - revealBand
	if got := countColour(canv, image.Rect(0, edge, panelWidth, edge+1), theme.Night.Rule); got != panelWidth {
		t.Errorf("the reveal's rule holds %d pixels, want %d", got, panelWidth)
	}
}
