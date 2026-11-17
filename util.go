package main

import "github.com/hajimehoshi/ebiten/v2/text/v2"

func (g *game) fitText(s string, maxW float64) string {
	if maxW <= 0 {
		return ""
	}
	if text.Advance(s, g.face) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		t := string(r) + ".."
		if text.Advance(t, g.face) <= maxW {
			return t
		}
	}
	return ".."
}
