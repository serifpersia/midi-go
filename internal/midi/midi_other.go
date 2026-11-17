//go:build !windows

package midi

type In struct{}

func (*In) Messages() <-chan []byte { return nil }

func (*In) Close() error { return nil }

type Out struct{}

func (*Out) Send([]byte) error { return ErrUnsupported }

func (*Out) Close() error { return nil }

func ListInputs() ([]Port, error) { return nil, ErrUnsupported }

func ListOutputs() ([]Port, error) { return nil, ErrUnsupported }

func OpenInput(int) (*In, error) { return nil, ErrUnsupported }

func OpenOutput(int) (*Out, error) { return nil, ErrUnsupported }
