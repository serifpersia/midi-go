//go:build !windows

package joy

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
	Axes      []uint32
	Buttons   uint32
	POV       uint32
	Connected bool
}

func List() []Device { return nil }

func Read(int) State { return State{} }
