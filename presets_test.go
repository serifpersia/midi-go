package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/serifpersia/midi-go/internal/midi"
)

func TestSnapshotRoundTrip(t *testing.T) {
	in := &node{kind: nodeInput, port: midi.Port{Name: "Keyboard"}, x: 10, y: 20}
	out := &node{kind: nodeOutput, port: midi.Port{Name: "Synth"}, x: 30, y: 40}
	g := &game{nodes: []*node{in, out}, conns: []conn{{from: in, to: out}}}

	data, err := json.Marshal(g.snapshotPreset())
	if err != nil {
		t.Fatal(err)
	}
	var p preset
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Nodes) != 2 || p.Nodes[0].Kind != "in" || p.Nodes[1].Kind != "out" {
		t.Fatalf("nodes: %+v", p.Nodes)
	}
	if len(p.Connections) != 1 {
		t.Fatalf("connections: %+v", p.Connections)
	}
	c := p.Connections[0]
	if c.From != "Keyboard" || c.To != "Synth" {
		t.Fatalf("connection: %+v", c)
	}
}

func TestSanitizePresetName(t *testing.T) {
	cases := map[string]string{
		"  my/band:set  ": "mybandset",
		"Live Set 1":      "Live Set 1",
		"":                "",
	}
	for in, want := range cases {
		if got := sanitizePresetName(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSavePresetWritesFile(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	in := &node{kind: nodeInput, port: midi.Port{Name: "In"}, x: 1, y: 2}
	out := &node{kind: nodeOutput, port: midi.Port{Name: "Out"}, x: 3, y: 4}
	g := &game{
		nodes:    []*node{in, out},
		conns:    []conn{{from: in, to: out}},
		statusCh: make(chan string, 8),
	}

	name := g.savePreset("My Set")
	if name != "My Set" {
		t.Fatalf("name = %q", name)
	}
	if _, err := os.Stat(filepath.Join("presets", "My Set.json")); err != nil {
		t.Fatal(err)
	}
	if len(g.presetNames) != 1 || g.presetNames[0] != "My Set" {
		t.Fatalf("presetNames = %v", g.presetNames)
	}
}
