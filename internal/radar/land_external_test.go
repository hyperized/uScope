package radar_test

import (
	"bytes"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// landTint is the land's colour on the palette every test in this file draws
// with, worked out again from OK and the fade rather than read off the palette.
//
//nolint:gochecknoglobals // color.RGBA is a struct, so this cannot be const.
var landTint = fadeInto(theme.Night.Field, theme.Night.OK, waterFadeAlpha)

// landAroundReceiver is a square of land five degrees either side of the
// scene's receiver, wider than anything the scope shows at the range these
// tests draw at, so it crosses every corner of the scope's square.
func landAroundReceiver(tb testing.TB) *shore.Set {
	tb.Helper()

	const halfSpanDeg = 5.0

	ring := shore.Polyline{
		{Lat: receiverLat - halfSpanDeg, Lon: receiverLon - halfSpanDeg},
		{Lat: receiverLat - halfSpanDeg, Lon: receiverLon + halfSpanDeg},
		{Lat: receiverLat + halfSpanDeg, Lon: receiverLon + halfSpanDeg},
		{Lat: receiverLat + halfSpanDeg, Lon: receiverLon - halfSpanDeg},
	}

	var lines, land bytes.Buffer

	if err := shore.Encode(&lines, nil); err != nil {
		tb.Fatalf("shore.Encode: %v", err)
	}

	if err := shore.EncodeLand(&land, []shore.Polyline{ring}); err != nil {
		tb.Fatalf("shore.EncodeLand: %v", err)
	}

	set, err := shore.DecodeWithLand(&lines, &land)
	if err != nil {
		tb.Fatalf("shore.DecodeWithLand: %v", err)
	}

	return set
}

// TestLandStaysInsideTheRing draws the full scope over land that covers all
// of it and checks the land tint is there, is nowhere in the column, and is
// nowhere outside the circle the scope's square holds. The land is filled
// against that square, so the corners between it and the ring are where a
// tint would leak if nothing painted them back.
func TestLandStaysInsideTheRing(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithShore(landAroundReceiver(t)))
	scene.Apply(radar.Settings{RangeNm: waterFleetRangeNm})
	scene.Draw(canv, 0)

	if got := countColour(canv, scopeBox, landTint); got == 0 {
		t.Fatal("the scope drew no land at all")
	}

	if got := countColour(canv, columnBox, landTint); got != 0 {
		t.Errorf("the column carries %d land pixels, want none", got)
	}

	centerX := float64(scopeBox.Min.X+scopeBox.Max.X-1) / 2
	centerY := float64(scopeBox.Min.Y+scopeBox.Max.Y-1) / 2
	reach := float64(scopeBox.Dx())/2 + 1

	for row := scopeBox.Min.Y; row < scopeBox.Max.Y; row++ {
		for col := scopeBox.Min.X; col < scopeBox.Max.X; col++ {
			if canv.Image().RGBAAt(col, row) != landTint {
				continue
			}

			if math.Hypot(float64(col)-centerX, float64(row)-centerY) > reach {
				t.Fatalf("(%d, %d) is land outside the scope's circle", col, row)
			}
		}
	}
}

// TestMinimalLandCoversTheCanvas is the bare flat view over the same land with
// the shore asked for: no ring, so the land runs into all four corners.
func TestMinimalLandCoversTheCanvas(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithShore(landAroundReceiver(t)))
	scene.Apply(radar.Settings{View: radar.ViewMinimal, RangeNm: waterFleetRangeNm, MinimalShore: true})
	scene.Draw(canv, pastKeyBarHold)

	assertCornersAre(t, canv, landTint)
}

// assertCornersAre fails for every corner pixel of the canvas that is not col.
func assertCornersAre(tb testing.TB, canv *canvas.Canvas, col color.RGBA) {
	tb.Helper()

	bounds := canv.Bounds()

	for _, corner := range [][2]int{
		{bounds.Min.X, bounds.Min.Y}, {bounds.Max.X - 1, bounds.Min.Y},
		{bounds.Min.X, bounds.Max.Y - 1}, {bounds.Max.X - 1, bounds.Max.Y - 1},
	} {
		if got := canv.Image().RGBAAt(corner[0], corner[1]); got != col {
			tb.Errorf("the corner %v is %v, want %v", corner, got, col)
		}
	}
}
