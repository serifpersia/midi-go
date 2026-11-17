package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/serifpersia/midi-go/internal/joy"
	"github.com/serifpersia/midi-go/internal/midi"
)

const presetsDir = "presets"

var (
	presetFieldRect = rect{x: 32, y: 92, w: 300, h: 34}
	presetSaveRect  = rect{x: 344, y: 92, w: 84, h: 34}
	presetListTop   = 152.0
	presetRowH      = 40.0
)

type preset struct {
	Version        int              `json:"version"`
	Nodes          []presetNode     `json:"nodes"`
	Connections    []presetConn     `json:"connections"`
	MapperNodes    []presetMapNode  `json:"mapper_nodes,omitempty"`
	MapperConns    []presetMapConn  `json:"mapper_connections,omitempty"`
}

type presetNode struct {
	Kind string  `json:"kind"`
	Port string  `json:"port"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

type presetConn struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type presetMapNode struct {
	Kind     string  `json:"kind"`
	Name     string  `json:"name,omitempty"`
	Port     string  `json:"port,omitempty"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Expanded bool    `json:"expanded,omitempty"`
	Conv     convCfg `json:"conv,omitempty"`
}

type presetMapConn struct {
	From int `json:"from"`
	To   int `json:"to"`
}

func presetRowRect(i int) rect {
	return rect{x: 32, y: presetListTop + float64(i)*presetRowH, w: 396, h: 34}
}

func presetDeleteRect(i int) rect {
	r := presetRowRect(i)
	return rect{x: r.x + r.w - 28, y: r.y + 6, w: 22, h: 22}
}

func (g *game) snapshotPreset() preset {
	p := preset{Version: 1}
	for _, n := range g.nodes {
		kind := "in"
		if n.kind == nodeOutput {
			kind = "out"
		}
		p.Nodes = append(p.Nodes, presetNode{Kind: kind, Port: n.port.Name, X: n.x, Y: n.y})
	}
	for _, c := range g.conns {
		p.Connections = append(p.Connections, presetConn{From: c.from.port.Name, To: c.to.port.Name})
	}
	idx := make(map[*mapNode]int, len(g.mapNodes))
	for i, n := range g.mapNodes {
		idx[n] = i
		kind := "joy"
		switch n.kind {
		case mapViz:
			kind = "viz"
		case mapConv:
			kind = "conv"
		case mapMidiOut:
			kind = "midi_out"
		}
		p.MapperNodes = append(p.MapperNodes, presetMapNode{Kind: kind, Name: n.name, Port: n.port.Name, X: n.x, Y: n.y, Expanded: n.expanded, Conv: n.conv})
	}
	for _, c := range g.mapConns {
		fi, ok1 := idx[c.from]
		ti, ok2 := idx[c.to]
		if ok1 && ok2 {
			p.MapperConns = append(p.MapperConns, presetMapConn{From: fi, To: ti})
		}
	}
	return p
}

func (g *game) savePreset(name string) string {
	name = sanitizePresetName(name)
	if name == "" {
		name = "untitled"
	}

	data, err := json.MarshalIndent(g.snapshotPreset(), "", "  ")
	if err != nil {
		g.report("preset: " + err.Error())
		return ""
	}
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		g.report("preset: " + err.Error())
		return ""
	}
	path := filepath.Join(presetsDir, name+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		g.report("preset: " + err.Error())
		return ""
	}

	g.activePreset = name
	g.dirty = false
	g.refreshPresets()
	g.report("Saved preset " + name)
	return name
}

func (g *game) loadPreset(name string) {
	data, err := os.ReadFile(filepath.Join(presetsDir, name+".json"))
	if err != nil {
		g.report("preset: " + err.Error())
		return
	}
	var p preset
	if err := json.Unmarshal(data, &p); err != nil {
		g.report("preset: " + err.Error())
		return
	}

	g.clearConns()
	for _, pn := range p.Nodes {
		n := g.findNode(pn.Kind, pn.Port)
		if n == nil {
			port, kind, ok := g.portForPreset(pn.Kind, pn.Port)
			if !ok {
				continue
			}
			g.nodes = append(g.nodes, &node{kind: kind, port: port, x: pn.X, y: pn.Y, w: nodeWidth, h: nodeHeight})
			continue
		}
		n.x, n.y = pn.X, pn.Y
	}

	missing := 0
	for _, pc := range p.Connections {
		from := g.findNode("in", pc.From)
		to := g.findNode("out", pc.To)
		if from == nil || to == nil {
			portIn, _, okIn := g.portForPreset("in", pc.From)
			portOut, _, okOut := g.portForPreset("out", pc.To)
			if okIn && from == nil {
				from = &node{kind: nodeInput, port: portIn, x: 80, y: 80, w: nodeWidth, h: nodeHeight}
				g.nodes = append(g.nodes, from)
			}
			if okOut && to == nil {
				to = &node{kind: nodeOutput, port: portOut, x: 360, y: 80, w: nodeWidth, h: nodeHeight}
				g.nodes = append(g.nodes, to)
			}
		}
		if from == nil || to == nil {
			missing++
			continue
		}
		g.connect(from, to)
	}

	g.clearMapper()
	for _, mn := range p.MapperNodes {
		n := &mapNode{x: mn.X, y: mn.Y, w: nodeWidth, h: nodeHeight, name: mn.Name, expanded: mn.Expanded, conv: mn.Conv}
		switch mn.Kind {
		case "joy":
			n.kind = mapJoy
			n.joyID = g.joyIDForName(mn.Name)
		case "viz":
			n.kind = mapViz
			if n.name == "" { n.name = "Visualizer" }
			n.last = "(none)"
		case "conv":
			n.kind = mapConv
			if n.name == "" { n.name = "Converter" }
			if n.conv.Ch == 0 && n.conv.InMax == 0 {
				n.conv = defaultConv()
			} else if n.conv.InMax > 255 {
				n.conv.InMin = n.conv.InMin * 255 / 65535
				n.conv.InMax = n.conv.InMax * 255 / 65535
				if n.conv.InMax <= n.conv.InMin { n.conv.InMax = 255 }
			}
		case "midi_out":
			n.kind = mapMidiOut
			port, ok := g.mapOutPortForName(mn.Port)
			if !ok { port, ok = g.mapOutPortForName(mn.Name) }
			if !ok { continue }
			n.port = port
			n.name = port.Name
		default:
			continue
		}
		g.mapNodes = append(g.mapNodes, n)
	}
	for _, mc := range p.MapperConns {
		if mc.From < 0 || mc.To < 0 || mc.From >= len(g.mapNodes) || mc.To >= len(g.mapNodes) { continue }
		g.mapConnect(g.mapNodes[mc.From], g.mapNodes[mc.To])
	}

	g.activePreset = name
	g.presetName = name
	g.dirty = false
	g.refreshPresets()
	if missing > 0 {
		g.report(fmt.Sprintf("Loaded %s (%d missing ports)", name, missing))
	} else {
		g.report("Loaded preset " + name)
	}
}

func (g *game) joyIDForName(name string) int {
	g.joyDevices = joy.List()
	for _, d := range g.joyDevices {
		if d.Name == name {
			return d.ID
		}
	}
	if len(g.joyDevices) > 0 {
		return g.joyDevices[0].ID
	}
	return 0
}

func (g *game) mapOutPortForName(name string) (midi.Port, bool) {
	for _, p := range g.availOut {
		if p.Name == name {
			return p, true
		}
	}
	return midi.Port{}, false
}

func (g *game) clearMapper() {
	for _, n := range g.mapNodes {
		if n.kind == mapMidiOut && n.open {
			midi.ReleaseOutput(n.port.ID)
			n.midiOut = nil
			n.open = false
		}
	}
	g.mapNodes = nil
	g.mapConns = nil
	g.mapSelected = map[*mapNode]bool{}
	g.mapDragging = nil
	g.mapDragSrc = nil
	g.mapDragDst = nil
	g.mapConvHoldNode = nil
}

func (g *game) findNode(kind, port string) *node {
	for _, n := range g.nodes {
		k := "in"
		if n.kind == nodeOutput {
			k = "out"
		}
		if k == kind && n.port.Name == port {
			return n
		}
	}
	return nil
}

func (g *game) portForPreset(kind, name string) (midi.Port, nodeKind, bool) {
	if kind == "in" {
		for _, p := range g.availIn {
			if p.Name == name {
				return p, nodeInput, true
			}
		}
		return midi.Port{}, nodeInput, false
	}
	for _, p := range g.availOut {
		if p.Name == name {
			return p, nodeOutput, true
		}
	}
	return midi.Port{}, nodeOutput, false
}

func (g *game) deletePreset(name string) {
	if err := os.Remove(filepath.Join(presetsDir, name+".json")); err != nil {
		g.report("preset: " + err.Error())
		return
	}
	if g.activePreset == name {
		g.activePreset = ""
		g.dirty = false
	}
	g.refreshPresets()
	g.report("Deleted preset " + name)
}

func (g *game) refreshPresets() {
	entries, err := os.ReadDir(presetsDir)
	if err != nil {
		g.presetNames = nil
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		}
	}
	sort.Strings(names)
	g.presetNames = names
}

func (g *game) updatePresets(leftDown, rightDown bool) {
	if g.presetNameFocused {
		if ebiten.IsKeyPressed(ebiten.KeyBackspace) {
			g.backspaceHold++
			if g.backspaceHold == 1 || (g.backspaceHold > 24 && g.backspaceHold%3 == 0) {
				g.presetName = dropLastRune(g.presetName)
			}
		} else {
			g.backspaceHold = 0
		}
		for _, r := range ebiten.AppendInputChars(nil) {
			if r >= 0x20 && r != 0x7f && len([]rune(g.presetName)) < 64 {
				g.presetName += string(r)
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
			g.commitPresetName()
			return
		}
	} else {
		g.backspaceHold = 0
	}

	if rightDown {
		x, y := g.mouseX, g.mouseY
		maxRows := visiblePresetRows()
		for i, name := range g.presetNames {
			if i >= maxRows {
				break
			}
			if presetRowRect(i).hit(x, y) {
				g.deletePreset(name)
				return
			}
		}
		return
	}
	if !leftDown {
		return
	}

	x, y := g.mouseX, g.mouseY
	if presetFieldRect.hit(x, y) {
		g.presetNameFocused = true
		return
	}
	g.presetNameFocused = false
	if presetSaveRect.hit(x, y) {
		g.commitPresetName()
		return
	}
	maxRows := visiblePresetRows()
	for i, name := range g.presetNames {
		if i >= maxRows {
			break
		}
		if presetDeleteRect(i).hit(x, y) {
			g.deletePreset(name)
			return
		}
		if presetRowRect(i).hit(x, y) {
			g.loadPreset(name)
			return
		}
	}
}

func (g *game) commitPresetName() {
	name := g.presetName
	if name == "" {
		name = g.activePreset
	}
	if name == "" {
		g.report("Type a preset name first")
		return
	}
	if saved := g.savePreset(name); saved != "" {
		g.presetName = saved
	}
}

func visiblePresetRows() int {
	n := int((screenHeight - presetListTop - 56) / presetRowH)
	if n < 1 {
		return 1
	}
	return n
}

func (g *game) drawPresets(dst *ebiten.Image) {
	g.drawText(dst, "Presets", 32, 56, colText)

	fieldHover := presetFieldRect.hit(g.mouseX, g.mouseY)
	fieldBorder := colBorder
	if g.presetNameFocused {
		fieldBorder = colInPort
	} else if fieldHover {
		fieldBorder = mix(colBorder, colMuted, 0.6)
	}
	fieldBg := colField
	if fieldHover && !g.presetNameFocused {
		fieldBg = mix(colField, colButton, 0.2)
	}
	vector.DrawFilledRect(dst, float32(presetFieldRect.x), float32(presetFieldRect.y), float32(presetFieldRect.w), float32(presetFieldRect.h), fieldBg, false)
	vector.StrokeRect(dst, float32(presetFieldRect.x), float32(presetFieldRect.y), float32(presetFieldRect.w), float32(presetFieldRect.h), 1, fieldBorder, false)

	name := g.presetName
	nameCol := colText
	if name == "" && !g.presetNameFocused {
		name = "preset name"
		nameCol = colMuted
	}
	if g.presetNameFocused {
		name += "|"
	}
	g.drawText(dst, truncate(name, 30), presetFieldRect.x+10, presetFieldRect.y+8, nameCol)

	saveHover := presetSaveRect.hit(g.mouseX, g.mouseY)
	saveBg := colButton
	saveBorder := colBorder
	if saveHover {
		saveBg = mix(colButton, colInHead, 0.35)
		saveBorder = mix(colBorder, colInPort, 0.4)
	}
	vector.DrawFilledRect(dst, float32(presetSaveRect.x), float32(presetSaveRect.y), float32(presetSaveRect.w), float32(presetSaveRect.h), saveBg, false)
	vector.StrokeRect(dst, float32(presetSaveRect.x), float32(presetSaveRect.y), float32(presetSaveRect.w), float32(presetSaveRect.h), 1, saveBorder, false)
	g.drawText(dst, "Save", presetSaveRect.x+18, presetSaveRect.y+8, colText)

	g.drawText(dst, "Click the name field to type. Enter or Save stores it. Autosaves active preset. Use x or right-click to delete.", 32, 132, colMuted)

	if len(g.presetNames) == 0 {
		g.drawText(dst, "No presets yet.", 32, presetListTop+8, colMuted)
		return
	}

	maxRows := visiblePresetRows()
	for i, pname := range g.presetNames {
		if i >= maxRows {
			g.drawText(dst, fmt.Sprintf("+%d more", len(g.presetNames)-maxRows), 32, presetListTop+float64(maxRows)*presetRowH+6, colMuted)
			break
		}
		r := presetRowRect(i)
		rowHover := r.hit(g.mouseX, g.mouseY)
		bg := colNode
		if rowHover {
			bg = colButton
		}
		if pname == g.activePreset {
			bg = colTabBg
			if rowHover {
				bg = mix(colTabBg, colButton, 0.35)
			}
		}
		rowBorder := colBorder
		if rowHover {
			rowBorder = mix(colBorder, colMuted, 0.5)
		}
		vector.DrawFilledRect(dst, float32(r.x), float32(r.y), float32(r.w), float32(r.h), bg, false)
		vector.StrokeRect(dst, float32(r.x), float32(r.y), float32(r.w), float32(r.h), 1, rowBorder, false)

		col := colText
		if pname == g.activePreset {
			col = colInPort
		}
		g.drawText(dst, truncate(pname, 28), r.x+10, r.y+7, col)
		if pname == g.activePreset {
			g.drawText(dst, "active", r.x+r.w-92, r.y+7, colMuted)
		}

		d := presetDeleteRect(i)
		delHover := d.hit(g.mouseX, g.mouseY)
		dBg := colField
		dCol := colMuted
		if delHover {
			dBg = mix(colField, colWarn, 0.2)
			dCol = colWarn
		}
		vector.DrawFilledRect(dst, float32(d.x), float32(d.y), float32(d.w), float32(d.h), dBg, false)
		vector.StrokeRect(dst, float32(d.x), float32(d.y), float32(d.w), float32(d.h), 1, dCol, false)
		g.drawText(dst, "x", d.x+7, d.y+2, dCol)
	}
}

func sanitizePresetName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 {
			continue
		}
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if r := []rune(out); len(r) > 64 {
		out = string(r[:64])
	}
	return out
}

func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}
