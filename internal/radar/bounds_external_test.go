package radar_test

import (
	"errors"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
)

// TestCheckBounds covers the checks the flag parser calls with a number the
// flag package already read: both edges of every range, the first value past
// each edge, NaN and the infinities.
func TestCheckBounds(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "zoom the fit", err: radar.CheckZoom(0)},
		{name: "zoom tightest", err: radar.CheckZoom(radar.MaxZoom)},
		{name: "zoom widest", err: radar.CheckZoom(radar.MinZoom)},
		{name: "zoom too tight", err: radar.CheckZoom(radar.MaxZoom + 1), wantErr: radar.ErrZoom},
		{name: "zoom too wide", err: radar.CheckZoom(radar.MinZoom - 1), wantErr: radar.ErrZoom},
		{name: "tilt at the floor", err: radar.CheckTilt(radar.MinTilt)},
		{name: "tilt at the ceiling", err: radar.CheckTilt(radar.MaxTilt)},
		{name: "tilt with a fraction", err: radar.CheckTilt(22.5)},
		{name: "tilt under the floor", err: radar.CheckTilt(9.9), wantErr: radar.ErrTilt},
		{name: "tilt over the ceiling", err: radar.CheckTilt(80.1), wantErr: radar.ErrTilt},
		{name: "tilt NaN", err: radar.CheckTilt(math.NaN()), wantErr: radar.ErrTilt},
		{name: "exaggerate at the floor", err: radar.CheckExaggerate(radar.MinExaggerate)},
		{name: "exaggerate at the ceiling", err: radar.CheckExaggerate(radar.MaxExaggerate)},
		{name: "exaggerate under", err: radar.CheckExaggerate(0.5), wantErr: radar.ErrExaggerate},
		{name: "exaggerate infinite", err: radar.CheckExaggerate(math.Inf(1)), wantErr: radar.ErrExaggerate},
		{name: "azimuth north", err: radar.CheckAzimuth(radar.MinAzimuth)},
		{name: "azimuth just short of north", err: radar.CheckAzimuth(359.9)},
		{name: "azimuth at the open end", err: radar.CheckAzimuth(radar.MaxAzimuth), wantErr: radar.ErrAzimuth},
		{name: "azimuth negative", err: radar.CheckAzimuth(-1), wantErr: radar.ErrAzimuth},
		{name: "azimuth NaN", err: radar.CheckAzimuth(math.NaN()), wantErr: radar.ErrAzimuth},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if !errors.Is(testCase.err, testCase.wantErr) {
				t.Errorf("error = %v, want %v", testCase.err, testCase.wantErr)
			}
		})
	}
}
