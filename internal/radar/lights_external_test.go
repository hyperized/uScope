package radar_test

import (
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/scope"
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
			set: radar.Settings{
				View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff, Lights: radar.LightsOn,
			},
		},
		{
			name: "3D, seen from in front", heading: 0,
			set: radar.Settings{
				View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff, Azimuth: 180,
				Lights: radar.LightsOn,
			},
		},
		{
			name: "flat, heading north", heading: 0, wantLeft: true,
			set: radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm, Lights: radar.LightsOn},
		},
		{
			name: "flat, heading south", heading: 180,
			set: radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm, Lights: radar.LightsOn},
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

			for index, lights := range []radar.LightsMode{radar.LightsOn, radar.LightsOff} {
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

	set := radar.Settings{
		View: radar.ViewMinimal3D, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff, Lights: radar.LightsOn,
	}
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

		// dusk draws the fleet with the sun five degrees up, where auto
		// mixes the lights in at half strength rather than filling them.
		dusk bool
	}{
		{name: "the scope", set: radar.Settings{Colour: radar.ColourAltitude, Lights: radar.LightsOn}},
		{
			name: "the bare flat view",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal, Lights: radar.LightsOn},
		},
		{
			name: "the perspective view",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D, Lights: radar.LightsOn},
		},
		{
			name: "the bare 3D view at 3840x2160",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D, Lights: radar.LightsOn},
			uhd:  true,
		},
		{
			name: "the scope with the lights on auto",
			set:  radar.Settings{Colour: radar.ColourAltitude, Lights: radar.LightsAuto},
		},
		{
			name: "the perspective view with the lights on auto at dusk",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D, Lights: radar.LightsAuto},
			dusk: true,
		},
		{
			name: "the bare flat view with the lights on auto at dusk",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal, Lights: radar.LightsAuto},
			dusk: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			scene, canv := sceneFor(t, false, exposureNone, 0)

			if testCase.dusk {
				frame := benchFrame()
				frame.Now = lightsDusk
				scene = radar.New(testFaces(t), &fakeSource{frame: frame}, scope.New(scope.WithCurrent(60)))
			}

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

	set := radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm, Lights: radar.LightsOn}
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

// TestLightsKeyCycles checks s walks the lights from off through auto and
// on and back to off in every view. After each press the scene has to draw
// exactly what a scene handed that mode through Apply draws, frame for frame
// at the same clocks, so the cap and the lights are both compared. The clock
// is noon, where auto and on differ in the picture as well as on the cap.
//
// Each step is drawn past the key bar's hold after its press, so a bare
// view's revealed caps are not part of what is compared.
func TestLightsKeyCycles(t *testing.T) {
	t.Parallel()

	modes := [...]radar.LightsMode{radar.LightsOff, radar.LightsAuto, radar.LightsOn}

	for _, view := range []radar.View{radar.ViewScope, radar.View3D, radar.ViewMinimal, radar.ViewMinimal3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(lightsPlane(0))
			frame.Now = lightsNoon

			set := radar.Settings{View: view, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff}
			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
			scene.Apply(set)

			refs, refCanvs := [len(modes)]*radar.Scene{}, [len(modes)]*canvas.Canvas{}

			for index, mode := range modes {
				refs[index], refCanvs[index], _ = sceneOn(t, panelWidth, panelHeight, frame)
				set.Lights = mode
				refs[index].Apply(set)
			}

			// Indexes into modes: off as it starts, then auto, on and off.
			for step, want := range []int{0, 1, 2, 0} {
				if step > 0 && !press(scene, 's') {
					t.Fatal("s was not handled")
				}

				if clock, same := drawsAsRef(scene, canv, refs[:], refCanvs[:], want, step); !same {
					t.Errorf("after %d presses at %v the picture is not %s's", step, clock, modes[want])
				}
			}

			if identical(refCanvs[1], refCanvs[2]) {
				t.Error("auto and on drew the same picture at noon, so the cycle shows nothing")
			}
		})
	}
}

// drawsAsRef draws scene and every reference at the same three clocks, the
// first of them past the key bar's hold after press number step, and reports
// the first clock at which scene's picture is not refs[want]'s.
func drawsAsRef(scene *radar.Scene, canv *canvas.Canvas, refs []*radar.Scene, refCanvs []*canvas.Canvas,
	want, step int,
) (time.Duration, bool) {
	start := time.Duration(step) * 2 * pastKeyBarHold

	for clock := start; clock < start+time.Second; clock += time.Second / 3 {
		scene.Draw(canv, clock)

		for index, ref := range refs {
			ref.Draw(refCanvs[index], clock)
		}

		if !identical(canv, refCanvs[want]) {
			return clock, false
		}
	}

	return 0, true
}

// lightsCapBox is the S cap in the full views' bar at the panel's resolution,
// scanned off a rendered frame like the boxes in scene_external_test.go.
//
//nolint:gochecknoglobals // a rectangle is data.
var lightsCapBox = image.Rect(414, 682, 476, 704)

// TestLightsCapFollowsTheKey checks the S cap's engaged bar is off on a
// fresh scene, where the lights start off, and on after a press of s, which
// puts them on auto. TestLightsKeyCycles compares the cap in all three modes.
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

// The clocks the auto cases draw at, for the test receiver near Schiphol on
// the day sceneClock is on. At noon the sun is 41 degrees up, at lightsDusk
// it is five, half way through the fade, and at lightsMidnight it is 35 below.
//
//nolint:gochecknoglobals // a time.Time cannot be a constant.
var (
	lightsNoon     = time.Date(2026, time.September, 15, 11, 40, 0, 0, time.UTC)
	lightsDusk     = time.Date(2026, time.September, 15, 17, 18, 0, 0, time.UTC)
	lightsMidnight = time.Date(2026, time.September, 15, 23, 40, 0, 0, time.UTC)
)

// TestParseLights checks the allow list --lights is read against: the three
// spellings and nothing else, case sensitive, with ErrLights for the rest.
func TestParseLights(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		text    string
		want    radar.LightsMode
		wantErr bool
	}{
		{text: "on", want: radar.LightsOn},
		{text: "off", want: radar.LightsOff},
		{text: "auto", want: radar.LightsAuto},
		{text: "Auto", want: radar.LightsOn, wantErr: true},
		{text: "true", want: radar.LightsOn, wantErr: true},
		{text: "", want: radar.LightsOn, wantErr: true},
	} {
		t.Run(testCase.text, func(t *testing.T) {
			t.Parallel()

			got, err := radar.ParseLights(testCase.text)
			if errors.Is(err, radar.ErrLights) != testCase.wantErr || got != testCase.want {
				t.Errorf("ParseLights(%q) = %q, %v, want %q and an error %v",
					testCase.text, got, err, testCase.want, testCase.wantErr)
			}
		})
	}
}

// TestLightsAutoFollowsTheSun checks auto against the two fixed modes on one
// still in each bare view: at noon it is off to the pixel, at midnight it is
// on to the pixel, and so it is with no receiver position at all, which the
// auto theme also takes for night. The palette is fixed and does not pin it:
// the night palette at noon draws none and the day palette at midnight draws
// them all, the way the stars follow the sun whatever the theme.
func TestLightsAutoFollowsTheSun(t *testing.T) {
	t.Parallel()

	noon, midnight, nowhere := lightsFrameAt(lightsNoon), lightsFrameAt(lightsMidnight), lightsFrameAt(lightsNoon)
	nowhere.Receiver.Mode, nowhere.Receiver.HasFix = source.FixNone, false

	for _, view := range []radar.View{radar.ViewMinimal, radar.ViewMinimal3D} {
		for _, testCase := range []struct {
			name    string
			frame   source.Frame
			pal     theme.Palette
			matches radar.LightsMode
		}{
			{name: "noon, night palette", frame: noon, pal: theme.Night, matches: radar.LightsOff},
			{name: "midnight, night palette", frame: midnight, pal: theme.Night, matches: radar.LightsOn},
			{name: "midnight, day palette", frame: midnight, pal: theme.Day, matches: radar.LightsOn},
			{name: "noon with no position", frame: nowhere, pal: theme.Night, matches: radar.LightsOn},
		} {
			t.Run(string(view)+" "+testCase.name, func(t *testing.T) {
				t.Parallel()

				still := lightsStill{frame: testCase.frame, pal: testCase.pal, view: view}
				auto, fixed := still.draw(t, radar.LightsAuto), still.draw(t, testCase.matches)

				other := radar.LightsOn
				if testCase.matches == radar.LightsOn {
					other = radar.LightsOff
				}

				if !identical(auto, fixed) {
					t.Errorf("auto drew a different picture from %s", testCase.matches)
				}

				if identical(auto, still.draw(t, other)) {
					t.Error("on and off drew the same picture, so the comparison shows nothing")
				}
			})
		}
	}
}

// lightsFrameAt is the light tests' aircraft over the test receiver at now.
func lightsFrameAt(now time.Time) source.Frame {
	frame := sceneFrame(lightsPlane(0))
	frame.Now = now

	return frame
}

// lightsStill is one still the auto cases compare: a frame, a fixed palette
// and a view, at a quarter of the panel's area to keep the cases cheap.
type lightsStill struct {
	frame source.Frame
	pal   theme.Palette
	view  radar.View
}

// draw renders the still with the lights in mode on a fresh scene.
func (still lightsStill) draw(tb testing.TB, mode radar.LightsMode) *canvas.Canvas {
	tb.Helper()

	scene, canv, _ := sceneOn(tb, panelWidth/2, panelHeight/2, still.frame)
	scene.SetPalette(still.pal)
	scene.Apply(radar.Settings{View: still.view, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff, Lights: mode})
	scene.Draw(canv, 0)

	return canv
}
