package main

import (
	"fmt"
	"image/color"
	"math"
	"sync/atomic"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/serifpersia/midi-go/internal/midi"
)

const (
	nodeWidth  = 220.0
	nodeHeight = 60.0
	portRadius = 7.0
	hitRadius  = 14.0
)

type nodeKind int

const (
	nodeInput nodeKind = iota
	nodeOutput
)

type node struct {
	kind nodeKind
	port midi.Port
	x, y float64
	w, h float64

	in    *midi.In
	out   *midi.Out
	open  bool
	flash int64
	seen  int64
	glow  float64
}

func (n *node) portPos() (float64, float64) {
	if n.kind == nodeInput {
		return n.x + n.w, n.y + n.h/2
	}
	return n.x, n.y + n.h/2
}

func (n *node) contains(x, y float64) bool {
	return x >= n.x && x <= n.x+n.w && y >= n.y && y <= n.y+n.h
}

type conn struct {
	from *node
	to   *node
}

func (g *game) updateRouter(leftDown, leftUp, rightDown bool) {
	wx, wy := g.screenToWorld(g.mouseX, g.mouseY)
	mid := ebiten.IsMouseButtonPressed(ebiten.MouseButtonMiddle)

	if g.menuOpen {
		if leftDown && !g.clickMenu(g.mouseX, g.mouseY) {
			g.menuOpen = false
		}
		if rightDown {
			g.menuOpen = false
		}
		return
	}

	if g.mouseY > tabBarHeight && !g.panning && g.dragging == nil && g.dragSrc == nil && g.dragDst == nil && !g.selecting {
		if _, wyh := ebiten.Wheel(); wyh != 0 {
			old := g.zoom
			g.zoom = clamp(g.zoom*math.Pow(1.1, wyh), 0.2, 4)
			r := g.zoom / old
			g.camX = wx - (wx-g.camX)/r
			g.camY = wy - (wy-g.camY)/r
			wx, wy = g.screenToWorld(g.mouseX, g.mouseY)
		}
	}

	if mid && g.mouseY > tabBarHeight && !g.panning && g.dragging == nil && g.dragSrc == nil && g.dragDst == nil && !g.selecting {
		g.panning = true
		g.panLastX, g.panLastY = g.mouseX, g.mouseY
	}
	if !mid {
		g.panning = false
	}

	if leftDown && g.mouseY > tabBarHeight && !mid {
		g.onPress(wx, wy)
	}
	if leftUp {
		if g.selecting {
			g.finishSelection()
		}
		g.onRelease(wx, wy)
	}
	if rightDown && g.mouseY > tabBarHeight {
		if g.nodeAt(wx, wy) != nil {
			g.onNodeRightClick(wx, wy)
		} else if !g.onRightClick(wx, wy) {
			g.openMenu(g.mouseX, g.mouseY, wx, wy)
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
	if g.dragging != nil {
		n := g.dragging
		n.x = wx - g.dragDX
		n.y = wy - g.dragDY
		g.dirty = true
	}

	if !g.presetNameFocused && (inpututil.IsKeyJustPressed(ebiten.KeyDelete) || inpututil.IsKeyJustPressed(ebiten.KeyBackspace)) {
		g.deleteSelected()
	}
}

func (g *game) openMenu(sx, sy, wx, wy float64) {
	items := []menuItem{{label: "Add MIDI IN", sep: true}}
	for _, p := range g.availIn {
		items = append(items, menuItem{label: p.Name, kind: nodeInput, port: p})
	}
	if len(g.availIn) == 0 {
		items = append(items, menuItem{label: "(no MIDI inputs)", sep: true})
	}
	items = append(items, menuItem{label: "Add MIDI OUT", sep: true})
	for _, p := range g.availOut {
		items = append(items, menuItem{label: p.Name, kind: nodeOutput, port: p})
	}
	if len(g.availOut) == 0 {
		items = append(items, menuItem{label: "(no MIDI outputs)", sep: true})
	}
	g.menuItems = items
	g.menuX, g.menuY = sx, sy
	g.menuWorldX, g.menuWorldY = wx, wy
	g.menuOpen = true
}

func menuLayout(items []menuItem, x, y float64) (rect, []rect) {
	w := 260.0
	h := 8.0
	rects := make([]rect, len(items))
	for i, it := range items {
		rh := 28.0
		if it.sep {
			rh = 22.0
		}
		rects[i] = rect{x, y + h, w, rh}
		h += rh
	}
	h += 6
	if y+h > screenHeight {
		y = screenHeight - h
	}
	if x+w > screenWidth {
		x = screenWidth - w
	}
	for i := range rects {
		rects[i].x = x
		rects[i].y = y + (rects[i].y - (gMenuBaseY(y, items))) + 8
	}
	_ = rects
	// recompute from adjusted origin
	ry := y + 8
	for i, it := range items {
		rh := 28.0
		if it.sep {
			rh = 22.0
		}
		rects[i] = rect{x, ry, w, rh}
		ry += rh
	}
	return rect{x, y, w, h}, rects
}

func gMenuBaseY(y float64, items []menuItem) float64 { return y }

func (g *game) clickMenu(sx, sy float64) bool {
	if !g.menuOpen {
		return false
	}
	box, rects := menuLayout(g.menuItems, g.menuX, g.menuY)
	if !box.hit(sx, sy) {
		return false
	}
	for i, r := range rects {
		if r.hit(sx, sy) && !g.menuItems[i].sep && g.menuItems[i].label[0] != '(' {
			it := g.menuItems[i]
			g.addNode(it.kind, it.port, g.menuWorldX, g.menuWorldY)
			g.menuOpen = false
			return true
		}
	}
	return true
}

func (g *game) addNode(kind nodeKind, port midi.Port, x, y float64) {
	g.nodes = append(g.nodes, &node{
		kind: kind, port: port,
		x: x, y: y, w: nodeWidth, h: nodeHeight,
	})
	g.dirty = true
}

func (g *game) onPress(x, y float64) {
	if n := g.inputPortAt(x, y); n != nil {
		g.dragSrc = n
		return
	}
	if n := g.outputPortAt(x, y); n != nil {
		if i, ok := g.lastConnTo(n); ok {
			src := g.conns[i].from
			g.removeConnAt(i)
			g.dragSrc = src
			return
		}
		g.dragDst = n
		return
	}
	if n := g.nodeAt(x, y); n != nil {
		g.selected = map[*node]bool{n: true}
		g.dragging = n
		g.dragDX = x - n.x
		g.dragDY = y - n.y
		return
	}
	if i := g.cableAt(x, y); i >= 0 {
		src := g.conns[i].from
		g.removeConnAt(i)
		g.dragSrc = src
		return
	}
	g.selected = map[*node]bool{}
	g.selecting = true
	g.selX1, g.selY1 = x, y
	g.selX2, g.selY2 = x, y
}

func (g *game) onRelease(x, y float64) {
	if g.dragSrc != nil {
		if t := g.outputPortAt(x, y); t != nil {
			g.connect(g.dragSrc, t)
		}
		g.dropIdleInput(g.dragSrc)
		g.dragSrc = nil
	}
	if g.dragDst != nil {
		if s := g.inputPortAt(x, y); s != nil {
			g.connect(s, g.dragDst)
		}
		g.dragDst = nil
	}
	g.dragging = nil
}

func (g *game) onRightClick(x, y float64) bool {
	i := g.cableAt(x, y)
	if i < 0 {
		return false
	}
	src := g.conns[i].from
	g.removeConnAt(i)
	g.dropIdleInput(src)
	return true
}

func (g *game) onNodeRightClick(x, y float64) {
	n := g.nodeAt(x, y)
	if n == nil {
		return
	}
	g.deleteNodes([]*node{n})
}

func (g *game) finishSelection() {
	x1, x2 := g.selX1, g.selX2
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	y1, y2 := g.selY1, g.selY2
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	if math.Abs(x2-x1) < 2 && math.Abs(y2-y1) < 2 {
		g.selecting = false
		return
	}
	g.selected = map[*node]bool{}
	for _, n := range g.nodes {
		nx2, ny2 := n.x+n.w, n.y+n.h
		if n.x <= x2 && nx2 >= x1 && n.y <= y2 && ny2 >= y1 {
			g.selected[n] = true
		}
	}
	g.selecting = false
}

func (g *game) deleteSelected() {
	if len(g.selected) == 0 {
		return
	}
	nodes := make([]*node, 0, len(g.selected))
	for n := range g.selected {
		nodes = append(nodes, n)
	}
	g.deleteNodes(nodes)
}

func (g *game) deleteNodes(nodes []*node) {
	if len(nodes) == 0 {
		return
	}
	del := make(map[*node]bool, len(nodes))
	for _, n := range nodes {
		del[n] = true
		if n.in != nil {
			n.in.Close()
			n.in = nil
			n.open = false
		}
		if n.out != nil {
			midi.ReleaseOutput(n.port.ID)
			n.out = nil
			n.open = false
		}
	}
	g.mu.Lock()
	kept := g.conns[:0]
	for _, c := range g.conns {
		if !del[c.from] && !del[c.to] {
			kept = append(kept, c)
		}
	}
	g.conns = kept
	g.mu.Unlock()
	keptNodes := g.nodes[:0]
	for _, n := range g.nodes {
		if !del[n] {
			keptNodes = append(keptNodes, n)
		}
	}
	g.nodes = keptNodes
	g.selected = map[*node]bool{}
	if g.dragging != nil && del[g.dragging] {
		g.dragging = nil
	}
	g.dirty = true
}

func (g *game) connect(from, to *node) {
	if from.kind != nodeInput || to.kind != nodeOutput {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.conns {
		if c.from == from && c.to == to {
			return
		}
	}
	if !to.open {
		out, err := midi.AcquireOutput(to.port.ID)
		if err != nil {
			g.status = fmt.Sprintf("open %q: %v", to.port.Name, err)
			return
		}
		to.out, to.open = out, true
	}
	if !from.open {
		in, err := midi.OpenInput(from.port.ID)
		if err != nil {
			g.status = fmt.Sprintf("open %q: %v", from.port.Name, err)
			return
		}
		from.in, from.open = in, true
		go g.readLoop(from, in)
	}
	g.conns = append(g.conns, conn{from: from, to: to})
	g.dirty = true
	g.status = ""
}

func (g *game) readLoop(n *node, in *midi.In) {
	var targets []*node
	for msg := range in.Messages() {
		atomic.AddInt64(&n.flash, 1)
		targets = targets[:0]
		g.mu.RLock()
		if n.in != in {
			g.mu.RUnlock()
			return
		}
		for _, c := range g.conns {
			if c.from == n && c.to.out != nil {
				targets = append(targets, c.to)
			}
		}
		g.mu.RUnlock()
		for _, t := range targets {
			if err := t.out.Send(msg); err != nil {
				g.report(fmt.Sprintf("send %q: %v", t.port.Name, err))
				continue
			}
			atomic.AddInt64(&t.flash, 1)
		}
	}
}

func (g *game) nodeAt(x, y float64) *node {
	for i := len(g.nodes) - 1; i >= 0; i-- {
		if g.nodes[i].contains(x, y) {
			return g.nodes[i]
		}
	}
	return nil
}

func (g *game) inputPortAt(x, y float64) *node {
	for i := len(g.nodes) - 1; i >= 0; i-- {
		n := g.nodes[i]
		if n.kind != nodeInput {
			continue
		}
		px, py := n.portPos()
		if math.Hypot(px-x, py-y) <= hitRadius {
			return n
		}
	}
	return nil
}

func (g *game) outputPortAt(x, y float64) *node {
	for i := len(g.nodes) - 1; i >= 0; i-- {
		n := g.nodes[i]
		if n.kind != nodeOutput {
			continue
		}
		px, py := n.portPos()
		if math.Hypot(px-x, py-y) <= hitRadius {
			return n
		}
	}
	return nil
}

func (g *game) cableAt(x, y float64) int {
	for i, c := range g.conns {
		sx, sy := c.from.portPos()
		tx, ty := c.to.portPos()
		if distToCurve(sx, sy, tx, ty, x, y) < 8/g.zoom {
			return i
		}
	}
	return -1
}

func (g *game) lastConnFrom(n *node) (int, bool) {
	for i := len(g.conns) - 1; i >= 0; i-- {
		if g.conns[i].from == n {
			return i, true
		}
	}
	return -1, false
}

func (g *game) lastConnTo(n *node) (int, bool) {
	for i := len(g.conns) - 1; i >= 0; i-- {
		if g.conns[i].to == n {
			return i, true
		}
	}
	return -1, false
}

func (g *game) removeConnAt(i int) {
	to := g.conns[i].to
	g.mu.Lock()
	g.conns = append(g.conns[:i], g.conns[i+1:]...)
	g.mu.Unlock()
	if _, ok := g.lastConnTo(to); !ok && to.out != nil {
		midi.ReleaseOutput(to.port.ID)
		to.out = nil
		to.open = false
	}
	g.dirty = true
}

func (g *game) clearConns() {
	g.mu.Lock()
	srcs := make(map[*node]bool, len(g.conns))
	outs := make(map[*node]bool)
	for _, c := range g.conns {
		srcs[c.from] = true
		outs[c.to] = true
	}
	g.conns = nil
	g.mu.Unlock()
	for n := range srcs {
		g.dropIdleInput(n)
	}
	for n := range outs {
		if _, ok := g.lastConnTo(n); !ok && n.out != nil {
			midi.ReleaseOutput(n.port.ID)
			n.out = nil
			n.open = false
		}
	}
}

func (g *game) dropIdleInput(n *node) {
	if n == nil || n.kind != nodeInput {
		return
	}
	if _, ok := g.lastConnFrom(n); ok {
		return
	}
	g.mu.Lock()
	in := n.in
	n.in = nil
	n.open = false
	g.mu.Unlock()
	if in != nil {
		in.Close()
	}
}

func (g *game) drawRouter(dst *ebiten.Image) {
	g.drawGrid(dst)
	wx, wy := g.screenToWorld(g.mouseX, g.mouseY)

	hoverCable := -1
	if g.dragSrc == nil && g.dragDst == nil {
		hoverCable = g.cableAt(wx, wy)
	}

	for i, c := range g.conns {
		active := math.Max(c.from.glow, c.to.glow)
		if i == hoverCable {
			active = math.Max(active, 0.45)
		}
		col := mix(colCable, colInPort, active)
		sx, sy := c.from.portPos()
		tx, ty := c.to.portPos()
		g.drawWorldBezier(dst, sx, sy, tx, ty, col, float32(2+2*active))
	}
	if g.dragSrc != nil {
		px, py := g.dragSrc.portPos()
		sx, sy := g.worldToScreen(px, py)
		g.drawScreenBezier(dst, sx, sy, g.mouseX, g.mouseY, colInPort, 2)
	}
	if g.dragDst != nil {
		px, py := g.dragDst.portPos()
		tx, ty := g.worldToScreen(px, py)
		g.drawScreenBezier(dst, g.mouseX, g.mouseY, tx, ty, colOutPort, 2)
	}

	for _, n := range g.nodes {
		g.drawNode(dst, n, wx, wy)
	}

	if g.selecting {
		x1, y1 := g.worldToScreen(g.selX1, g.selY1)
		x2, y2 := g.worldToScreen(g.selX2, g.selY2)
		if x1 > x2 {
			x1, x2 = x2, x1
		}
		if y1 > y2 {
			y1, y2 = y2, y1
		}
		vector.DrawFilledRect(dst, float32(x1), float32(y1), float32(x2-x1), float32(y2-y1), color.NRGBA{R: 0x6c, G: 0xb6, B: 0xff, A: 0x18}, false)
		vector.StrokeRect(dst, float32(x1), float32(y1), float32(x2-x1), float32(y2-y1), 1, colInPort, false)
	}

	if g.menuOpen {
		g.drawMenu(dst)
	}
}

func cWorld(g *game, x, y float64) (float64, float64) { return g.worldToScreen(x, y) }

func (g *game) drawGrid(dst *ebiten.Image) {
	left, top := g.camX, g.camY
	right := g.camX + screenWidth/g.zoom
	bottom := g.camY + (screenHeight-tabBarHeight)/g.zoom
	step := 32.0
	startX := math.Floor(left/step) * step
	startY := math.Floor(top/step) * step
	for x := startX; x <= right; x += step {
		sx, _ := g.worldToScreen(x, 0)
		vector.StrokeLine(dst, float32(sx), tabBarHeight, float32(sx), screenHeight, 1, colGrid, false)
	}
	for y := startY; y <= bottom; y += step {
		_, sy := g.worldToScreen(0, y)
		if sy < tabBarHeight {
			continue
		}
		vector.StrokeLine(dst, 0, float32(sy), screenWidth, float32(sy), 1, colGrid, false)
	}
}

func (g *game) drawNode(dst *ebiten.Image, n *node, wx, wy float64) {
	sx, sy := g.worldToScreen(n.x, n.y)
	w, h := n.w*g.zoom, n.h*g.zoom
	px, py := n.portPos()
	spx, spy := g.worldToScreen(px, py)
	portHover := math.Hypot(px-wx, py-wy) <= hitRadius
	nodeHover := n.contains(wx, wy)

	fill := colNode
	if nodeHover {
		fill = mix(colNode, colButton, 0.35)
	}
	vector.DrawFilledRect(dst, float32(sx), float32(sy), float32(w), float32(h), fill, true)

	head, label, portCol := colInHead, "MIDI IN", colInPort
	if n.kind == nodeOutput {
		head, label, portCol = colOutHead, "MIDI OUT", colOutPort
	}
	if nodeHover {
		head = mix(head, portCol, 0.15)
	}
	vector.DrawFilledRect(dst, float32(sx), float32(sy), float32(w), float32(24*g.zoom), head, true)

	hi := n.glow
	if portHover {
		hi = math.Max(hi, 0.5)
	}
	border := mix(colBorder, portCol, hi)
	if g.selected[n] {
		border = colInPort
	}
	bw := float32((1.5 + 2*hi) * g.zoom)
	if g.selected[n] {
		bw = float32(2.5 * g.zoom)
	}
	vector.StrokeRect(dst, float32(sx), float32(sy), float32(w), float32(h), bw, border, true)

	if g.zoom > 0.35 {
		g.drawText(dst, label, sx+10*g.zoom, sy+2*g.zoom, colText)
		g.drawText(dst, g.fitText(n.port.Name, w-20*g.zoom), sx+10*g.zoom, sy+30*g.zoom, colText)
	}
	vector.DrawFilledCircle(dst, float32(spx), float32(spy), float32((portRadius+hi*3)*g.zoom), portCol, true)
}

func (g *game) drawWorldBezier(dst *ebiten.Image, sx, sy, tx, ty float64, col color.RGBA, width float32) {
	ax, ay := g.worldToScreen(sx, sy)
	bx, by := g.worldToScreen(tx, ty)
	g.drawScreenBezier(dst, ax, ay, bx, by, col, width*float32(g.zoom))
}

func (g *game) drawScreenBezier(dst *ebiten.Image, sx, sy, tx, ty float64, col color.RGBA, width float32) {
	px, py := sx, sy
	const steps = 28
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		x, y := bezier(sx, sy, tx, ty, t)
		vector.StrokeLine(dst, float32(px), float32(py), float32(x), float32(y), width, col, true)
		px, py = x, y
	}
}

func (g *game) drawMenu(dst *ebiten.Image) {
	box, rects := menuLayout(g.menuItems, g.menuX, g.menuY)
	vector.DrawFilledRect(dst, float32(box.x), float32(box.y), float32(box.w), float32(box.h), colMenu, false)
	vector.StrokeRect(dst, float32(box.x), float32(box.y), float32(box.w), float32(box.h), 1, colBorder, false)
	for i, r := range rects {
		it := g.menuItems[i]
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

func bezier(sx, sy, tx, ty, t float64) (float64, float64) {
	dx := math.Max(50, math.Abs(tx-sx)*0.5)
	p1x, p1y := sx+dx, sy
	p2x, p2y := tx-dx, ty
	u := 1 - t
	x := u*u*u*sx + 3*u*u*t*p1x + 3*u*t*t*p2x + t*t*t*tx
	y := u*u*u*sy + 3*u*u*t*p1y + 3*u*t*t*p2y + t*t*t*ty
	return x, y
}

func distToCurve(sx, sy, tx, ty, x, y float64) float64 {
	best := math.Inf(1)
	px, py := sx, sy
	for i := 1; i <= 64; i++ {
		t := float64(i) / 64
		bx, by := bezier(sx, sy, tx, ty, t)
		best = math.Min(best, pointSegmentDist(x, y, px, py, bx, by))
		px, py = bx, by
	}
	return best
}

func pointSegmentDist(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	den := dx*dx + dy*dy
	if den == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / den
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}
