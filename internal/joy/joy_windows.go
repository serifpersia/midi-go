//go:build windows

package joy

import (
	"fmt"
	"syscall"
	"unsafe"
)

var winmm = syscall.NewLazyDLL("winmm.dll")

var (
	procJoyGetNumDevs = winmm.NewProc("joyGetNumDevs")
	procJoyGetDevCaps = winmm.NewProc("joyGetDevCapsW")
	procJoyGetPosEx   = winmm.NewProc("joyGetPosEx")
)

const (
	joyReturnAll = 0x000000FF
	joyPovCentered = 0xFFFF
)

type joyCaps struct {
	wMid           uint16
	wPid           uint16
	szPname        [32]uint16
	xMin           uint32
	xMax           uint32
	yMin           uint32
	yMax           uint32
	zMin           uint32
	zMax           uint32
	wNumButtons    uint32
	wPeriodMin     uint32
	wPeriodMax     uint32
	rMin           uint32
	rMax           uint32
	uMin           uint32
	uMax           uint32
	vMin           uint32
	vMax           uint32
	wCaps          uint32
	wMaxAxes       uint32
	wNumAxes       uint32
	wMaxButtons    uint32
	szRegKey       [32]uint16
	szOEMVxD       [260]uint16
}

type joyInfoEx struct {
	dwSize    uint32
	dwFlags   uint32
	dwXpos    uint32
	dwYpos    uint32
	dwZpos    uint32
	dwRpos    uint32
	dwUpos    uint32
	dwVpos    uint32
	dwButtons uint32
	dwButtonNumber uint32
	dwPOV     uint32
	dwReserved1 uint32
	dwReserved2 uint32
}

type Device struct {
	ID   int
	Name string
	Caps Caps
}

type Caps struct {
	NumAxes    int
	NumButtons int
}

type State struct {
	Axes    []uint32
	Buttons uint32
	POV     uint32
	Connected bool
}

func List() []Device {
	n, _, _ := procJoyGetNumDevs.Call()
	var out []Device
	for i := uintptr(0); i < n; i++ {
		var caps joyCaps
		r, _, _ := procJoyGetDevCaps.Call(i, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps))
		if r != 0 {
			continue
		}
		name := syscall.UTF16ToString(caps.szPname[:])
		if name == "" {
			name = fmt.Sprintf("Joystick %d", i)
		}
		out = append(out, Device{
			ID: int(i), Name: name,
			Caps: Caps{NumAxes: int(caps.wNumAxes), NumButtons: int(caps.wNumButtons)},
		})
	}
	return out
}

func Read(id int) State {
	var info joyInfoEx
	info.dwSize = uint32(unsafe.Sizeof(info))
	info.dwFlags = joyReturnAll
	r, _, _ := procJoyGetPosEx.Call(uintptr(id), uintptr(unsafe.Pointer(&info)))
	if r != 0 {
		return State{}
	}
	axes := []uint32{info.dwXpos, info.dwYpos, info.dwZpos, info.dwRpos, info.dwUpos, info.dwVpos}
	return State{Axes: axes, Buttons: info.dwButtons, POV: info.dwPOV, Connected: true}
}
