package history_test

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/history"
)

//nolint:gochecknoglobals // read-only test fixture.
var epoch = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

// sample is a two-trail snapshot with every field set.
func sample() history.Snapshot {
	return history.Snapshot{Trails: []history.Trail{
		{
			ICAO: "484C58", Callsign: "KLM1234", Altitude: 3500, Class: 1, LostAt: epoch,
			Points: []airplane.PositionEntry{
				{Latitude: 52.3, Longitude: 4.76, Altitude: 3500}, {Latitude: 52.31, Longitude: 4.77},
			},
		},
		{
			ICAO: "4951BA", Altitude: 0, Class: 0, LostAt: epoch.Add(time.Minute),
			Points: []airplane.PositionEntry{{Latitude: -33.9, Longitude: 151.2, Altitude: 1}},
		},
	}}
}

func encode(t *testing.T, snap history.Snapshot) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := history.Encode(&buf, &snap); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	want := sample()

	got, err := history.Decode(bytes.NewReader(encode(t, want)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(got.Trails) != len(want.Trails) {
		t.Fatalf("got %d trails, want %d", len(got.Trails), len(want.Trails))
	}

	for index := range want.Trails {
		have, need := got.Trails[index], want.Trails[index]
		if have.ICAO != need.ICAO || have.Callsign != need.Callsign || have.Altitude != need.Altitude ||
			have.Class != need.Class || !have.LostAt.Equal(need.LostAt) || !slices.Equal(have.Points, need.Points) {
			t.Fatalf("trail %d: got %+v, want %+v", index, have, need)
		}
	}
}

func TestDecodeRefuses(t *testing.T) {
	t.Parallel()

	good := encode(t, sample())

	nan := sample()
	nan.Trails[0].Points[1].Latitude = math.NaN()

	for _, testCase := range []struct {
		name string
		data []byte
		want error
	}{
		{name: "empty", data: nil, want: history.ErrMagic},
		{name: "another file", data: []byte("name value\n"), want: history.ErrMagic},
		{name: "magic only", data: good[:7], want: history.ErrTruncated},
		{name: "a later version", data: append([]byte("uScopeH\x02"), good[8:]...), want: history.ErrVersion},
		{name: "header only", data: good[:8], want: history.ErrTruncated},
		{name: "cut short", data: good[:len(good)-5], want: history.ErrTruncated},
		{name: "a length cut short", data: []byte("uScopeH\x01\xfe\x01"), want: history.ErrTruncated},
		{name: "a length too wide", data: []byte("uScopeH\x01\xf0\x01"), want: history.ErrCorrupt},
		{name: "a hostile length", data: []byte("uScopeH\x01\xfc\x40\x00\x00\x00"), want: history.ErrTruncated},
		{name: "whole but not gob", data: []byte("uScopeH\x01\x03abc"), want: history.ErrCorrupt},
		{name: "a fix off the earth", data: encode(t, nan), want: history.ErrCorrupt},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, err := history.Decode(bytes.NewReader(testCase.data)); !errors.Is(err, testCase.want) {
				t.Fatalf("got %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestDecodeRefusesBadTrails(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		spoil func(*history.Trail)
	}{
		{name: "no ICAO", spoil: func(tr *history.Trail) { tr.ICAO = "" }},
		{name: "a long ICAO", spoil: func(tr *history.Trail) { tr.ICAO = strings.Repeat("A", 17) }},
		{name: "a control byte in the callsign", spoil: func(tr *history.Trail) { tr.Callsign = "KLM\x1b" }},
		{name: "a byte past ASCII", spoil: func(tr *history.Trail) { tr.Callsign = "KLM\x7f" }},
		{name: "an infinite altitude", spoil: func(tr *history.Trail) { tr.Altitude = math.Inf(1) }},
		{name: "a longitude past 180", spoil: func(tr *history.Trail) { tr.Points[0].Longitude = 181 }},
		{name: "a NaN fix altitude", spoil: func(tr *history.Trail) { tr.Points[0].Altitude = math.NaN() }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			snap := sample()
			testCase.spoil(&snap.Trails[0])

			if _, err := history.Decode(bytes.NewReader(encode(t, snap))); !errors.Is(err, history.ErrCorrupt) {
				t.Fatalf("got %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestDecodeRefusesTooLarge(t *testing.T) {
	t.Parallel()

	_, err := history.Decode(io.LimitReader(zeros{}, history.MaxFileSize+1))
	if !errors.Is(err, history.ErrTooLarge) {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
}

// zeros reads as an endless run of zero bytes.
type zeros struct{}

func (zeros) Read(dst []byte) (int, error) {
	clear(dst)

	return len(dst), nil
}

var errRead = errors.New("scripted read failure")

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errRead }

func TestDecodeReportsReadFailure(t *testing.T) {
	t.Parallel()

	if _, err := history.Decode(failingReader{}); !errors.Is(err, errRead) {
		t.Fatalf("got %v, want the read error", err)
	}
}

func TestEmptySnapshotRoundTrips(t *testing.T) {
	t.Parallel()

	got, err := history.Decode(bytes.NewReader(encode(t, history.Snapshot{})))
	if err != nil || len(got.Trails) != 0 {
		t.Fatalf("got %+v, %v; want nothing and no error", got, err)
	}
}

func TestWriterAndLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "history")

	if _, err := history.Load(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load of a missing file: got %v, want os.ErrNotExist", err)
	}

	var warn bytes.Buffer

	snap := sample()
	if !history.NewWriter(path, &warn).Write(&snap) {
		t.Fatalf("Write failed: %s", warn.String())
	}

	got, err := history.Load(path)
	if err != nil || len(got.Trails) != len(snap.Trails) {
		t.Fatalf("Load: got %d trails, %v", len(got.Trails), err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the directory holds %d entries, %v; want the file alone", len(entries), err)
	}
}

func TestWriterReportsOncePerDistinctError(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "missing", "history")

	var warn bytes.Buffer

	writer := history.NewWriter(dir, &warn)
	snap := sample()

	for range 3 {
		if writer.Write(&snap) {
			t.Fatal("Write into a missing directory succeeded")
		}
	}

	if lines := strings.Count(warn.String(), "\n"); lines != 1 {
		t.Fatalf("got %d warning lines, want 1:\n%s", lines, warn.String())
	}
}

func FuzzDecode(f *testing.F) {
	var buf bytes.Buffer

	snap := sample()
	_ = history.Encode(&buf, &snap)

	f.Add(buf.Bytes())
	f.Add([]byte("uScopeH\x01"))
	f.Add([]byte("uScopeH\x01\xfc\x40\x00\x00\x00"))

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := history.Decode(bytes.NewReader(data))
		if err != nil {
			return
		}

		var again bytes.Buffer
		if err := history.Encode(&again, &got); err != nil {
			t.Fatalf("a decoded snapshot does not encode: %v", err)
		}
	})
}
