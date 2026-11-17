# midi-go

Native MIDI router and joystick-to-MIDI mapper for Windows.

## Features
- **Router:** infinite pan/zoom canvas, add MIDI IN/OUT nodes, drag cables to route, live activity, drag-select & delete nodes, per-port shared open/close.
- **Mapper:** joystick input nodes, input visualizer, converter (axis→CC / button→note or CC, channel, CC/note, invert, in/out range with hold-to-repeat, 0–255 normalized input), MIDI OUT; rename converter labels; activity on converter→MIDI out.
- **Presets:** save/load/autosave router + mapper layouts.

## Build & run
Requires [Go](https://go.dev/dl/) 1.25+ (1.27 tested).

```bat
build.bat            :: release -> dist\midi-go-windows64.exe
build.bat debug      :: debug   -> dist\midi-go-debug.exe
```
`build.bat` checks for Go and prompts to install/retry if missing.

Or manually:
```sh
go run .
go build -trimpath -ldflags "-H=windowsgui -s -w" -o dist/midi-go-windows64.exe .
```

## Download
Get the latest Windows 64-bit build from [Releases](https://github.com/serifpersia/midi-go/releases/latest) (`midi-go-windows64.exe`).

## License

Distributed under the terms of the [MIT License](LICENSE).
