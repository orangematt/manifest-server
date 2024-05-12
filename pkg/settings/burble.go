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

func (s *Settings) ShortNameRewrites() map[string]string {
	raw_rewrites := s.config.Get("burble.short_name_rewrites")
	rewrites, ok := raw_rewrites.([]interface{})
	if !ok {
		return nil
	}

	result := make(map[string]string)
	for _, r := range rewrites {
		rr, rrok := r.(map[string]interface{})
		if !rrok {
			continue
		}

		from, fok := rr["from"].(string)
		if !fok {
			fmt.Fprintf(os.Stderr, "error: missing from for burble.short_name_rewrites\n")
			continue
		}

		to, tok := rr["to"].(string)
		if !tok {
			fmt.Fprintf(os.Stderr, "error: missing to for burlbe.short_name_rewrites\n")
			continue
		}

		result[from] = to
	}
	return result
}
