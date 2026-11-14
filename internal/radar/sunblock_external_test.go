package radar_test

import (
	"image"
	"testing"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
)

// TestSunBlockGlyphs pins the reason the sun block has no RISE and SET
// spelling: every embedded face carries both arrows.
func TestSunBlockGlyphs(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		load func() (*psf.Font, error)
	}{
		{"small", fonts.Small},
		{"body", fonts.Body},
		{"body bold", fonts.BodyBold},
		{"large", fonts.Large},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			face, err := testCase.load()
			if err != nil {
				t.Fatalf("load: %v", err)
			}

			for _, arrow := range []rune{'↑', '↓'} {
				if _, has := face.Glyph(arrow); !has {
					t.Errorf("the %s face has no %q", testCase.name, arrow)
				}
			}
		})
	}
}

// TestHeaderSunBlock checks the header carries the sun block between the
// receiver line and the UTC clock when the receiver has a position, and
// nothing there without one.
func TestHeaderSunBlock(t *testing.T) {
	t.Parallel()

	// The strip right of the receiver line and left of the UTC clock at the
	// panel's size. The sun block's NIGHT ↑ 07:24 ↓ 19:52 lands in it.
	strip := image.Rect(500, 8, 1000, 56)

	for _, testCase := range []struct {
		name     string
		receiver source.Receiver
		want     bool
	}{
		{"with a position", sceneFrame().Receiver, true},
		{"with none", source.Receiver{}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
			frame.Receiver = testCase.receiver

			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
			scene.Draw(canv, 0)

			inked := strip.Dx()*strip.Dy()-countColour(canv, strip, theme.Night.Band) > 0
			if inked != testCase.want {
				t.Errorf("ink between the receiver line and the clocks = %v, want %v", inked, testCase.want)
			}
		})
	}
}

// TestSunBlockGivesWayOnANarrowHeader checks the block drops whole at 640
// wide when a long source label needs the room, rather than cutting the
// label, and that the same canvas still holds it beside a short one.
func TestSunBlockGivesWayOnANarrowHeader(t *testing.T) {
	t.Parallel()

	const width, height = 640, 480

	// Right of the receiver line, so the only thing that can differ between
	// a frame with a position and one without is the sun block.
	strip := image.Rect(220, 0, width, 60)

	for _, testCase := range []struct {
		name, label string
		wantBlock   bool
	}{
		{"a short label leaves room", "LIVE", true},
		{"a long label takes it", "BEAST 192.168.1.159:30005", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
			frame.Source.Label = testCase.label

			with, withCanvas, _ := sceneOn(t, width, height, frame)
			with.Draw(withCanvas, 0)

			frame.Receiver = source.Receiver{}

			without, withoutCanvas, _ := sceneOn(t, width, height, frame)
			without.Draw(withoutCanvas, 0)

			if drawn := !identicalIn(withCanvas, withoutCanvas, strip); drawn != testCase.wantBlock {
				t.Errorf("sun block drawn = %v, want %v", drawn, testCase.wantBlock)
			}
		})
	}
}
