package radar_test

import (
	"os"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
)

// TestLooksRenderPNG writes one picture of each of the two looks the k key
// added: phosphor and mono, night and day, plus phosphor tilted into the 3D
// view.
//
// It is skipped unless USCOPE_PNG_DIR is set, the same as TestGlassRenderPNG:
// nothing else in this suite touches disk. A palette's pixel counts can be
// checked test by test, but whether the whole picture still reads is not
// something a pixel count answers, and the four pairs here are the decisions
// worth looking at over a picture rather than a number: does the CRT green
// still separate an aeroplane from its field, does the day paper hold the
// same grammar, does mono's contrast-only selection actually stand out with
// no accent hue behind it, and does phosphor still make sense tilted into the
// perspective view.
//
// The aircraft is pinned with the same number of n presses
// glass-strips-cursor.png uses, so all five pictures here and that one show
// the same aeroplane selected and can be set side by side.
func TestLooksRenderPNG(t *testing.T) {
	t.Parallel()

	dir := os.Getenv("USCOPE_PNG_DIR")
	if dir == "" {
		t.Skip("USCOPE_PNG_DIR is not set")
	}

	phosphorNight := []radar.Option{radar.WithPalette(theme.PhosphorNight)}
	phosphorDay := []radar.Option{radar.WithPalette(theme.PhosphorDay)}
	monoNight := []radar.Option{radar.WithPalette(theme.MonoNight)}
	monoDay := []radar.Option{radar.WithPalette(theme.MonoDay)}

	writeGlassPNG(t, dir, "phosphor-night.png", radar.Settings{}, phosphorNight, steps(cursorStep))
	writeGlassPNG(t, dir, "phosphor-day.png", radar.Settings{}, phosphorDay, steps(cursorStep))
	writeGlassPNG(t, dir, "mono-night.png", radar.Settings{}, monoNight, steps(cursorStep))
	writeGlassPNG(t, dir, "mono-day.png", radar.Settings{}, monoDay, steps(cursorStep))
	writeGlassPNG(t, dir, "phosphor-3d.png", radar.Settings{View: radar.View3D}, phosphorNight, nil)
}
