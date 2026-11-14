package radar

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// A 2560x1440 frame, which the star field is sized for.
const (
	starsWidth  = 2560
	starsHeight = 1440
)

// TestStarFieldIsFixed checks the field is the same field every time it is
// made from the seed, sits between the horizon and starTopDeg, and holds
// every class with the faintest the commonest.
func TestStarFieldIsFixed(t *testing.T) {
	t.Parallel()

	if makeStarField(starSeed) != starField {
		t.Fatal("the field made from the seed again differs from the package's")
	}

	if makeStarField(starSeed+1) == starField {
		t.Error("another seed made the same field")
	}

	top := math.Sin(starTopDeg * math.Pi / halfCircle)

	var counts [len(starClasses)]int

	for index, entry := range starField {
		if entry.sinAlt < 0 || entry.sinAlt >= top {
			t.Errorf("star %d is at sin(altitude) %g, want 0 up to %g", index, entry.sinAlt, top)
		}

		if entry.fade < 0 || entry.fade > 1 {
			t.Errorf("star %d fades to %g, want 0 to 1", index, entry.fade)
		}

		counts[entry.class]++
	}

	for class, count := range counts {
		if count == 0 {
			t.Errorf("class %d has no stars", class)
		}
	}

	if last := counts[len(counts)-1]; last <= counts[0] || last <= counts[1] || last <= counts[2] {
		t.Errorf("class counts %v, want the faintest the commonest", counts)
	}
}

// TestStarClassAt pins the share boundaries, and the draw of exactly one,
// which the generator never makes but the fallback still has to answer.
func TestStarClassAt(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		draw float64
		want uint8
	}{
		{draw: 0, want: 0},
		{draw: 0.0599, want: 0},
		{draw: 0.06, want: 1},
		{draw: 0.26, want: 2},
		{draw: 0.6, want: 3},
		{draw: 0.9999, want: 3},
		{draw: 1, want: 3},
	} {
		if got := starClassAt(testCase.draw); got != testCase.want {
			t.Errorf("starClassAt(%g) = %d, want %d", testCase.draw, got, testCase.want)
		}
	}
}

// TestStarsVisibility pins the fade: full from the end of civil twilight
// down, none from the horizon up, linear between, and none for a NaN.
func TestStarsVisibility(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		elevation float64
		want      float64
	}{
		{elevation: -30, want: 1},
		{elevation: -6, want: 1},
		{elevation: -3, want: 0.5},
		{elevation: 0, want: 0},
		{elevation: 20, want: 0},
		{elevation: math.NaN(), want: 0},
	} {
		if got := starsVisibility(testCase.elevation); math.Abs(got-testCase.want) > 1e-12 {
			t.Errorf("starsVisibility(%g) = %g, want %g", testCase.elevation, got, testCase.want)
		}
	}
}

// TestStarTargetStaysUnderTheCeiling checks no dark palette lets a star past
// the luma the night sky is held under, and that the one-hued looks keep
// their hue.
func TestStarTargetStaysUnderTheCeiling(t *testing.T) {
	t.Parallel()

	for _, pal := range []theme.Palette{theme.Night, theme.PhosphorNight, theme.MonoNight} {
		target := starTarget(pal)
		if got := luma(target); got > nightSkyLumaCeiling {
			t.Errorf("%s: the brightest star is luma %.1f, want at most %g", pal.Look, got, nightSkyLumaCeiling)
		}

		if target == pal.Field {
			t.Errorf("%s: the brightest star is the field", pal.Look)
		}
	}

	if target := starTarget(theme.PhosphorNight); target.G <= target.R || target.G <= target.B {
		t.Errorf("a phosphor star is %v, want it green", target)
	}
}

// TestLighten checks a star never darkens the sky under it.
func TestLighten(t *testing.T) {
	t.Parallel()

	sky := color.RGBA{R: 200, G: 40, B: 10, A: opaque}
	ink := color.RGBA{R: 80, G: 80, B: 90, A: opaque}

	if got, want := lighten(sky, ink, 1), (color.RGBA{R: 200, G: 80, B: 90, A: opaque}); got != want {
		t.Errorf("lighten at full = %v, want %v", got, want)
	}

	if got := lighten(sky, ink, math.NaN()); got != sky {
		t.Errorf("lighten by NaN = %v, want the sky %v", got, sky)
	}
}

// starsCanvas is a 2560x1440 canvas with a night sky painted on it by
// scene, and a copy of its pixels to compare against.
func starsCanvas(tb testing.TB, scene *Scene) (*canvas.Canvas, image.Rectangle, []uint8) {
	tb.Helper()

	canv, err := canvas.New(starsWidth, starsHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	box := canv.Bounds()
	scene.paintSky(canv, box)

	return canv, box, append([]uint8(nil), canv.Image().Pix...)
}

// changedRows lists, for every pixel that differs from before, its row.
func changedRows(canv *canvas.Canvas, before []uint8) []int {
	img := canv.Image()

	var rows []int

	for offset := 0; offset < len(before); offset += 4 {
		if img.Pix[offset] != before[offset] || img.Pix[offset+1] != before[offset+1] ||
			img.Pix[offset+2] != before[offset+2] {
			rows = append(rows, offset/img.Stride)
		}
	}

	return rows
}

// TestDrawStarsAtNight checks a night sky gets its stars, all of them above
// the horizon, and that no pixel is darker for one.
func TestDrawStarsAtNight(t *testing.T) {
	t.Parallel()

	scene, _ := skyScene(t, -20, theme.Night)
	scene.orbiting, scene.azimuth = false, 143

	canv, box, before := starsCanvas(t, scene)
	scene.drawStars(canv, box)

	rows := changedRows(canv, before)
	if len(rows) < 30 {
		t.Fatalf("%d pixels of star, want a few dozen at least", len(rows))
	}

	horizon := horizonRow(box, scene.elevation)
	for _, row := range rows {
		if row >= horizon {
			t.Fatalf("a star landed on row %d, at or under the horizon on %d", row, horizon)
		}
	}

	for offset := 0; offset < len(before); offset += 4 {
		if canv.Image().Pix[offset] < before[offset] {
			t.Fatal("a star made a pixel darker")
		}
	}
}

// TestDrawStarsRefuses checks every case that draws nothing: the stars off,
// no sun to say it is night, a sun up, and a camera tilted so far down that
// there is no sky in the picture.
func TestDrawStarsRefuses(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		elevation float64
		change    func(*Scene)
	}{
		{name: "stars off", elevation: -20, change: func(scene *Scene) { scene.stars = false }},
		{name: "no sun", elevation: -20, change: func(scene *Scene) { scene.sky.known = false }},
		{name: "daylight", elevation: 5, change: func(*Scene) {}},
		{name: "no sky", elevation: -20, change: func(scene *Scene) { scene.elevation = maxElevation }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, _ := skyScene(t, testCase.elevation, theme.Night)
			testCase.change(scene)

			canv, box, before := starsCanvas(t, scene)
			scene.drawStars(canv, box)

			if rows := changedRows(canv, before); len(rows) != 0 {
				t.Errorf("%d pixels changed, want none", len(rows))
			}
		})
	}
}

// TestStarsProjectLikeTheCamera checks every star lands where the 3D camera
// puts a point far out along the same direction, so the stars turn with the
// ground at the ground's own rate.
func TestStarsProjectLikeTheCamera(t *testing.T) {
	t.Parallel()

	const far = 1e5

	box := image.Rect(0, 0, starsWidth, starsHeight)

	for _, azimuth := range []float64{0, 143, 153, 271.5} {
		cam, _ := newCamera3(box, framing3{scopeNm: 60, azimuth: azimuth, elevation: minElevation})

		sinAz, cosAz := math.Sincos(azimuth * math.Pi / halfCircle)
		sinEl, cosEl := math.Sincos(minElevation * math.Pi / halfCircle)
		view := starView{
			sinAz: sinAz, cosAz: cosAz, sinEl: sinEl, cosEl: cosEl,
			focal: starsWidth, centerX: starsWidth / 2, centerY: starsHeight / 2,
		}

		seen := 0

		for index := range starField {
			entry := &starField[index]

			starX, starY, ok := view.project(entry)
			if !ok {
				continue
			}

			camX, camY, camOK := cam.at(point3{
				east:  cam.eye.east + far*entry.sinAz*entry.cosAlt,
				north: cam.eye.north + far*entry.cosAz*entry.cosAlt,
				up:    cam.eye.up + far*entry.sinAlt,
			})
			if !camOK {
				continue
			}

			seen++

			if max(starX-camX, camX-starX) > 1 || max(starY-camY, camY-starY) > 1 {
				t.Errorf("azimuth %g, star %d: projected to %d,%d, the camera says %d,%d",
					azimuth, index, starX, starY, camX, camY)
			}
		}

		if seen == 0 {
			t.Errorf("azimuth %g: no star both sides could see", azimuth)
		}
	}
}

// TestStarBehindTheCameraIsDropped checks a star opposite the camera's
// heading projects to nothing rather than to a mirrored ghost.
func TestStarBehindTheCameraIsDropped(t *testing.T) {
	t.Parallel()

	view := starView{cosAz: 1, cosEl: 1, focal: starsWidth}
	behind := star{cosAz: -1, cosAlt: 1}

	if _, _, ok := view.project(&behind); ok {
		t.Error("a star behind the camera was projected")
	}
}

// TestStarPaintStaysInTheSky checks a square that straddles the top of the
// window or the horizon is painted only on the sky rows it covers.
func TestStarPaintStaysInTheSky(t *testing.T) {
	t.Parallel()

	canv, err := canvas.New(20, 20)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	field := color.RGBA{A: opaque}
	canv.Clear(field)

	white := color.RGBA{R: opaque, G: opaque, B: opaque, A: opaque}
	view := starView{rows: []color.RGBA{field, field, field}, top: 5, target: white}

	view.paint(canv, image.Rect(2, 3, 4, 10), 1)

	img := canv.Image()

	for row := range 20 {
		want := field
		if row >= 5 && row < 8 {
			want = white
		}

		if got := img.RGBAAt(2, row); got != want {
			t.Errorf("row %d is %v, want %v", row, got, want)
		}
	}
}

// TestDrawStarsAllocations holds the stars to the tilted views' rule of no
// allocation per frame.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestDrawStarsAllocations(t *testing.T) {
	scene, _ := skyScene(t, -20, theme.Night)
	canv, box, _ := starsCanvas(t, scene)

	if got := testing.AllocsPerRun(50, func() { scene.drawStars(canv, box) }); got != 0 {
		t.Errorf("drawStars allocated %.1f times, want 0", got)
	}
}

// BenchmarkDrawStars is one frame's stars at 2560x1440: the whole
// field projected and the visible part painted.
func BenchmarkDrawStars(b *testing.B) {
	scene, _ := skyScene(b, -20, theme.Night)
	scene.orbiting, scene.azimuth = false, 143
	canv, box, _ := starsCanvas(b, scene)

	b.ReportAllocs()

	for b.Loop() {
		scene.drawStars(canv, box)
	}
}
