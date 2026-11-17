package main

type joyEvent struct {
	label  string
	axis   int
	button int
	isBtn  bool
	on     bool
	value  uint32
}

type convCfg struct {
	UseAxis bool `json:"use_axis"`
	Index   int  `json:"index"`
	Ch      int  `json:"ch"`
	IsCC    bool `json:"is_cc"`
	Num     int  `json:"num"`
	Inv     bool `json:"inv"`
	InMin   int  `json:"in_min"`
	InMax   int  `json:"in_max"`
	OutMin  int  `json:"out_min"`
	OutMax  int  `json:"out_max"`
}

func defaultConv() convCfg {
	return convCfg{UseAxis: true, Index: 0, Ch: 1, IsCC: true, Num: 1, InMin: 0, InMax: 255, OutMin: 0, OutMax: 127}
}
