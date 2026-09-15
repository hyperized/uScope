//go:build linux

package fbdev

import (
	"errors"
	"fmt"
	"image"
	"os"
	"strconv"
	"sync"
	"unsafe"

	"github.com/hyperized/uScope/pkg/rotate"
)

// ioctl request numbers from <linux/fb.h>. They are plain _IOR numbers with
// no size encoded, which is why they are this short.
const (
	fbioGetVScreeninfo = 0x4600
	fbioGetFScreeninfo = 0x4602
)

const (
	bitsPerByte = 8
	rgbaBytes   = 4
	depth16     = 16
	depth32     = 32

	// Expected sizes of the two kernel structs on a 64-bit kernel. Getting
	// these wrong silently shifts every field after the mismatch.
	varScreeninfoSize = 160
	fixScreeninfoSize = 80
)

// The layout checks. A wrong struct size stops the build here rather than
// producing a device that reports nonsense geometry at 3am on the uConsole.
const (
	_ uintptr = unsafe.Sizeof(varScreeninfo{}) - varScreeninfoSize
	_ uintptr = varScreeninfoSize - unsafe.Sizeof(varScreeninfo{})
	_ uintptr = unsafe.Sizeof(fixScreeninfo{}) - fixScreeninfoSize
	_ uintptr = fixScreeninfoSize - unsafe.Sizeof(fixScreeninfo{})
)

// fbBitfield describes where one colour component sits inside a pixel. On
// the uConsole the panel is RGB565, so Red is offset 11 length 5.
type fbBitfield struct {
	Offset, Length, MsbRight uint32
}

// varScreeninfo mirrors struct fb_var_screeninfo from <linux/fb.h>. Field
// order and count are load-bearing: the kernel fills this by memcpy.
type varScreeninfo struct {
	Xres, Yres, XresVirtual, YresVirtual, Xoffset, Yoffset uint32
	BitsPerPixel, Grayscale                                uint32
	Red, Green, Blue, Transp                               fbBitfield
	Nonstd, Activate, Height, Width, AccelFlags            uint32
	Pixclock                                               uint32
	LeftMargin, RightMargin, UpperMargin, LowerMargin      uint32
	HsyncLen, VsyncLen, Sync, Vmode, Rotate, Colorspace    uint32
	Reserved                                               [4]uint32
}

// fixScreeninfo mirrors struct fb_fix_screeninfo from <linux/fb.h>. The
// padding the compiler inserts around the uint16 run is part of the ABI, so
// the field order must not be tidied.
type fixScreeninfo struct {
	ID                             [16]byte
	SmemStart                      uint64
	SmemLen, Type, TypeAux, Visual uint32
	Xpanstep, Ypanstep, Ywrapstep  uint16
	LineLength                     uint32
	MmioStart                      uint64
	MmioLen, Accel                 uint32
	Capabilities                   uint16
	Reserved                       [2]uint16
}

// sys is the seam over the three syscalls this package needs. Tests swap in
// a fake so the packing, rotation and lifecycle logic can be exercised
// without a real framebuffer.
type sys interface {
	ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error
	mmap(fd int, length int) ([]byte, error)
	munmap(mem []byte) error
}

// Device is an open framebuffer.
//
// Blit and Close are safe to call from any goroutine; they serialise on one
// mutex because both touch the mapping and the staging buffer. Nothing else
// about the device is mutable after Open.
type Device struct {
	mu       sync.Mutex
	path     string
	file     *os.File
	syscalls sys
	vinfo    varScreeninfo
	finfo    fixScreeninfo
	mem      []byte
	stage    []byte
}

// Option configures a Device. No options ship in slice 1; the variadic is
// here so adding one later is not a breaking change.
type Option func(*Device)

// withSys replaces the syscall seam. Unexported on purpose: it is a test
// hook, not part of the contract.
func withSys(seam sys) Option {
	return func(d *Device) { d.syscalls = seam }
}

// Open maps a framebuffer device and reads back its geometry.
//
// The caller must Close it. On any failure during probing the file is closed
// before returning, so a failed Open leaks nothing.
func Open(path string, opts ...Option) (*Device, error) {
	dev := &Device{path: path, syscalls: realSys{}}
	for _, opt := range opts {
		opt(dev)
	}

	// The path is an operator-supplied device node. Choosing it is what --fb
	// is for, so there is nothing to allow-list against here.
	file, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // device node by design.
	if err != nil {
		return nil, fmt.Errorf("fbdev: open %s: %w", path, err)
	}

	dev.file = file

	if err := dev.probe(); err != nil {
		_ = dev.Close() //nolint:errcheck // the probe error is the one worth reporting.

		return nil, err
	}

	return dev, nil
}

// Width is the framebuffer width in pixels, before rotation.
func (d *Device) Width() int { return int(d.vinfo.Xres) }

// Height is the framebuffer height in pixels, before rotation.
func (d *Device) Height() int { return int(d.vinfo.Yres) }

// BitsPerPixel is the pixel depth. 16 and 32 are supported.
func (d *Device) BitsPerPixel() int { return int(d.vinfo.BitsPerPixel) }

// Stride is the bytes per scanline, which can exceed width times the pixel
// size when the driver pads rows.
func (d *Device) Stride() int { return int(d.finfo.LineLength) }

// String summarises the device, for example "720x1280 16bpp stride=1440".
func (d *Device) String() string {
	return strconv.Itoa(d.Width()) + "x" + strconv.Itoa(d.Height()) + " " +
		strconv.Itoa(d.BitsPerPixel()) + "bpp stride=" + strconv.Itoa(d.Stride())
}

// Blit paints one frame. img must be exactly the logical size for rot, which
// is what rotate.Logical reports for this device's physical geometry.
//
// The frame is packed into a staging buffer allocated once at Open and then
// copied into the mapping in a single memmove. Writing pixel by pixel
// straight into the mapping is much slower: framebuffer memory is uncached.
func (d *Device) Blit(img *image.RGBA, rot rotate.Rotation) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.mem == nil {
		return ErrClosed
	}

	physW, physH := int(d.vinfo.Xres), int(d.vinfo.Yres)
	wantW, wantH := rot.Logical(physW, physH)
	bounds := img.Bounds()

	if bounds.Dx() != wantW || bounds.Dy() != wantH {
		return fmt.Errorf("%w: got %dx%d, want %dx%d at rotation %s",
			ErrSize, bounds.Dx(), bounds.Dy(), wantW, wantH, rot)
	}

	d.render(img, rot, physW, physH)
	copy(d.mem, d.stage)

	return nil
}

// Close unmaps and closes the device. It is safe to call more than once, so
// the failure path in Open and a deferred Close cannot double-free.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var errs []error

	if d.mem != nil {
		if err := d.syscalls.munmap(d.mem); err != nil {
			errs = append(errs, fmt.Errorf("fbdev: munmap %s: %w", d.path, err))
		}

		d.mem = nil
	}

	if d.file != nil {
		if err := d.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("fbdev: close %s: %w", d.path, err))
		}

		d.file = nil
	}

	d.stage = nil

	return errors.Join(errs...)
}

// probe reads the two screeninfo structs, checks the geometry is usable and
// maps the buffer.
//
//nolint:gosec // G103: ioctl exists to hand the kernel a struct address.
func (d *Device) probe() error {
	descriptor := d.file.Fd()
	if err := d.syscalls.ioctl(descriptor, fbioGetVScreeninfo, unsafe.Pointer(&d.vinfo)); err != nil {
		return fmt.Errorf("fbdev: FBIOGET_VSCREENINFO on %s: %w", d.path, err)
	}

	if err := d.syscalls.ioctl(descriptor, fbioGetFScreeninfo, unsafe.Pointer(&d.finfo)); err != nil {
		return fmt.Errorf("fbdev: FBIOGET_FSCREENINFO on %s: %w", d.path, err)
	}

	if err := d.checkGeometry(); err != nil {
		return err
	}

	mem, err := d.syscalls.mmap(int(descriptor), int(d.finfo.SmemLen))
	if err != nil {
		return fmt.Errorf("fbdev: mmap %s: %w", d.path, err)
	}

	d.mem = mem
	d.stage = make([]byte, int(d.finfo.LineLength)*int(d.vinfo.Yres))

	return nil
}

// checkGeometry rejects what the kernel reported before we trust it for
// pointer arithmetic. A driver that has not come up yet reports zeroes.
func (d *Device) checkGeometry() error {
	if d.vinfo.BitsPerPixel != depth16 && d.vinfo.BitsPerPixel != depth32 {
		return fmt.Errorf("%w: %s reports %d bpp", ErrDepth, d.path, d.vinfo.BitsPerPixel)
	}

	if d.vinfo.Xres == 0 || d.vinfo.Yres == 0 || d.finfo.LineLength == 0 {
		return fmt.Errorf("%w: %s reports %dx%d stride %d",
			ErrSize, d.path, d.vinfo.Xres, d.vinfo.Yres, d.finfo.LineLength)
	}

	need := d.vinfo.Xres * d.vinfo.BitsPerPixel / bitsPerByte
	if d.finfo.LineLength < need {
		return fmt.Errorf("%w: %s stride %d is shorter than one %d bpp row of %d",
			ErrSize, d.path, d.finfo.LineLength, d.vinfo.BitsPerPixel, d.vinfo.Xres)
	}

	return nil
}

// render walks every logical pixel into its physical slot in the staging
// buffer.
//
// This is a per-pixel loop with a function call in the middle of it, which is
// honest but not fast: at 1280x720 it is 921600 iterations a frame. Slice 1
// wants correctness on the panel first. BenchmarkBlit is here so the next
// pass has a number to beat.
//
//nolint:varnamelen // x, y index pixels; longer names would only obscure it.
func (d *Device) render(img *image.RGBA, rot rotate.Rotation, physW, physH int) {
	stride := int(d.finfo.LineLength)
	pixBytes := int(d.vinfo.BitsPerPixel) / bitsPerByte
	bounds := img.Bounds()

	for y := range bounds.Dy() {
		rowBase := img.PixOffset(bounds.Min.X, bounds.Min.Y+y)

		for x := range bounds.Dx() {
			src := rowBase + x*rgbaBytes
			value := d.pack(img.Pix[src], img.Pix[src+1], img.Pix[src+2], img.Pix[src+3])

			physX, physY := rot.Map(x, y, physW, physH)
			dst := physY*stride + physX*pixBytes

			for i := range pixBytes {
				// Truncation is the point: this stores the value little-endian.
				d.stage[dst+i] = byte(value >> (bitsPerByte * i)) //nolint:gosec // deliberate.
			}
		}
	}
}

// pack folds an RGBA pixel into the device's own layout using the bitfields
// the driver reported, so RGB565 and XRGB8888 share one code path.
func (d *Device) pack(red, green, blue, alpha uint8) uint32 {
	return component(red, d.vinfo.Red) |
		component(green, d.vinfo.Green) |
		component(blue, d.vinfo.Blue) |
		component(alpha, d.vinfo.Transp)
}

// component scales one 8-bit channel into a field of Length bits at Offset.
// A zero-length field contributes nothing, which is how a framebuffer with
// no alpha channel drops the alpha.
func component(value uint8, field fbBitfield) uint32 {
	switch {
	case field.Length == 0:
		return 0
	case field.Length >= bitsPerByte:
		return uint32(value) << (field.Length - bitsPerByte) << field.Offset
	default:
		return uint32(value>>(bitsPerByte-field.Length)) << field.Offset
	}
}
