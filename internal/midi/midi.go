// Package midi provides native MIDI device enumeration and I/O.
package midi

import "errors"

type Port struct {
	ID   int
	Name string
}

var ErrUnsupported = errors.New("midi: not supported on this platform")
