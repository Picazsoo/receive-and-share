// Capture a Linux framebuffer as PNG without changing the display.
// Build for Kobo: CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build screenshot.go
package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"syscall"
	"unsafe"
)

type bitfield struct{ Offset, Length, MSBRight uint32 }
type screenInfo struct {
	XRes, YRes, XVirtual, YVirtual, XOffset, YOffset, BPP, Grayscale uint32
	Red, Green, Blue, Alpha                                          bitfield
	NonStd, Activate, Height, Width, AccelFlags, PixelClock          uint32
	LeftMargin, RightMargin, UpperMargin, LowerMargin, HSync, VSync  uint32
	Sync, VMode, Rotate, ColorSpace                                  uint32
	Reserved                                                         [4]uint32
}

func ioctl(fd uintptr, request uintptr, p unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(p))
	if errno != 0 {
		return errno
	}
	return nil
}
func component(pixel uint32, f bitfield) uint8 {
	if f.Length == 0 {
		return 0
	}
	mask := uint32((uint64(1) << f.Length) - 1)
	return uint8((uint64((pixel>>f.Offset)&mask)*255 + uint64(mask)/2) / uint64(mask))
}
func capture() error {
	f, err := os.Open("/dev/fb0")
	if err != nil {
		return err
	}
	defer f.Close()
	var info screenInfo
	if err = ioctl(f.Fd(), 0x4600, unsafe.Pointer(&info)); err != nil {
		return fmt.Errorf("screen info: %w", err)
	}
	var fixed [128]byte
	if err = ioctl(f.Fd(), 0x4602, unsafe.Pointer(&fixed[0])); err != nil {
		return fmt.Errorf("stride info: %w", err)
	}
	strideOffset := 44
	if unsafe.Sizeof(uintptr(0)) == 8 {
		strideOffset = 48
	}
	stride := binary.LittleEndian.Uint32(fixed[strideOffset : strideOffset+4])
	if info.XRes == 0 || info.YRes == 0 || info.XRes > 8192 || info.YRes > 8192 {
		return fmt.Errorf("unexpected geometry: %dx%d", info.XRes, info.YRes)
	}
	if info.BPP != 8 && info.BPP != 16 && info.BPP != 24 && info.BPP != 32 {
		return fmt.Errorf("unsupported pixel depth %d", info.BPP)
	}
	for _, field := range []bitfield{info.Red, info.Green, info.Blue} {
		if field.Length > 16 || field.Offset+field.Length > 32 || field.MSBRight != 0 {
			return fmt.Errorf("unsupported pixel bitfield")
		}
	}
	bytesPerPixel := info.BPP / 8
	rowSize := info.XRes * bytesPerPixel
	if stride < (info.XOffset+info.XRes)*bytesPerPixel {
		return fmt.Errorf("framebuffer stride is too short")
	}
	frame := image.NewRGBA(image.Rect(0, 0, int(info.XRes), int(info.YRes)))
	row := make([]byte, rowSize)
	for y := uint32(0); y < info.YRes; y++ {
		offset := int64(y+info.YOffset)*int64(stride) + int64(info.XOffset)*int64(bytesPerPixel)
		if _, err = f.ReadAt(row, offset); err != nil {
			return err
		}
		for x := uint32(0); x < info.XRes; x++ {
			at := x * bytesPerPixel
			if info.BPP == 8 {
				v := row[at]
				frame.SetRGBA(int(x), int(y), color.RGBA{v, v, v, 255})
				continue
			}
			var pixel uint32
			for b := uint32(0); b < bytesPerPixel; b++ {
				pixel |= uint32(row[at+b]) << (8 * b)
			}
			frame.SetRGBA(int(x), int(y), color.RGBA{component(pixel, info.Red), component(pixel, info.Green), component(pixel, info.Blue), 255})
		}
	}
	fmt.Fprintf(os.Stderr, "Captured %dx%d, %d bpp, stride %d, rotation %d\n", info.XRes, info.YRes, info.BPP, stride, info.Rotate)
	return png.Encode(os.Stdout, frame)
}
func main() {
	if err := capture(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
