// (c) Copyright 2017-2021 Matt Messier

package winds

type Sample struct {
	Altitude         int  `json:"altitude"`
	Heading          int  `json:"heading"`
	Speed            int  `json:"speed"`
	Temperature      int  `json:"temperature"`
	LightAndVariable bool `json:"is_variable,omitempty"`
}
