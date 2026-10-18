package computer

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/bits"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

const (
	nativeX11Timeout   = 5 * time.Second
	nativeX11MaxPixels = 64 * 1024 * 1024
)

// captureNativeX11 is a local-display fallback for ImageMagick failures. A
// separate WSLg backend owns Windows capture; this path never switches desktops.
func captureNativeX11(ctx context.Context, display, path string, region image.Rectangle) error {
	if runtime.GOOS != "linux" {
		return errors.New("native X11 capture requires Linux")
	}
	displayNumber, screenNumber, err := localX11Display(display)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, nativeX11Timeout)
	defer cancel()
	cookie, err := localX11Cookie(strconv.Itoa(displayNumber))
	if err != nil {
		return fmt.Errorf("read X11 authority: %w", err)
	}
	dialer := net.Dialer{}
	socket := filepath.Join("/tmp/.X11-unix", "X"+strconv.Itoa(displayNumber))
	raw, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("connect to local X11 display: %w", err)
	}
	defer raw.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := raw.SetDeadline(deadline); err != nil {
			return err
		}
	}
	stopClose := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stopClose()

	var conn *xgb.Conn
	if cookie != "" {
		conn, err = xgb.NewConnNetWithCookieHex(raw, cookie)
	} else {
		conn, err = xgb.NewConnNet(raw)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("X11 setup: %w", err)
	}
	defer conn.Close()
	conn.DisplayNumber, conn.DefaultScreen = displayNumber, screenNumber
	setup := xproto.Setup(conn)
	if screenNumber >= len(setup.Roots) {
		return fmt.Errorf("X11 screen %d is unavailable", screenNumber)
	}
	screen := &setup.Roots[screenNumber]
	if region.Empty() {
		region = image.Rect(0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels))
	}
	if region.Min.X < 0 || region.Min.Y < 0 || region.Max.X > int(screen.WidthInPixels) || region.Max.Y > int(screen.HeightInPixels) || region.Min.X > 32767 || region.Min.Y > 32767 {
		return fmt.Errorf("native X11 capture region is outside the screen: %v", region)
	}
	if region.Empty() || int64(region.Dx())*int64(region.Dy()) > nativeX11MaxPixels {
		return fmt.Errorf("native X11 capture region has an unsupported size: %v", region)
	}
	visual, ok := rootVisualInfo(screen)
	if !ok || visual.Class != xproto.VisualClassTrueColor {
		return errors.New("native X11 capture requires a TrueColor root visual")
	}
	format, ok := pixmapFormat(setup, screen.RootDepth)
	if !ok || (format.BitsPerPixel != 8 && format.BitsPerPixel != 16 && format.BitsPerPixel != 24 && format.BitsPerPixel != 32) {
		return errors.New("unsupported native X11 pixmap format")
	}
	reply, err := xproto.GetImage(conn, xproto.ImageFormatZPixmap, xproto.Drawable(screen.Root), int16(region.Min.X), int16(region.Min.Y), uint16(region.Dx()), uint16(region.Dy()), 0xffffffff).Reply()
	if err != nil || reply == nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("X11 GetImage failed: %v", err)
	}
	rgba, err := x11RGBA(ctx, reply.Data, region.Dx(), region.Dy(), format, visual, setup.ImageByteOrder)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(x11ContextWriter{ctx: ctx, writer: file}, rgba)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func localX11Display(display string) (int, int, error) {
	value := strings.TrimPrefix(strings.TrimSpace(display), "unix/")
	value = strings.TrimPrefix(value, "unix")
	if !strings.HasPrefix(value, ":") {
		return 0, 0, errors.New("native X11 fallback requires a local DISPLAY such as :0 or :0.1")
	}
	parts := strings.Split(strings.TrimPrefix(value, ":"), ".")
	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("invalid local X11 display %q", display)
	}
	number, err := strconv.Atoi(parts[0])
	if err != nil || number < 0 {
		return 0, 0, fmt.Errorf("invalid local X11 display %q", display)
	}
	screen := 0
	if len(parts) == 2 {
		screen, err = strconv.Atoi(parts[1])
		if err != nil || screen < 0 {
			return 0, 0, fmt.Errorf("invalid local X11 screen %q", display)
		}
	}
	return number, screen, nil
}

// Xauthority records contain a big-endian family followed by four length-prefixed
// byte strings: address, display, protocol and cookie. Keep the cookie in memory.
func localX11Cookie(display string) (string, error) {
	path := strings.TrimSpace(os.Getenv("XAUTHORITY"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".Xauthority")
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return "", errors.New("Xauthority must be a regular file no larger than 1 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return "", errors.New("Xauthority must be a regular file no larger than 1 MiB")
	}
	host, err := os.Hostname()
	if err != nil {
		return "", err
	}
	reader := bufio.NewReader(io.LimitReader(file, 1024*1024))
	for {
		var family uint16
		if err := binary.Read(reader, binary.BigEndian, &family); err != nil {
			if errors.Is(err, io.EOF) {
				return "", nil
			}
			return "", err
		}
		var fields [4][]byte
		for i := range fields {
			var size uint16
			if err := binary.Read(reader, binary.BigEndian, &size); err != nil {
				return "", err
			}
			fields[i] = make([]byte, int(size))
			if _, err := io.ReadFull(reader, fields[i]); err != nil {
				return "", err
			}
		}
		if (family == 65535 || family == 256 && string(fields[0]) == host) && (len(fields[1]) == 0 || string(fields[1]) == display) {
			if string(fields[2]) != "MIT-MAGIC-COOKIE-1" || len(fields[3]) != 16 {
				return "", errors.New("unsupported X11 authority protocol")
			}
			return hex.EncodeToString(fields[3]), nil
		}
	}
}

func rootVisualInfo(screen *xproto.ScreenInfo) (xproto.VisualInfo, bool) {
	for _, depth := range screen.AllowedDepths {
		for _, visual := range depth.Visuals {
			if visual.VisualId == screen.RootVisual {
				return visual, true
			}
		}
	}
	return xproto.VisualInfo{}, false
}

func pixmapFormat(setup *xproto.SetupInfo, depth byte) (xproto.Format, bool) {
	for _, format := range setup.PixmapFormats {
		if format.Depth == depth {
			return format, true
		}
	}
	return xproto.Format{}, false
}

func x11RGBA(ctx context.Context, data []byte, width, height int, format xproto.Format, visual xproto.VisualInfo, byteOrder byte) (*image.RGBA, error) {
	if width <= 0 || height <= 0 || int64(width)*int64(height) > nativeX11MaxPixels || format.ScanlinePad == 0 || format.ScanlinePad%8 != 0 {
		return nil, errors.New("invalid native X11 image dimensions or padding")
	}
	bytesPerPixel := (int(format.BitsPerPixel) + 7) / 8
	stride := ((width*int(format.BitsPerPixel) + int(format.ScanlinePad) - 1) / int(format.ScanlinePad)) * (int(format.ScanlinePad) / 8)
	if bytesPerPixel < 1 || bytesPerPixel > 4 || stride < width*bytesPerPixel || stride*height > len(data) {
		return nil, errors.New("truncated native X11 image data")
	}
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := data[y*stride : (y+1)*stride]
		for x := 0; x < width; x++ {
			raw := row[x*bytesPerPixel : (x+1)*bytesPerPixel]
			var pixel uint32
			if byteOrder == xproto.ImageOrderMSBFirst {
				for _, value := range raw {
					pixel = pixel<<8 | uint32(value)
				}
			} else {
				for i := len(raw) - 1; i >= 0; i-- {
					pixel = pixel<<8 | uint32(raw[i])
				}
			}
			result.SetRGBA(x, y, color.RGBA{R: colorFromMask(pixel, visual.RedMask), G: colorFromMask(pixel, visual.GreenMask), B: colorFromMask(pixel, visual.BlueMask), A: 255})
		}
	}
	return result, nil
}

func colorFromMask(pixel, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := bits.TrailingZeros32(mask)
	value, maximum := (pixel&mask)>>shift, mask>>shift
	return uint8((uint64(value)*255 + uint64(maximum)/2) / uint64(maximum))
}

type x11ContextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w x11ContextWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(data)
}
