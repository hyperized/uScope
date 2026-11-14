package radar_test

import (
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
	"github.com/hyperized/uScope/pkg/sun"
)

// Where the test receiver near Schiphol stands, which is the position the
// sky's dusk and the demo fleet are worked out around.
const (
	schipholLat = 52.3105
	schipholLon = 4.7683
)

// A 1080p frame, which is what the sky is drawn for.
const (
	skyWidth  = 1920
	skyHeight = 1080

	// skyHorizonRow is where the horizon falls on that frame at the default
	// tilt of ten degrees, worked out here from the lens rather than asked of the
	// package: the camera's focal length is the frame's width, so the
	// horizon sits width times tan(10 degrees), 338.55 pixels and 339 once
	// rounded, above the middle row, 540. It is the first row of ground.
	skyHorizonRow = 201
)

// skyEvening is an instant the sun stands at about target degrees at the
// test receiver near Schiphol, found on the evening of the March equinox.
func skyEvening(tb testing.TB, target float64) time.Time {
	tb.Helper()

	instant := time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC)
	for range 24 * 360 {
		if sun.Elevation(schipholLat, schipholLon, instant) <= target {
			return instant
		}

		instant = instant.Add(10 * time.Second)
	}

	tb.Fatalf("the sun never fell to %g degrees", target)

	return instant
}

// skyDemo is the demo fleet around the test receiver near Schiphol with its clock
// pinned where the sun stands at about elevation degrees.
func skyDemo(tb testing.TB, elevation float64) *source.Demo {
	tb.Helper()

	instant := skyEvening(tb, elevation)

	demo, err := source.NewDemo(
		source.WithDemoLocation(schipholLat, schipholLon),
		source.WithDemoClock(func() time.Time { return instant }),
	)
	if err != nil {
		tb.Fatalf("source.NewDemo: %v", err)
	}

	return demo
}

// lowerTilt takes a tilted view's camera down to its lowest elevation, ten
// degrees, the way the [ key does on the device. Six presses from the default
// of 35 is one more than it needs; the last is refused at the bottom.
func lowerTilt(tb testing.TB, scene *radar.Scene) {
	tb.Helper()

	for range 6 {
		press(scene, '[')
	}
}

// skyDraw draws one frame of a tilted view at a 1080p size and ten degrees of tilt
// over the real coastline, from src, and returns the scene and its canvas.
func skyDraw(tb testing.TB, src source.Source, pal theme.Palette, view radar.View) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	set, err := shore.Load()
	if err != nil {
		tb.Fatalf("shore.Load: %v", err)
	}

	canv, err := canvas.New(skyWidth, skyHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene := radar.New(testFaces(tb), src, scope.New(), radar.WithShore(set))
	scene.Apply(radar.Settings{View: view})
	lowerTilt(tb, scene)
	scene.SetPalette(pal)
	scene.Draw(canv, 0)

	return scene, canv
}

// TestSkyAtDusk checks the picture a dusk frame shows: a sky that is not
// the field across the top, warm along the horizon, and the field from the
// horizon down, where nothing models the ground and a dark land under the
// sky is what a horizon looks like.
func TestSkyAtDusk(t *testing.T) {
	t.Parallel()

	_, canv := skyDraw(t, skyDemo(t, -3), theme.Night, radar.ViewMinimal3D)
	img := canv.Image()

	top := img.RGBAAt(skyWidth/2, 0)
	above := img.RGBAAt(skyWidth/2, skyHorizonRow-1)

	if top == theme.Night.Field {
		t.Errorf("the top of a dusk sky is the field %v", top)
	}

	if above.R <= above.B {
		t.Errorf("the sky's last row %v is not warm at dusk", above)
	}

	for _, x := range []int{0, skyWidth / 4, skyWidth - 1} {
		for _, y := range []int{skyHorizonRow, skyHorizonRow + 1} {
			if got := img.RGBAAt(x, y); got != theme.Night.Field {
				t.Errorf("under the horizon at (%d, %d) is %v, want the field", x, y, got)
			}
		}
	}
}

// TestSkyIsTheFieldWithoutASun checks the two cases that must look exactly as
// they did before the sky: deep night, and no position at all. The bare view
// is the one whose sky is its only clear; the full view keeps the plain
// field clear and is covered by the white-box TestPaintSky.
func TestSkyIsTheFieldWithoutASun(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		src  source.Source
	}{
		{name: "the bare view at night", src: skyDemo(t, -20)},
		{name: "the bare view with no position", src: &fakeSource{frame: source.Frame{Now: skyEvening(t, -3)}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, canv := skyDraw(t, testCase.src, theme.Night, radar.ViewMinimal3D)
			img := canv.Image()

			for _, y := range []int{0, skyHorizonRow / 2, skyHorizonRow} {
				if got := img.RGBAAt(skyWidth/3, y); got != theme.Night.Field {
					t.Errorf("row %d is %v, want the field", y, got)
				}
			}
		})
	}
}

// TestSkyDrawAllocations holds the sky to the tilted views' rule: nothing
// allocated per frame once the scratch has grown, on a steady frame and on
// frames whose clock walks past the sky's cadence every time, so every one
// asks the sun again and mixes the colours afresh.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestSkyDrawAllocations(t *testing.T) {
	for _, view := range []radar.View{radar.ViewMinimal3D, radar.View3D} {
		scene, canv := skyDraw(t, skyDemo(t, -3), theme.Night, view)
		scene.Draw(canv, 0)

		if got := testing.AllocsPerRun(50, func() { scene.Draw(canv, 0) }); got != 0 {
			t.Errorf("a steady dusk frame in %v allocated %.1f times, want 0", view, got)
		}
	}

	instant := skyEvening(t, -3)

	demo, err := source.NewDemo(
		source.WithDemoLocation(schipholLat, schipholLon),
		source.WithDemoClock(func() time.Time { return instant }),
	)
	if err != nil {
		t.Fatalf("source.NewDemo: %v", err)
	}

	scene, canv := skyDraw(t, demo, theme.Night, radar.ViewMinimal3D)
	scene.Draw(canv, 0)

	walk := func() {
		instant = instant.Add(time.Minute)

		scene.Draw(canv, 0)
	}

	if got := testing.AllocsPerRun(50, walk); got != 0 {
		t.Errorf("a frame that asks the sun again allocated %.1f times, want 0", got)
	}
}
