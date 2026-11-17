package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/color"
	"log"
	"math"
	"sync"
	"sync/atomic"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/serifpersia/midi-go/internal/joy"
	"github.com/serifpersia/midi-go/internal/midi"
	"golang.org/x/image/font/gofont/goregular"
)

const (
	screenWidth  = 960
	screenHeight = 600
	tabBarHeight = 44
)

var (
	colBg      = color.RGBA{0x12, 0x16, 0x1c, 0xff}
	colGrid    = color.RGBA{0x1c, 0x23, 0x2d, 0xff}
	colNode    = color.RGBA{0x21, 0x29, 0x35, 0xff}
	colBorder  = color.RGBA{0x35, 0x41, 0x52, 0xff}
	colInHead  = color.RGBA{0x2b, 0x4a, 0x7a, 0xff}
	colOutHead = color.RGBA{0x2f, 0x5a, 0x3c, 0xff}
	colInPort  = color.RGBA{0x6c, 0xb6, 0xff, 0xff}
	colOutPort = color.RGBA{0x9e, 0xce, 0x6a, 0xff}
	colCable   = color.RGBA{0x4a, 0x5e, 0x78, 0xff}
	colText    = color.RGBA{0xd6, 0xdd, 0xe8, 0xff}
	colMuted   = color.RGBA{0x8b, 0x96, 0xa8, 0xff}
	colWarn    = color.RGBA{0xe0, 0xaf, 0x68, 0xff}
	colBar     = color.RGBA{0x0e, 0x11, 0x16, 0xff}
	colTabBg   = color.RGBA{0x1c, 0x23, 0x2d, 0xff}
	colField   = color.RGBA{0x1a, 0x20, 0x29, 0xff}
	colButton  = color.RGBA{0x26, 0x30, 0x3e, 0xff}
	colMenu    = color.RGBA{0x18, 0x1e, 0x28, 0xff}
	colMenuHi  = color.RGBA{0x26, 0x33, 0x45, 0xff}
)

type tab int

const (
	tabRouter tab = iota
	tabMapper
	tabPresets
)

var tabLabels = [...]string{"Router", "Mapper", "Presets"}

type rect struct {
	x, y, w, h float64
}

func (r rect) hit(x, y float64) bool {
	return x >= r.x && x <= r.x+r.w && y >= r.y && y <= r.y+r.h
}

type menuItem struct {
	label string
	kind  nodeKind
	port  midi.Port
	sep   bool
}

type game struct {
	face     text.Face
	nodes    []*node
	conns    []conn
	mu       sync.RWMutex
	status   string
	statusCh chan string

	tab tab

	availIn  []midi.Port
	availOut []midi.Port

	camX, camY float64
	zoom       float64

	menuOpen   bool
	menuX      float64
	menuY      float64
	menuWorldX float64
	menuWorldY float64
	menuItems  []menuItem

	dragging  *node
	dragDX    float64
	dragDY    float64
	dragSrc   *node
	dragDst   *node
	panning   bool
	panLastX  float64
	panLastY  float64
	selecting bool
	selX1     float64
	selY1     float64
	selX2     float64
	selY2     float64
	selected  map[*node]bool

	mapNodes    []*mapNode
	mapConns    []mapConn
	mapMenuOpen bool
	mapMenuItems []mapMenuItem
	mapDragging  *mapNode
	mapDragDX    float64
	mapDragDY    float64
	mapDragSrc   *mapNode
	mapDragDst   *mapNode
	mapSelected  map[*mapNode]bool
	mapPressNode *mapNode
	mapPressX    float64
	mapPressY    float64
	mapConvHoldNode *mapNode
	mapConvHoldField string
	mapConvHoldD    int
	mapConvHoldSide int
	mapConvHoldTicks int
	mapEditing   *mapNode
	joyDevices   []joy.Device

	mouseX, mouseY      float64
	prevLeft, prevRight bool
	prevMid             bool

	presetName        string
	presetNames       []string
	activePreset      string
	dirty             bool
	backspaceHold     int
	presetNameFocused bool
}

var fontSource *text.GoTextFaceSource

func loadFace(size float64) (text.Face, error) {
	if fontSource == nil {
		src, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
		if err != nil {
			return nil, err
		}
		fontSource = src
	}
	return &text.GoTextFace{Source: fontSource, Size: size}, nil
}

func main() {
	list := flag.Bool("list", false, "print MIDI ports and exit")
	flag.Parse()

	inputs, inErr := midi.ListInputs()
	outputs, outErr := midi.ListOutputs()

	if *list {
		printPorts("MIDI inputs", inputs, inErr)
		printPorts("MIDI outputs", outputs, outErr)
		return
	}

	face, err := loadFace(16)
	if err != nil {
		log.Fatal(err)
	}

	g := newGame(face, inputs, outputs)
	switch {
	case inErr != nil:
		g.status = "MIDI inputs: " + inErr.Error()
	case outErr != nil:
		g.status = "MIDI outputs: " + outErr.Error()
	}

	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("midi-go")
	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
	g.shutdown()
}

func newGame(face text.Face, inputs, outputs []midi.Port) *game {
	g := &game{
		face:     face,
		statusCh: make(chan string, 16),
		availIn:  inputs,
		availOut: outputs,
		zoom:     1,
		selected: map[*node]bool{},
	}
	g.refreshPresets()
	return g
}

func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func (g *game) shutdown() {
	for _, n := range g.nodes {
		if n.in != nil {
			n.in.Close()
		}
		if n.out != nil {
			n.out.Close()
		}
	}
}

func (g *game) screenToWorld(sx, sy float64) (float64, float64) {
	return sx/g.zoom + g.camX, (sy-tabBarHeight)/g.zoom + g.camY
}

func (g *game) worldToScreen(wx, wy float64) (float64, float64) {
	return (wx - g.camX) * g.zoom, (wy-g.camY)*g.zoom + tabBarHeight
}

func (g *game) Update() error {
	g.drainStatus()

	mx, my := ebiten.CursorPosition()
	g.mouseX, g.mouseY = float64(mx), float64(my)

	left := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	right := ebiten.IsMouseButtonPressed(ebiten.MouseButtonRight)
	mid := ebiten.IsMouseButtonPressed(ebiten.MouseButtonMiddle)
	leftDown := left && !g.prevLeft
	leftUp := !left && g.prevLeft
	rightDown := right && !g.prevRight
	g.prevLeft, g.prevRight = left, right
	g.prevMid = mid

	g.decayGlow()
	g.initMapper()
	g.pollJoy()

	if !(leftDown && g.clickTab(mx, my)) {
		switch g.tab {
		case tabRouter:
			g.updateRouter(leftDown, leftUp, rightDown)
		case tabMapper:
			g.updateMapper(leftDown, leftUp, rightDown)
		case tabPresets:
			g.updatePresets(leftDown, rightDown)
		}
	}

	g.maybeAutosave()
	return nil
}

func (g *game) Draw(screen *ebiten.Image) {
	screen.Fill(colBg)
	switch g.tab {
	case tabRouter:
		g.drawRouter(screen)
	case tabMapper:
		g.drawMapper(screen)
	case tabPresets:
		g.drawPresets(screen)
	}
	g.drawTabs(screen)
	g.drawFooter(screen)
}



func (g *game) drawFooter(dst *ebiten.Image) {
	switch g.tab {
	case tabRouter, tabMapper:
		g.drawText(dst, "Right-click: add/menu  -  drag empty: select  -  middle drag: pan  -  wheel: zoom  -  Del: delete", 24, screenHeight-32, colMuted)
	}
	if g.status != "" {
		g.drawText(dst, truncate(g.status, 110), 24, screenHeight-56, colWarn)
	}
}

func (g *game) decayGlow() {
	for _, n := range g.nodes {
		if f := atomic.LoadInt64(&n.flash); f != n.seen {
			n.seen = f
			n.glow = 1
		}
		if n.glow > 0.02 {
			n.glow *= 0.9
		} else {
			n.glow = 0
		}
	}
	for _, n := range g.mapNodes {
		if n.glow > 0.02 {
			n.glow *= 0.9
		} else {
			n.glow = 0
		}
	}
}

func (g *game) drainStatus() {
	for len(g.statusCh) > 0 {
		g.status = <-g.statusCh
	}
}

func (g *game) report(s string) {
	select {
	case g.statusCh <- s:
	default:
	}
}

func tabRect(i int) rect {
	return rect{x: 24 + float64(i)*130, y: 0, w: 104, h: tabBarHeight}
}

func (g *game) clickTab(mx, my int) bool {
	x, y := float64(mx), float64(my)
	if y > tabBarHeight {
		return false
	}
	for i := range tabLabels {
		if tabRect(i).hit(x, y) {
			g.setTab(tab(i))
			return true
		}
	}
	return true
}

func (g *game) setTab(t tab) {
	if g.tab == t {
		return
	}
	g.tab = t
	g.dragging = nil
	g.dragSrc = nil
	g.dragDst = nil
	g.mapDragging = nil
	g.mapDragSrc = nil
	g.mapDragDst = nil
	g.mapEditing = nil
	g.panning = false
	g.selecting = false
	g.menuOpen = false
	g.mapMenuOpen = false
	g.presetNameFocused = t == tabPresets
	if t == tabPresets {
		g.refreshPresets()
	}
}

func (g *game) drawTabs(dst *ebiten.Image) {
	vector.DrawFilledRect(dst, 0, 0, screenWidth, tabBarHeight, colBar, false)
	for i := range tabLabels {
		r := tabRect(i)
		active := tab(i) == g.tab
		hover := r.hit(g.mouseX, g.mouseY)
		if active {
			vector.DrawFilledRect(dst, float32(r.x), 0, float32(r.w), tabBarHeight, colTabBg, false)
			vector.DrawFilledRect(dst, float32(r.x), tabBarHeight-3, float32(r.w), 3, colInPort, false)
		} else if hover {
			vector.DrawFilledRect(dst, float32(r.x), 0, float32(r.w), tabBarHeight, mix(colBar, colTabBg, 0.5), false)
		}
		col := colMuted
		if active || hover {
			col = colText
		}
		g.drawText(dst, tabLabels[i], r.x+18, 12, col)
	}
	vector.StrokeLine(dst, 0, tabBarHeight, screenWidth, tabBarHeight, 1, colBorder, false)
}

func (g *game) drawText(dst *ebiten.Image, s string, x, y float64, col color.Color) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(x, y)
	op.ColorScale.ScaleWithColor(col)
	text.Draw(dst, s, g.face, op)
}

func (g *game) maybeAutosave() {
	if !g.dirty || g.activePreset == "" {
		return
	}
	if g.dragging != nil || g.dragSrc != nil || g.dragDst != nil || g.panning || g.mapDragging != nil || g.mapDragSrc != nil || g.mapDragDst != nil {
		return
	}
	g.savePreset(g.activePreset)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-2]) + ".."
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	ch := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t))
	}
	return color.RGBA{ch(a.R, b.R), ch(a.G, b.G), ch(a.B, b.B), 0xff}
}

func printPorts(label string, ports []midi.Port, err error) {
	fmt.Println(label + ":")
	if err != nil {
		fmt.Println("  error:", err)
		return
	}
	if len(ports) == 0 {
		fmt.Println("  (none)")
		return
	}
	for _, p := range ports {
		fmt.Printf("  [%d] %s\n", p.ID, p.Name)
	}
}
