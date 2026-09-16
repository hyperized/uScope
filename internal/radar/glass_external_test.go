package radar_test

import (
	"os"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The keys these pictures reach for, named so the calls below say which
// control they are pressing rather than which letter.
const (
	keyWide   = 'w'
	keyFilter = 'f'
)

// TestGlassRenderPNG writes one picture of every part of the glass grammar to
// a directory an operator names.
//
// It is skipped unless USCOPE_PNG_DIR is set, the same as the other render
// tests here: nothing else in the suite touches disk, and a CI run has no
// directory worth writing these into. What it is for is the half a pixel count
// cannot check, which for a palette is whether the thing actually reads.
//
// The seven cases are the seven decisions worth looking at over a picture: the
// two themes, the column hidden, the perspective view, an emergency on the
// board, the airline colours against the new palette, and the filter.
func TestGlassRenderPNG(t *testing.T) {
	t.Parallel()

	dir := os.Getenv("USCOPE_PNG_DIR")
	if dir == "" {
		t.Skip("USCOPE_PNG_DIR is not set")
	}

	night := []radar.Option{radar.WithPalette(theme.Night)}
	day := []radar.Option{radar.WithPalette(theme.Day)}
	airline := []radar.Option{radar.WithPalette(theme.Night), radar.WithColour(radar.ColourAirline)}

	writeGlassPNG(t, dir, "glass-scope-night.png", radar.Settings{}, night, nil)
	writeGlassPNG(t, dir, "glass-scope-day.png", radar.Settings{}, day, nil)
	writeGlassPNG(t, dir, "glass-wide-night.png", radar.Settings{}, night, []rune{keyWide})
	writeGlassPNG(t, dir, "glass-3d-night.png", radar.Settings{View: radar.View3D}, night, nil)
	writeGlassPNG(t, dir, "glass-airline-night.png", radar.Settings{}, airline, nil)
	writeGlassPNG(t, dir, "glass-filter.png", radar.Settings{}, night, []rune{keyFilter})

	// The emergency picture is the plain scope with nothing pressed, because
	// the demo fleet's nearest contact is the one squawking 7600 and the
	// selection starts on the nearest. The assertion below is what stops that
	// quietly becoming untrue: a fleet with no emergency in it would still
	// render, and the picture would prove nothing.
	requireEmergencyIsNearest(t)
	writeGlassPNG(t, dir, "glass-strips-emergency.png", radar.Settings{}, night, nil)
}

// requireEmergencyIsNearest checks the demo fleet still opens with the
// aircraft that is squawking an emergency, which is what makes
// glass-strips-emergency.png a picture of the warning box on a full strip
// rather than a picture of some other aeroplane.
//
// The fleet arrives sorted by distance and the selection starts on the nearest,
// so the first aircraft in the frame is the one that gets the full strip.
func requireEmergencyIsNearest(tb testing.TB) {
	tb.Helper()

	demo, err := source.NewDemo()
	if err != nil {
		tb.Fatalf("source.NewDemo: %v", err)
	}

	planes := demo.Frame().Planes
	if len(planes) == 0 {
		tb.Fatal("the demo fleet is empty, so the emergency picture would show nothing")
	}

	if !planes[0].Emergency {
		tb.Fatalf("the demo fleet's nearest contact %s is not squawking an emergency, so the picture is mislabelled",
			planes[0].ICAO)
	}
}

// writeGlassPNG draws one frame of the demo fleet under a settings block and a
// palette, sends any keys after the first frame, and writes the result.
//
// The keys go after a frame rather than before it for the reason
// writeFilterPNG sends its own that way: the filter walks the legend, and the
// legend is only known once a frame has been counted.
func writeGlassPNG(
	tb testing.TB, dir, name string, set radar.Settings, opts []radar.Option, presses []rune,
) {
	tb.Helper()

	demo, err := source.NewDemo()
	if err != nil {
		tb.Fatalf("source.NewDemo: %v", err)
	}

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene := radar.New(testFaces(tb), demo, scope.New(), opts...)
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
