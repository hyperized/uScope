//go:build linux

package fbdev

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"unsafe"

	"github.com/hyperized/uScope/pkg/rotate"
)

// Geometry and colour constants for the fakes below. Named here instead of
// inline so mnd does not flag them and so a reader can see where a number
// like 0xF800 comes from.
const (
	uconsoleXres       = 720
	uconsoleYres       = 1280
	uconsoleLineLength = 1440
	uconsoleSmemLen    = uconsoleLineLength * uconsoleYres

	bytesPerPixel32 = 4
	lineLength32    = uconsoleXres * bytesPerPixel32
	smemLen32       = lineLength32 * uconsoleYres

	pixBytes16 = depth16 / bitsPerByte
	pixBytes32 = depth32 / bitsPerByte

	redOffset565   = 11
	redLength565   = 5
	greenOffset565 = 5
	greenLength565 = 6
	blueLength565  = 5

	redOffset888    = 16
	redLength888    = 8
	greenOffset888  = 8
	greenLength888  = 8
	blueLength888   = 8
	transpOffset888 = 24
	transpLength888 = 8

	fullChannel = 255

	// RGB565 packing of the five test colours, worked out from component():
	// a 5-bit field keeps value>>3, a 6-bit field keeps value>>2, each then
	// shifted to its offset. Green sits in the middle 6 bits, red and blue
	// in 5-bit fields either side.
	rgb565Red   = 0xF800
	rgb565Green = 0x07E0
	rgb565Blue  = 0x001F
	rgb565White = 0xFFFF
	rgb565Black = 0x0000

	// Same idea for XRGB8888, except every field including Transp is a full
	// 8 bits, so an opaque pixel's alpha lands at bit 24. "Black" here is
	// 0xFF000000, not zero, because alpha is still 255.
	xrgb8888Red   = 0xFFFF0000
	xrgb8888Green = 0xFF00FF00
	xrgb8888Blue  = 0xFF0000FF
	xrgb8888White = 0xFFFFFFFF
	xrgb8888Black = 0xFF000000

	redComponent888 = 0x00FF0000

	wideFieldOffset = 2
	wideFieldLength = 10
	wideFieldWant   = 0x0FF0

	rejectedDepth8  = 8
	rejectedDepth24 = 24
	shortLineLength = 100

	wantVarScreeninfoSize = 160
	wantFixScreeninfoSize = 80

	defaultFileMode = 0o600

	hexBase = 16
)

// errFake is a stand-in failure used across the fakeSys error-path tests.
// Only its identity matters for errors.Is, so one sentinel covers all of
// them.
var errFake = errors.New("fbdev test: fake sys failure")

// fakeSys is the test double for the sys seam. It answers the two geometry
// ioctls from canned structs, can be told to fail any of the three calls on
// demand, and keeps a call log so a test can check what ran and how often.
type fakeSys struct {
	vinfo          varScreeninfo
	finfo          fixScreeninfo
	calls          []string
	mem            []byte
	failIoctlAt    int
	failIoctlErr   error
	failMmapErr    error
	failMunmapErr  error
	ioctlCallCount int
}

func (f *fakeSys) ioctl(_ uintptr, req uintptr, arg unsafe.Pointer) error {
	f.ioctlCallCount++

	reqHex := strconv.FormatUint(uint64(req), hexBase) //nolint:gosec // req is a fixed, small ioctl number.
	f.calls = append(f.calls, "ioctl:"+reqHex)

	if f.failIoctlAt != 0 && f.ioctlCallCount == f.failIoctlAt {
		return f.failIoctlErr
	}

	switch req {
	case fbioGetVScreeninfo:
		*(*varScreeninfo)(arg) = f.vinfo //nolint:gosec // fake mirrors the real ioctl's unsafe struct write.
	case fbioGetFScreeninfo:
		*(*fixScreeninfo)(arg) = f.finfo //nolint:gosec // fake mirrors the real ioctl's unsafe struct write.
	default:
		// Neither geometry ioctl: nothing for the fake to fill in.
	}

	return nil
}

func (f *fakeSys) mmap(_ int, length int) ([]byte, error) {
	f.calls = append(f.calls, "mmap")

	if f.failMmapErr != nil {
		return nil, f.failMmapErr
	}

	f.mem = make([]byte, length)

	return f.mem, nil
}

func (f *fakeSys) munmap(_ []byte) error {
	f.calls = append(f.calls, "munmap")

	return f.failMunmapErr
}

// newUConsoleFake reports the geometry the uConsole panel actually reports:
// 720x1280, 16bpp, RGB565.
func newUConsoleFake() *fakeSys {
	return &fakeSys{
		vinfo: varScreeninfo{
			Xres:         uconsoleXres,
			Yres:         uconsoleYres,
			BitsPerPixel: depth16,
			Red:          fbBitfield{Offset: redOffset565, Length: redLength565},
			Green:        fbBitfield{Offset: greenOffset565, Length: greenLength565},
			Blue:         fbBitfield{Length: blueLength565},
		},
		finfo: fixScreeninfo{
			LineLength: uconsoleLineLength,
			SmemLen:    uconsoleSmemLen,
		},
	}
}

// newUConsole32Fake reports the same panel size but at 32bpp XRGB8888 with
// alpha, the other depth the driver may report.
func newUConsole32Fake() *fakeSys {
	return &fakeSys{
		vinfo: varScreeninfo{
			Xres:         uconsoleXres,
			Yres:         uconsoleYres,
			BitsPerPixel: depth32,
			Red:          fbBitfield{Offset: redOffset888, Length: redLength888},
			Green:        fbBitfield{Offset: greenOffset888, Length: greenLength888},
			Blue:         fbBitfield{Length: blueLength888},
			Transp:       fbBitfield{Offset: transpOffset888, Length: transpLength888},
		},
		finfo: fixScreeninfo{
			LineLength: lineLength32,
			SmemLen:    smemLen32,
		},
	}
}

// newTempDevicePath creates a plain file that os.OpenFile can open. Open
// still does a real open() syscall; only what happens after is faked.
func newTempDevicePath(tb testing.TB) string {
	tb.Helper()

	path := filepath.Join(tb.TempDir(), "fb0")
	if err := os.WriteFile(path, nil, defaultFileMode); err != nil {
		tb.Fatalf("write temp device file: %v", err)
	}

	return path
}

func countCalls(calls []string, want string) int {
	count := 0

	for _, call := range calls {
		if call == want {
			count++
		}
	}

	return count
}

// readPixel reads one packed pixel back out of a mapped buffer, little
// endian, at the given depth.
func readPixel(mem []byte, offset, pixBytes int) uint32 {
	if pixBytes == pixBytes32 {
		return binary.LittleEndian.Uint32(mem[offset : offset+pixBytes32])
	}

	return uint32(binary.LittleEndian.Uint16(mem[offset : offset+pixBytes16]))
}

// assertOnlyNonZeroAt scans a whole mapped buffer and fails if anything
// outside [offset, offset+length) is nonzero. It is how the rotation test
// proves a frame landed only where it should, not merely that it landed
// somewhere.
func assertOnlyNonZeroAt(t *testing.T, mem []byte, offset, length int) {
	t.Helper()

	for idx, value := range mem {
		inRange := idx >= offset && idx < offset+length
		if !inRange && value != 0 {
			t.Fatalf("unexpected nonzero byte at offset %d: %#x", idx, value)
		}
	}
}

// blitAndCheckPixel opens dev on fake, paints a single pixel at logical
// (0,0) with no rotation, and checks the packed bytes that landed in the
// fake's buffer.
func blitAndCheckPixel(t *testing.T, fake *fakeSys, pixel color.RGBA, want uint32) {
	t.Helper()

	path := newTempDevicePath(t)

	dev, err := Open(path, withSys(fake))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { _ = dev.Close() }) //nolint:errcheck // cleanup; the test asserts the packing, not Close.

	img := image.NewRGBA(image.Rect(0, 0, dev.Width(), dev.Height()))
	img.SetRGBA(0, 0, pixel)

	if err := dev.Blit(img, rotate.None); err != nil {
		t.Fatalf("Blit: %v", err)
	}

	pixBytes := dev.BitsPerPixel() / bitsPerByte

	got := readPixel(fake.mem, 0, pixBytes)
	if got != want {
		t.Errorf("packed pixel = %#x, want %#x", got, want)
	}
}

func TestStructLayout(t *testing.T) {
	t.Parallel()

	if got := unsafe.Sizeof(varScreeninfo{}); got != wantVarScreeninfoSize {
		t.Errorf("unsafe.Sizeof(varScreeninfo{}) = %d, want %d", got, wantVarScreeninfoSize)
	}

	if got := unsafe.Sizeof(fixScreeninfo{}); got != wantFixScreeninfoSize {
		t.Errorf("unsafe.Sizeof(fixScreeninfo{}) = %d, want %d", got, wantFixScreeninfoSize)
	}
}

// deviceWant is the geometry TestOpen_Success expects back from a device,
// beyond the width and height every uConsole fake shares.
type deviceWant struct {
	bpp    int
	stride int
	str    string
}

// checkOpenedDevice asserts every getter on dev against want. Split out of
// TestOpen_Success so the test itself stays under the cognitive-complexity
// limit.
func checkOpenedDevice(t *testing.T, dev *Device, want deviceWant) {
	t.Helper()

	if got := dev.Width(); got != uconsoleXres {
		t.Errorf("Width() = %d, want %d", got, uconsoleXres)
	}

	if got := dev.Height(); got != uconsoleYres {
		t.Errorf("Height() = %d, want %d", got, uconsoleYres)
	}

	if got := dev.BitsPerPixel(); got != want.bpp {
		t.Errorf("BitsPerPixel() = %d, want %d", got, want.bpp)
	}

	if got := dev.Stride(); got != want.stride {
		t.Errorf("Stride() = %d, want %d", got, want.stride)
	}

	if got := dev.String(); got != want.str {
		t.Errorf("String() = %q, want %q", got, want.str)
	}
}

func TestOpen_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		newFake func() *fakeSys
		want    deviceWant
	}{
		{
			name:    "uconsole 16bpp",
			newFake: newUConsoleFake,
			want:    deviceWant{bpp: depth16, stride: uconsoleLineLength, str: "720x1280 16bpp stride=1440"},
		},
		{
			name:    "uconsole 32bpp",
			newFake: newUConsole32Fake,
			want:    deviceWant{bpp: depth32, stride: lineLength32, str: "720x1280 32bpp stride=2880"},
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			path := newTempDevicePath(t)

			dev, err := Open(path, withSys(tcase.newFake()))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			t.Cleanup(func() { _ = dev.Close() }) //nolint:errcheck // cleanup only; geometry is what this test checks.

			checkOpenedDevice(t, dev, tcase.want)
		})
	}
}

func TestOpen_PathNotFound(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := Open(path, withSys(newUConsoleFake()))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open() error = %v, want to wrap os.ErrNotExist", err)
	}
}

func TestOpen_IoctlFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		failIoctlAt int
	}{
		{name: "first ioctl fails", failIoctlAt: 1},
		{name: "second ioctl fails", failIoctlAt: 2},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			path := newTempDevicePath(t)
			fake := newUConsoleFake()
			fake.failIoctlAt = tcase.failIoctlAt
			fake.failIoctlErr = errFake

			_, err := Open(path, withSys(fake))
			if !errors.Is(err, errFake) {
				t.Errorf("Open() error = %v, want to wrap %v", err, errFake)
			}

			if got := countCalls(fake.calls, "munmap"); got != 0 {
				t.Errorf("munmap called %d times after a failed ioctl probe, want 0", got)
			}
		})
	}
}

func TestOpen_DepthRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		bpp  uint32
	}{
		{name: "8 bpp", bpp: rejectedDepth8},
		{name: "24 bpp", bpp: rejectedDepth24},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			path := newTempDevicePath(t)
			fake := newUConsoleFake()
			fake.vinfo.BitsPerPixel = tcase.bpp

			_, err := Open(path, withSys(fake))
			if !errors.Is(err, ErrDepth) {
				t.Errorf("Open() error = %v, want ErrDepth", err)
			}
		})
	}
}

func TestOpen_SizeRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*fakeSys)
	}{
		{name: "xres zero", mutate: func(f *fakeSys) { f.vinfo.Xres = 0 }},
		{name: "yres zero", mutate: func(f *fakeSys) { f.vinfo.Yres = 0 }},
		{name: "linelength zero", mutate: func(f *fakeSys) { f.finfo.LineLength = 0 }},
		{name: "linelength shorter than one row", mutate: func(f *fakeSys) { f.finfo.LineLength = shortLineLength }},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			path := newTempDevicePath(t)
			fake := newUConsoleFake()
			tcase.mutate(fake)

			_, err := Open(path, withSys(fake))
			if !errors.Is(err, ErrSize) {
				t.Errorf("Open() error = %v, want ErrSize", err)
			}
		})
	}
}

func TestOpen_MmapFails(t *testing.T) {
	t.Parallel()

	path := newTempDevicePath(t)
	fake := newUConsoleFake()
	fake.failMmapErr = errFake

	_, err := Open(path, withSys(fake))
	if !errors.Is(err, errFake) {
		t.Errorf("Open() error = %v, want to wrap %v", err, errFake)
	}

	if got := countCalls(fake.calls, "munmap"); got != 0 {
		t.Errorf("munmap called %d times after a failed mmap, want 0", got)
	}
}

// packedWant is the expected packed value for each of the five test colours
// at one pixel depth.
type packedWant struct {
	red, green, blue, white, black uint32
}

// colorCases builds the five-colour table shared by every depth: pure red,
// green, blue, white and black, each opaque. Only the expected packed value
// changes between depths, which is why this is a function instead of a
// second copy of the table.
func colorCases(want packedWant) []struct {
	name  string
	pixel color.RGBA
	want  uint32
} {
	return []struct {
		name  string
		pixel color.RGBA
		want  uint32
	}{
		{name: "red", pixel: color.RGBA{R: fullChannel, A: fullChannel}, want: want.red},
		{name: "green", pixel: color.RGBA{G: fullChannel, A: fullChannel}, want: want.green},
		{name: "blue", pixel: color.RGBA{B: fullChannel, A: fullChannel}, want: want.blue},
		{
			name:  "white",
			pixel: color.RGBA{R: fullChannel, G: fullChannel, B: fullChannel, A: fullChannel},
			want:  want.white,
		},
		{name: "black", pixel: color.RGBA{A: fullChannel}, want: want.black},
	}
}

func TestBlit_PixelPacking(t *testing.T) {
	t.Parallel()

	depths := []struct {
		name    string
		newFake func() *fakeSys
		want    packedWant
	}{
		{
			name:    "16bpp RGB565",
			newFake: newUConsoleFake,
			want: packedWant{
				red: rgb565Red, green: rgb565Green, blue: rgb565Blue, white: rgb565White, black: rgb565Black,
			},
		},
		{
			name:    "32bpp XRGB8888",
			newFake: newUConsole32Fake,
			want: packedWant{
				red: xrgb8888Red, green: xrgb8888Green, blue: xrgb8888Blue, white: xrgb8888White, black: xrgb8888Black,
			},
		},
	}

	for _, depth := range depths {
		t.Run(depth.name, func(t *testing.T) {
			t.Parallel()

			for _, tcase := range colorCases(depth.want) {
				t.Run(tcase.name, func(t *testing.T) {
					t.Parallel()
					blitAndCheckPixel(t, depth.newFake(), tcase.pixel, tcase.want)
				})
			}
		})
	}
}

// TestBlit_RotationPlacement is the test that catches a frame landing on the
// wrong edge of the panel: it puts one pixel at logical (0,0) and checks it
// physically lands exactly where rotate.Map says it should, for each
// rotation, rather than trusting that Blit and Map agree.
func TestBlit_RotationPlacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rot  rotate.Rotation
	}{
		{name: "no rotation", rot: rotate.None},
		{name: "clockwise", rot: rotate.Clockwise},
		{name: "upside down", rot: rotate.UpsideDown},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			path := newTempDevicePath(t)
			fake := newUConsoleFake()

			dev, err := Open(path, withSys(fake))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			t.Cleanup(func() { _ = dev.Close() }) //nolint:errcheck // cleanup; placement is what this test checks.

			logicalW, logicalH := tcase.rot.Logical(dev.Width(), dev.Height())
			img := image.NewRGBA(image.Rect(0, 0, logicalW, logicalH))
			img.SetRGBA(0, 0, color.RGBA{R: fullChannel, G: fullChannel, B: fullChannel, A: fullChannel})

			if err := dev.Blit(img, tcase.rot); err != nil {
				t.Fatalf("Blit: %v", err)
			}

			physX, physY := tcase.rot.Map(0, 0, dev.Width(), dev.Height())
			wantOffset := physY*dev.Stride() + physX*pixBytes16

			got := binary.LittleEndian.Uint16(fake.mem[wantOffset : wantOffset+pixBytes16])
			if got != rgb565White {
				t.Errorf("pixel at physical offset %d = %#x, want %#x", wantOffset, got, rgb565White)
			}

			assertOnlyNonZeroAt(t, fake.mem, wantOffset, pixBytes16)
		})
	}
}

func TestBlit_SizeMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rot    rotate.Rotation
		width  int
		height int
	}{
		{name: "too small", rot: rotate.None, width: uconsoleXres - 1, height: uconsoleYres},
		{name: "too large", rot: rotate.None, width: uconsoleXres + 1, height: uconsoleYres},
		{name: "right size wrong rotation", rot: rotate.Clockwise, width: uconsoleXres, height: uconsoleYres},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			path := newTempDevicePath(t)

			dev, err := Open(path, withSys(newUConsoleFake()))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			t.Cleanup(func() { _ = dev.Close() }) //nolint:errcheck // cleanup only; size is what this test checks.

			img := image.NewRGBA(image.Rect(0, 0, tcase.width, tcase.height))

			err = dev.Blit(img, tcase.rot)
			if !errors.Is(err, ErrSize) {
				t.Errorf("Blit() error = %v, want ErrSize", err)
			}
		})
	}
}

func TestBlit_AfterClose(t *testing.T) {
	t.Parallel()

	path := newTempDevicePath(t)

	dev, err := Open(path, withSys(newUConsoleFake()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := dev.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, dev.Width(), dev.Height()))

	err = dev.Blit(img, rotate.None)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("Blit() after Close error = %v, want ErrClosed", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	t.Parallel()

	path := newTempDevicePath(t)
	fake := newUConsoleFake()

	dev, err := Open(path, withSys(fake))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := dev.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	if err := dev.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if got := countCalls(fake.calls, "munmap"); got != 1 {
		t.Errorf("munmap called %d times across two Close calls, want 1", got)
	}
}

// TestClose_ErrorPropagation closes the real file out from under the device
// before calling Close, so the file's own Close() also fails alongside a
// faked munmap failure, exercising the errors.Join path with two errors.
func TestClose_ErrorPropagation(t *testing.T) {
	t.Parallel()

	path := newTempDevicePath(t)
	fake := newUConsoleFake()
	fake.failMunmapErr = errFake

	dev, err := Open(path, withSys(fake))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := dev.file.Close(); err != nil {
		t.Fatalf("pre-closing the underlying file: %v", err)
	}

	err = dev.Close()
	if !errors.Is(err, errFake) {
		t.Errorf("Close() error = %v, want to wrap %v", err, errFake)
	}

	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("Close() error = %v, want to wrap os.ErrClosed", err)
	}
}

func TestComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value uint8
		field fbBitfield
		want  uint32
	}{
		{
			name:  "zero length field contributes nothing",
			value: fullChannel,
			field: fbBitfield{Offset: 3, Length: 0},
			want:  0,
		},
		{
			name:  "five bit field like RGB565 red",
			value: fullChannel,
			field: fbBitfield{Offset: redOffset565, Length: redLength565},
			want:  rgb565Red,
		},
		{
			name:  "eight bit field like XRGB8888 red",
			value: fullChannel,
			field: fbBitfield{Offset: redOffset888, Length: redLength888},
			want:  redComponent888,
		},
		{
			name:  "field wider than a byte",
			value: fullChannel,
			field: fbBitfield{Offset: wideFieldOffset, Length: wideFieldLength},
			want:  wideFieldWant,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			if got := component(tcase.value, tcase.field); got != tcase.want {
				t.Errorf("component(%d, %+v) = %#x, want %#x", tcase.value, tcase.field, got, tcase.want)
			}
		})
	}
}

// BenchmarkBlit measures a full realistic frame at the uConsole's real
// logical size, so a later optimisation pass on render has a number to beat.
func BenchmarkBlit(b *testing.B) {
	path := newTempDevicePath(b)

	dev, err := Open(path, withSys(newUConsoleFake()))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}

	defer func() { _ = dev.Close() }() //nolint:errcheck // cleanup; the benchmark measures Blit, not Close.

	img := image.NewRGBA(image.Rect(0, 0, dev.Width(), dev.Height()))

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if err := dev.Blit(img, rotate.None); err != nil {
			b.Fatalf("Blit: %v", err)
		}
	}
}
