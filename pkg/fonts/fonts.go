// Package fonts carries the console fonts uScope sets type in.
//
// The four faces are Debian console-setup's Uni3 builds of Terminus Font,
// gzipped exactly as that package ships them and compiled into the binary, so
// uScope stays one static file with nothing to install beside it. See
// README.md in this directory for the provenance and the licence.
//
// Each accessor gunzips and parses its face the first time it is called and
// hands back the same *psf.Font afterwards, including the same error if the
// parse failed. A font costs a few kilobytes of glyph bitmaps and a map, and
// a program that only ever draws in one of them should not pay for the other
// three, which is why there is no eager init here.
//
// Every accessor is safe to call from any goroutine, and the *psf.Font it
// returns is read-only, so the caller may share it freely.
package fonts

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"sync"

	"github.com/hyperized/uScope/pkg/psf"
)

// The font files, byte for byte as console-setup ships them. The names are
// kept because the SIL Open Font License reserves "Terminus Font": renaming
// the files would be the first step towards shipping a modified font under
// the reserved name.
//
//nolint:gochecknoglobals // go:embed needs package-level variables.
var (
	//go:embed Uni3-Terminus12x6.psf.gz
	smallGZ []byte

	//go:embed Uni3-Terminus16.psf.gz
	bodyGZ []byte

	//go:embed Uni3-TerminusBold16.psf.gz
	bodyBoldGZ []byte

	//go:embed Uni3-TerminusBold32x16.psf.gz
	largeGZ []byte

	//go:embed OFL-Terminus.txt
	licence string
)

// One cache per face. They are pointers so the sync.Once inside is never
// copied.
//
//nolint:gochecknoglobals // the cache has to outlive the call that fills it.
var (
	small    = &face{data: smallGZ}
	body     = &face{data: bodyGZ}
	bodyBold = &face{data: bodyBoldGZ}
	large    = &face{data: largeGZ}
)

// face is one embedded font and the parse that happens at most once.
type face struct {
	once sync.Once
	data []byte
	font *psf.Font
	err  error
}

// Small returns Terminus 6x12, the face for labels and unit suffixes.
func Small() (*psf.Font, error) { return small.load() }

// Body returns Terminus 8x16, the face for ordinary text.
func Body() (*psf.Font, error) { return body.load() }

// BodyBold returns Terminus Bold 8x16, the same size as Body so the two can
// be mixed on one line without the baseline moving.
func BodyBold() (*psf.Font, error) { return bodyBold.load() }

// Large returns Terminus Bold 16x32, the face for the figures on a card.
func Large() (*psf.Font, error) { return large.load() }

// Licence returns the SIL Open Font License 1.1 text that covers all four
// faces. A program that shows an about screen is expected to show this.
func Licence() string { return licence }

// load parses the face once and replays the outcome to every later caller.
func (f *face) load() (*psf.Font, error) {
	f.once.Do(func() {
		f.font, f.err = decode(f.data)
	})

	return f.font, f.err
}

// decode gunzips one embedded file and parses it.
func decode(compressed []byte) (*psf.Font, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("fonts: reading gzip header: %w", err)
	}

	defer func() { _ = reader.Close() }()

	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("fonts: decompressing: %w", err)
	}

	font, err := psf.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("fonts: %w", err)
	}

	return font, nil
}
