//go:build windows

package midi

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var winmm = syscall.NewLazyDLL("winmm.dll")

var (
	procMidiInGetNumDevs  = winmm.NewProc("midiInGetNumDevs")
	procMidiInGetDevCapsW = winmm.NewProc("midiInGetDevCapsW")
	procMidiInOpen        = winmm.NewProc("midiInOpen")
	procMidiInStart       = winmm.NewProc("midiInStart")
	procMidiInStop        = winmm.NewProc("midiInStop")
	procMidiInReset       = winmm.NewProc("midiInReset")
	procMidiInClose       = winmm.NewProc("midiInClose")

	procMidiOutGetNumDevs  = winmm.NewProc("midiOutGetNumDevs")
	procMidiOutGetDevCapsW = winmm.NewProc("midiOutGetDevCapsW")
	procMidiOutOpen        = winmm.NewProc("midiOutOpen")
	procMidiOutClose       = winmm.NewProc("midiOutClose")
	procMidiOutShortMsg    = winmm.NewProc("midiOutShortMsg")
)

const (
	maxPnameLen = 32

	callbackFunction = 0x00030000

	mimData = 0x3C3

	mmSysErrNoError = 0
)

type midiInCapsW struct {
	wMid, wPid     uint16
	vDriverVersion uint32
	szPname        [maxPnameLen]uint16
	dwSupport      uint32
}

type midiOutCapsW struct {
	wMid, wPid     uint16
	vDriverVersion uint32
	szPname        [maxPnameLen]uint16
	wTechnology    uint16
	wVoices        uint16
	wNotes         uint16
	wChannelMask   uint16
	dwSupport      uint32
}

var mmErrNames = map[uint32]string{
	1:  "error",
	2:  "bad device id",
	3:  "not enabled",
	4:  "already allocated",
	5:  "invalid handle",
	6:  "no driver",
	7:  "no memory",
	8:  "not supported",
	9:  "bad error number",
	10: "invalid element",
	11: "not found",
}

func mmErr(op string, code uintptr) error {
	c := uint32(code)
	if c == mmSysErrNoError {
		return nil
	}
	if name, ok := mmErrNames[c]; ok {
		return fmt.Errorf("%s: %s (MMSYSERR %d)", op, name, c)
	}
	return fmt.Errorf("%s: MMSYSERR %d", op, c)
}

func ListInputs() ([]Port, error) {
	n, _, _ := procMidiInGetNumDevs.Call()
	return listPorts(n, func(i uintptr) (string, error) {
		var caps midiInCapsW
		r, _, _ := procMidiInGetDevCapsW.Call(i, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps))
		if err := mmErr("midiInGetDevCapsW", r); err != nil {
			return "", err
		}
		return syscall.UTF16ToString(caps.szPname[:]), nil
	})
}

func ListOutputs() ([]Port, error) {
	n, _, _ := procMidiOutGetNumDevs.Call()
	return listPorts(n, func(i uintptr) (string, error) {
		var caps midiOutCapsW
		r, _, _ := procMidiOutGetDevCapsW.Call(i, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps))
		if err := mmErr("midiOutGetDevCapsW", r); err != nil {
			return "", err
		}
		return syscall.UTF16ToString(caps.szPname[:]), nil
	})
}

func listPorts(n uintptr, name func(uintptr) (string, error)) ([]Port, error) {
	if n == 0 {
		return nil, nil
	}
	ports := make([]Port, 0, n)
	for i := uintptr(0); i < n; i++ {
		s, err := name(i)
		if err != nil || s == "" {
			continue
		}
		ports = append(ports, Port{ID: int(i), Name: s})
	}
	return ports, nil
}

type In struct {
	h    uintptr
	id   uintptr
	msgs chan []byte

	closeOnce sync.Once
	closeErr  error
}

var (
	insMu    sync.RWMutex
	ins      = map[uintptr]*In{}
	nextInID atomic.Uintptr
)

var midiInCallback = syscall.NewCallback(func(_ uintptr, msg, instance, param1, _ uintptr) uintptr {
	if uint32(msg) != mimData {
		return 0
	}
	insMu.RLock()
	in := ins[instance]
	if in != nil {
		select {
		case in.msgs <- decodeShortMessage(param1):
		default:
		}
	}
	insMu.RUnlock()
	return 0
})

func decodeShortMessage(p uintptr) []byte {
	status := byte(p)
	if status < 0x80 {
		return nil
	}
	n := 1
	switch status & 0xF0 {
	case 0x80, 0x90, 0xA0, 0xB0, 0xE0:
		n = 3
	case 0xC0, 0xD0:
		n = 2
	}
	msg := make([]byte, n)
	msg[0] = status
	if n >= 2 {
		msg[1] = byte(p >> 8)
	}
	if n == 3 {
		msg[2] = byte(p >> 16)
	}
	return msg
}

func OpenInput(port int) (*In, error) {
	in := &In{msgs: make(chan []byte, 256)}
	id := nextInID.Add(1)
	in.id = id

	var h uintptr
	r, _, _ := procMidiInOpen.Call(
		uintptr(unsafe.Pointer(&h)),
		uintptr(port),
		midiInCallback,
		id,
		callbackFunction,
	)
	if err := mmErr("midiInOpen", r); err != nil {
		return nil, err
	}
	in.h = h

	insMu.Lock()
	ins[id] = in
	insMu.Unlock()

	if r, _, _ := procMidiInStart.Call(h); mmErr("midiInStart", r) != nil {
		insMu.Lock()
		delete(ins, id)
		insMu.Unlock()
		procMidiInClose.Call(h)
		return nil, mmErr("midiInStart", r)
	}
	return in, nil
}

func (in *In) Messages() <-chan []byte {
	return in.msgs
}

func (in *In) Close() error {
	in.closeOnce.Do(func() {
		insMu.Lock()
		delete(ins, in.id)
		insMu.Unlock()

		procMidiInStop.Call(in.h)
		procMidiInReset.Call(in.h)
		if r, _, _ := procMidiInClose.Call(in.h); mmErr("midiInClose", r) != nil {
			in.closeErr = mmErr("midiInClose", r)
		}
		close(in.msgs)
	})
	return in.closeErr
}

type Out struct {
	h      uintptr
	mu     sync.Mutex
	closed bool
}

func OpenOutput(port int) (*Out, error) {
	var h uintptr
	r, _, _ := procMidiOutOpen.Call(uintptr(unsafe.Pointer(&h)), uintptr(port), 0, 0, 0)
	if err := mmErr("midiOutOpen", r); err != nil {
		return nil, err
	}
	return &Out{h: h}, nil
}

func (o *Out) Send(msg []byte) error {
	if len(msg) == 0 || len(msg) > 3 {
		return fmt.Errorf("midi: short message must be 1-3 bytes, got %d", len(msg))
	}
	var packed uint32
	for i, b := range msg {
		packed |= uint32(b) << (8 * uint(i))
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return errors.New("midi: output is closed")
	}
	r, _, _ := procMidiOutShortMsg.Call(o.h, uintptr(packed))
	return mmErr("midiOutShortMsg", r)
}

func (o *Out) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil
	}
	o.closed = true
	r, _, _ := procMidiOutClose.Call(o.h)
	return mmErr("midiOutClose", r)
}
