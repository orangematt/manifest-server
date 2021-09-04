// (c) Copyright 2017-2021 Matt Messier

package settings

func (s *Settings) BurbleDropzoneID() int {
	return s.config.GetInt("burble.dzid")
}
