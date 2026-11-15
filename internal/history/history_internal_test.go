package history

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
)

var errScripted = errors.New("scripted failure")

// fakeFile is a temporary file whose steps fail on request.
type fakeFile struct {
	failWrite, failSync, failClose bool
	data                           bytes.Buffer
}

func (f *fakeFile) Write(data []byte) (int, error) {
	if f.failWrite {
		return 0, errScripted
	}

	return f.data.Write(data) //nolint:wrapcheck // stands in for *os.File, which does not wrap either.
}

func (f *fakeFile) Sync() error {
	if f.failSync {
		return errScripted
	}

	return nil
}

func (f *fakeFile) Close() error {
	if f.failClose {
		return errScripted
	}

	return nil
}

func (*fakeFile) Name() string { return "/var/lib/uscope/.history.123" }

// fakeDisk is a writer's three filesystem seams, recording what happened.
type fakeDisk struct {
	file       *fakeFile
	failCreate bool
	failRename bool
	renamed    bool
	removed    []string
}

// Write runs one write through the disk's seams.
func (d *fakeDisk) Write(warn *bytes.Buffer, snap *Snapshot) bool {
	return d.writer(warn).Write(snap)
}

func (d *fakeDisk) writer(warn *bytes.Buffer) *Writer {
	writer := NewWriter("/var/lib/uscope/history", warn)
	writer.createTemp = func(string, string) (tempFile, error) {
		if d.failCreate {
			return nil, errScripted
		}

		return d.file, nil
	}
	writer.rename = func(string, string) error {
		if d.failRename {
			return errScripted
		}

		d.renamed = true

		return nil
	}
	writer.remove = func(name string) error {
		d.removed = append(d.removed, name)

		return nil
	}

	return writer
}

// bigSnapshot encodes to more than the write buffer, so a failing file is
// found by the encoder rather than by the final flush.
func bigSnapshot() *Snapshot {
	points := make([]airplane.PositionEntry, bufferSize/8)
	for index := range points {
		points[index] = airplane.PositionEntry{Latitude: 52.123456789, Longitude: 4.987654321, Altitude: 35000.5}
	}

	return &Snapshot{Trails: []Trail{{ICAO: "484C58", Points: points}}}
}

func TestWriterSteps(t *testing.T) {
	t.Parallel()

	small := &Snapshot{Trails: []Trail{{ICAO: "484C58", Points: []airplane.PositionEntry{{Latitude: 1}}}}}

	for _, testCase := range []struct {
		name       string
		disk       fakeDisk
		snap       *Snapshot
		want       string
		wantRemove bool
	}{
		{name: "create", disk: fakeDisk{failCreate: true}, snap: small, want: "creating a temporary file"},
		{name: "flush", disk: fakeDisk{file: &fakeFile{failWrite: true}}, snap: small, want: "writing",
			wantRemove: true},
		{
			name: "encode", disk: fakeDisk{file: &fakeFile{failWrite: true}}, snap: bigSnapshot(), want: "encoding",
			wantRemove: true,
		},
		{name: "sync", disk: fakeDisk{file: &fakeFile{failSync: true}}, snap: small, want: "syncing",
			wantRemove: true},
		{name: "close", disk: fakeDisk{file: &fakeFile{failClose: true}}, snap: small, want: "closing",
			wantRemove: true},
		{
			name: "rename", disk: fakeDisk{file: &fakeFile{}, failRename: true}, snap: small, want: "replacing",
			wantRemove: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var warn bytes.Buffer

			disk := testCase.disk
			if disk.Write(&warn, testCase.snap) {
				t.Fatal("Write reported success")
			}

			if !strings.Contains(warn.String(), testCase.want) {
				t.Fatalf("warning %q does not mention %q", warn.String(), testCase.want)
			}

			if (len(disk.removed) == 1) != testCase.wantRemove {
				t.Fatalf("removed %v, want a removal: %t", disk.removed, testCase.wantRemove)
			}
		})
	}
}

func TestWriterReportsAgainAfterSuccess(t *testing.T) {
	t.Parallel()

	var warn bytes.Buffer

	disk := &fakeDisk{file: &fakeFile{}, failRename: true}
	writer := disk.writer(&warn)
	snap := &Snapshot{}

	writer.Write(snap)
	writer.Write(snap)

	disk.failRename = false
	if !writer.Write(snap) || !disk.renamed {
		t.Fatal("the write after the failure did not land")
	}

	disk.failRename = true

	writer.Write(snap)

	if lines := strings.Count(warn.String(), "\n"); lines != 2 {
		t.Fatalf("got %d warning lines, want 2:\n%s", lines, warn.String())
	}
}

// failingWriter refuses every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errScripted }

func TestEncodeReportsHeaderFailure(t *testing.T) {
	t.Parallel()

	if err := Encode(failingWriter{}, &Snapshot{}); !errors.Is(err, errScripted) {
		t.Fatalf("got %v, want the write error", err)
	}
}

func TestGobUint(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		data      []byte
		want      uint64
		wantWidth int
		wantErr   error
	}{
		{name: "one byte", data: []byte{0x7f}, want: 0x7f, wantWidth: 1},
		{name: "two bytes", data: []byte{0xff, 0x80}, want: 0x80, wantWidth: 2},
		{name: "eight bytes", data: []byte{0xf8, 1, 0, 0, 0, 0, 0, 0, 0}, want: 1 << 56, wantWidth: 9},
		{name: "nine bytes", data: []byte{0xf7}, wantErr: ErrCorrupt},
		{name: "cut short", data: []byte{0xfd, 1}, wantErr: ErrTruncated},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, width, err := gobUint(testCase.data)
			if !errors.Is(err, testCase.wantErr) || got != testCase.want || width != testCase.wantWidth {
				t.Fatalf("got %d, %d, %v; want %d, %d, %v",
					got, width, err, testCase.want, testCase.wantWidth, testCase.wantErr)
			}
		})
	}
}
