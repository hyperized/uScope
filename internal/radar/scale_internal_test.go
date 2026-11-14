package radar

import (
	"image"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
)

// TestUIScale pins the scale to the canvas height: whole multiples of 1080,
// rounded down, and never below one however small the canvas.
func TestUIScale(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		bounds image.Rectangle
		want   int
	}{
		{name: "the panel", bounds: image.Rect(0, 0, 1280, 720), want: 1},
		{name: "one pixel short of the base", bounds: image.Rect(0, 0, 1920, 1079), want: 1},
		{name: "the base itself", bounds: image.Rect(0, 0, 1920, 1080), want: 1},
		{name: "1440 rounds down", bounds: image.Rect(0, 0, 2560, 1440), want: 1},
		{name: "one pixel short of twice the base", bounds: image.Rect(0, 0, 3840, 2159), want: 1},
		{name: "4K", bounds: image.Rect(0, 0, 3840, 2160), want: 2},
		{name: "a wide canvas scales on its height", bounds: image.Rect(0, 0, 7680, 2160), want: 2},
		{name: "a tall narrow canvas scales on its height", bounds: image.Rect(0, 0, 640, 2160), want: 2},
		{name: "8K", bounds: image.Rect(0, 0, 7680, 4320), want: 4},
		{name: "a few half blocks", bounds: image.Rect(0, 0, 80, 48), want: 1},
		{name: "an empty box", bounds: image.Rectangle{}, want: 1},
		{name: "a box off the origin", bounds: image.Rect(100, 2000, 200, 4160), want: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := uiScale(testCase.bounds); got != testCase.want {
				t.Errorf("uiScale(%v) = %d, want %d", testCase.bounds, got, testCase.want)
			}
		})
	}
}

// TestLayoutUnits covers the layout's scale arithmetic, the zero value
// included: a layout written out as a literal in a test has no scale, and it
// has to draw at the design size rather than at nothing.
func TestLayoutUnits(t *testing.T) {
	t.Parallel()

	small := rowPlanSmall(t)

	for _, testCase := range []struct {
		name  string
		scale int
		unit  int
	}{
		{name: "zero reads as one", scale: 0, unit: 1},
		{name: "a negative scale reads as one", scale: -3, unit: 1},
		{name: "one", scale: 1, unit: 1},
		{name: "two", scale: 2, unit: 2},
		{name: "four", scale: 4, unit: 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lay := layout{scale: testCase.scale}
			unit := testCase.unit
			tracked := (3*small.Width() + 2*labelTracking) * unit

			for _, check := range []struct {
				what      string
				got, want int
			}{
				{what: "unit()", got: lay.unit(), want: unit},
				{what: "px(blockGap)", got: lay.px(blockGap), want: blockGap * unit},
				{what: "pxf(arrowCell)", got: int(lay.pxf(arrowCell)), want: arrowCell * unit},
				{what: "lineHeight", got: lay.lineHeight(small), want: small.Height() * unit},
				{what: "glyphWidth", got: lay.glyphWidth(small), want: small.Width() * unit},
				{what: "measure(ABC)", got: lay.measure(small, "ABC"), want: 3 * small.Width() * unit},
				{what: "measureTracked(ABC)", got: lay.measureTracked(small, "ABC"), want: tracked},
				{what: "trackedWidth(3)", got: lay.trackedWidth(small, 3), want: tracked},
				{what: "fitRunes", got: lay.fitRunes(small, tracked, labelTracking), want: 3},
			} {
				if check.got != check.want {
					t.Errorf("%s = %d, want %d", check.what, check.got, check.want)
				}
			}
		})
	}
}

// TestLayoutMetricsWithoutAFace keeps the nil-face answer at every scale: a
// missing face measures zero, which is how a block decides to skip itself.
func TestLayoutMetricsWithoutAFace(t *testing.T) {
	t.Parallel()

	lay := layout{scale: 2}

	if got := lay.lineHeight(nil); got != 0 {
		t.Errorf("lineHeight(nil) = %d, want 0", got)
	}

	if got := lay.glyphWidth(nil); got != 0 {
		t.Errorf("glyphWidth(nil) = %d, want 0", got)
	}
}

// TestStripPlanScale checks the plan's own two sizes follow the scale it was
// made at, with zero standing for one the way a layout's does.
func TestStripPlanScale(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		scale int
		unit  int
	}{
		{name: "zero reads as one", scale: 0, unit: 1},
		{name: "one", scale: 1, unit: 1},
		{name: "two", scale: 2, unit: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plan := stripPlan{scale: testCase.scale}

			if got, want := plan.gap(), stripGap*testCase.unit; got != want {
				t.Errorf("gap() = %d, want %d", got, want)
			}

			if got, want := plan.cell(), attCell*testCase.unit; got != want {
				t.Errorf("cell() = %d, want %d", got, want)
			}
		})
	}
}

// TestStripPlanPlaceRoundsToDesignPixels is the reason place rounds its share
// of the slack: a board planned at twice the size, with twice the slack, puts
// every field and every rule at exactly twice where the small board puts them.
func TestStripPlanPlaceRoundsToDesignPixels(t *testing.T) {
	t.Parallel()

	const left, extra = 10, 7

	build := func(scale int) stripPlan {
		plan := stripPlan{scale: scale}

		for _, field := range []int{fieldIdent, fieldLevel, fieldSpeed, fieldRange} {
			plan.on[field] = true
			plan.width[field] = (field + 3) * 11 * scale
		}

		plan.place(left*scale, plan.total()+extra*scale)

		return plan
	}

	small, large := build(1), build(2)

	for field := range fieldCount {
		if !small.on[field] {
			continue
		}

		if got, want := large.left[field], 2*small.left[field]; got != want {
			t.Errorf("field %d left = %d at scale two, want %d", field, got, want)
		}

		if got, want := large.rule[field], 2*small.rule[field]; got != want {
			t.Errorf("field %d rule = %d at scale two, want %d", field, got, want)
		}
	}
}

// layoutFaces is the four embedded faces, which the doubling test needs all
// of: the header, the key bar and the column each measure a different one.
func layoutFaces(tb testing.TB) Faces {
	tb.Helper()

	load := func(name string, open func() (*psf.Font, error)) *psf.Font {
		face, err := open()
		if err != nil {
			tb.Fatalf("fonts.%s: %v", name, err)
		}

		return face
	}

	return Faces{
		Small:    load("Small", fonts.Small),
		Body:     load("Body", fonts.Body),
		BodyBold: load("BodyBold", fonts.BodyBold),
		Large:    load("Large", fonts.Large),
	}
}

// TestLayoutDoublesAt2160 is the acceptance test for a 4K canvas: the frame
// carved at 3840x2160 is the frame carved at 1920x1080 with every edge,
// every block and every threshold decision at exactly twice the size.
func TestLayoutDoublesAt2160(t *testing.T) {
	t.Parallel()

	carve := func(width, height int) layout {
		canv, err := canvas.New(width, height)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scene := New(layoutFaces(t), &stubSource{}, scope.New())
		lay := scene.newLayout(canv)
		lay.bottom -= scene.keyBarHeight(&lay)
		lay.top += scene.headerHeight(&lay)
		lay.split()

		return lay
	}

	small, large := carve(1920, 1080), carve(3840, 2160)

	if small.scale != 1 || large.scale != 2 {
		t.Fatalf("scales = %d and %d, want 1 and 2", small.scale, large.scale)
	}

	double := func(rect image.Rectangle) image.Rectangle {
		return image.Rectangle{Min: rect.Min.Mul(2), Max: rect.Max.Mul(2)}
	}

	if got, want := large.scope, double(small.scope); got != want {
		t.Errorf("scope at 2160 = %v, want %v", got, want)
	}

	if got, want := large.column, double(small.column); got != want {
		t.Errorf("column at 2160 = %v, want %v", got, want)
	}

	if large.labels != small.labels {
		t.Errorf("labels at 2160 = %v, want %v", large.labels, small.labels)
	}
}

// TestColumnAtScale checks the column decision a canvas makes at twice the
// size. A 2160-tall canvas needs most of its width for the square scope
// before minColumnWidth is ever asked, so the cases are the ones the scaled
// threshold and the square decide between them.
func TestColumnAtScale(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		width  int
		height int
		column bool
	}{
		{name: "1200 wide keeps the column at 1080", width: 1200, height: 1080, column: true},
		{name: "1200 wide loses it at 2160", width: 1200, height: 2160, column: false},
		{name: "2400 wide keeps it at 2160", width: 2400, height: 2160, column: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(testCase.width, testCase.height)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			scene := New(Faces{}, &stubSource{}, scope.New())
			lay := scene.newLayout(canv)
			lay.split()

			if got := !lay.column.Empty(); got != testCase.column {
				t.Errorf("column drawn = %v, want %v (scope %v)", got, testCase.column, lay.scope)
			}
		})
	}
}

// design is a layout at the design size, for the tests that measure a block
// without a canvas of their own to draw it on.
func design() *layout { return &layout{} }

// TestDrawCapsDoublesAtItsWidestState draws the widest bar of
// TestDrawCapsFitsAtItsWidestState at a scale of two on a 3840 wide canvas: it
// has to end at exactly twice the x it ends at on the panel, inside the
// doubled margin.
func TestDrawCapsDoublesAtItsWidestState(t *testing.T) {
	t.Parallel()

	const uhdWidth, uhdHeight = 3840, 2160

	widest := func(width, height, scale int) (int, int) {
		canv, err := canvas.New(width, height)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scene := &Scene{
			faces:         layerTestFaces(t),
			shown:         View3D,
			biasSupported: true,
			colour:        ColourAirline,
			trail:         trailShort,
			filter:        filterState{kind: filterBand, band: bandMid},
			sunTheme:      autoTheme{on: true},
		}
		scene.SetPalette(theme.PhosphorNight)

		lay := layout{dst: canv, right: width - baseMargin*scale, scale: scale}

		pen := scene.drawCaps(&lay, baseMargin*scale, 0, keyCaps[:])
		pen = scene.drawCaps(&lay, pen, 0, biasCap[:])

		return scene.drawCaps(&lay, pen, 0, view3DCaps[:]), lay.right
	}

	small, _ := widest(layerCanvasWidth, layerCanvasHeight, 1)
	large, right := widest(uhdWidth, uhdHeight, 2)

	if large != 2*small {
		t.Errorf("the widest bar at scale two ended at x=%d, want %d", large, 2*small)
	}

	if large >= right {
		t.Errorf("the widest bar at scale two reached x=%d, want it to stop before the %d margin", large, right)
	}
}
