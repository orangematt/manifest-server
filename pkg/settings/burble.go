// (c) Copyright 2017-2021 Matt Messier

package settings

import "strings"

func (s *Settings) BurbleDropzoneID() int {
	return s.config.GetInt("burble.dzid")
}

func (s *Settings) OrganizerStrings() []string {
	o := s.config.GetStringSlice("burble.organizer_strings")
	if len(o) == 0 {
		o = []string{"organizer"}
	} else {
		lo := make([]string, len(o))
		for i := range o {
			lo[i] = strings.ToLower(o[i])
		}
	}
	return o
}
