package radar

import (
	"image"
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// revealKey is a key nothing binds in any view, so a press of it changes no
// state except the reveal's.
var revealKey = input.Key{Kind: input.Rune, Rune: 'x'} //nolint:gochecknoglobals // test fixture.

func TestKeyBarHoldIsFourSeconds(t *testing.T) {
	t.Parallel()

	if keyBarHold != 4*time.Second {
		t.Errorf("keyBarHold = %v, want 4s", keyBarHold)
	}
}

// TestKeyBarRevealed walks the hold through a bare view and out of one: it
// starts on a press, runs for keyBarHold, and is never on in a full view.
func TestKeyBarRevealed(t *testing.T) {
	t.Parallel()

	const pressed = 10 * time.Second

	for _, testCase := range []struct {
		name    string
		view    View
		press   bool
		leave   bool
		elapsed time.Duration
		want    bool
	}{
		{name: "no press yet", view: ViewMinimal, elapsed: pressed},
		{name: "on the frame of the press", view: ViewMinimal, press: true, elapsed: pressed, want: true},
		{
			name: "just inside the hold", view: ViewMinimal3D, press: true,
			elapsed: pressed + keyBarHold - time.Millisecond, want: true,
		},
		{name: "at the end of the hold", view: ViewMinimal, press: true, elapsed: pressed + keyBarHold},
		{name: "a clock started again", view: ViewMinimal, press: true, elapsed: pressed - time.Second},
		{name: "a press in the scope", view: ViewScope, press: true, elapsed: pressed},
		{name: "a press in the 3d view", view: View3D, press: true, elapsed: pressed},
		{name: "v away inside the hold", view: ViewMinimal, press: true, leave: true, elapsed: pressed},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, _ := barScene(t, testCase.view, 1280, 720)
			scene.elapsed = pressed

			if testCase.press {
				scene.Handle(revealKey)
			}

			if testCase.leave {
				scene.shown = ViewScope
			}

			if got := scene.keyBarRevealed(testCase.elapsed); got != testCase.want {
				t.Errorf("keyBarRevealed(%v) = %v, want %v", testCase.elapsed, got, testCase.want)
			}
		})
	}
}

// TestNoteKeyRestartsTheHold checks a second press inside the hold moves its
// end out to keyBarHold after the second press.
func TestNoteKeyRestartsTheHold(t *testing.T) {
	t.Parallel()

	scene, _ := barScene(t, ViewMinimal, 1280, 720)
	scene.Handle(revealKey)

	scene.elapsed = 3 * time.Second
	scene.Handle(revealKey)

	if !scene.keyBarRevealed(6 * time.Second) {
		t.Error("the bar is gone 3s after the second press, want it held")
	}

	if scene.keyBarRevealed(7 * time.Second) {
		t.Error("the bar is still up 4s after the second press, want it gone")
	}
}

// TestNoteKeyInAFullViewCancels checks v out of a bare view drops the hold,
// so it does not come back on screen when v cycles round again.
func TestNoteKeyInAFullViewCancels(t *testing.T) {
	t.Parallel()

	scene, _ := barScene(t, ViewMinimal3D, 1280, 720)
	scene.Handle(revealKey)

	if !scene.keyPressed {
		t.Fatal("a press in the bare 3d view did not start the hold")
	}

	scene.Handle(input.Key{Kind: input.Rune, Rune: 'v'})

	if scene.keyPressed {
		t.Error("v into the scope left the hold running")
	}
}

// TestBareCapsAreKeysThatWork presses every key the bare rows show, in the
// bare view that shows it, and checks each one does something there: taken
// by the scene, or one of the three the run loop binds (q, l and k).
func TestBareCapsAreKeysThatWork(t *testing.T) {
	t.Parallel()

	loop := map[string]bool{"Q": true, "L": true, "K": true}

	keys := map[string]input.Key{
		"+/-": {Kind: input.Rune, Rune: '+'},
		"←→":  {Kind: input.Left},
		"[ ]": {Kind: input.Rune, Rune: '['},
	}

	for _, testCase := range []struct {
		view View
		caps []keyCap
	}{
		{ViewMinimal, bareCaps[:]},
		{ViewMinimal3D, bareCaps[:]},
		{ViewMinimal3D, bare3DCaps[:]},
	} {
		for _, entry := range testCase.caps {
			t.Run(string(testCase.view)+" "+entry.key, func(t *testing.T) {
				t.Parallel()

				scene, _ := barScene(t, testCase.view, 1280, 720)
				scene.biasSupported = true

				key, mapped := keys[entry.key]
				if !mapped {
					key = input.Key{Kind: input.Rune, Rune: []rune(entry.key)[0] + ('a' - 'A')}
				}

				if got := scene.Handle(key); got == loop[entry.key] {
					t.Errorf("Handle(%s) = %v in %s, want %v", entry.key, got, testCase.view, !loop[entry.key])
				}
			})
		}
	}
}

// TestBareCapsLeaveOutDeadKeys pins the three caps the bare rows drop.
func TestBareCapsLeaveOutDeadKeys(t *testing.T) {
	t.Parallel()

	dead := []string{"W", "E", "↑↓"}

	for _, entry := range append(bareCaps[:], bare3DCaps[:]...) {
		if slices.Contains(dead, entry.key) {
			t.Errorf("the bare rows carry %s, which does nothing in a bare view", entry.key)
		}
	}
}

// TestCapEngagedReadsTheBarePair checks the A, M and H caps light from the
// toggles a bare view actually draws with, not from the scope's.
func TestCapEngagedReadsTheBarePair(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		view  View
		which capToggle
		want  bool
	}{
		{name: "shore in minimal", view: ViewMinimal, which: capShore, want: true},
		{name: "airports in minimal", view: ViewMinimal, which: capAirports},
		{name: "bar in minimal", view: ViewMinimal, which: capBar, want: true},
		{name: "shore in the scope", view: ViewScope, which: capShore},
		{name: "airports in the scope", view: ViewScope, which: capAirports, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, _ := barScene(t, testCase.view, 1280, 720)
			scene.shoreOn, scene.minimalShore = false, true
			scene.airports, scene.minimalAirports = true, false

			if got := scene.capEngaged(testCase.which); got != testCase.want {
				t.Errorf("capEngaged = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestCapLabelForTheBarCap(t *testing.T) {
	t.Parallel()

	scene, _ := barScene(t, ViewMinimal, 1280, 720)

	if got := scene.capLabel(bareCaps[len(bareCaps)-1]); got != "BAR" {
		t.Errorf("capLabel(H) = %q, want BAR", got)
	}
}

// TestRevealWithNoFacesDrawsNothing checks a reveal on a scene with nothing
// to set the caps in leaves the canvas alone rather than painting an empty
// band.
func TestRevealWithNoFacesDrawsNothing(t *testing.T) {
	t.Parallel()

	scene, canv := barScene(t, ViewMinimal, 320, 240)
	scene.faces = Faces{}
	scene.Handle(revealKey)
	canv.Clear(scene.pal.Field)

	lay := scene.newLayout(canv)
	scene.drawRevealedKeyBar(&lay, 0)

	if got := paintedIn(canv, canv.Bounds()); got != 0 {
		t.Errorf("drawRevealedKeyBar painted %d pixels with no faces, want none", got)
	}
}

// TestBareCapsFitAtTheirWidestState measures the two rows a reveal draws at
// their widest, with the bias-tee cap and every cycling cap on its longest
// word, and checks each stops inside the margin with its last cap drawn. The
// ends are the pen past the last box and its key gap, set to measured values
// so a change to any cap shows up here.
func TestBareCapsFitAtTheirWidestState(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		view View
		end  int
	}{
		{name: "bare flat", view: ViewMinimal, end: 956},
		{name: "bare 3D", view: ViewMinimal3D, end: 1206},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			scene := &Scene{
				faces:         layerTestFaces(t),
				shown:         testCase.view,
				biasSupported: true,
				colour:        ColourAirline,
				trail:         trailShort,
				filter:        filterState{kind: filterBand, band: bandMid},
				sunTheme:      autoTheme{on: true},
			}
			scene.SetPalette(theme.PhosphorNight)

			lay := layout{dst: canv, right: layerCanvasWidth - baseMargin}

			pen := scene.drawCaps(&lay, baseMargin, 0, bareCaps[:])
			pen = scene.drawCaps(&lay, pen, 0, biasCap[:])

			if scene.perspective() {
				pen = scene.drawCaps(&lay, pen, 0, bare3DCaps[:])
			}

			if pen >= lay.right {
				t.Errorf("the widest row reached x=%d, want it to stop before the %d margin", pen, lay.right)
			}

			if pen != testCase.end {
				t.Errorf("the widest row ended at x=%d, want %d", pen, testCase.end)
			}
		})
	}
}

// TestDrawCapRowStopsAtTheMargin checks a row whose own caps already fill the
// width draws no tail after them.
func TestDrawCapRowStopsAtTheMargin(t *testing.T) {
	t.Parallel()

	canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene := &Scene{faces: layerTestFaces(t), shown: View3D, biasSupported: true}
	scene.SetPalette(theme.Night)

	lay := layout{dst: canv, right: baseMargin + 1}

	scene.drawCapRow(&lay, 0, keyCaps[:1], nil, view3DCaps[:])

	// The SELECT cap, the one drawn, ends at x=86.
	tail := image.Rect(100, 0, layerCanvasWidth, layerCanvasHeight)
	for y := tail.Min.Y; y < tail.Max.Y; y++ {
		for x := tail.Min.X; x < tail.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) != (color.RGBA{}) {
				t.Fatalf("a pixel at (%d, %d) was drawn past the margin's first cap", x, y)
			}
		}
	}
}
