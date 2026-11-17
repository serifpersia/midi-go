//go:build windows

package midi

import (
	"bytes"
	"testing"
)

func TestDecodeShortMessage(t *testing.T) {
	tests := []struct {
		name string
		raw  uintptr
		want []byte
	}{
		{"note on", 0x90 | 0x3C<<8 | 0x64<<16, []byte{0x90, 0x3C, 0x64}},
		{"program change", 0xC0 | 0x05<<8, []byte{0xC0, 0x05}},
		{"cc", 0xB0 | 0x07<<8 | 0x7F<<16, []byte{0xB0, 0x07, 0x7F}},
		{"clock", 0xF8, []byte{0xF8}},
		{"running status ignored", 0x3C, nil},
	}
	for _, tt := range tests {
		if got := decodeShortMessage(tt.raw); !bytes.Equal(got, tt.want) {
			t.Errorf("%s: got % X, want % X", tt.name, got, tt.want)
		}
	}
}
