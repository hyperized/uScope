package radar

import (
	"image"
	"time"
)

// keyBarHold is how long a key press in a bare view keeps the key bar on
// screen. Four seconds is long enough to read a row of caps and find the one
// you wanted, and short enough that the bar is gone before it gets in the way
// of watching.
const keyBarHold = 4 * time.Second

// bareCaps is the key legend a bare view reveals.
//
// It is keyCaps with the keys that do nothing in a bare view taken out and
// the one only a bare view binds put in. S LIGHTS and U TUBES stay, because
// the lights and the tubes are drawn in every view. U sits after M here rather
// than on a tail of its own as in the full views, because the bare rows have
// no select arrows, W or E to pay for. The select arrows go because the
// selection has nothing to show on a bare field: no ring, no card, and no
// strip marked on the board. W goes because toggleWide refuses there. H comes
// in, after V, because it is the bare views' own switch for the bar across the
// top. A and M stay and read the bare pair of toggles, which capEngaged gets
// from airportsDrawn and shoreDrawn.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var bareCaps = [...]keyCap{
	{key: "+/-", label: "RANGE"},
	{key: "R", label: "AUTO", state: capAuto},
	{key: "T", label: trailBarLabel, state: capTrails},
	{key: "A", label: "AIRPORTS", state: capAirports},
	{key: "M", label: "SHORE", state: capShore},
	{key: "U", label: tubesBarLabel, state: capCorridors},
	{key: "S", label: "LIGHTS", state: capLights},
	{key: "C", label: "COLOUR", state: capColour},
	{key: "F", label: filterBarLabel, state: capFilter},
	{key: "L", label: "THEME", state: capTheme},
	{key: "K", label: lookBarLabel, state: capLook},
	{key: "V", label: "VIEW"},
	{key: "H", label: "BAR", state: capBar},
}

// bare3DCaps is view3DCaps less E, which toggleEnvelope refuses in the bare
// 3D view, and with Z STARS, which only fits in the room E leaves. See the
// note on view3DCaps.
//
// J BOARD came in after Z. At the row's widest, with the bias-tee cap and
// every cycling cap on its longest word (CORRIDOR on the C cap), it would have
// left the pen at x=1273, past the 1264 margin. The arrows and the brackets
// share one CAMERA cap here rather than having a TURN and a TILT box each,
// which saves 31 pixels and leaves the pen at x=1242. The full 3D bar keeps
// the two boxes: it has no J to pay for.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var bare3DCaps = [...]keyCap{
	{key: "O", label: "ORBIT", state: capOrbit},
	{key: "Z", label: "STARS", state: capStars},
	{key: "J", label: "BOARD", state: capBoard},
	{key: "←→[ ]", label: "CAMERA"},
}

// noteKey starts or restarts the key bar's hold after a press.
//
// It runs after the key has been handled, so v into a bare view shows the
// bar for the view it landed in, and v out of one cancels the hold rather
// than leaving it to surface if v comes round again inside four seconds. The
// time is the render clock the last frame was drawn at, the one the orbit
// keys read, so a replay or a test sees the same reveal on the same frames.
func (s *Scene) noteKey() {
	s.keyPressed = s.bare()
	s.keyPressAt = s.elapsed
}

// keyBarRevealed reports whether the frame at elapsed is inside the hold.
//
// A frame earlier than the press is outside it. The render clock only goes
// backwards when a caller starts it again, and a hold carried over from the
// old count would last however long the gap happened to be.
func (s *Scene) keyBarRevealed(elapsed time.Duration) bool {
	if !s.keyPressed || !s.bare() {
		return false
	}

	since := elapsed - s.keyPressAt

	return since >= 0 && since < keyBarHold
}

// drawRevealedKeyBar paints the bare views' key bar over the bottom of the
// picture while the hold lasts, and does nothing otherwise.
//
// It is painted over the picture rather than taken off the scope box the way
// the top bar is. The top bar stays until h puts it away, so it is worth
// moving the horizon for. A bar that is gone again in four seconds would make
// the whole picture jump twice for every press.
//
// The caps sit where the full views put theirs, on a band of the field colour
// that runs the full width and down to the bottom edge, so they read the same
// over sky, water and trails as they do over an empty field. The rule across
// the band's top is the edge the top bar has under it.
func (s *Scene) drawRevealedKeyBar(lay *layout, elapsed time.Duration) {
	if !s.keyBarRevealed(elapsed) {
		return
	}

	reserved := s.keyBarHeight(lay)
	if reserved == 0 {
		return
	}

	bounds := lay.dst.Bounds()
	top := lay.bottom - (reserved - lay.px(blockGap))
	edge := top - (bounds.Max.Y - lay.bottom)

	lay.dst.FillRect(image.Rect(bounds.Min.X, edge, bounds.Max.X, bounds.Max.Y), s.pal.Field)
	lay.dst.FillRect(image.Rect(bounds.Min.X, edge, bounds.Max.X, edge+lay.px(ruleHeight)), s.pal.Rule)

	s.drawCapRow(lay, top, bareCaps[:], nil, bare3DCaps[:])
}
