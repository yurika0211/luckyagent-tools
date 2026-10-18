package computer

import (
	"context"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/jezek/xgb/xproto"
)

func TestLocalX11DisplaySelection(t *testing.T) {
	for _, display := range []string{":2.1", "unix/:2.1", "unix:2.1"} {
		number, screen, err := localX11Display(display)
		if err != nil || number != 2 || screen != 1 {
			t.Fatalf("%q = %d,%d,%v", display, number, screen, err)
		}
	}
	for _, display := range []string{"localhost:0", "host:1", ":-1", ":0.-1", ":0.1.2", ""} {
		if _, _, err := localX11Display(display); err == nil {
			t.Errorf("accepted %q", display)
		}
	}
}

func TestX11NativePixelFormats(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		format xproto.Format
		order  byte
		visual xproto.VisualInfo
		want   color.RGBA
	}{
		{"bgr32", []byte{0x33, 0x22, 0x11, 0}, xproto.Format{BitsPerPixel: 32, ScanlinePad: 32}, xproto.ImageOrderLSBFirst, xproto.VisualInfo{RedMask: 0xff0000, GreenMask: 0xff00, BlueMask: 0xff}, color.RGBA{0x11, 0x22, 0x33, 255}},
		{"rgb24-padded", []byte{0x11, 0x22, 0x33, 0}, xproto.Format{BitsPerPixel: 24, ScanlinePad: 32}, xproto.ImageOrderMSBFirst, xproto.VisualInfo{RedMask: 0xff0000, GreenMask: 0xff00, BlueMask: 0xff}, color.RGBA{0x11, 0x22, 0x33, 255}},
		{"rgb565", []byte{0, 0xf8}, xproto.Format{BitsPerPixel: 16, ScanlinePad: 16}, xproto.ImageOrderLSBFirst, xproto.VisualInfo{RedMask: 0xf800, GreenMask: 0x7e0, BlueMask: 0x1f}, color.RGBA{255, 0, 0, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img, err := x11RGBA(context.Background(), tc.data, 1, 1, tc.format, tc.visual, tc.order)
			if err != nil {
				t.Fatal(err)
			}
			if got := img.RGBAAt(0, 0); got != tc.want {
				t.Fatalf("pixel = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestX11NativeCaptureBoundsAndCancellation(t *testing.T) {
	format := xproto.Format{BitsPerPixel: 32, ScanlinePad: 32}
	if _, err := x11RGBA(context.Background(), nil, 1, 1, format, xproto.VisualInfo{}, 0); err == nil {
		t.Fatal("accepted truncated data")
	}
	if _, err := x11RGBA(context.Background(), nil, nativeX11MaxPixels, 2, format, xproto.VisualInfo{}, 0); err == nil {
		t.Fatal("accepted oversized image")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := x11RGBA(ctx, make([]byte, 4), 1, 1, format, xproto.VisualInfo{}, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	backend := NewX11Backend(X11Config{Display: ":0", ScreenshotBinary: "does-not-exist"})
	if err := backend.captureScreenshot(ctx, "unused", image.Rectangle{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("capture cancel = %v", err)
	}
}

func TestX11AuthorityRejectsNonRegularFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority-directory")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XAUTHORITY", path)
	if _, err := localX11Cookie("0"); err == nil {
		t.Fatal("accepted directory as Xauthority")
	}
}
