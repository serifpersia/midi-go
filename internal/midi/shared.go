package midi

import "sync"

type sharedOut struct {
	out   *Out
	refs  int
}

var (
	outMu     sync.Mutex
	sharedOuts = map[int]*sharedOut{}
)

func AcquireOutput(port int) (*Out, error) {
	outMu.Lock()
	defer outMu.Unlock()
	if s, ok := sharedOuts[port]; ok {
		s.refs++
		return s.out, nil
	}
	out, err := OpenOutput(port)
	if err != nil {
		return nil, err
	}
	sharedOuts[port] = &sharedOut{out: out, refs: 1}
	return out, nil
}

func ReleaseOutput(port int) {
	outMu.Lock()
	s, ok := sharedOuts[port]
	if !ok {
		outMu.Unlock()
		return
	}
	s.refs--
	if s.refs <= 0 {
		delete(sharedOuts, port)
		outMu.Unlock()
		s.out.Close()
		return
	}
	outMu.Unlock()
}
