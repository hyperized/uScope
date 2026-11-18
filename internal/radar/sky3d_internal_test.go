package radar

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
	"github.com/hyperized/uScope/pkg/sun"
)

// allPalettes is every palette the sky has to hold up in.
//
//nolint:gochecknoglobals // a table of package values, read-only.
var allPalettes = []theme.Palette{
	theme.Night, theme.Day, theme.PhosphorNight, theme.PhosphorDay, theme.MonoNight, theme.MonoDay,
}

// nightSkyLumaCeiling is the brightest a dark palette's sky may get, in Rec.
// 601 luma: well under mid-grey, so the white ink and the trails stand off
// it. The measured worst case is pinned in TestNightSkyStaysDark.
const nightSkyLumaCeiling = 96.0

// sunDown is an instant the sun stands at about target degrees at the test
// receiver, found by walking the evening of the March equinox in ten second
// steps from noon, which passes every elevation the sky table cares about.
func sunDown(tb testing.TB, target float64) time.Time {
	tb.Helper()

	instant := time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC)
	for range 24 * 360 {
		if sun.Elevation(layerBaseLat, layerBaseLon, instant) <= target {
			return instant
		}

		instant = instant.Add(skyEvery)
	}

	tb.Fatalf("the sun never fell to %g degrees", target)

	return instant
}

// TestSkyKeyframesAreExact pins the keyframes themselves: at a keyframe's own
// elevation the colours are that keyframe's, mixed into the palette, with no
// share of either neighbour.
func TestSkyKeyframesAreExact(t *testing.T) {
	t.Parallel()

	for _, pal := range allPalettes {
		for _, key := range skyKeys {
			zenith, horizon := skyInks(pal, key.elevation)
			if zenith != skyInk(pal, key.zenith) || horizon != skyInk(pal, key.horizon) {
				t.Errorf("%s at %g: got %v / %v, want the keyframe's %v / %v", pal.Look, key.elevation,
					zenith, horizon, skyInk(pal, key.zenith), skyInk(pal, key.horizon))
			}
		}
	}
}

// TestSkySlidesMonotonically checks the slide between keyframes: every
// channel of both colours moves one way only from one keyframe to the next,
// so the sky never flickers back on its way through a twilight.
func TestSkySlidesMonotonically(t *testing.T) {
	t.Parallel()

	for _, pal := range allPalettes {
		for index := 1; index < len(skyKeys); index++ {
			low, high := skyKeys[index-1].elevation, skyKeys[index].elevation
			lowZenith, lowHorizon := skyInks(pal, low)
			highZenith, highHorizon := skyInks(pal, high)

			lastZenith, lastHorizon := lowZenith, lowHorizon

			for step := 1; step <= 64; step++ {
				zenith, horizon := skyInks(pal, low+float64(step)/64*(high-low))
				assertOneWay(t, "zenith", lowZenith, lastZenith, zenith, highZenith)
				assertOneWay(t, "horizon", lowHorizon, lastHorizon, horizon, highHorizon)
				lastZenith, lastHorizon = zenith, horizon
			}
		}
	}
}

// assertOneWay fails when any channel of next has stepped back from last,
// against the direction from start to end.
func assertOneWay(tb testing.TB, what string, start, last, next, end color.RGBA) {
	tb.Helper()

	for _, channel := range [][4]uint8{
		{start.R, last.R, next.R, end.R}, {start.G, last.G, next.G, end.G}, {start.B, last.B, next.B, end.B},
	} {
		rising := channel[3] >= channel[0]
		if (rising && channel[2] < channel[1]) || (!rising && channel[2] > channel[1]) {
			tb.Errorf("%s stepped back from %v to %v on its way from %v to %v", what, last, next, start, end)

			return
		}
	}
}

// TestSkyClampsOutsideTheTable checks both ends hold: deep night is the night
// keyframe and a high sun the last one, however far out, and a NaN is night.
func TestSkyClampsOutsideTheTable(t *testing.T) {
	t.Parallel()

	first, last := skyKeys[0], skyKeys[len(skyKeys)-1]

	for _, testCase := range []struct {
		name      string
		elevation float64
		want      skyKey
	}{
		{name: "the sun at the nadir", elevation: -90, want: first},
		{name: "just under night", elevation: nightElevation - 0.01, want: first},
		{name: "the sun at the zenith", elevation: 90, want: last},
		{name: "a sum gone wrong", elevation: math.NaN(), want: first},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			zenith, horizon := skyInks(theme.Night, testCase.elevation)
			wantZenith := skyInk(theme.Night, testCase.want.zenith)
			wantHorizon := skyInk(theme.Night, testCase.want.horizon)

			if zenith != wantZenith || horizon != wantHorizon {
				t.Errorf("got %v / %v, want the %g keyframe", zenith, horizon, testCase.want.elevation)
			}
		})
	}
}

// TestNightSkyIsTheField is the promise that night looks exactly as it did
// before there was a sky, in every palette, and that no position is the same
// as night.
func TestNightSkyIsTheField(t *testing.T) {
	t.Parallel()

	for _, pal := range allPalettes {
		for _, elevation := range []float64{-90, -30, nightElevation} {
			if zenith, horizon := skyInks(pal, elevation); zenith != pal.Field || horizon != pal.Field {
				t.Errorf("%v at %g: %v / %v, want the field %v twice", pal.Look, elevation, zenith, horizon, pal.Field)
			}
		}

		scene := &Scene{shown: ViewMinimal3D}
		scene.SetPalette(pal)
		scene.resolveSky(source.Frame{Now: time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC)})

		if scene.sky.zenith != pal.Field || scene.sky.horizon != pal.Field {
			t.Errorf("%v with no sun: %v / %v, want the field twice", pal.Look, scene.sky.zenith, scene.sky.horizon)
		}
	}
}

// TestNightSkyStaysDark holds every dark palette's sky under the luma
// ceiling at every elevation, which is what keeps the picture dark
// even when --theme night runs through the day. The brightest it gets is a
// daytime horizon in the phosphor look, at about 83.
func TestNightSkyStaysDark(t *testing.T) {
	t.Parallel()

	brightest := 0.0

	for _, pal := range []theme.Palette{theme.Night, theme.PhosphorNight, theme.MonoNight} {
		for elevation := -20.0; elevation <= 40; elevation += 0.25 {
			zenith, horizon := skyInks(pal, elevation)
			brightest = max(brightest, luma(zenith), luma(horizon))
		}
	}

	if brightest > nightSkyLumaCeiling {
		t.Errorf("the brightest night sky has a luma of %.1f, want at most %.0f", brightest, nightSkyLumaCeiling)
	}

	if brightest < nightSkyLumaCeiling/2 {
		t.Errorf("the brightest night sky has a luma of %.1f, which is too dark to see", brightest)
	}
}

// TestSkyInLooks checks each look's character survives: glass keeps the warm
// dusk, mono is grey all the way, and phosphor leans green.
func TestSkyInLooks(t *testing.T) {
	t.Parallel()

	dusk := -3.0

	if _, horizon := skyInks(theme.Night, dusk); horizon.R <= horizon.B {
		t.Errorf("the glass dusk horizon %v is not warm", horizon)
	}

	// Mono maps the sky onto its own grey ink before the mix; what the mix
	// then adds is only the warmth of its paper, which belongs to the field.
	for _, pal := range []theme.Palette{theme.MonoNight, theme.MonoDay} {
		if col := lookTint(pal, unpackRGB(civilHorizon)); col.R != col.G || col.G != col.B {
			t.Errorf("the mono look turns the dusk horizon into %v, want a grey", col)
		}
	}

	if _, horizon := skyInks(theme.PhosphorNight, dusk); horizon.G <= horizon.R || horizon.G <= horizon.B {
		t.Errorf("the phosphor dusk horizon %v is not green", horizon)
	}

	// The light palettes are let off their field by less than half, so even
	// the darkest zenith stays a tint of the page rather than a dark block.
	if zenith, _ := skyInks(theme.Day, dusk); luma(zenith) < luma(theme.Day.Field)/2 {
		t.Errorf("the day palette's dusk zenith %v is darker than half its field", zenith)
	}
}

// TestScaleChannelSaturates covers the one-hued looks' brightest skies, where
// a pale horizon scaled onto a dark data colour runs past full.
func TestScaleChannelSaturates(t *testing.T) {
	t.Parallel()

	if got := scaleChannel(200, 3); got != opaque {
		t.Errorf("scaleChannel(200, 3) = %d, want %d", got, opaque)
	}

	if got := scaleChannel(100, 0.5); got != 50 {
		t.Errorf("scaleChannel(100, 0.5) = %d, want 50", got)
	}
}

// TestHorizonRowMatchesTheCamera ties horizonRow to the camera it describes:
// a point on the ground very far away projects at or below the row, at every
// tilt and bearing, and a point far away but higher than the eye projects
// above it.
func TestHorizonRowMatchesTheCamera(t *testing.T) {
	t.Parallel()

	box := image.Rect(0, 0, 1920, 1080)

	for _, elevation := range []float64{minElevation, 12, defaultElevation, maxElevation} {
		cam, _ := newCamera3(box, framing3{scopeNm: cameraRangeNm, elevation: elevation, topNm: cameraTopNm})
		row := horizonRow(box, elevation)

		for _, bearing := range []float64{-20, 0, 15} {
			sin, cos := math.Sincos(bearing * math.Pi / halfCircle)
			far := point3{east: cam.eye.east + 1e6*(cam.forward.east*cos-cam.forward.north*sin),
				north: cam.eye.north + 1e6*(cam.forward.north*cos+cam.forward.east*sin)}

			// The guard box would refuse the pixel at the steep tilts, where
			// the horizon is far above the picture; pixel has no guard.
			if got := cam.pixel(far); got.Y < row {
				t.Errorf("tilt %g bearing %g: a far ground point lands on row %d, above the horizon %d",
					elevation, bearing, got.Y, row)
			}

			high := far
			high.up = cam.eye.up + 1e5

			if got := cam.pixel(high); got.Y >= row {
				t.Errorf("tilt %g bearing %g: a point above the eye lands on row %d, not above the horizon %d",
					elevation, bearing, got.Y, row)
			}
		}
	}

	if row := horizonRow(box, minElevation); row <= box.Min.Y || row >= box.Max.Y/2 {
		t.Errorf("at the shallowest tilt the horizon is on row %d, want it in the top half of the picture", row)
	}

	if row := horizonRow(box, defaultElevation); row >= box.Min.Y {
		t.Errorf("at the default tilt the horizon is on row %d, want it above the picture", row)
	}
}

// skyScene is a bare tilted scene over the test receiver with the sun at
// about elevation degrees, its sky resolved and nothing else drawn.
func skyScene(tb testing.TB, elevation float64, pal theme.Palette) (*Scene, source.Frame) {
	tb.Helper()

	frame := source.Frame{
		Receiver: source.Receiver{
			Latitude: layerBaseLat, Longitude: layerBaseLon, HasFix: true, Mode: source.FixManual,
		},
		Now: sunDown(tb, elevation),
	}

	scene, _ := view3DScene(tb, frame)
	scene.shown = ViewMinimal3D
	scene.elevation = minElevation
	scene.SetPalette(pal)
	scene.resolveSky(frame)

	return scene, frame
}

// TestResolveSkyCadence checks the sun is asked once per skyEvery and the
// colours are mixed again only when that answer or the palette changes.
func TestResolveSkyCadence(t *testing.T) {
	t.Parallel()

	scene, frame := skyScene(t, -3, theme.Night)
	first := scene.sky.horizon

	if !scene.sky.known || first == theme.Night.Field {
		t.Fatalf("a dusk with a known receiver resolved to %v, known %v", first, scene.sky.known)
	}

	// Inside the cadence nothing is asked, even though the sun has moved.
	later := frame
	later.Now = frame.Now.Add(skyEvery - time.Second)
	scene.resolveSky(later)

	if !scene.sky.at.Equal(frame.Now) {
		t.Errorf("the sun was asked again %v after the last answer", skyEvery-time.Second)
	}

	// A palette change remixes the same answer.
	scene.SetPalette(theme.MonoNight)
	scene.resolveSky(later)

	if scene.sky.pal != theme.MonoNight || scene.sky.horizon == first {
		t.Errorf("a new palette kept the old sky %v", scene.sky.horizon)
	}

	// Past the cadence it is asked again.
	later.Now = frame.Now.Add(skyEvery)
	scene.resolveSky(later)

	if !scene.sky.at.Equal(later.Now) {
		t.Errorf("the sun was not asked again after %v", skyEvery)
	}

	// Losing the receiver turns the sky off straight away.
	later.Receiver = source.Receiver{}
	scene.resolveSky(later)

	if scene.sky.known || scene.skyHorizon() != scene.pal.Field {
		t.Errorf("a lost receiver left the sky on, horizon %v", scene.skyHorizon())
	}
}

// TestResolveSkyIsTiltedOnly keeps the flat views out of it altogether.
func TestResolveSkyIsTiltedOnly(t *testing.T) {
	t.Parallel()

	scene, frame := skyScene(t, -3, theme.Night)
	scene.shown = ViewScope
	frame.Now = frame.Now.Add(time.Hour)
	scene.resolveSky(frame)

	if scene.sky.at.Equal(frame.Now) {
		t.Error("the flat scope asked the sun")
	}
}

// TestSkyRowsAreKept checks the gradient is worked out once and handed back
// until something it depends on moves, and that a window wholly under the
// horizon gets an empty answer that still counts as worked out.
func TestSkyRowsAreKept(t *testing.T) {
	t.Parallel()

	scene, _ := skyScene(t, -3, theme.Night)
	box := image.Rect(0, 0, 1920, 1080)

	rows := scene.skyRows(box, box)
	if len(rows) != horizonRow(box, minElevation) {
		t.Fatalf("%d sky rows, want one per row above the horizon at %d", len(rows), horizonRow(box, minElevation))
	}

	if rows[0] != scene.sky.zenith && luma(rows[0]) >= luma(rows[len(rows)-1]) {
		t.Errorf("the top row %v is neither the zenith nor darker than the horizon row %v", rows[0], rows[len(rows)-1])
	}

	if again := scene.skyRows(box, box); &again[0] != &rows[0] {
		t.Error("the rows were worked out again for the same key")
	}

	under := image.Rect(0, 600, 1920, 1080)
	if got := scene.skyRows(box, under); got == nil || len(got) != 0 {
		t.Errorf("a window under the horizon got %v, want empty and not nil", got)
	}

	if got := scene.skyRows(box, under); got == nil {
		t.Error("the empty answer was not kept")
	}

	scene.sky.known = false
	if got := scene.skyRows(box, box); got != nil {
		t.Errorf("no sun drew %d sky rows", len(got))
	}
}

// TestPaintSky covers the three clears: the bare view's sky with the ground
// fading into the horizon, the same with no ground to fade (the field under
// the horizon), and the full view with no sun, which leaves its frame alone.
func TestPaintSky(t *testing.T) {
	t.Parallel()

	box := image.Rect(0, 0, 640, 360)
	horizon := horizonRow(box, minElevation)

	scene, _ := skyScene(t, -3, theme.Night)
	scene.minimalShore = true

	set, err := shore.Load()
	if err != nil {
		t.Fatalf("shore.Load: %v", err)
	}

	scene.shoreSet = set

	canv, err := canvas.New(box.Dx(), box.Dy())
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.paintSky(canv, box, nil)

	if got := canv.Image().RGBAAt(10, horizon); got != scene.sky.horizon {
		t.Errorf("under the horizon with the shore on is %v, want the horizon colour %v", got, scene.sky.horizon)
	}

	if got := canv.Image().RGBAAt(10, horizon-1); got == scene.pal.Field {
		t.Error("the row above the horizon is the field, want sky")
	}

	scene.minimalShore = false
	scene.paintSky(canv, box, nil)

	if got := canv.Image().RGBAAt(10, box.Max.Y-1); got != scene.pal.Field {
		t.Errorf("under the horizon with no ground is %v, want the field", got)
	}

	full, _ := skyScene(t, -3, theme.Night)
	full.shown = View3D
	full.sky.known = false

	canv.Clear(canvas.Red)
	full.paintSky(canv, box, nil)

	if got := canv.Image().RGBAAt(10, 10); got != canvas.Red {
		t.Errorf("the full view with no sun painted %v over its frame", got)
	}
}

// TestDraw3DClearsWithNoCamera is the bare view's clear when the camera
// cannot be built: the frame still comes out the field rather than whatever
// the last frame left, because the sky is the only clear it gets.
func TestDraw3DClearsWithNoCamera(t *testing.T) {
	t.Parallel()

	scene, frame := skyScene(t, -20, theme.Night)
	scene.scopeRange = &scope.Scope{}

	canv := blankCanvas(t)
	canv.Clear(canvas.Red)

	lay := layout{dst: canv, scope: canv.Bounds()}
	scene.draw3D(&lay, frame, 0)

	if got := canv.Image().RGBAAt(0, 0); got != scene.pal.Field {
		t.Errorf("a frame with no camera came out %v, want the field", got)
	}
}

// TestHazeEndsOnTheHorizon checks the ground, the land and the coastline all
// fade into the sky's horizon colour: the land past the haze is that colour
// exactly, and the last band of each is a shade off it, nearer to it than to
// where the fade started.
func TestHazeEndsOnTheHorizon(t *testing.T) {
	t.Parallel()

	scene, _ := skyScene(t, -3, theme.Night)
	far := scene.skyHorizon()

	land := scene.landInks3()
	if land[0] != far || land[len(land)-1] != scene.landInk() {
		t.Errorf("land runs %v to %v, want the land tint inside and the horizon %v past the haze",
			land[len(land)-1], land[0], far)
	}

	water := scene.waterInks3()
	coast := scene.coastInks3()

	for _, pair := range []struct {
		name      string
		near, end color.RGBA
	}{
		{name: "water", near: water[len(water)-1], end: water[1]},
		{name: "land", near: land[len(land)-1], end: land[1]},
		{name: "coast", near: coast[0], end: coast[hazeBands]},
	} {
		if distance(pair.end, far) >= distance(pair.end, pair.near) {
			t.Errorf("the %s's last band %v is nearer where it started %v than the horizon %v",
				pair.name, pair.end, pair.near, far)
		}
	}
}

// TestSkyLeavesGroundRows checks the rows the sky leaves uncleared are ones
// the ground then paints edge to edge: the same frame drawn over a red canvas
// and over a blue one comes out identical, and the sky did skip rows, so the
// comparison means something.
func TestSkyLeavesGroundRows(t *testing.T) {
	t.Parallel()

	set, err := shore.Load()
	if err != nil {
		t.Fatalf("shore.Load: %v", err)
	}

	var pictures [2][]byte

	for index, under := range []color.RGBA{canvas.Red, canvas.Blue} {
		scene, frame := skyScene(t, -3, theme.Night)
		scene.minimalShore, scene.shoreSet = true, set

		canv := blankCanvas(t)
		canv.Clear(under)

		lay := layout{dst: canv, scope: canv.Bounds()}
		scene.draw3D(&lay, frame, 0)

		if !slices.Contains(scene.groundRows, true) {
			t.Fatal("the sky cleared every row, so nothing was left for the ground")
		}

		if scene.hazeReady {
			t.Error("the ground left the haze marked ready for the next frame")
		}

		pictures[index] = canv.Image().Pix
	}

	if !bytes.Equal(pictures[0], pictures[1]) {
		t.Error("a row the sky skipped kept what the canvas held before the frame")
	}
}

// TestGroundRowsWithoutGround checks no row is left uncleared when no ground
// will be drawn: the shore off, or a view with nothing plottable.
func TestGroundRowsWithoutGround(t *testing.T) {
	t.Parallel()

	scene, frame := skyScene(t, -3, theme.Night)
	canv := blankCanvas(t)

	view, drawable := scene.measure3D(&layout{dst: canv, scope: canv.Bounds()}, frame, 0)
	if !drawable {
		t.Fatal("measure3D found no room")
	}

	unplottable := view
	unplottable.plottable = false

	for _, testCase := range []struct {
		name  string
		view  scene3
		shore bool
	}{
		{name: "shore off", view: view},
		{name: "nothing plottable", view: unplottable, shore: true},
	} {
		scene.minimalShore = testCase.shore

		if rows := scene.groundRows3(canv, testCase.view); len(rows) != 0 || scene.hazeReady {
			t.Errorf("%s: %d rows marked, haze ready %v, want none", testCase.name, len(rows), scene.hazeReady)
		}
	}
}
