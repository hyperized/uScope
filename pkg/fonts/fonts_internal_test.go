package fonts

import (
	"bytes"
	"compress/gzip"
	"errors"
	"strings"
	"testing"

	"github.com/hyperized/uScope/pkg/psf"
)

// gzipBytes compresses data and fails the test if gzip itself errors, which
// would mean the fixture is broken rather than the code under test.
func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer

	writer := gzip.NewWriter(&compressed)

	if _, err := writer.Write(data); err != nil {
		t.Fatalf("gzip.Write: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}

	return compressed.Bytes()
}

func TestDecodeNotGzip(t *testing.T) {
	t.Parallel()

	_, err := decode([]byte("this is plainly not a gzip stream"))
	if err == nil {
		t.Fatal("decode() error = nil, want non-nil")
	}

	const want = "reading gzip header"

	if !strings.Contains(err.Error(), want) {
		t.Errorf("decode() error = %q, want to contain %q", err.Error(), want)
	}
}

func TestDecodeTruncatedGzip(t *testing.T) {
	t.Parallel()

	compressed := gzipBytes(t, []byte("enough plain bytes that truncating the trailer still leaves a valid header"))

	const trailerCut = 4

	truncated := compressed[:len(compressed)-trailerCut]

	_, err := decode(truncated)
	if err == nil {
		t.Fatal("decode() error = nil, want non-nil")
	}

	const want = "decompressing"

	if !strings.Contains(err.Error(), want) {
		t.Errorf("decode() error = %q, want to contain %q", err.Error(), want)
	}
}

func TestDecodeValidGzipNotFont(t *testing.T) {
	t.Parallel()

	compressed := gzipBytes(t, []byte("just some ordinary bytes, not a PSF font at all"))

	_, err := decode(compressed)
	if !errors.Is(err, psf.ErrMagic) {
		t.Errorf("decode() error = %v, want ErrMagic", err)
	}
}

// TestDecodeValidFont reuses the embedded Small face bytes directly, since
// this internal test can already see the package-level slice decode reads.
func TestDecodeValidFont(t *testing.T) {
	t.Parallel()

	font, err := decode(smallGZ)
	if err != nil {
		t.Fatalf("decode(smallGZ) error = %v, want nil", err)
	}

	const (
		wantWidth  = 6
		wantHeight = 12
	)

	if got := font.Width(); got != wantWidth {
		t.Errorf("decode(smallGZ) Width() = %d, want %d", got, wantWidth)
	}

	if got := font.Height(); got != wantHeight {
		t.Errorf("decode(smallGZ) Height() = %d, want %d", got, wantHeight)
	}
}

// TestFaceLoadCachesError drives a face with deliberately bad data through
// load twice, which is the only way to reach the replay path: sync.Once
// only runs decode once, so the second call has to hand back whatever the
// first call already stored.
func TestFaceLoadCachesError(t *testing.T) {
	t.Parallel()

	badFace := &face{data: []byte("not a gzip stream")}

	firstFont, firstErr := badFace.load()
	if firstErr == nil {
		t.Fatal("load() error = nil, want non-nil")
	}

	if firstFont != nil {
		t.Errorf("load() font = %v, want nil", firstFont)
	}

	secondFont, secondErr := badFace.load()
	if secondFont != nil {
		t.Errorf("second load() font = %v, want nil", secondFont)
	}

	if !errors.Is(secondErr, firstErr) {
		t.Errorf("second load() error = %v, want the same error as the first call (%v)", secondErr, firstErr)
	}
}
