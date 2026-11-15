package radar_test

import (
	"image"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// TestTubesKey checks u is taken in all four views and takes the corridors
// off the picture there, and that U puts them back.
func TestTubesKey(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewScope, radar.View3D, radar.ViewMinimal, radar.ViewMinimal3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			scene, canv := benchSceneWith(t, exposureFrame(t))
			scene.Apply(radar.Settings{View: view})

			if !press(scene, 'u') {
				t.Fatal("u was not taken")
			}

			scene.Draw(canv, 0)
			off := snapshot(canv)

			if !press(scene, 'U') {
				t.Fatal("U was not taken")
			}

			scene.Draw(canv, 0)

			if identical(off, canv) {
				t.Error("the picture after U is the one after u, want the corridors back on it")
			}
		})
	}
}

// snapshot copies a canvas, so a picture can be held while the scene draws
// the next one over it.
func snapshot(canv *canvas.Canvas) *canvas.Canvas {
	bounds := canv.Bounds()

	held, err := canvas.New(bounds.Dx(), bounds.Dy())
	if err != nil {
		panic(err)
	}

	copy(held.Image().Pix, canv.Image().Pix)

	return held
}

// TestTubesCapFollowsTheKey checks the flat scope's U cap carries the engaged
// bar while the corridors are on, which is how a scene starts, and loses it
// after u.
func TestTubesCapFollowsTheKey(t *testing.T) {
	t.Parallel()

	engaged := image.Rect(
		tubesCapBox.Min.X+keyEngagedInsetTest, tubesCapBox.Max.Y-keyPadYTest,
		tubesCapBox.Max.X-keyEngagedInsetTest, tubesCapBox.Max.Y-keyPadYTest+keyEngagedHeightTest,
	)

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame())
	scene.Draw(canv, 0)

	if got := countColour(canv, engaged, theme.Night.OK); got == 0 {
		t.Error("fresh scene: no OK pixels under the U cap, want the engaged bar")
	}

	press(scene, 'u')
	scene.Draw(canv, 0)

	if got := countColour(canv, engaged, theme.Night.OK); got != 0 {
		t.Errorf("after u: %d OK pixels under the U cap, want none", got)
	}
}

// TestTransitRoutesGetNoTube checks a transit route paints nothing in the
// flat scope and both tilted views, where an inbound route on the same path
// does: the tubes are for the field's arrivals and departures, and through
// traffic keeps only its hairlines, which the tilted views do not draw.
func TestTransitRoutesGetNoTube(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewScope, radar.View3D, radar.ViewMinimal3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			draw := func(class source.TrackClass, routed bool) *canvas.Canvas {
				frame := exposureFrame(t)
				frame.Routes = nil

				if routed {
					route := tubeCap()[1]
					route.Class = class
					frame.Routes = []source.Route{route}
				}

				return drawExposureCase(t, frame, radar.Settings{View: view, Shore: radar.ToggleOff}, theme.Night)
			}

			bare := draw(source.TrackInbound, false)

			if got := differing(draw(source.TrackTransit, true), bare); got != 0 {
				t.Errorf("a transit route changed %d pixels, want none", got)
			}

			if got := differing(draw(source.TrackInbound, true), bare); got == 0 {
				t.Error("an inbound route on the same path changed nothing")
			}
		})
	}
}
