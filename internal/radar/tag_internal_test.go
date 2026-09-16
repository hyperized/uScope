package radar

import (
	"image"
	"image/color"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The canvas the tag is drawn on and where the contact sits in it. The tag
// stacks upward and to the right of the contact, so the contact goes low and
// left and the canvas is sized to hold the whole block above it.
const (
	tagCanvasWidth  = 240
	tagCanvasHeight = 160
	tagContactX     = 20
	tagContactY     = 140

	// The aircraft the picture is of: climbing, so the middle line carries a
	// trend arrow, and far enough off the receiver for a real bearing.
	tagAltitudeFt   = 2400.0
	tagRateFpm      = 1800.0
	tagVelocityKt   = 190.0
	tagHeadingDeg   = 41.0
	tagPlaneLat     = 52.45
	tagPlaneLon     = 4.9
	tagReceiverLat  = 52.3105
	tagReceiverLon  = 4.7683
	tagTestCallsign = "KLM123"
)

// tagPlane is the aircraft every case below draws.
func tagPlane() airplane.Snapshot {
	return airplane.Snapshot{
		ICAO:      icaoFirst,
		Callsign:  tagTestCallsign,
		Altitude:  tagAltitudeFt,
		VertRate:  tagRateFpm,
		Velocity:  tagVelocityKt,
		Heading:   tagHeadingDeg,
		Latitude:  tagPlaneLat,
		Longitude: tagPlaneLon,
	}
}

// drawTagOn draws one selection, ring, leader and tag, onto a fresh canvas.
func drawTagOn(tb testing.TB, plane airplane.Snapshot) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(tagCanvasWidth, tagCanvasHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)

	scene := &Scene{faces: layerTestFaces(tb), pal: theme.Night}
	lay := &layout{dst: canv, labels: true}

	scene.drawSelection(lay, tagContactX, tagContactY, plane,
		source.Receiver{Latitude: tagReceiverLat, Longitude: tagReceiverLon})

	return canv
}

// tagLineBand is the strip of canvas one line of the tag is set in: from the
// tag's own left edge to the right margin, and one tagLead tall.
//
// The bands are measured off tagLead rather than off the body face's own
// height, which is what makes this a check on the pitch as well as on the
// colours. At the face's sixteen the first line's glyphs would reach four
// pixels into the second band and the case below would fail on the stray ink.
func tagLineBand(line int) image.Rectangle {
	left := tagContactX + leaderRun + labelTracking
	top := tagContactY - leaderRun - tagLines*tagLead + line*tagLead

	return image.Rect(left, top, tagCanvasWidth, top+tagLead)
}

// tagLineInks is the colour each line of the tag is set in, in the order the
// lines are read down the block.
//
//nolint:gochecknoglobals // a colour row is data, and color.RGBA cannot be const.
var tagLineInks = [tagLines]color.RGBA{theme.Night.Accent, theme.Night.Ink, theme.Night.Muted}

// TestTagLineColours pins the three-colour grammar of the scope's data block.
//
// The block used to be one colour, on the argument that the tag is the
// selection and the selection is one thing. That spent the accent on six
// figures rather than on the pick. The callsign wears it now, the level and the
// speed take the reading ink because that is what they are, and the range and
// the bearing go muted as the quietest pair of the six, so the eye lands on the
// middle line first.
//
// Each case asserts both halves: the colour its own line is set in is there,
// and the other two are not. A block that had slipped a line would still paint
// all three colours somewhere inside it.
func TestTagLineColours(t *testing.T) {
	t.Parallel()

	canv := drawTagOn(t, tagPlane())

	for line, ink := range tagLineInks {
		t.Run(tagLineNames[line], func(t *testing.T) {
			t.Parallel()

			band := tagLineBand(line)

			if got := colourCount(canv, band, ink); got == 0 {
				t.Errorf("the %s line painted no %v pixels, want it set in that colour", tagLineNames[line], ink)
			}

			for other, wrong := range tagLineInks {
				if other == line {
					continue
				}

				if got := colourCount(canv, band, wrong); got != 0 {
					t.Errorf("the %s line painted %d %v pixels, which belong to the %s line",
						tagLineNames[line], got, wrong, tagLineNames[other])
				}
			}
		})
	}
}

// tagLineNames is what each line of the block is, used to name the cases above.
//
//nolint:gochecknoglobals // a name row is data, and an array cannot be const.
var tagLineNames = [tagLines]string{"callsign", "level and speed", "range and bearing"}

// TestTagLeaderStartsOnTheRing checks where the leader line begins.
//
// It used to start at half the ring's radius, which drew its first few pixels
// across the inside of the ring it was meant to leave. The foot is now on the
// ring itself, which is what the data block in the study shows and what a
// controller's own leader has always done.
func TestTagLeaderStartsOnTheRing(t *testing.T) {
	t.Parallel()

	canv := drawTagOn(t, tagPlane())

	// A square just outside the ring on the leader's own diagonal. Nothing but
	// the leader reaches it: the ring is inside it and the tag is up and to the
	// right of it.
	const reach = 3

	foot := image.Rect(
		tagContactX+leaderFoot, tagContactY-leaderFoot-reach,
		tagContactX+leaderFoot+reach, tagContactY-leaderFoot,
	)

	if got := colourCount(canv, foot, theme.Night.Accent); got == 0 {
		t.Error("nothing was painted where the leader leaves the ring, want the accent")
	}
}

// TestTagWithoutAVelocity checks the middle line's other half: an aircraft
// whose velocity message has not arrived writes dashes where the ground speed
// goes, in the same reading ink as the figure it stands in for.
//
// uAirwaves marks the undecoded state with -1, so the line is about the sign
// rather than about an equality against a sentinel.
func TestTagWithoutAVelocity(t *testing.T) {
	t.Parallel()

	const undecoded = -1.0

	plane := tagPlane()
	plane.Velocity = undecoded

	canv := drawTagOn(t, plane)

	band := tagLineBand(1)
	if got := colourCount(canv, band, theme.Night.Ink); got == 0 {
		t.Error("an aircraft with no velocity painted nothing on the middle line, want the dashes")
	}

	if got := colourCount(canv, band, theme.Night.Muted); got != 0 {
		t.Errorf("the dashes painted %d muted pixels, want them in the line's own reading ink", got)
	}
}
