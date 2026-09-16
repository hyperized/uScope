package termbackend

import (
	"image"
	"math"
	"os"
	"testing"

	"github.com/hyperized/uScope/pkg/winsize"
)

// Geometry used by more than one table below, named so mnd does not flag
// them and so a reader can see where a number like 23 comes from.
const (
	// A grid and cell shape realistic enough to read as "a terminal",
	// distinct from the package's own pipe-mode defaults so a test failure
	// cannot be confused with a mistakenly-hardcoded pipeCols/pipeRows.
	gridCols = 100
	gridRows = 40

	// The height-limited fitCells case: a canvas twice as tall as it is
	// wide, fitted into a square grid of square cells. cols*cellWidth*
	// canvasHeight/(canvasWidth*cellHeight) works out taller than the
	// grid, so the fit has to go by height instead of width.
	heightLimitedCanvasHeight = 4
	heightLimitedGrid         = 10
	heightLimitedWantCols     = 5

	// The floor case: a canvas absurdly wide for a one-column grid, so the
	// row count fitCells computes before atLeastOne's floor is zero.
	floorCanvasWidth = 1000

	// fitCells in pipe mode is called with the package's own defaults, and
	// the row count it comes back with is not a round multiplication, so
	// it is spelled out here rather than derived.
	pipeFitWantRows = 23

	// cellPixels geometry, matching the shape winsize's own tests use so a
	// reader who has seen one recognises the other: cols and rows differ
	// from each other, and so do the pixel dimensions, so a field-mixup
	// would fail the test.
	pixelCols    = 80
	pixelRows    = 24
	pixelCellW   = 8
	pixelCellH   = 16
	pixelsWide   = pixelCols * pixelCellW
	pixelsTall   = pixelRows * pixelCellH
	tooFewPixels = 10

	// divRound cases: an exact division, one that rounds up, one that
	// rounds down, and one sitting exactly on the half.
	divRoundExactNumerator = 10
	divRoundExactDenom     = 5
	divRoundExactWant      = 2

	divRoundUpNumerator = 11
	divRoundUpDenom     = 4
	divRoundUpWant      = 3

	divRoundDownNumerator = 9
	divRoundDownDenom     = 4
	divRoundDownWant      = 2

	divRoundTieNumerator = 10
	divRoundTieDenom     = 4
	divRoundTieWant      = 3

	// atLeastOne cases beyond the zero already covered by fitCells above.
	atLeastOneNegative = -5
	atLeastOneLarge    = 42

	// A window wide enough, and short enough, that cells() in kitty mode
	// hits the one-row margin held back for the cursor: an 80x24 window
	// happens to come out the same with or without the margin, so this
	// case needs to be genuinely wide to prove the margin is there.
	kittyWideCols     = 200
	kittyWideRows     = 50
	kittyWideWantCols = 174
	kittyWideWantRows = 49
)

// TestFitCells covers the width-limited case, which is what termbackend
// actually calls fitCells with in pipe mode, the height-limited case, and
// the floor that keeps a degenerate ratio from reaching zero cells.
//
// The height-limited case never needs the min() in fitCols to actually
// pick cols over the computed value: fitRows and fitCols are related by
// fitRows*fitCols == cols*rows in exact arithmetic, so whenever fitRows
// overshoots the grid, the computed fitCols is mathematically already
// below it. min() is still worth keeping, as a cheap guarantee against
// rounding pushing it over by one in a case this table has not thought of.
func TestFitCells(t *testing.T) {
	t.Parallel()

	for _, tcase := range fitCellsCases() {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			gotCols, gotRows := fitCells(
				tcase.canvasWidth, tcase.canvasHeight,
				tcase.cols, tcase.rows,
				tcase.cellWidth, tcase.cellHeight,
			)
			if gotCols != tcase.wantCols || gotRows != tcase.wantRows {
				t.Errorf("fitCells(%d, %d, %d, %d, %d, %d) = (%d, %d), want (%d, %d)",
					tcase.canvasWidth, tcase.canvasHeight, tcase.cols, tcase.rows, tcase.cellWidth, tcase.cellHeight,
					gotCols, gotRows, tcase.wantCols, tcase.wantRows)
			}
		})
	}
}

func fitCellsCases() []struct {
	name                      string
	canvasWidth, canvasHeight int
	cols, rows                int
	cellWidth, cellHeight     int
	wantCols, wantRows        int
} {
	return []struct {
		name                      string
		canvasWidth, canvasHeight int
		cols, rows                int
		cellWidth, cellHeight     int
		wantCols, wantRows        int
	}{
		{
			name:        "width-limited: the real pipe-mode geometry",
			canvasWidth: defaultCanvasWidth, canvasHeight: defaultCanvasHeight,
			cols: pipeCols, rows: pipeRows,
			cellWidth: fallbackCellWidth, cellHeight: fallbackCellHeight,
			wantCols: pipeCols, wantRows: pipeFitWantRows,
		},
		{
			name:        "height-limited: a canvas taller than the grid can follow",
			canvasWidth: 2, canvasHeight: heightLimitedCanvasHeight,
			cols: heightLimitedGrid, rows: heightLimitedGrid,
			cellWidth: 1, cellHeight: 1,
			wantCols: heightLimitedWantCols, wantRows: heightLimitedGrid,
		},
		{
			name:        "floor: a ratio that would round to zero rows stays at one",
			canvasWidth: floorCanvasWidth, canvasHeight: 1,
			cols: 1, rows: 1,
			cellWidth: 1, cellHeight: 1,
			wantCols: 1, wantRows: 1,
		},
	}
}

// TestCellPixels covers the fallback rule: most terminals never fill in
// the pixel fields of TIOCGWINSZ, and the few numbers that do arrive still
// have to make sense (at least one pixel per cell) before they are used.
func TestCellPixels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		size           winsize.Size
		wantCellWidth  int
		wantCellHeight int
	}{
		{
			name:          "terminal reports real pixel dimensions",
			size:          winsize.Size{Cols: pixelCols, Rows: pixelRows, XPixels: pixelsWide, YPixels: pixelsTall},
			wantCellWidth: pixelCellW, wantCellHeight: pixelCellH,
		},
		{
			name:          "terminal reports zero pixels, the common case",
			size:          winsize.Size{Cols: pixelCols, Rows: pixelRows, XPixels: 0, YPixels: 0},
			wantCellWidth: fallbackCellWidth, wantCellHeight: fallbackCellHeight,
		},
		{
			name: "terminal reports fewer pixels than cells, which is nonsense",
			size: winsize.Size{
				Cols: pixelCols, Rows: pixelRows,
				XPixels: tooFewPixels, YPixels: tooFewPixels,
			},
			wantCellWidth: fallbackCellWidth, wantCellHeight: fallbackCellHeight,
		},
		{
			name:          "terminal reports zero cells",
			size:          winsize.Size{Cols: 0, Rows: 0, XPixels: pixelsWide, YPixels: pixelsTall},
			wantCellWidth: fallbackCellWidth, wantCellHeight: fallbackCellHeight,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			term := &Terminal{size: tcase.size}

			gotWidth, gotHeight := term.cellPixels()
			if gotWidth != tcase.wantCellWidth || gotHeight != tcase.wantCellHeight {
				t.Errorf("cellPixels() = (%d, %d), want (%d, %d)",
					gotWidth, gotHeight, tcase.wantCellWidth, tcase.wantCellHeight)
			}
		})
	}
}

func TestDivRound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		numerator   int
		denominator int
		want        int
	}{
		{
			name:      "divides evenly",
			numerator: divRoundExactNumerator, denominator: divRoundExactDenom,
			want: divRoundExactWant,
		},
		{
			name:      "rounds up past the half",
			numerator: divRoundUpNumerator, denominator: divRoundUpDenom,
			want: divRoundUpWant,
		},
		{
			name:      "rounds down below the half",
			numerator: divRoundDownNumerator, denominator: divRoundDownDenom,
			want: divRoundDownWant,
		},
		{
			name:      "rounds up on an exact half",
			numerator: divRoundTieNumerator, denominator: divRoundTieDenom,
			want: divRoundTieWant,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			if got := divRound(tcase.numerator, tcase.denominator); got != tcase.want {
				t.Errorf("divRound(%d, %d) = %d, want %d", tcase.numerator, tcase.denominator, got, tcase.want)
			}
		})
	}
}

func TestAtLeastOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value int
		want  int
	}{
		{name: "zero floors to one", value: 0, want: 1},
		{name: "negative floors to one", value: atLeastOneNegative, want: 1},
		{name: "one is left alone", value: 1, want: 1},
		{name: "anything larger is left alone", value: atLeastOneLarge, want: atLeastOneLarge},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			if got := atLeastOne(tcase.value); got != tcase.want {
				t.Errorf("atLeastOne(%d) = %d, want %d", tcase.value, got, tcase.want)
			}
		})
	}
}

// TestCanvasSize checks the two modes canvasSize can be asked about: blocks
// mode doubles the cell grid, kitty mode reports the fixed canvas no
// matter what the terminal measured, and both floor a terminal that
// reports nothing sensible to at least one pixel, since canvas.New rejects
// an empty canvas.
func TestCanvasSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		term       *Terminal
		wantWidth  int
		wantHeight int
	}{
		{
			name:       "blocks mode doubles the row count",
			term:       &Terminal{mode: Blocks, size: winsize.Size{Cols: gridCols, Rows: gridRows}},
			wantWidth:  gridCols,
			wantHeight: gridRows * pixelsPerCell,
		},
		{
			name:       "blocks mode floors a zero column count",
			term:       &Terminal{mode: Blocks, size: winsize.Size{Cols: 0, Rows: gridRows}},
			wantWidth:  1,
			wantHeight: gridRows * pixelsPerCell,
		},
		{
			name:       "blocks mode floors a zero row count",
			term:       &Terminal{mode: Blocks, size: winsize.Size{Cols: gridCols, Rows: 0}},
			wantWidth:  gridCols,
			wantHeight: pixelsPerCell,
		},
		{
			name: "kitty mode reports the canvas regardless of terminal size",
			term: &Terminal{
				mode:   Kitty,
				size:   winsize.Size{Cols: 1, Rows: 1},
				canvas: image.Pt(defaultCanvasWidth, defaultCanvasHeight),
			},
			wantWidth:  defaultCanvasWidth,
			wantHeight: defaultCanvasHeight,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			gotWidth, gotHeight := tcase.term.canvasSize()
			if gotWidth != tcase.wantWidth || gotHeight != tcase.wantHeight {
				t.Errorf("canvasSize() = (%d, %d), want (%d, %d)",
					gotWidth, gotHeight, tcase.wantWidth, tcase.wantHeight)
			}
		})
	}
}

// TestCells checks the character grid a frame is drawn into: blocks mode
// hands the cell count straight through, kitty mode fits the canvas into
// it. The kitty case reuses the pipe-mode geometry, which is the same
// computation TestFitCells already checks in isolation; this proves cells
// wires cellPixels and fitCells together correctly rather than testing the
// arithmetic a second time.
func TestCells(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		term     *Terminal
		wantCols int
		wantRows int
	}{
		{
			name:     "blocks mode returns the cell grid unchanged",
			term:     &Terminal{mode: Blocks, size: winsize.Size{Cols: gridCols, Rows: gridRows}},
			wantCols: gridCols, wantRows: gridRows,
		},
		{
			name:     "blocks mode floors a terminal reporting nothing",
			term:     &Terminal{mode: Blocks, size: winsize.Size{Cols: 0, Rows: 0}},
			wantCols: 1, wantRows: 1,
		},
		{
			name: "kitty mode fits the canvas into the grid using the fallback cell shape",
			term: &Terminal{
				mode:   Kitty,
				size:   winsize.Size{Cols: pipeCols, Rows: pipeRows},
				canvas: image.Pt(defaultCanvasWidth, defaultCanvasHeight),
			},
			wantCols: pipeCols, wantRows: pipeFitWantRows,
		},
		{
			// The one-row cursor margin: a window this wide and this short
			// is the case where holding a row back actually changes the
			// result (200x50 fits to 174x49, not 174x50), unlike the
			// 80x24 case above which comes out identical either way.
			name: "kitty mode holds back one row for the cursor",
			term: &Terminal{
				mode:   Kitty,
				size:   winsize.Size{Cols: kittyWideCols, Rows: kittyWideRows},
				canvas: image.Pt(defaultCanvasWidth, defaultCanvasHeight),
			},
			wantCols: kittyWideWantCols, wantRows: kittyWideWantRows,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			gotCols, gotRows := tcase.term.cells()
			if gotCols != tcase.wantCols || gotRows != tcase.wantRows {
				t.Errorf("cells() = (%d, %d), want (%d, %d)", gotCols, gotRows, tcase.wantCols, tcase.wantRows)
			}
		})
	}
}

// TestNotifyResizeAndStopResize exercises the real signal subscription
// directly, on a channel the test owns rather than the one a run loop
// would use. It installs a SIGWINCH handler and immediately removes it
// again, which is the only thing there is to check: neither function
// branches on anything.
func TestNotifyResizeAndStopResize(t *testing.T) {
	t.Parallel()

	ch := make(chan os.Signal, resizeBuffer)

	notifyResize(ch)
	stopResize(ch)
}

// The two Ghostty-shaped windows the aspect test drives, and the cell the
// terminal reports for both. Nine by eighteen is a common monospaced cell on a
// laptop panel, and it is exactly the 1:2 shape the fallback assumes, which is
// what makes the third case below comparable with the first.
const (
	aspectCellW = 9
	aspectCellH = 18

	aspectWideCols = 200
	aspectWideRows = 50

	aspectShortCols = 92
	aspectShortRows = 12

	// aspectSlack is how far the fitted picture may sit from the canvas's own
	// shape, in cells. One cell is the finest a terminal can be asked to place
	// an image to; anything inside that is rounding rather than distortion.
	aspectSlack = 1.0
)

// TestCellsKeepTheCanvasAspect measures what a Ghostty-shaped window actually
// gets: cellPixels reads the cell out of what the terminal reported, fitCells
// picks the cell area, and the picture that lands has to keep the canvas's own
// 16:9 shape to within one cell.
//
// This is the calculation behind a report of a stretched frame, so it is
// checked end to end through cells() rather than on fitCells alone: a correct
// fit fed a wrong cell size would draw exactly the reported stretch, and only
// the pair together can rule that out.
func TestCellsKeepTheCanvasAspect(t *testing.T) {
	t.Parallel()

	for _, tcase := range []struct {
		name               string
		size               winsize.Size
		wantCols, wantRows int
		wantCellW          int
		wantCellH          int
	}{
		{
			name: "a wide window with the cell size reported",
			size: winsize.Size{
				Cols: aspectWideCols, Rows: aspectWideRows,
				XPixels: aspectWideCols * aspectCellW, YPixels: aspectWideRows * aspectCellH,
			},
			wantCols: 174, wantRows: 49, wantCellW: aspectCellW, wantCellH: aspectCellH,
		},
		{
			name: "a short window with the cell size reported",
			size: winsize.Size{
				Cols: aspectShortCols, Rows: aspectShortRows,
				XPixels: aspectShortCols * aspectCellW, YPixels: aspectShortRows * aspectCellH,
			},
			wantCols: 39, wantRows: 11, wantCellW: aspectCellW, wantCellH: aspectCellH,
		},
		{
			// Most terminals report nothing for the pixel fields, so the 1:2
			// fallback is the common case rather than the exception. It lands
			// on the same cell area as the first case, because a 9x18 cell is
			// 1:2: the fallback is right in proportion to how close the real
			// cell is to that shape, and wrong in proportion to how far it is.
			name:     "a wide window with no cell size reported",
			size:     winsize.Size{Cols: aspectWideCols, Rows: aspectWideRows},
			wantCols: 174, wantRows: 49,
			wantCellW: fallbackCellWidth, wantCellH: fallbackCellHeight,
		},
	} {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			term := &Terminal{
				mode:   Kitty,
				canvas: image.Pt(defaultCanvasWidth, defaultCanvasHeight),
				size:   tcase.size,
			}

			if gotW, gotH := term.cellPixels(); gotW != tcase.wantCellW || gotH != tcase.wantCellH {
				t.Fatalf("cellPixels() = (%d, %d), want (%d, %d)", gotW, gotH, tcase.wantCellW, tcase.wantCellH)
			}

			cols, rows := term.cells()
			if cols != tcase.wantCols || rows != tcase.wantRows {
				t.Fatalf("cells() = (%d, %d), want (%d, %d)", cols, rows, tcase.wantCols, tcase.wantRows)
			}

			if rows > tcase.size.Rows-1 || cols > tcase.size.Cols {
				t.Errorf("cells() = (%d, %d), which does not fit a %dx%d window with a row held back",
					cols, rows, tcase.size.Cols, tcase.size.Rows)
			}

			// The picture covers cols*cellWidth by rows*cellHeight physical
			// pixels, and that box has to have the canvas's own shape. Both
			// directions are checked: a fit that is right in one and a
			// cell short in the other is still a stretched frame.
			assertAspect(t, cols, rows, aspectCellW, aspectCellH)
		})
	}
}

// assertAspect checks that a cols by rows area of cellW by cellH pixels holds
// the default canvas's shape to within aspectSlack cells, in both directions.
func assertAspect(t *testing.T, cols, rows, cellW, cellH int) {
	t.Helper()

	idealRows := float64(cols*cellW*defaultCanvasHeight) / float64(defaultCanvasWidth*cellH)
	if diff := math.Abs(idealRows - float64(rows)); diff > aspectSlack {
		t.Errorf("%d columns want %.2f rows to keep the canvas shape, got %d, off by %.2f",
			cols, idealRows, rows, diff)
	}

	idealCols := float64(rows*cellH*defaultCanvasWidth) / float64(defaultCanvasHeight*cellW)
	if diff := math.Abs(idealCols - float64(cols)); diff > aspectSlack {
		t.Errorf("%d rows want %.2f columns to keep the canvas shape, got %d, off by %.2f",
			rows, idealCols, cols, diff)
	}
}
