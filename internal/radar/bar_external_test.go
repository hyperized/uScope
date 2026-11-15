package radar_test

import (
	"image"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
)

// barProbeX is a column of the bar that holds no type at the panel's size:
// right of the wordmark and the source label, left of the sun block.
const barProbeX = panelWidth / 2

// TestBarDrawsAcrossTheTop draws both bare views with the bar on and off. On,
// the top rows are the band and a rule closes it; off, the band colour is
// nowhere in that strip.
func TestBarDrawsAcrossTheTop(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewMinimal, radar.ViewMinimal3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
			scene.Apply(radar.Settings{View: view, MinimalBar: true})
			scene.Draw(canv, 0)

			if got := canv.Image().RGBAAt(barProbeX, 1); got != theme.Night.Band {
				t.Errorf("top row = %v, want the band %v", got, theme.Night.Band)
			}

			if countColour(canv, image.Rect(0, 0, panelWidth, panelHeight/4), theme.Night.Rule) < panelWidth {
				t.Error("no full-width rule under the bar")
			}

			if painted(canv, image.Rect(panelWidth*3/5, 0, panelWidth, 40)) == 0 {
				t.Error("nothing on the right of the bar, want the clocks")
			}

			bare, plain, _ := sceneOn(t, panelWidth, panelHeight, frame)
			bare.Apply(radar.Settings{View: view})
			bare.Draw(plain, 0)

			if countColour(plain, image.Rect(0, 0, panelWidth, 4), theme.Night.Band) != 0 {
				t.Error("the band colour is on the top rows with the bar off")
			}
		})
	}
}

// TestBarIsInertInTheFullViews checks MinimalBar changes nothing in the
// views that have a header instead.
func TestBarIsInertInTheFullViews(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewScope, radar.View3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

			with, withCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
			with.Apply(radar.Settings{View: view, MinimalBar: true})
			with.Draw(withCanvas, 0)

			without, withoutCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
			without.Apply(radar.Settings{View: view})
			without.Draw(withoutCanvas, 0)

			if !identical(withCanvas, withoutCanvas) {
				t.Error("MinimalBar changed a full view")
			}
		})
	}
}
