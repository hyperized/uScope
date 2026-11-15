package radar

import (
	"image"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
)

// The list grouped by class.
//
// In corridor colouring the strips stop being one list by distance with the
// arrivals and the departures mixed through it. They are three sections, IN,
// OUT and OTHER, each with a heading line, and each still in distance order
// inside itself. The colour on the picture already sorts the traffic into
// those three; a list that then interleaved them made the eye do the sorting
// a second time.
//
// The same index serves the full views' column and the bare 3D view's board,
// and the selection steps through it in the order it is drawn.
const (
	// groupNone marks an aircraft of the frame that has no row: the filter is
	// keeping it off the field.
	groupNone = classCount

	// groupRuleGap is the air between a heading's words and the hairline either
	// side of them.
	groupRuleGap = 4
)

// classOrder is the classes in the order the sections are stacked, which is
// the order the legend names them and the f key walks them.
//
//nolint:gochecknoglobals // an order is data, and an array cannot be const.
var classOrder = [classCount]source.TrackClass{
	source.TrackInbound, source.TrackOutbound, source.TrackTransit,
}

// groupIndex is the frame's rows sorted into sections.
//
// The slices are the Scene's own and are cut back to zero length every frame
// rather than made again, so a steady frame adds rows to room it already has.
type groupIndex struct {
	// on is whether this frame's list is grouped, which is whether the scene
	// is coloured by corridor. Every drawing path reads this rather than the
	// colour mode, so a c pressed between two frames cannot leave the drawing
	// reading an index built under the other rule.
	on bool

	// start[g] is the row the section g opens at in Scene.icaos, and
	// start[classCount] the end of the last one.
	start [classCount + 1]int

	// nearest is the row of the nearest aircraft on the field, wherever its
	// section put it. It is what an unpinned selection stands on.
	nearest int

	// rows is the frame index of every row, in the order of Scene.icaos, and
	// of is each of the frame's aircraft's section, or groupNone.
	rows []int
	of   []uint8
}

// groupOf is the section a class is listed in.
func groupOf(class source.TrackClass) uint8 {
	switch class {
	case source.TrackInbound:
		return 0
	case source.TrackOutbound:
		return 1
	case source.TrackTransit:
		fallthrough
	default:
		return classCount - 1
	}
}

// indexRows rebuilds the ICAO index syncSelection keeps, grouped or not.
func (s *Scene) indexRows(frame source.Frame) {
	s.icaos = s.icaos[:0]
	s.groups.on = s.colour == ColourCorridor
	s.groups.nearest = 0

	if s.groups.on {
		s.groupRows(frame)

		return
	}

	for _, plane := range frame.Planes {
		if s.visible(plane) {
			s.icaos = append(s.icaos, plane.ICAO)
		}
	}
}

// groupRows fills the index section by section.
//
// Each aircraft is classified once, and that one answer decides both the
// filter and the section. It takes three walks over the frame's sections
// rather than a sort, because a sort of a slice of indices has to be told
// how to compare through a closure, and stability by distance is what the
// frame's own order already gives for nothing.
func (s *Scene) groupRows(frame source.Frame) {
	groups := &s.groups
	groups.rows = groups.rows[:0]
	groups.of = groups.of[:0]
	first := -1

	for index, plane := range frame.Planes {
		group := s.groupFor(plane)
		if group != groupNone && first < 0 {
			first = index
		}

		groups.of = append(groups.of, group)
	}

	for group := range uint8(classCount) {
		groups.start[group] = len(s.icaos)

		for index, in := range groups.of {
			if in != group {
				continue
			}

			if index == first {
				groups.nearest = len(s.icaos)
			}

			s.icaos = append(s.icaos, frame.Planes[index].ICAO)
			groups.rows = append(groups.rows, index)
		}
	}

	groups.start[classCount] = len(s.icaos)
}

// groupFor is the section one aircraft is listed in, or groupNone when the
// filter is keeping it off the field. It is visible with the class worked out
// once and handed to both questions.
func (s *Scene) groupFor(plane airplane.Snapshot) uint8 {
	class := s.exposure.corridors.Classify(plane)

	if !s.passes(plane.ICAO, plane.Altitude, plane.Callsign, class) {
		return groupNone
	}

	return groupOf(class)
}

// groupAt is the section a row is in. Only asked of a row that exists.
func (g *groupIndex) groupAt(row int) int {
	group := 0

	for group < classCount-1 && row >= g.start[group+1] {
		group++
	}

	return group
}

// rowRank is a row's place in the list as its strip numbers it: from one at
// the top of the whole list, or from one at the top of its own section once
// the list is grouped.
func (s *Scene) rowRank(row int) int {
	if !s.groups.on {
		return row + 1
	}

	return row - s.groups.start[s.groups.groupAt(row)] + 1
}

// groupsDrawn is which sections get a heading. All three, so the column does
// not jump when a class empties, except while the f key is holding to one
// class: then the other two have nothing in them by construction, and their
// headings would be two lines of zero saying what the filter already says.
func (s *Scene) groupsDrawn() [classCount]bool {
	if s.filter.kind != filterClass {
		return [classCount]bool{true, true, true}
	}

	var drawn [classCount]bool

	drawn[groupOf(s.filter.class)] = true

	return drawn
}

// shareLines shares the lines a board has between its sections, headings
// first.
//
// Every drawn section has its heading, and the rest is dealt out one line at
// a time, IN then OUT then OTHER, round and round, skipping a section once it
// has a line for every aircraft it holds. Dealt that way each section gets a
// third, the remainder goes to the earlier ones, and a section that needs
// less than its third leaves the spare to be dealt to the others in the same
// order. A section the deal leaves short draws one line fewer and a +N MORE
// on the last line; that is drawGroup's to decide, not this.
//
// It is a function of three small arrays and returns a fourth, so the whole
// rule can be read off a table in the test.
func shareLines(lines int, need [classCount]int, drawn [classCount]bool) [classCount]int {
	var share [classCount]int

	for group := range classCount {
		if drawn[group] {
			lines--
		}
	}

	for lines > 0 {
		dealt := false

		for group := range classCount {
			if lines > 0 && drawn[group] && share[group] < need[group] {
				share[group]++
				lines--
				dealt = true
			}
		}

		if !dealt {
			break
		}
	}

	return share
}

// groupWindow is the part of one section that has strips.
type groupWindow struct {
	// first is the section's own row the window opens at, count how many
	// strips it draws, and hidden how many of the section it leaves off,
	// above the window and below it together.
	first  int
	count  int
	hidden int
}

// windowFor fits a section of need aircraft into lines lines, keeping the
// section's own row sel in view when it has one. A section that does not fit
// gives its last line to the +N MORE.
//
// One line counts the aircraft cut off at both ends, where the ungrouped
// board writes one at each. Three sections each with two such lines would
// spend six of a quarter column's lines on counting.
func windowFor(need, lines, sel int) groupWindow {
	count := need
	if need > lines {
		count = max(lines-1, 0)
	}

	return groupWindow{first: stripFirst(need, count, sel), count: count, hidden: need - count}
}

// drawGroups draws the grouped board under the row of field names: each
// drawn section's heading, its strips, and its +N MORE when it has one.
//
// sel is the row the selection is standing on, or notSelected. The window of
// the section holding it scrolls to keep it on screen; the other two open at
// their tops.
func (s *Scene) drawGroups(col *layout, plan stripPlan, frame source.Frame, sel int) {
	pitch := s.halfStripHeight(col)
	drawn := s.groupsDrawn()

	var need [classCount]int

	for group := range classCount {
		need[group] = s.groups.start[group+1] - s.groups.start[group]
	}

	share := shareLines((col.bottom-col.top)/pitch, need, drawn)
	top := col.top

	for group := range classCount {
		if !drawn[group] || top+pitch > col.bottom {
			continue
		}

		s.drawGroupHead(col, plan, group, need[group], top)
		top = s.drawGroup(col, plan, frame, group, share[group], sel, top+pitch)
	}
}

// drawGroup draws one section's window and returns the y under it.
func (s *Scene) drawGroup(
	col *layout, plan stripPlan, frame source.Frame, group, lines, sel, top int,
) int {
	start, end := s.groups.start[group], s.groups.start[group+1]

	local := notSelected
	if sel >= start && sel < end {
		local = sel - start
	}

	window := windowFor(end-start, lines, local)
	pitch := s.halfStripHeight(col)

	for row := window.first; row < window.first+window.count; row++ {
		plane := frame.Planes[s.groups.rows[start+row]]
		s.drawHalfStrip(col, plan, frame, plane, top, stripPen{rank: row + 1, picked: start+row == sel})
		top += pitch
	}

	// A section dealt no lines at all has only its heading, whose count
	// already says what the line would.
	if window.hidden > 0 && window.count < lines {
		s.drawMoreStrip(col, plan, top, window.hidden, moreSuffix)
		top += pitch
	}

	return top
}

// drawGroupHead writes a section's heading: a short hairline, the class's
// word in its own colour, the count, and the hairline on to the right edge.
//
// It takes a strip's height and is set in the face the field names are, so
// the column keeps one pitch from top to bottom and reads as one table with
// three parts rather than three tables. The word starts where the ranks do,
// which puts IN over the 01 that opens its section.
func (s *Scene) drawGroupHead(col *layout, plan stripPlan, group, count, top int) {
	face := s.faces.Small
	pitch := s.halfStripHeight(col)
	line := top + (pitch-col.lineHeight(face))/2
	rule := col.px(ruleHeight)
	middle := top + pitch/2
	gap := col.px(groupRuleGap)
	left := plan.left[fieldIdent]
	class := classOrder[group]

	col.dst.FillRect(image.Rect(col.left, middle, left-gap, middle+rule), s.pal.Rule)

	pen := col.tracked(face, left, line, classLabel(class), s.classColour(class)) + col.glyphWidth(face)
	pen = col.drawBytesTracked(face, pen, line, s.count(count), s.pal.Muted)

	col.dst.FillRect(image.Rect(pen+gap, middle, col.right, middle+rule), s.pal.Rule)
}
