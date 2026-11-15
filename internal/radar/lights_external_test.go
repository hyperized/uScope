package radar_test

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The aircraft the light tests fly: due north of the receiver, heading north,
// in the middle altitude band. The middle band is drawn in Ink, so neither the
// model nor its trail is the red or the green a light is, and every pixel of
// either colour on the canvas is a light.
const (
	lightsDistanceNm = 10
	lightsAltitude   = 18000
)

// lightsPlane is the light tests' aircraft on heading.
func lightsPlane(heading float64) airplane.Snapshot {
	return scenePlane(icaoSample, callsignSample, 0, lightsDistanceNm, lightsAltitude, heading)
}

// lightsCanvas draws frame once in view under the given settings changes, on
// the panel's own size, and hands back the canvas.
func lightsCanvas(tb testing.TB, frame source.Frame, set radar.Settings) *canvas.Canvas {
	tb.Helper()

	scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, frame)
	scene.Apply(set)
	scene.Draw(canv, 0)

	return canv
}

// meanX is the average column of every pixel on canv that is exactly col, and
// false when there is none.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func meanX(canv *canvas.Canvas, col color.RGBA) (float64, bool) {
	sum, count := 0, 0
	bounds := canv.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) == col {
				sum += x
				count++
			}
		}
	}

	if count == 0 {
		return 0, false
	}

	return float64(sum) / float64(count), true
}

// TestLightsPortIsRedAndStarboardGreen checks the convention every view has
// to get right: the red light on the left wing and the green on the right.
//
// Which side of the picture each lands on depends on where it is seen from.
// From behind a northbound aircraft, which is where the 3D camera stands at
// azimuth zero and where the flat scope always looks down from with north up,
// port is on the left of the screen. From in front, or with the aircraft
// turned round, the two swap over, and a light drawn on the wrong vertex would
// be on the same side both times.
func TestLightsPortIsRedAndStarboardGreen(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		set      radar.Settings
		heading  float64
		wantLeft bool
	}{
		{
			name: "3D, seen from behind", heading: 0, wantLeft: true,
			set: radar.Settings{View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff, Lights: true},
		},
		{
			name: "3D, seen from in front", heading: 0,
			set: radar.Settings{
				View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff, Azimuth: 180,
				Lights: true,
			},
		},
		{
			name: "flat, heading north", heading: 0, wantLeft: true,
			set: radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm, Lights: true},
		},
		{
			name: "flat, heading south", heading: 180,
			set: radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm, Lights: true},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := lightsCanvas(t, sceneFrame(lightsPlane(testCase.heading)), testCase.set)

			red, haveRed := meanX(canv, theme.Night.Warn)
			green, haveGreen := meanX(canv, theme.Night.OK)

			if !haveRed || !haveGreen {
				t.Fatalf("red drawn %v, green drawn %v, want both wingtip lights", haveRed, haveGreen)
			}

			if left := red < green; left != testCase.wantLeft {
				t.Errorf("red at x=%.1f, green at x=%.1f: red on the left = %v, want %v",
					red, green, left, testCase.wantLeft)
			}
		})
	}
}

// TestLightsAreOnlyWhereThereIsAModel checks the three places no light goes:
// anywhere with the setting off, on an aircraft with no heading, which is
// drawn as a disc with no wingtips, and on a ghost, which has no aircraft at
// its head at all. Each is drawn with the lights on and off, and the two
// pictures have to be the same, in both kinds of view.
func TestLightsAreOnlyWhereThereIsAModel(t *testing.T) {
	t.Parallel()

	ghostOnly := ghostFrame([]source.Trail{ghostOf(lightsPlane(0))})

	for _, testCase := range []struct {
		name  string
		frame source.Frame
		set   radar.Settings
		trail int
	}{
		{
			name: "3D disc", frame: sceneFrame(lightsPlane(noHeading)),
			set: radar.Settings{View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff},
		},
		{
			name: "flat disc", frame: sceneFrame(lightsPlane(noHeading)),
			set: radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm},
		},
		{
			name: "3D ghost", frame: ghostOnly, trail: pressAll,
			set: radar.Settings{View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff},
		},
		{
			name: "flat ghost", frame: ghostOnly, trail: pressAll,
			set: radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pictures := [2]*canvas.Canvas{}

			for index, lights := range []bool{true, false} {
				scene, canv, _ := sceneOn(t, panelWidth, panelHeight, testCase.frame)
				set := testCase.set
				set.Lights = lights
				scene.Apply(set)
				pressTrails(t, scene, testCase.trail)
				// Drawn after the key bar's four-second hold, because the t
				// presses reveal the bar in a bare view and its S cap shows
				// the very setting this case flips.
				scene.Draw(canv, 5*time.Second)
				pictures[index] = canv
			}

			if !identical(pictures[0], pictures[1]) {
				t.Error("turning the lights off changed the picture, want nothing to have carried them")
			}
		})
	}

	t.Run("a model on a fresh scene, where the lights start off", func(t *testing.T) {
		t.Parallel()

		set := radar.Settings{View: radar.ViewMinimal3D, RangeNm: sceneRangeNm}
		canv := lightsCanvas(t, sceneFrame(lightsPlane(0)), set)

		bounds := canv.Bounds()

		red, green := countColour(canv, bounds, theme.Night.Warn), countColour(canv, bounds, theme.Night.OK)
		if red != 0 || green != 0 {
			t.Errorf("%d red and %d green pixels on a fresh scene, want none", red, green)
		}
	})
}

// TestLightsFlashOverTime checks the flashes are wired to the render clock:
// a run of frames at 30 fps over two seconds draws pictures that
// differ from one another, with the camera held still and nothing else on
// the canvas moving.
func TestLightsFlashOverTime(t *testing.T) {
	t.Parallel()

	const (
		frame = time.Second / 30
		span  = 2 * time.Second
	)

	set := radar.Settings{View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff, Lights: true}
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(lightsPlane(0)))
	scene.Apply(set)
	scene.Draw(canv, 0)

	first, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(first, 0)

	for at := frame; at <= span; at += frame {
		scene.Draw(canv, at)

		if !identical(canv, first) {
			return
		}
	}

	t.Errorf("%v of frames at 30 fps drew the same picture every time, want a strobe or the beacon to flash", span)
}

// TestLightsAllocations checks the lights keep the frame at zero allocations
// with the clock moving, which is when the flashes do their work.
// TestDrawAllocations draws every frame at the same reading, and with the
// lights at their default, which is off.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestLightsAllocations(t *testing.T) {
	for _, testCase := range []struct {
		name string
		set  radar.Settings
		uhd  bool
	}{
		{name: "the scope", set: radar.Settings{Colour: radar.ColourAltitude, Lights: true}},
		{
			name: "the bare flat view",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal, Lights: true},
		},
		{
			name: "the perspective view",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D, Lights: true},
		},
		{
			name: "the bare 3D view at 3840x2160",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D, Lights: true},
			uhd:  true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			scene, canv := sceneFor(t, false, exposureNone)
			canv = sizedFor(t, canv, testCase.uhd)
			scene.Apply(testCase.set)
			scene.Draw(canv, 0)

			at := time.Duration(0)

			got := testing.AllocsPerRun(50, func() {
				at += time.Second / 30
				scene.Draw(canv, at)
			})
			if got != 0 {
				t.Errorf("Draw allocated %.1f times per frame in %s with the clock moving, want 0", got, testCase.name)
			}
		})
	}
}

// TestLightsFlatBeaconFlashes checks the flat views carry the beacon: over a
// beacon period at 30 fps, some frame has more red on it than the one port
// pixel a dark frame has, and the extra red is in the middle of the sprite,
// on the fuselage, rather than out at a wingtip.
func TestLightsFlatBeaconFlashes(t *testing.T) {
	t.Parallel()

	const frame = time.Second / 30

	set := radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm, Lights: true}
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(lightsPlane(0)))
	scene.Apply(set)

	bounds := canv.Bounds()
	fewest, most := -1, 0

	for at := time.Duration(0); at <= 2*time.Second; at += frame {
		scene.Draw(canv, at)

		red := countColour(canv, bounds, theme.Night.Warn)
		most = max(most, red)

		if fewest < 0 || red < fewest {
			fewest = red
		}
	}

	// The 3x3 plus less its core is four red pixels over the one port tip.
	if most < fewest+4 {
		t.Errorf("red ran from %d to %d pixels over two seconds, want a frame with the beacon's four more",
			fewest, most)
	}
}

// TestLightsKeyToggles checks s turns the lights on and S off again in every
// view, by comparing frames with ones drawn at the same clocks by a scene that
// never had them. A fresh scene starts without them.
//
// Each frame is drawn past the key bar's hold after the press before it, so
// a bare view's revealed caps are not part of what is compared.
func TestLightsKeyToggles(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewScope, radar.View3D, radar.ViewMinimal, radar.ViewMinimal3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			set := radar.Settings{View: view, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff}
			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(lightsPlane(0)))
			scene.Apply(set)

			dark, darkCanv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(lightsPlane(0)))
			dark.Apply(set)

			for press, key := range []rune{0, 's', 'S'} {
				if key != 0 && !scene.Handle(input.Key{Kind: input.Rune, Rune: key}) {
					t.Fatalf("%q was not handled", key)
				}

				if lit, want := litAfter(scene, dark, canv, darkCanv, press), press == 1; lit != want {
					t.Errorf("after press %d the lights are drawn = %v, want %v", press, lit, want)
				}
			}
		})
	}
}

// litAfter reports whether scene draws anything dark does not over a strobe
// period of frames, the first of them past the key bar's hold after press
// number step.
//
// One frame is not enough. The test aircraft is in the band drawn in Ink, and
// a strobe turns the flat sprite's three tips to Ink as well, so a frame that
// happens to land on a strobe with the beacon dark carries no light at all.
func litAfter(scene, dark *radar.Scene, canv, darkCanv *canvas.Canvas, step int) bool {
	const frame = time.Second / 10

	start := time.Duration(step) * 2 * pastKeyBarHold
	lit := false

	for at := start; at < start+3*time.Second; at += frame {
		scene.Draw(canv, at)
		dark.Draw(darkCanv, at)

		lit = lit || !identical(canv, darkCanv)
	}

	return lit
}

// lightsCapBox is the S cap in the full views' bar at the panel's resolution,
// scanned off a rendered frame like the boxes in scene_external_test.go.
//
//nolint:gochecknoglobals // a rectangle is data.
var lightsCapBox = image.Rect(414, 682, 476, 704)

// TestLightsCapFollowsTheKey checks the S cap's engaged bar is off on a
// fresh scene, where the lights start off, and on after a press of s.
func TestLightsCapFollowsTheKey(t *testing.T) {
	t.Parallel()

	engaged := image.Rect(
		lightsCapBox.Min.X+keyEngagedInsetTest, lightsCapBox.Max.Y-keyPadYTest,
		lightsCapBox.Max.X-keyEngagedInsetTest, lightsCapBox.Max.Y-keyPadYTest+keyEngagedHeightTest,
	)

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(lightsPlane(0)))
	scene.Draw(canv, 0)

	if got := countColour(canv, engaged, theme.Night.OK); got != 0 {
		t.Errorf("fresh scene: %d OK pixels under the lights cap, want none", got)
	}

	press(scene, 's')
	scene.Draw(canv, 0)

	if got := countColour(canv, engaged, theme.Night.OK); got == 0 {
		t.Error("after s: no OK pixels under the lights cap, want the engaged bar")
	}
}
