package radar_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The board's own geometry at panelWidth x panelHeight, night theme, one
// column, verified once against a real Draw before these tests were written:
// the column runs x 621..1264, the first strip starts at y 313, every strip
// is 30 pixels tall, and the board's own room ends at y 638 where the legend
// takes over. The accent edge is the left 3 pixels of a marked strip's band
// and the index cell is the rank digits just past it.
const (
	boardFirstStripTop = 313
	boardStripHeight   = 30
	boardLegendTop     = 638
	boardColumnLeft    = 621
	boardColumnRight   = 1264
	boardAccentLeft    = 621
	boardAccentRight   = 624
	boardIndexLeft     = 632
	boardIndexRight    = 650
)

// accentWidth is how many pixels wide the marked strip's accent edge is,
// which is boardAccentRight less boardAccentLeft rather than a second literal
// that could drift out of step with the two above.
const accentWidth = boardAccentRight - boardAccentLeft

// stripRowBox is the band one strip's fields are drawn in, at slot places in
// the window counting down from the first strip.
func stripRowBox(slot int) image.Rectangle {
	top := boardFirstStripTop + slot*boardStripHeight

	return image.Rect(boardColumnLeft, top, boardColumnRight, top+boardStripHeight)
}

// accentBox is the slice of a strip's own band the accent edge is drawn in.
func accentBox(slot int) image.Rectangle {
	top := boardFirstStripTop + slot*boardStripHeight

	return image.Rect(boardAccentLeft, top, boardAccentRight, top+boardStripHeight)
}

// firstAccentTop finds the top of the one strip band inside box that is
// filled with col, reading down a single column of pixels, or reports false
// if the colour never appears there. It is how the scrolling test below finds
// the marked strip without already knowing which slot the window put it at.
func firstAccentTop(canv *canvas.Canvas, box image.Rectangle, col color.RGBA) (int, bool) {
	area := box.Intersect(canv.Bounds())

	for y := area.Min.Y; y < area.Max.Y; y++ {
		if canv.Image().RGBAAt(area.Min.X, y) == col {
			return y, true
		}
	}

	return 0, false
}

// indexCellBox is the rank digits at the left of one strip's ident field, at
// slot places in the window the same way stripRowBox counts them.
func indexCellBox(slot int) image.Rectangle {
	top := boardFirstStripTop + slot*boardStripHeight

	return image.Rect(boardIndexLeft, top, boardIndexRight, top+boardStripHeight)
}

// TestBoardMarksSelectionAtItsOwnListIndex is the rule the board was
// rewritten around: pressing n moves the mark to wherever the chosen
// aircraft sits in the list, rather than lifting it to the top the way the
// full-height card used to. A fleet that fits on one screen keeps the window
// at its first strip throughout, so the k-th press of n has to land the
// accent edge on slot k and nowhere else.
func TestBoardMarksSelectionAtItsOwnListIndex(t *testing.T) {
	t.Parallel()

	const boardSize = 8 // Fewer than the ten strips a 1280x720 column holds, so the window never scrolls.

	frame := sceneFrame(rowFleet(boardSize)...)

	for _, testCase := range []struct {
		name string
		slot int
	}{
		{name: "the first aircraft in the list", slot: 0},
		{name: "an aircraft in the middle of the list", slot: 3},
		{name: "the last aircraft in the list", slot: boardSize - 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
			scene.Draw(canv, 0)

			for range testCase.slot {
				press(scene, 'n')
			}

			scene.Draw(canv, 0)

			want := accentWidth * boardStripHeight
			if got := countColour(canv, accentBox(testCase.slot), theme.Night.Accent); got != want {
				t.Errorf("accent pixels at slot %d = %d, want %d", testCase.slot, got, want)
			}
		})
	}
}

// TestBoardKeepsUnselectedStripsInPlace is the other half of the rule above:
// moving the mark to a different aeroplane must not shuffle the strips that
// were never selected either way. Slot 1 never carries the mark in this
// fleet, so its row has to come out pixel for pixel the same whichever of the
// other two selections put it there, which is what "the list order is fixed"
// means at the pixel level.
func TestBoardKeepsUnselectedStripsInPlace(t *testing.T) {
	t.Parallel()

	const boardSize = 8

	frame := sceneFrame(rowFleet(boardSize)...)

	render := func(slot int) *canvas.Canvas {
		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
		scene.Draw(canv, 0)

		for range slot {
			press(scene, 'n')
		}

		scene.Draw(canv, 0)

		return canv
	}

	const (
		untouchedSlot = 1
		middleSlot    = 3
	)

	middle := render(middleSlot)
	last := render(boardSize - 1)

	if !identicalIn(middle, last, stripRowBox(untouchedSlot)) {
		t.Error("slot 1 changed between two selections that never stood on it, want the list order left alone")
	}
}

// TestBoardScrollKeepsTheMarkInsideTheColumn covers the scrolling: with far
// more aircraft than the window holds, stepping the selection past its
// bottom has to bring the window with it, and the mark it carries has to stay
// a whole strip, fully between the board's own top and the legend under it,
// at every step rather than only at the ones a smaller fleet would exercise.
func TestBoardScrollKeepsTheMarkInsideTheColumn(t *testing.T) {
	t.Parallel()

	const crowd = 60

	frame := sceneFrame(rowFleet(crowd)...)
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	want := accentWidth * boardStripHeight
	scanBox := image.Rect(boardAccentLeft, boardFirstStripTop, boardAccentLeft+1, boardLegendTop)

	for step := range crowd {
		press(scene, 'n')
		scene.Draw(canv, 0)

		top, found := firstAccentTop(canv, scanBox, theme.Night.Accent)
		if !found {
			t.Fatalf("step %d: no accent pixel found between the board's top and the legend", step)
		}

		if (top-boardFirstStripTop)%boardStripHeight != 0 {
			t.Fatalf("step %d: accent band starts at y=%d, not aligned to a strip boundary", step, top)
		}

		band := image.Rect(boardAccentLeft, top, boardAccentRight, top+boardStripHeight)
		if band.Max.Y > boardLegendTop {
			t.Fatalf("step %d: accent band %v runs past the legend at y=%d", step, band, boardLegendTop)
		}

		if got := countColour(canv, band, theme.Night.Accent); got != want {
			t.Fatalf("step %d: accent pixels in its own band = %d, want %d", step, got, want)
		}
	}
}

// TestBoardIndexInkMarksTheSelectedStrip checks the one thing the index cell
// alone says about the mark: the selected strip's own number is set in the
// reading ink and every other strip's is muted, which is what lets the
// number be read as "which one" without also reading the accent edge beside
// it.
func TestBoardIndexInkMarksTheSelectedStrip(t *testing.T) {
	t.Parallel()

	const (
		boardSize = 8
		marked    = 3
	)

	frame := sceneFrame(rowFleet(boardSize)...)
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	for range marked {
		press(scene, 'n')
	}

	scene.Draw(canv, 0)

	for slot := range boardSize {
		box := indexCellBox(slot)
		ink := countColour(canv, box, theme.Night.Ink)
		muted := countColour(canv, box, theme.Night.Muted)

		if slot == marked {
			if ink == 0 {
				t.Errorf("slot %d: no ink pixels in the index cell, want the marked strip's own number lit", slot)
			}
		} else if muted == 0 {
			t.Errorf("slot %d: no muted pixels in the index cell, want an unselected number", slot)
		}
	}
}

// TestBoardStepsMoveTheMarkByOneStrip checks n and p against the board
// itself: each press has to move the accent edge to the very next or
// previous slot, never skipping one and never leaving it standing still.
func TestBoardStepsMoveTheMarkByOneStrip(t *testing.T) {
	t.Parallel()

	const boardSize = 8

	frame := sceneFrame(rowFleet(boardSize)...)
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	want := accentWidth * boardStripHeight

	assertSlot := func(t *testing.T, slot int) {
		t.Helper()

		if got := countColour(canv, accentBox(slot), theme.Night.Accent); got != want {
			t.Errorf("accent pixels at slot %d = %d, want %d", slot, got, want)
		}
	}

	assertSlot(t, 0)

	for _, step := range []struct {
		key  rune
		slot int
	}{
		{key: 'n', slot: 1}, {key: 'n', slot: 2}, {key: 'n', slot: 3},
		{key: 'p', slot: 2}, {key: 'p', slot: 1}, {key: 'p', slot: 0},
	} {
		press(scene, step.key)
		scene.Draw(canv, 0)
		assertSlot(t, step.slot)
	}
}
