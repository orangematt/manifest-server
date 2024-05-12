// (c) Copyright 2017-2021 Matt Messier

package settings

import (
	"fmt"
	"os"
	"strings"
)

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

type GroupByJumpType struct {
	JumpType        string
	ManifestHeading string
}

func (s *Settings) GroupByJumpTypes() []GroupByJumpType {
	jumptype_groups := s.config.Get("burble.jumptype_groups")
	groups, ok := jumptype_groups.([]interface{})
	if !ok {
		return nil
	}

	result := make([]GroupByJumpType, 0, len(groups))
	for _, g := range groups {
		gg, ggok := g.(map[string]interface{})
		if !ggok {
			continue
		}

		typ, tok := gg["type"].(string)
		if !tok {
			fmt.Fprintf(os.Stderr, "error: missing type for burble.jumptype_group\n")
			continue
		}

		heading, hok := gg["group"].(string)
		if !hok {
			heading = strings.ToTitle(typ)
		}
		r := GroupByJumpType{
			JumpType:        typ,
			ManifestHeading: heading,
		}
		result = append(result, r)
	}
	return result
}
