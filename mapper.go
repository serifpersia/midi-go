package main

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/serifpersia/midi-go/internal/joy"
	"github.com/serifpersia/midi-go/internal/midi"
)

type mapKind int

const (
	mapJoy mapKind = iota
	mapViz
	mapConv
	mapMidiOut
)

type mapNode struct {
	kind mapKind
	name string
	x, y float64
	w, h float64

	joyID int
	prev  joy.State
	last  string

	port    midi.Port
	midiOut *midi.Out
	open    bool

	expanded bool
	conv     convCfg
	glow     float64
}


func (n *mapNode) baseH() float64 { return nodeHeight }
func (n *mapNode) height() float64 {
	if n.kind == mapConv && n.expanded {
		return nodeHeight + 8 + 9*24 + 10
	}
	return nodeHeight
}
func (n *mapNode) hasOut() bool { return n.kind != mapViz && n.kind != mapMidiOut }
func (n *mapNode) hasIn() bool  { return n.kind != mapJoy }
func (n *mapNode) inPos() (float64, float64)  { return n.x, n.y + n.baseH()/2 }
func (n *mapNode) outPos() (float64, float64) { return n.x + n.w, n.y + n.baseH()/2 }
func (n *mapNode) contains(x, y float64) bool {
	return x >= n.x && x <= n.x+n.w && y >= n.y && y <= n.y+n.height()
}

type mapConn struct{ from, to *mapNode }

type mapMenuItem struct {
	label string
	kind  mapKind
	joy   joy.Device
	port  midi.Port
	sep   bool
}

func (g *game) initMapper() {
	if g.mapSelected == nil {
		g.mapSelected = map[*mapNode]bool{}
	}
	if g.joyDevices == nil {
		g.joyDevices = joy.List()
	}
}

func (g *game) updateMapper(leftDown, leftUp, rightDown bool) {
	wx, wy := g.screenToWorld(g.mouseX, g.mouseY)
	mid := ebiten.IsMouseButtonPressed(ebiten.MouseButtonMiddle)

	if g.mapMenuOpen {
		if leftDown && !g.clickMapMenu(g.mouseX, g.mouseY) {
			g.mapMenuOpen = false
		}
		if rightDown {
			g.mapMenuOpen = false
		}
		return
	}

	if g.mouseY > tabBarHeight && !g.panning && g.mapDragging == nil && g.mapDragSrc == nil && g.mapDragDst == nil && !g.selecting {
		if _, wyh := ebiten.Wheel(); wyh != 0 {
			old := g.zoom
			g.zoom = clamp(g.zoom*math.Pow(1.1, wyh), 0.2, 4)
			r := g.zoom / old
			g.camX = wx - (wx-g.camX)/r
			g.camY = wy - (wy-g.camY)/r
			wx, wy = g.screenToWorld(g.mouseX, g.mouseY)
		}
	}

	if mid && g.mouseY > tabBarHeight && !g.panning && g.mapDragging == nil && g.mapDragSrc == nil && g.mapDragDst == nil && !g.selecting {
		g.panning = true
		g.panLastX, g.panLastY = g.mouseX, g.mouseY
	}
	if !mid {
		g.panning = false
	}

	if leftDown && g.mouseY > tabBarHeight && !mid {
		g.onMapPress(wx, wy)
	}
	if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		g.mapConvHoldNode = nil
	}
	if leftUp {
		if g.selecting {
			g.finishMapSelection()
		}
		g.onMapRelease(wx, wy)
	}
	if rightDown && g.mouseY > tabBarHeight {
		g.mapEditing = nil
		if n := g.mapNodeAt(wx, wy); n != nil {
			g.deleteMapNodes([]*mapNode{n})
		} else if i := g.mapCableAt(wx, wy); i >= 0 {
			g.mapRemoveConnAt(i)
		} else {
			g.openMapMenu(g.mouseX, g.mouseY, wx, wy)
		}
	}

	if g.panning {
		g.camX += (g.panLastX - g.mouseX) / g.zoom
		g.camY += (g.panLastY - g.mouseY) / g.zoom
		g.panLastX, g.panLastY = g.mouseX, g.mouseY
	}
	if g.selecting {
		g.selX2, g.selY2 = wx, wy
	}
	if g.mapDragging != nil {
		n := g.mapDragging
		n.x = wx - g.mapDragDX
		n.y = wy - g.mapDragDY
	}
	g.updateConvHold(wx, wy)
	if g.mapEditing != nil {
		g.updateMapEditing()
	}
	if g.mapEditing == nil && !g.presetNameFocused && (inpututil.IsKeyJustPressed(ebiten.KeyDelete) || inpututil.IsKeyJustPressed(ebiten.KeyBackspace)) {
		g.deleteMapSelected()
	}
}

func (g *game) pollJoy() {
	states := map[int]joy.State{}
	for _, n := range g.mapNodes {
		if n.kind != mapJoy {
			continue
		}
		s, ok := states[n.joyID]
		if !ok {
			s = joy.Read(n.joyID)
			states[n.joyID] = s
		}
		if !s.Connected {
			n.prev = s
			continue
		}
		if n.prev.Connected {
			evs := diffJoyEvs(n.prev, s)
			if len(evs) > 0 {
				n.last = evs[len(evs)-1].label
				for _, ev := range evs {
					g.propagateJoy(n, ev)
				}
			}
		}
		n.prev = s
	}
}

func diffJoyEvs(prev, cur joy.State) []joyEvent {
	var evs []joyEvent
	for i := 0; i < len(cur.Axes) && i < len(prev.Axes); i++ {
		d := int64(cur.Axes[i]) - int64(prev.Axes[i])
		if d > 1500 || d < -1500 {
			evs = append(evs, joyEvent{label: fmt.Sprintf("Axis %d: %d", i, cur.Axes[i]), axis: i, value: cur.Axes[i]})
		}
	}
	if cur.Buttons != prev.Buttons {
		diff := cur.Buttons ^ prev.Buttons
		for b := 0; b < 32; b++ {
			if diff&(1<<uint(b)) != 0 {
				on := cur.Buttons&(1<<uint(b)) != 0
				lbl := fmt.Sprintf("Btn %d: off", b+1)
				if on {
					lbl = fmt.Sprintf("Btn %d: on", b+1)
				}
				evs = append(evs, joyEvent{label: lbl, button: b, isBtn: true, on: on, value: cur.Buttons})
			}
		}
	}
	if cur.POV != prev.POV {
		evs = append(evs, joyEvent{label: fmt.Sprintf("POV: %d", cur.POV), value: cur.POV})
	}
	return evs
}

func (g *game) propagateJoy(from *mapNode, ev joyEvent) {
	for _, c := range g.mapConns {
		if c.from == from {
			g.sendToMapNode(c.to, ev)
		}
	}
}

func (g *game) sendToMapNode(n *mapNode, ev joyEvent) {
	switch n.kind {
	case mapViz:
		n.last = ev.label
	case mapConv:
		msg, ok := convJoyToMidi(ev, n.conv)
		if !ok {
			return
		}
		n.glow = 1
		for _, c := range g.mapConns {
			if c.from == n {
				g.sendMidiToMapNode(c.to, msg)
			}
		}
	}
}

func (g *game) sendMidiToMapNode(n *mapNode, msg []byte) {
	if n.kind != mapMidiOut || n.midiOut == nil {
		return
	}
	if err := n.midiOut.Send(msg); err == nil {
		n.glow = 1
	}
}

func convJoyToMidi(ev joyEvent, c convCfg) ([]byte, bool) {
	ch := c.Ch
	if ch < 1 || ch > 16 {
		ch = 1
	}
	st := byte(ch - 1)
	if c.UseAxis {
		if ev.isBtn || ev.axis != c.Index {
			return nil, false
		}
		v := int((uint64(ev.value) * 255) / 65535)
		if v < c.InMin || v > c.InMax {
			return nil, false
		}
		out := scaleRange(v, c.InMin, c.InMax, c.OutMin, c.OutMax, c.Inv)
		return []byte{0xB0 | st, byte(clampInt(c.Num, 0, 127)), byte(out)}, true
	}
	if !ev.isBtn || ev.button != c.Index {
		return nil, false
	}
	num := byte(clampInt(c.Num, 0, 127))
	if c.IsCC {
		v := byte(clampInt(c.OutMin, 0, 127))
		if ev.on {
			v = byte(clampInt(c.OutMax, 0, 127))
		}
		return []byte{0xB0 | st, num, v}, true
	}
	if ev.on {
		return []byte{0x90 | st, num, byte(clampInt(c.OutMax, 1, 127))}, true
	}
	return []byte{0x80 | st, num, 0}, true
}

func scaleRange(v, inMin, inMax, outMin, outMax int, inv bool) int {
	if inMax <= inMin {
		inMax = inMin + 1
	}
	if v < inMin {
		v = inMin
	}
	if v > inMax {
		v = inMax
	}
	r := float64(v-inMin) / float64(inMax-inMin)
	if inv {
		r = 1 - r
	}
	o := outMin + int(math.Round(r*float64(outMax-outMin)))
	return clampInt(o, 0, 127)
}
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (g *game) openMapMenu(sx, sy, wx, wy float64) {
	items := []mapMenuItem{{label: "Add Joystick", sep: true}}
	g.joyDevices = joy.List()
	for _, d := range g.joyDevices {
		items = append(items, mapMenuItem{label: d.Name, kind: mapJoy, joy: d})
	}
	if len(g.joyDevices) == 0 {
		items = append(items, mapMenuItem{label: "(no joysticks)", sep: true})
	}
	items = append(items, mapMenuItem{label: "Add Node", sep: true})
	items = append(items, mapMenuItem{label: "Input Visualizer", kind: mapViz})
	items = append(items, mapMenuItem{label: "Converter", kind: mapConv})
	items = append(items, mapMenuItem{label: "Add MIDI OUT", sep: true})
	for _, p := range g.availOut {
		items = append(items, mapMenuItem{label: p.Name, kind: mapMidiOut, port: p})
	}
	if len(g.availOut) == 0 {
		items = append(items, mapMenuItem{label: "(no MIDI outputs)", sep: true})
	}
	g.mapMenuItems = items
	g.menuX, g.menuY = sx, sy
	g.menuWorldX, g.menuWorldY = wx, wy
	g.mapMenuOpen = true
}

func mapMenuLayout(items []mapMenuItem, x, y float64) (rect, []rect) {
	w := 260.0
	h := 8.0
	rects := make([]rect, len(items))
	ry := y + 8
	for i, it := range items {
		rh := 28.0
		if it.sep {
			rh = 22.0
		}
		rects[i] = rect{x, ry, w, rh}
		ry += rh
		h += rh
	}
	h += 6
	if y+h > screenHeight {
		dy := screenHeight - h - y
		y += dy
		for i := range rects {
			rects[i].y += dy
		}
	}
	if x+w > screenWidth {
		dx := screenWidth - w - x
		x += dx
		for i := range rects {
			rects[i].x += dx
		}
	}
	return rect{x, y, w, h}, rects
}

func (g *game) clickMapMenu(sx, sy float64) bool {
	box, rects := mapMenuLayout(g.mapMenuItems, g.menuX, g.menuY)
	if !box.hit(sx, sy) {
		return false
	}
	for i, r := range rects {
		if r.hit(sx, sy) && !g.mapMenuItems[i].sep && g.mapMenuItems[i].label[0] != '(' {
			it := g.mapMenuItems[i]
			n := &mapNode{kind: it.kind, x: g.menuWorldX, y: g.menuWorldY, w: nodeWidth, h: nodeHeight}
			switch it.kind {
			case mapJoy:
				n.name = it.joy.Name
				n.joyID = it.joy.ID
			case mapViz:
				n.name = "Visualizer"
				n.last = "(none)"
			case mapConv:
				n.name = "Converter"
				n.conv = defaultConv()
			case mapMidiOut:
				n.name = it.port.Name
				n.port = it.port
			}
			g.mapNodes = append(g.mapNodes, n)
			g.mapMenuOpen = false
			return true
		}
	}
	return true
}

func (g *game) onMapPress(x, y float64) {
	if n := g.mapOutPortAt(x, y); n != nil {
		g.mapEditing = nil
		if i, ok := g.lastMapConnFrom(n); ok {
			dst := g.mapConns[i].to
			g.mapRemoveConnAt(i)
			g.mapDragDst = dst
			return
		}
		g.mapDragSrc = n
		return
	}
	if n := g.mapInPortAt(x, y); n != nil {
		g.mapEditing = nil
		if i, ok := g.lastMapConnTo(n); ok {
			src := g.mapConns[i].from
			g.mapRemoveConnAt(i)
			g.mapDragSrc = src
			return
		}
		g.mapDragDst = n
		return
	}
	if n := g.mapNodeAt(x, y); n != nil {
		if g.mapEditing != nil && g.mapEditing != n {
			g.mapEditing = nil
		}
		g.mapSelected = map[*mapNode]bool{n: true}
		if n.kind == mapConv {
			if r := (rect{x: n.x + 8, y: n.y + 26, w: n.w - 16, h: 20}); r.hit(x, y) {
				g.mapEditing = n
				g.mapPressNode = nil
				return
			}
			if n.expanded && g.clickConvUI(n, x, y) {
				g.mapEditing = nil
				return
			}
		} else {
			g.mapEditing = nil
		}
		g.mapDragging = n
		g.mapDragDX = x - n.x
		g.mapDragDY = y - n.y
		g.mapPressNode = n
		g.mapPressX, g.mapPressY = x, y
		return
	}
	g.mapEditing = nil
	if i := g.mapCableAt(x, y); i >= 0 {
		src := g.mapConns[i].from
		g.mapRemoveConnAt(i)
		g.mapDragSrc = src
		return
	}
	g.mapSelected = map[*mapNode]bool{}
	g.selecting = true
	g.selX1, g.selY1 = x, y
	g.selX2, g.selY2 = x, y
}

func (g *game) updateMapEditing() {
	n := g.mapEditing
	if n == nil {
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		if strings.TrimSpace(n.name) == "" {
			n.name = "Converter"
		}
		g.mapEditing = nil
		g.dirty = true
		return
	}
	if ebiten.IsKeyPressed(ebiten.KeyBackspace) {
		g.backspaceHold++
		if g.backspaceHold == 1 || (g.backspaceHold > 24 && g.backspaceHold%3 == 0) {
			n.name = dropLastRune(n.name)
			g.dirty = true
		}
	} else {
		g.backspaceHold = 0
	}
	for _, r := range ebiten.AppendInputChars(nil) {
		if r >= 0x20 && r != 0x7f && len([]rune(n.name)) < 32 {
			n.name += string(r)
			g.dirty = true
		}
	}
}

func (g *game) onMapRelease(x, y float64) {
	if g.mapDragSrc != nil {
		if t := g.mapInPortAt(x, y); t != nil {
			g.mapConnect(g.mapDragSrc, t)
		}
		g.mapDragSrc = nil
	}
	if g.mapDragDst != nil {
		if s := g.mapOutPortAt(x, y); s != nil {
			g.mapConnect(s, g.mapDragDst)
		}
		g.mapDragDst = nil
	}
	if g.mapDragging != nil && g.mapPressNode == g.mapDragging && math.Hypot(x-g.mapPressX, y-g.mapPressY) < 3 && g.mapDragging.kind == mapConv {
		g.mapDragging.expanded = !g.mapDragging.expanded
	}
	g.mapDragging = nil
	g.mapPressNode = nil
}

func (g *game) mapConnect(from, to *mapNode) {
	if !from.hasOut() || !to.hasIn() || from == to {
		return
	}
	for _, c := range g.mapConns {
		if c.from == from && c.to == to {
			return
		}
	}
	if to.kind == mapMidiOut && !to.open {
		out, err := midi.AcquireOutput(to.port.ID)
		if err != nil {
			g.status = fmt.Sprintf("open %q: %v", to.port.Name, err)
			return
		}
		to.midiOut, to.open = out, true
	}
	g.mapConns = append(g.mapConns, mapConn{from: from, to: to})
	if from.last != "" && to.kind == mapViz {
		to.last = from.last
	}
}

func (g *game) lastMapConnFrom(n *mapNode) (int, bool) {
	for i := len(g.mapConns) - 1; i >= 0; i-- {
		if g.mapConns[i].from == n {
			return i, true
		}
	}
	return -1, false
}
func (g *game) lastMapConnTo(n *mapNode) (int, bool) {
	for i := len(g.mapConns) - 1; i >= 0; i-- {
		if g.mapConns[i].to == n {
			return i, true
		}
	}
	return -1, false
}
func (g *game) mapRemoveConnAt(i int) {
	to := g.mapConns[i].to
	g.mapConns = append(g.mapConns[:i], g.mapConns[i+1:]...)
	g.dropIdleMapOut(to)
}
func (g *game) dropIdleMapOut(n *mapNode) {
	if n == nil || n.kind != mapMidiOut {
		return
	}
	if _, ok := g.lastMapConnTo(n); ok {
		return
	}
	if n.open {
		midi.ReleaseOutput(n.port.ID)
		n.midiOut = nil
		n.open = false
	}
}

func (g *game) finishMapSelection() {
	x1, x2 := g.selX1, g.selX2
	if x1 > x2 { x1, x2 = x2, x1 }
	y1, y2 := g.selY1, g.selY2
	if y1 > y2 { y1, y2 = y2, y1 }
	if math.Abs(x2-x1) < 2 && math.Abs(y2-y1) < 2 {
		g.selecting = false
		return
	}
	g.mapSelected = map[*mapNode]bool{}
	for _, n := range g.mapNodes {
		nx2, ny2 := n.x+n.w, n.y+n.height()
		if n.x <= x2 && nx2 >= x1 && n.y <= y2 && ny2 >= y1 {
			g.mapSelected[n] = true
		}
	}
	g.selecting = false
}

func (g *game) deleteMapSelected() {
	if len(g.mapSelected) == 0 {
		return
	}
	nodes := make([]*mapNode, 0, len(g.mapSelected))
	for n := range g.mapSelected {
		nodes = append(nodes, n)
	}
	g.deleteMapNodes(nodes)
}
func (g *game) deleteMapNodes(nodes []*mapNode) {
	if len(nodes) == 0 {
		return
	}
	del := make(map[*mapNode]bool, len(nodes))
	for _, n := range nodes {
		del[n] = true
		if n.kind == mapMidiOut && n.open {
			midi.ReleaseOutput(n.port.ID)
			n.midiOut = nil
			n.open = false
		}
	}
	kept := g.mapConns[:0]
	for _, c := range g.mapConns {
		if !del[c.from] && !del[c.to] {
			kept = append(kept, c)
		}
	}
	g.mapConns = kept
	keptN := g.mapNodes[:0]
	for _, n := range g.mapNodes {
		if !del[n] {
			keptN = append(keptN, n)
		}
	}
	g.mapNodes = keptN
	g.mapSelected = map[*mapNode]bool{}
	if g.mapDragging != nil && del[g.mapDragging] {
		g.mapDragging = nil
	}
}

func (g *game) mapNodeAt(x, y float64) *mapNode {
	for i := len(g.mapNodes) - 1; i >= 0; i-- {
		if g.mapNodes[i].contains(x, y) {
			return g.mapNodes[i]
		}
	}
	return nil
}
func (g *game) mapOutPortAt(x, y float64) *mapNode {
	for i := len(g.mapNodes) - 1; i >= 0; i-- {
		n := g.mapNodes[i]
		if !n.hasOut() { continue }
		px, py := n.outPos()
		if math.Hypot(px-x, py-y) <= hitRadius { return n }
	}
	return nil
}
func (g *game) mapInPortAt(x, y float64) *mapNode {
	for i := len(g.mapNodes) - 1; i >= 0; i-- {
		n := g.mapNodes[i]
		if !n.hasIn() { continue }
		px, py := n.inPos()
		if math.Hypot(px-x, py-y) <= hitRadius { return n }
	}
	return nil
}
func (g *game) mapCableAt(x, y float64) int {
	for i, c := range g.mapConns {
		sx, sy := c.from.outPos()
		tx, ty := c.to.inPos()
		if distToCurve(sx, sy, tx, ty, x, y) < 8/g.zoom {
			return i
		}
	}
	return -1
}

type convField struct {
	name string
	r    rect
}

func convRows(n *mapNode) []convField {
	x := n.x + 8
	w := n.w - 16
	y := n.y + n.baseH() + 6
	rh := 22.0
	step := 24.0
	rows := []string{"src", "idx", "ch", "num", "inv", "inmin", "inmax", "outmin", "outmax"}
	out := make([]convField, 0, len(rows))
	for _, name := range rows {
		out = append(out, convField{name, rect{x, y, w, rh}})
		y += step
	}
	return out
}

func (g *game) clickConvUI(n *mapNode, x, y float64) bool {
	for _, f := range convRows(n) {
		if !f.r.hit(x, y) { continue }
		l := rect{f.r.x, f.r.y, 22, f.r.h}
		r := rect{f.r.x + f.r.w - 22, f.r.y, 22, f.r.h}
		d := 0
		side := 0
		if r.hit(x, y) { d = 1; side = 1 }
		if l.hit(x, y) { d = -1; side = -1 }
		if d == 0 {
			if f.name == "src" { n.conv.UseAxis = !n.conv.UseAxis; g.dirty = true }
			if f.name == "inv" { n.conv.Inv = !n.conv.Inv; g.dirty = true }
			return true
		}
		g.applyConvStep(n, f.name, d, side, x)
		g.mapConvHoldNode = n
		g.mapConvHoldField = f.name
		g.mapConvHoldD = d
		g.mapConvHoldSide = side
		g.mapConvHoldTicks = 0
		g.dirty = true
		return true
	}
	return false
}

func (g *game) applyConvStep(n *mapNode, field string, d, side int, x float64) {
	switch field {
	case "src":
		n.conv.UseAxis = !n.conv.UseAxis
	case "idx":
		n.conv.Index = clampInt(n.conv.Index+d, 0, 31)
	case "ch":
		n.conv.Ch = clampInt(n.conv.Ch+d, 1, 16)
	case "num":
		n.conv.Num = clampInt(n.conv.Num+d, 0, 127)
	case "inv":
		n.conv.Inv = !n.conv.Inv
	case "inmin":
		n.conv.InMin = clampInt(n.conv.InMin+d, 0, 254)
	case "inmax":
		n.conv.InMax = clampInt(n.conv.InMax+d, 1, 255)
	case "outmin":
		n.conv.OutMin = clampInt(n.conv.OutMin+d, 0, 127)
	case "outmax":
		n.conv.OutMax = clampInt(n.conv.OutMax+d, 0, 127)
	}
	if n.conv.InMax <= n.conv.InMin { n.conv.InMax = n.conv.InMin + 1 }
}

func (g *game) updateConvHold(wx, wy float64) {
	n := g.mapConvHoldNode
	if n == nil || !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		g.mapConvHoldNode = nil
		return
	}
	g.mapConvHoldTicks++
	if g.mapConvHoldTicks < 18 || g.mapConvHoldTicks%4 != 0 {
		return
	}
	for _, f := range convRows(n) {
		if f.name != g.mapConvHoldField || !f.r.hit(wx, wy) { continue }
		l := rect{f.r.x, f.r.y, 22, f.r.h}
		r := rect{f.r.x + f.r.w - 22, f.r.y, 22, f.r.h}
		if (g.mapConvHoldD > 0 && r.hit(wx, wy)) || (g.mapConvHoldD < 0 && l.hit(wx, wy)) {
			g.applyConvStep(n, f.name, g.mapConvHoldD, g.mapConvHoldSide, wx)
			g.dirty = true
		}
		return
	}
}

func (g *game) drawMapper(dst *ebiten.Image) {
	g.initMapper()
	g.drawGrid(dst)
	wx, wy := g.screenToWorld(g.mouseX, g.mouseY)
	for _, c := range g.mapConns {
		sx, sy := c.from.outPos()
		tx, ty := c.to.inPos()
		g.drawWorldBezier(dst, sx, sy, tx, ty, colCable, 2)
	}
	if g.mapDragSrc != nil {
		px, py := g.mapDragSrc.outPos()
		sx, sy := g.worldToScreen(px, py)
		g.drawScreenBezier(dst, sx, sy, g.mouseX, g.mouseY, colOutPort, 2)
	}
	if g.mapDragDst != nil {
		px, py := g.mapDragDst.inPos()
		sx, sy := g.worldToScreen(px, py)
		g.drawScreenBezier(dst, g.mouseX, g.mouseY, sx, sy, colInPort, 2)
	}
	for _, n := range g.mapNodes {
		g.drawMapNode(dst, n, wx, wy)
	}
	if g.selecting {
		x1, y1 := g.worldToScreen(g.selX1, g.selY1)
		x2, y2 := g.worldToScreen(g.selX2, g.selY2)
		if x1 > x2 { x1, x2 = x2, x1 }
		if y1 > y2 { y1, y2 = y2, y1 }
		vector.DrawFilledRect(dst, float32(x1), float32(y1), float32(x2-x1), float32(y2-y1), color.NRGBA{R: 0x6c, G: 0xb6, B: 0xff, A: 0x18}, false)
		vector.StrokeRect(dst, float32(x1), float32(y1), float32(x2-x1), float32(y2-y1), 1, colInPort, false)
	}
	if g.mapMenuOpen {
		box, rects := mapMenuLayout(g.mapMenuItems, g.menuX, g.menuY)
		vector.DrawFilledRect(dst, float32(box.x), float32(box.y), float32(box.w), float32(box.h), colMenu, false)
		vector.StrokeRect(dst, float32(box.x), float32(box.y), float32(box.w), float32(box.h), 1, colBorder, false)
		for i, r := range rects {
			it := g.mapMenuItems[i]
			if it.sep {
				g.drawText(dst, it.label, r.x+10, r.y+2, colMuted)
				vector.StrokeLine(dst, float32(r.x+8), float32(r.y+r.h-2), float32(r.x+r.w-8), float32(r.y+r.h-2), 1, colBorder, false)
				continue
			}
			if r.hit(g.mouseX, g.mouseY) {
				vector.DrawFilledRect(dst, float32(r.x+4), float32(r.y), float32(r.w-8), float32(r.h), colMenuHi, false)
			}
			g.drawText(dst, truncate(it.label, 34), r.x+14, r.y+4, colText)
		}
	}
}

func (g *game) drawMapNode(dst *ebiten.Image, n *mapNode, wx, wy float64) {
	sx, sy := g.worldToScreen(n.x, n.y)
	w, h := n.w*g.zoom, n.height()*g.zoom
	nodeHover := n.contains(wx, wy)
	fill := colNode
	if nodeHover { fill = mix(colNode, colButton, 0.35) }
	vector.DrawFilledRect(dst, float32(sx), float32(sy), float32(w), float32(h), fill, true)
	head, label, glowCol := colInHead, "JOYSTICK", colOutPort
	switch n.kind {
	case mapViz:
		head, label, glowCol = colOutHead, "VISUALIZER", colInPort
	case mapConv:
		head, label, glowCol = colInHead, "CONVERTER", colInPort
	case mapMidiOut:
		head, label, glowCol = colOutHead, "MIDI OUT", colOutPort
	}
	if nodeHover { head = mix(head, colInPort, 0.15) }
	vector.DrawFilledRect(dst, float32(sx), float32(sy), float32(w), float32(24*g.zoom), head, true)
	hi := n.glow
	border := mix(colBorder, glowCol, hi)
	bw := float32((1.5 + 2*hi) * g.zoom)
	if g.mapSelected[n] { border = colInPort; bw = float32(2.5 * g.zoom) }
	vector.StrokeRect(dst, float32(sx), float32(sy), float32(w), float32(h), bw, border, true)
	if g.zoom > 0.35 {
		g.drawText(dst, label, sx+10*g.zoom, sy+2*g.zoom, colText)
		body := n.name
		if n.kind == mapViz && n.last != "" { body = n.last }
		if n.kind == mapConv {
			body = n.name
			if g.mapEditing == n { body += "|" }
		}
		g.drawText(dst, g.fitText(body, w-20*g.zoom), sx+10*g.zoom, sy+30*g.zoom, colText)
		if n.kind == mapConv && n.expanded {
			g.drawConvUI(dst, n)
		}
	}
	if n.hasIn() {
		px, py := n.inPos()
		spx, spy := g.worldToScreen(px, py)
		vector.DrawFilledCircle(dst, float32(spx), float32(spy), float32((portRadius+hi*3)*g.zoom), colInPort, true)
	}
	if n.hasOut() {
		px, py := n.outPos()
		spx, spy := g.worldToScreen(px, py)
		vector.DrawFilledCircle(dst, float32(spx), float32(spy), float32((portRadius+hi*3)*g.zoom), colOutPort, true)
	}
}

func (g *game) drawConvUI(dst *ebiten.Image, n *mapNode) {
	for _, f := range convRows(n) {
		sx, sy := g.worldToScreen(f.r.x, f.r.y)
		sw, sh := f.r.w*g.zoom, f.r.h*g.zoom
		vector.DrawFilledRect(dst, float32(sx), float32(sy), float32(sw), float32(sh), colField, false)
		vector.StrokeRect(dst, float32(sx), float32(sy), float32(sw), float32(sh), 1, colBorder, false)
		lx := sx + 4*g.zoom
		rx := sx + sw - 16*g.zoom
		g.drawText(dst, "<", lx, sy+2*g.zoom, colMuted)
		g.drawText(dst, ">", rx, sy+2*g.zoom, colMuted)
		val := ""
		switch f.name {
		case "src":
			if n.conv.UseAxis { val = "Axis -> CC" } else if n.conv.IsCC { val = "Btn -> CC" } else { val = "Btn -> Note" }
		case "idx":
			if n.conv.UseAxis { val = fmt.Sprintf("Axis %d", n.conv.Index) } else { val = fmt.Sprintf("Btn %d", n.conv.Index+1) }
		case "ch":
			val = fmt.Sprintf("Ch %d", n.conv.Ch)
		case "num":
			if n.conv.UseAxis || n.conv.IsCC { val = fmt.Sprintf("CC %d", n.conv.Num) } else { val = fmt.Sprintf("Note %d", n.conv.Num) }
		case "inv":
			if n.conv.Inv { val = "Inv: on" } else { val = "Inv: off" }
		case "inmin":
			val = fmt.Sprintf("In Min %d", n.conv.InMin)
		case "inmax":
			val = fmt.Sprintf("In Max %d", n.conv.InMax)
		case "outmin":
			val = fmt.Sprintf("Out Min %d", n.conv.OutMin)
		case "outmax":
			val = fmt.Sprintf("Out Max %d", n.conv.OutMax)
		}
		g.drawText(dst, g.fitText(val, sw-40*g.zoom), sx+20*g.zoom, sy+2*g.zoom, colText)
	}
}

