# X11 screenshot fallback

The `computer` X11 backend first uses ImageMagick `import`. If that command is
missing or fails and the caller has not cancelled, it tries native X11
`GetImage` over the local Unix socket. Input still uses `xdotool`; window
selection and window bounds also retain their existing `xdotool` dependency.

## Supported native capture

- Linux local displays such as `:0`, `:0.1` and `unix/:0`.
- Root-screen screenshots and the existing clipped window rectangle.
- TrueColor root visuals with 8, 16, 24 or 32 bits per pixel, including server
  byte order, scanline padding and RGB masks.
- `MIT-MAGIC-COOKIE-1` from `XAUTHORITY` or `~/.Xauthority`. Missing authority
  files allow an unauthenticated connection if the X server permits it.

The fallback has a five-second budget, closes its connection on cancellation,
and limits the requested image to 64 Mi pixels. PNG encoding checks cancellation
as well. Xauthority must be a regular file no larger than 1 MiB. Unsupported
visuals, image formats and remote DISPLAY values return an error.

Remote X11 displays continue to require ImageMagick. Native fallback never
switches to Windows capture; the separate WSLg backend owns that desktop and
coordinate system. No new LuckyAgent configuration field is needed. The new
Go dependency is `github.com/jezek/xgb`; the native path itself requires neither
ImageMagick nor a new C library.

The backend's capability report does not prove a live X server is reachable.
Desktop capture still requires the running user's display/socket permissions.
Build checks alone do not verify capture against a real desktop.
