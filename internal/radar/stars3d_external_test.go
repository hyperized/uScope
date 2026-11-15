package radar_test

import (
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// starsAfter is a render clock past the key bar's hold, so a frame drawn
// after a key press carries no bar and compares with one that had no press.
const starsAfter = 10 * time.Second

// starsScene is the bare tilted view over the demo fleet with the sun at
// about elevation degrees, the camera at its lowest tilt and stopped after
// turns presses of Right, with the stars as given. Each press is fifteen
// degrees, and the first one is also what stops the orbit, so turns has to be
// at least one for two frames drawn at different render clocks to agree. The
// lights are left at their default, off, for the same reason.
func starsScene(tb testing.TB, elevation float64, turns int, stars radar.Toggle) *radar.Scene {
	tb.Helper()

	set, err := shore.Load()
	if err != nil {
		tb.Fatalf("shore.Load: %v", err)
	}

	scene := radar.New(testFaces(tb), skyDemo(tb, elevation), scope.New(), radar.WithShore(set))
	scene.Apply(radar.Settings{View: radar.ViewMinimal3D, Stars: stars})
	lowerTilt(tb, scene)

	for range turns {
		scene.Handle(input.Key{Kind: input.Right})
	}

	scene.SetPalette(theme.Night)

	return scene
}

// starsFrame draws one frame of scene at the sky tests' size.
func starsFrame(tb testing.TB, scene *radar.Scene) *canvas.Canvas {
	tb.Helper()

	return starsFrameAt(tb, scene, starsAfter)
}

// starsFrameAt is starsFrame at a render clock of the caller's choosing, for
// a scene that has had a key pressed after its last frame and has to be drawn
// past that press's hold.
func starsFrameAt(tb testing.TB, scene *radar.Scene, elapsed time.Duration) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(skyWidth, skyHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(canv, elapsed)

	return canv
}

// starLight is how much brighter the on frame is than the off one, summed
// over every pixel and channel, and the lowest row that differs at all.
func starLight(tb testing.TB, on, off *canvas.Canvas) (int, int) {
	tb.Helper()

	onPix, offPix := on.Image().Pix, off.Image().Pix
	stride := on.Image().Stride
	light, lowest := 0, -1

	for offset := range onPix {
		if offset%4 == 3 || onPix[offset] == offPix[offset] {
			continue
		}

		if onPix[offset] < offPix[offset] {
			tb.Fatalf("a star darkened byte %d", offset)
		}

		light += int(onPix[offset]) - int(offPix[offset])
		lowest = max(lowest, offset/stride)
	}

	return light, lowest
}

// TestStarsFadeThroughDusk checks the stars are in a night sky and only above
// the horizon, dimmer half way through civil twilight, and gone once the sun
// is up.
func TestStarsFadeThroughDusk(t *testing.T) {
	t.Parallel()

	lightAt := func(elevation float64) (int, int) {
		on := starsFrame(t, starsScene(t, elevation, 1, radar.ToggleOn))
		off := starsFrame(t, starsScene(t, elevation, 1, radar.ToggleOff))

		return starLight(t, on, off)
	}

	night, lowest := lightAt(-20)
	if night == 0 {
		t.Fatal("no stars in a night sky")
	}

	if lowest >= skyHorizonRow {
		t.Errorf("a star landed on row %d, at or under the horizon on %d", lowest, skyHorizonRow)
	}

	if dusk, _ := lightAt(-3); dusk == 0 || dusk >= night {
		t.Errorf("stars at dusk add %d, want some and less than the %d of night", dusk, night)
	}

	if day, _ := lightAt(2); day != 0 {
		t.Errorf("stars by day add %d, want none", day)
	}
}

// TestStarsSlideWithTheCamera checks turning the camera moves the field, and
// a whole turn puts it back where it was.
func TestStarsSlideWithTheCamera(t *testing.T) {
	t.Parallel()

	const wholeTurn = 360 / 15

	first := starsFrame(t, starsScene(t, -20, 1, radar.ToggleOn)).Image().Pix
	turned := starsFrame(t, starsScene(t, -20, 2, radar.ToggleOn)).Image().Pix
	round := starsFrame(t, starsScene(t, -20, 1+wholeTurn, radar.ToggleOn)).Image().Pix

	if string(first) == string(turned) {
		t.Error("fifteen degrees of turn left the frame as it was")
	}

	if string(first) != string(round) {
		t.Error("a whole turn round did not put the stars back")
	}
}

// TestStarsKey checks z turns the stars off and on in the tilted views, so
// the frame after it matches one built with the stars off and the frame after
// Z one built with them on, and that z falls through in the flat views.
func TestStarsKey(t *testing.T) {
	t.Parallel()

	press := input.Key{Kind: input.Rune, Rune: 'z'}

	scene := starsScene(t, -20, 1, radar.ToggleOn)
	if !scene.Handle(press) {
		t.Fatal("z was not taken in minimal3d")
	}

	off := starsFrame(t, starsScene(t, -20, 1, radar.ToggleOff))
	if got := starsFrame(t, scene); string(got.Image().Pix) != string(off.Image().Pix) {
		t.Error("the frame after z differs from one with the stars off")
	}

	if !scene.Handle(input.Key{Kind: input.Rune, Rune: 'Z'}) {
		t.Fatal("Z was not taken in minimal3d")
	}

	on := starsFrame(t, starsScene(t, -20, 1, radar.ToggleOn))
	if got := starsFrameAt(t, scene, 2*starsAfter); string(got.Image().Pix) != string(on.Image().Pix) {
		t.Error("the frame after Z differs from one with the stars on")
	}

	flat := radar.New(testFaces(t), &fakeSource{frame: source.Frame{}}, scope.New())
	if flat.Handle(press) {
		t.Error("z was taken in the scope view, want it to fall through")
	}
}
