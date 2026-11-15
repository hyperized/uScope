package radar_test

import (
	"bytes"
	"image"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// boardFrameScene draws two aircraft in the view set asks for and returns the
// pixels.
func boardFrameScene(t *testing.T, set radar.Settings) []byte {
	t.Helper()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)

	_, canv := minimal3DScene(t, frame, set)

	return bytes.Clone(canv.Image().Pix)
}

// TestBoardLeavesTheFull3DViewAlone checks that Board means nothing outside the
// bare 3D view: the full 3D view draws byte for byte the same picture with it
// on, camera framing included.
func TestBoardLeavesTheFull3DViewAlone(t *testing.T) {
	t.Parallel()

	off := view3DSettings()
	on := off
	on.Board = true

	if !bytes.Equal(boardFrameScene(t, off), boardFrameScene(t, on)) {
		t.Error("Board changed the full 3D view's picture")
	}
}

// TestBoardDrawsTheColumn checks the bare 3D view with the board on gives the
// right quarter to a column with a hairline down its left edge and type in it,
// and that the bare view with it off has neither.
func TestBoardDrawsTheColumn(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)

	set := minimal3DSettings()
	set.Board = true

	_, canv := minimal3DScene(t, frame, set)

	edge := panelWidth - panelWidth/4
	rule := image.Rect(edge, 0, edge+1, panelHeight)
	column := image.Rect(edge+1, 0, panelWidth, panelHeight)

	if got := countColour(canv, rule, theme.Night.Rule); got != panelHeight {
		t.Errorf("the column's left hairline has %d rule pixels, want %d", got, panelHeight)
	}

	if countColour(canv, column, theme.Night.Ink) == 0 {
		t.Error("the column set no type in the reading ink: no callsigns on the board")
	}

	_, bare := minimal3DScene(t, frame, minimal3DSettings())

	if got := countColour(bare, rule, theme.Night.Rule); got != 0 {
		t.Errorf("the bare 3D view with the board off drew %d rule pixels down the column's edge", got)
	}
}

// checkBoardToggle checks j left the column's hairline drawn and that J takes
// the board off again, in the scene and on the canvas.
func checkBoardToggle(t *testing.T, scene *radar.Scene, canv *canvas.Canvas, rule image.Rectangle) {
	t.Helper()

	scene.Draw(canv, pastKeyBarHold)

	if got := countColour(canv, rule, theme.Night.Rule); got != panelHeight {
		t.Errorf("after j the column's hairline has %d rule pixels, want %d", got, panelHeight)
	}

	if !press(scene, 'J') {
		t.Fatal("J was not handled in the bare 3D view")
	}

	if scene.Current().Board {
		t.Error("J did not flip the board back off")
	}

	scene.Draw(canv, 2*pastKeyBarHold)

	if got := countColour(canv, rule, theme.Night.Rule); got != 0 {
		t.Errorf("after J the column's edge still has %d rule pixels", got)
	}
}

// TestBoardKey checks j flips the board in the bare 3D view, which the
// column's hairline shows, is reported back through Current, and falls
// through in every other view.
func TestBoardKey(t *testing.T) {
	t.Parallel()

	edge := panelWidth - panelWidth/4
	rule := image.Rect(edge, 0, edge+1, panelHeight)

	for _, testCase := range []struct {
		view radar.View
		want bool
	}{
		{view: radar.ViewMinimal3D, want: true},
		{view: radar.ViewMinimal},
		{view: radar.View3D},
		{view: radar.ViewScope},
	} {
		t.Run(string(testCase.view), func(t *testing.T) {
			t.Parallel()

			scene, canv := benchScene(t)
			scene.Apply(radar.Settings{View: testCase.view})

			if got := press(scene, 'j'); got != testCase.want {
				t.Fatalf("j in %s = %v, want %v", testCase.view, got, testCase.want)
			}

			if got := scene.Current().Board; got != testCase.want {
				t.Errorf("Current().Board after j in %s = %v, want %v", testCase.view, got, testCase.want)
			}

			if !testCase.want {
				return
			}

			checkBoardToggle(t, scene, canv, rule)
		})
	}
}

// TestBoardRoundTrips checks Apply(Current()) keeps the board on.
func TestBoardRoundTrips(t *testing.T) {
	t.Parallel()

	scene, _ := benchScene(t)
	scene.Apply(radar.Settings{View: radar.ViewMinimal3D, Board: true})

	scene.Apply(scene.Current())

	if !scene.Current().Board {
		t.Error("Apply(Current()) turned the board off")
	}
}
