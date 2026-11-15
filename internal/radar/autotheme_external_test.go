package radar_test

import (
	"image"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/sun"
)

// Instants at Schiphol, the tests' receiver, kept hours away from either
// twilight so the sun's answer is not in doubt.
//
//nolint:gochecknoglobals // fixed instants, read-only.
var (
	schipholNoon     = time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC)
	schipholMidnight = time.Date(2026, time.June, 21, 23, 0, 0, 0, time.UTC)
)

// Auckland is in its own night at Schiphol's noon, which is what makes it the
// place to move the receiver to.
const (
	aucklandLat = -36.85
	aucklandLon = 174.76
)

// fieldProbe is a pixel in the margin below the key bar, which is always the
// palette's field whatever else is drawn. The top margin will not do: the
// header band reaches the canvas edge.
//
//nolint:gochecknoglobals // a point is data, and image.Point cannot be const.
var fieldProbe = image.Pt(panelWidth-2, panelHeight-2)

// autoScene builds a scene with the auto theme on over a source the test can
// move, so the clock and the receiver change between frames the way a live
// feed changes them.
func autoScene(tb testing.TB, frame source.Frame, opts ...radar.Option) (*radar.Scene, *fakeSource, *canvas.Canvas) {
	tb.Helper()

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	src := &fakeSource{frame: frame}
	scene := radar.New(testFaces(tb), src, scope.New(scope.WithCurrent(sceneRangeNm)), opts...)
	scene.SetAutoTheme(true)

	return scene, src, canv
}

// fieldAfterDraw draws one frame and reads back the field colour, which says
// which palette the frame was drawn in.
func fieldAfterDraw(scene *radar.Scene, canv *canvas.Canvas) theme.Palette {
	scene.Draw(canv, 0)

	switch canv.Image().RGBAAt(fieldProbe.X, fieldProbe.Y) {
	case theme.Day.Field:
		return theme.Day
	case theme.Night.Field:
		return theme.Night
	default:
		return theme.Palette{}
	}
}

// autoThemeEveryForTest is the auto theme's interval as the black-box tests
// see it; the internal test pins the real constant to the sky's cadence.
const autoThemeEveryForTest = 10 * time.Second

// horizonDusk finds the instant the sun crosses the horizon going down at the
// tests' receiver on sceneClock's day, to the second, by bisection on sun.IsDay
// itself. Finding it rather than writing it down keeps the cadence tests
// honest about which side of the line each frame is on.
func horizonDusk(tb testing.TB) time.Time {
	tb.Helper()

	day := time.Date(2026, time.September, 15, 16, 0, 0, 0, time.UTC)
	night := time.Date(2026, time.September, 15, 20, 0, 0, 0, time.UTC)

	if !sun.IsDay(receiverLat, receiverLon, day) || sun.IsDay(receiverLat, receiverLon, night) {
		tb.Fatal("the bracket around dusk is wrong, so the bisection would find nothing")
	}

	for night.Sub(day) > time.Second {
		mid := day.Add(night.Sub(day) / 2)
		if sun.IsDay(receiverLat, receiverLon, mid) {
			day = mid
		} else {
			night = mid
		}
	}

	return night
}

// TestAutoThemeResolves checks the three answers auto can give on a first
// frame: day in daylight, night in the dark, and night when the receiver's
// position is not known at all, whatever the sun is doing.
func TestAutoThemeResolves(t *testing.T) {
	t.Parallel()

	nowhere := sceneFrame()
	nowhere.Receiver = source.Receiver{Label: source.LabelNone}
	nowhere.Now = schipholNoon

	noon := sceneFrame()
	noon.Now = schipholNoon

	midnight := sceneFrame()
	midnight.Now = schipholMidnight

	for _, testCase := range []struct {
		name  string
		frame source.Frame
		want  theme.Palette
	}{
		{name: "noon at the receiver is day", frame: noon, want: theme.Day},
		{name: "midnight at the receiver is night", frame: midnight, want: theme.Night},
		{name: "no position is night even at noon", frame: nowhere, want: theme.Night},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, _, canv := autoScene(t, testCase.frame)

			if got := fieldAfterDraw(scene, canv); got != testCase.want {
				t.Errorf("field after the first frame = %v, want %v's", got.Field, testCase.want.Field)
			}
		})
	}
}

// TestAutoThemeMatchesTheFixedTheme checks that auto resolving to day draws
// the scope exactly as --theme day does: the switch goes through the same
// SetPalette the l key uses, so the background layer and everything keyed on
// the palette follow it. The key bar is left out, since its L cap is the one
// thing meant to differ.
func TestAutoThemeMatchesTheFixedTheme(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	frame.Now = schipholNoon

	auto, _, autoCanv := autoScene(t, frame)
	auto.Draw(autoCanv, 0)

	fixed, fixedCanv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithPalette(theme.Day))
	fixed.Draw(fixedCanv, 0)

	above := image.Rect(0, 0, panelWidth, keyBarBox.Min.Y)
	if !identicalIn(autoCanv, fixedCanv, above) {
		t.Error("auto at noon drew something other than the day theme above the key bar")
	}
}

// TestAutoThemeCadence checks that the sun is asked at most once per
// autoThemeEvery: a frame after dusk keeps the answer worked out before it,
// and the frame a full interval after that answer takes the new one. A clock
// that runs backwards, as a replayed capture can, asks again at once.
func TestAutoThemeCadence(t *testing.T) {
	t.Parallel()

	dusk := horizonDusk(t)
	before := dusk.Add(-5 * time.Second)

	after := dusk.Add(2 * time.Second)

	for _, testCase := range []struct {
		name        string
		start, then time.Time
		first, want theme.Palette
	}{
		{
			name:  "seven seconds on keeps the answer",
			start: before, then: after, first: theme.Day, want: theme.Day,
		},
		{
			name:  "just under the interval keeps the answer",
			start: before, then: before.Add(autoThemeEveryForTest - time.Second), first: theme.Day, want: theme.Day,
		},
		{
			name:  "a full interval on asks again",
			start: before, then: before.Add(autoThemeEveryForTest), first: theme.Day, want: theme.Night,
		},
		{
			name:  "a clock that went back asks again",
			start: after, then: before, first: theme.Night, want: theme.Day,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame()
			frame.Now = testCase.start

			scene, src, canv := autoScene(t, frame)

			if got := fieldAfterDraw(scene, canv); got != testCase.first {
				t.Fatalf("field on the first frame = %v, want %v's", got.Field, testCase.first.Field)
			}

			src.frame.Now = testCase.then

			if got := fieldAfterDraw(scene, canv); got != testCase.want {
				t.Errorf("field at %s = %v, want %v's",
					testCase.then.Format(time.TimeOnly), got.Field, testCase.want.Field)
			}
		})
	}
}

// TestAutoThemeAsksAgainWhenTheReceiverMoves checks the two exceptions to the
// interval: a receiver that moves, or loses its position, gets a fresh answer on
// the very next frame rather than one worked out for where it used to be.
func TestAutoThemeAsksAgainWhenTheReceiverMoves(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		receiver source.Receiver
	}{
		{
			name:     "moved to where it is night",
			receiver: source.Receiver{Latitude: aucklandLat, Longitude: aucklandLon, Mode: source.FixGPS3D},
		},
		{
			name:     "moved along the same latitude",
			receiver: source.Receiver{Latitude: receiverLat, Longitude: aucklandLon, Mode: source.FixGPS3D},
		},
		{name: "lost its position", receiver: source.Receiver{Label: source.LabelNone}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame()
			frame.Now = schipholNoon

			scene, src, canv := autoScene(t, frame)
			fieldAfterDraw(scene, canv)

			src.frame.Receiver = testCase.receiver
			src.frame.Now = schipholNoon.Add(time.Second)

			if got := fieldAfterDraw(scene, canv); got != theme.Night {
				t.Errorf("field a second after the receiver changed = %v, want night's", got.Field)
			}
		})
	}
}

// TestSetAutoThemeAsksAtOnce checks the other two exceptions: cycling into
// auto again answers on the next frame however recently the last answer was
// worked out, and turning it off leaves the palette alone from then on.
func TestSetAutoThemeAsksAtOnce(t *testing.T) {
	t.Parallel()

	dusk := horizonDusk(t)

	t.Run("on again asks at once", func(t *testing.T) {
		t.Parallel()

		frame := sceneFrame()
		frame.Now = dusk.Add(-5 * time.Second)

		scene, src, canv := autoScene(t, frame)
		fieldAfterDraw(scene, canv)

		src.frame.Now = dusk.Add(5 * time.Second)

		scene.SetAutoTheme(true)

		if got := fieldAfterDraw(scene, canv); got != theme.Night {
			t.Errorf("field after turning auto on again = %v, want night's", got.Field)
		}
	})

	t.Run("off stops asking", func(t *testing.T) {
		t.Parallel()

		frame := sceneFrame()
		frame.Now = schipholNoon

		scene, src, canv := autoScene(t, frame)
		fieldAfterDraw(scene, canv)

		scene.SetAutoTheme(false)

		src.frame.Now = schipholMidnight

		if got := fieldAfterDraw(scene, canv); got != theme.Day {
			t.Errorf("field after turning auto off = %v, want the day it was left on", got.Field)
		}
	})
}

// TestAutoThemeKeepsTheLook checks that the sun picks between the two palettes
// of the look on screen rather than falling back to glass.
func TestAutoThemeKeepsTheLook(t *testing.T) {
	t.Parallel()

	frame := sceneFrame()
	frame.Now = schipholNoon

	scene, _, canv := autoScene(t, frame, radar.WithPalette(theme.MonoNight))
	scene.Draw(canv, 0)

	if got := canv.Image().RGBAAt(fieldProbe.X, fieldProbe.Y); got != theme.MonoDay.Field {
		t.Errorf("field = %v, want MonoDay's %v", got, theme.MonoDay.Field)
	}
}

// TestAutoThemeKeyBarFitsAtPanelWidth is TestView3DKeyBarFitsAtPanelWidth at
// the widest the bar can get with the L cap on its longest word, AUTO·NIGHT:
// the 3D view on a dongle with a bias-tee, corridor colours, the short trails
// and the phosphor look, whose cap word is the longest of the three.
func TestAutoThemeKeyBarFitsAtPanelWidth(t *testing.T) {
	t.Parallel()

	frame := covered(sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)))
	frame.BiasTee = source.BiasTeeState{Supported: true}

	// Midnight, so the sun picks night and the L cap reads AUTO·NIGHT, the
	// longer of its two auto words, over the phosphor night field the probe
	// below compares against.
	frame.Now = schipholMidnight

	scene, _, canv := autoScene(t, frame,
		radar.WithColour(radar.ColourCorridor),
		radar.WithPalette(theme.PhosphorNight),
		radar.WithBiasTee(&fakeToggler{}))
	scene.Apply(radar.Settings{View: radar.View3D, RangeNm: sceneRangeNm, Colour: radar.ColourCorridor})
	pressTrails(t, scene, pressShort)
	scene.Draw(canv, 0)

	last := -1

	for x := keyBarBox.Min.X; x < keyBarBox.Max.X; x++ {
		for y := keyBarBox.Min.Y; y < keyBarBox.Max.Y; y++ {
			if canv.Image().RGBAAt(x, y) != theme.PhosphorNight.Field {
				last = x

				break
			}
		}
	}

	// baseMargin is 16, so the bar has to end before 1264 at this width.
	if rightMargin := panelWidth - 16; last >= rightMargin {
		t.Errorf("the 3D key bar reached x=%d, want it to stop before the %d margin", last, rightMargin)
	}

	if last < tiltCapBox.Max.X {
		t.Errorf("the 3D key bar ended at x=%d, before the tilt cap at %d", last, tiltCapBox.Max.X)
	}

	t.Logf("the widest 3D key bar with AUTO·NIGHT ends at x=%d", last)
}

// TestAutoThemeDrawAllocations checks that auto keeps Draw off the heap on a
// frame that asks the sun as well as on one that does not: every frame here
// is an interval after the last, so every one of them asks.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestAutoThemeDrawAllocations(t *testing.T) {
	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	frame.Now = schipholNoon

	scene, src, canv := autoScene(t, frame)
	scene.Draw(canv, 0)

	got := testing.AllocsPerRun(50, func() {
		src.frame.Now = src.frame.Now.Add(autoThemeEveryForTest)

		scene.Draw(canv, 0)
	})
	if got != 0 {
		t.Errorf("Draw allocated %.1f times per frame under auto, want 0", got)
	}
}
