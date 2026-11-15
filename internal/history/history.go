package history

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
)

// MaxFileSize is the largest file Decode reads. The ring's own caps, two
// thousand trails and a million fixes, encode to about 31 MB at the very
// worst (every trail at full length and every coordinate at full float64
// precision), so 48 MiB leaves room for the headers and refuses anything that
// could only have come from somewhere else.
const MaxFileSize = 48 << 20

// Version is the format version this build writes and the only one it reads.
const Version = 1

// The limits Decode checks every trail against. A Mode S address is six hex
// digits and a callsign eight characters; the room above both is for a
// source that spells either a little differently, not for a payload.
const (
	maxICAO      = 16
	maxCallsign  = 16
	maxLatitude  = 90.0
	maxLongitude = 180.0
)

// magic opens every history file, followed by the version byte.
const magic = "uScopeH"

// headerSize is the magic and the version byte.
const headerSize = len(magic) + 1

// How gob writes an unsigned integer: a byte under gobSmall is the value
// itself, and any other byte is the negated count, at most gobMaxUintBytes,
// of the big-endian bytes that follow.
const (
	gobSmall        = 0x80
	gobMaxUintBytes = 8
	byteBits        = 8
	byteValues      = 256
)

// Errors Decode returns. Each is wrapped with what it found.
var (
	// ErrMagic is a file that does not start with the history magic.
	ErrMagic = errors.New("history: not a history file")

	// ErrVersion is a history file of a format version this build does not
	// read.
	ErrVersion = errors.New("history: unsupported version")

	// ErrTooLarge is a file over MaxFileSize.
	ErrTooLarge = errors.New("history: file too large")

	// ErrTruncated is a file that ends part way through a message.
	ErrTruncated = errors.New("history: file truncated")

	// ErrCorrupt is a file whose messages are whole but do not decode to a
	// snapshot, or decode to one with a value no track can have.
	ErrCorrupt = errors.New("history: file corrupt")
)

// Trail is one lost track as the file holds it.
//
// Points is shared, not copied, in both directions: a Snapshot built from the
// ring aliases the ring's fixes, which are never written after a trail goes
// in, and a decoded one hands its slices to the ring.
type Trail struct {
	ICAO     string
	Callsign string
	Altitude float64
	Points   []airplane.PositionEntry
	LostAt   time.Time
	Class    uint8
}

// Snapshot is the whole ring, oldest trail first.
type Snapshot struct {
	Trails []Trail
}

// Encode writes the header and the snapshot to dst.
func Encode(dst io.Writer, snap *Snapshot) error {
	if _, err := io.WriteString(dst, magic+string(rune(Version))); err != nil {
		return fmt.Errorf("history: writing the header: %w", err)
	}

	if err := gob.NewEncoder(dst).Encode(snap); err != nil {
		return fmt.Errorf("history: encoding: %w", err)
	}

	return nil
}

// Decode reads one snapshot from src, refusing anything over MaxFileSize and
// anything that is not a whole, sane history file.
func Decode(src io.Reader) (Snapshot, error) {
	data, err := io.ReadAll(io.LimitReader(src, MaxFileSize+1))
	if err != nil {
		return Snapshot{}, fmt.Errorf("history: reading: %w", err)
	}

	if len(data) > MaxFileSize {
		return Snapshot{}, fmt.Errorf("%w: over %d bytes", ErrTooLarge, MaxFileSize)
	}

	body, err := header(data)
	if err != nil {
		return Snapshot{}, err
	}

	if err := checkMessages(body); err != nil {
		return Snapshot{}, err
	}

	var snap Snapshot
	if err := gob.NewDecoder(bytes.NewReader(body)).Decode(&snap); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}

	if err := snap.check(); err != nil {
		return Snapshot{}, err
	}

	return snap, nil
}

// Load decodes the file at path. A missing file comes back as an error that
// errors.Is matches against os.ErrNotExist, which a first run expects.
func Load(path string) (Snapshot, error) {
	file, err := os.Open(path) //nolint:gosec // the path is the operator's own flag.
	if err != nil {
		return Snapshot{}, fmt.Errorf("history: %w", err)
	}

	defer func() { _ = file.Close() }()

	return Decode(file)
}

// header checks the magic and the version and returns what follows them.
func header(data []byte) ([]byte, error) {
	if len(data) < len(magic) || string(data[:len(magic)]) != magic {
		return nil, ErrMagic
	}

	if len(data) < headerSize {
		return nil, fmt.Errorf("%w: no version byte", ErrTruncated)
	}

	if version := data[len(magic)]; version != Version {
		return nil, fmt.Errorf("%w: %d, this build reads %d", ErrVersion, version, Version)
	}

	return data[headerSize:], nil
}

// checkMessages walks the gob stream's message frames and refuses one whose
// length prefix claims more bytes than the file has left.
//
// That is the whole defence against a hostile length: gob checks slice and
// string lengths against the message it is decoding, but it sizes the
// message buffer itself from the prefix before reading it. With every prefix
// proved to fit, nothing gob allocates can be larger than the file.
func checkMessages(body []byte) error {
	if len(body) == 0 {
		return fmt.Errorf("%w: no snapshot after the header", ErrTruncated)
	}

	for len(body) > 0 {
		length, width, err := gobUint(body)
		if err != nil {
			return err
		}

		body = body[width:]

		if length > uint64(len(body)) {
			return fmt.Errorf("%w: a message claims %d bytes and %d are left", ErrTruncated, length, len(body))
		}

		body = body[length:]
	}

	return nil
}

// gobUint reads one gob unsigned integer: a byte under 0x80 is the value, and
// anything else is the negated count of big-endian bytes that follow.
func gobUint(data []byte) (uint64, int, error) {
	first := data[0]
	if first < gobSmall {
		return uint64(first), 1, nil
	}

	count := byteValues - int(first)
	if count > gobMaxUintBytes {
		return 0, 0, fmt.Errorf("%w: a length of %d bytes", ErrCorrupt, count)
	}

	if len(data) < 1+count {
		return 0, 0, fmt.Errorf("%w: a length cut short", ErrTruncated)
	}

	var value uint64
	for _, digit := range data[1 : 1+count] {
		value = value<<byteBits | uint64(digit)
	}

	return value, 1 + count, nil
}

// check refuses a snapshot holding a value no track can have. One bad trail
// condemns the file: it means the bytes came from somewhere other than this
// program, and the trails beside it are no more trustworthy.
func (s *Snapshot) check() error {
	for index := range s.Trails {
		if err := s.Trails[index].check(); err != nil {
			return fmt.Errorf("%w: trail %d: %w", ErrCorrupt, index, err)
		}
	}

	return nil
}

// errField is what check wraps when one field is out of bounds.
var errField = errors.New("value out of range")

// check reports the first field of the trail that is out of bounds.
func (t *Trail) check() error {
	switch {
	case !printable(t.ICAO, 1, maxICAO):
		return fmt.Errorf("%w: ICAO %q", errField, t.ICAO)
	case !printable(t.Callsign, 0, maxCallsign):
		return fmt.Errorf("%w: callsign %q", errField, t.Callsign)
	case !finite(t.Altitude):
		return fmt.Errorf("%w: altitude", errField)
	}

	for index := range t.Points {
		if !sanePoint(&t.Points[index]) {
			return fmt.Errorf("%w: fix %d", errField, index)
		}
	}

	return nil
}

// printable reports whether text is between least and most bytes of printable
// ASCII. The radar draws both strings through a bitmap face, and a control
// byte has no business in either.
func printable(text string, least, most int) bool {
	if len(text) < least || len(text) > most {
		return false
	}

	for index := range len(text) {
		if text[index] < ' ' || text[index] > '~' {
			return false
		}
	}

	return true
}

// sanePoint reports whether a fix is somewhere on the earth at a finite
// altitude.
func sanePoint(point *airplane.PositionEntry) bool {
	return math.Abs(point.Latitude) <= maxLatitude &&
		math.Abs(point.Longitude) <= maxLongitude &&
		finite(point.Altitude)
}

// finite reports whether a value is neither NaN nor an infinity.
func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
