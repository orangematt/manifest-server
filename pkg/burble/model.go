// (c) Copyright 2017-2021 Matt Messier

package burble

import "strings"

type Jumper struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Nickname       string    `json:"nickname"`
	ShortName      string    `json:"short_name"`
	RigName        string    `json:"rig_name"`
	IsInstructor   bool      `json:"is_instructor"`
	IsTandem       bool      `json:"is_tandem"`
	IsStudent      bool      `json:"is_student"`
	IsVideographer bool      `json:"is_videographer"`
	GroupMembers   []*Jumper `json:"group_members"`
}

func NewJumper(id int64, name, nickname, shortName string) *Jumper {
	j := &Jumper{
		ID:        id,
		Name:      strings.TrimSpace(name),
		Nickname:  strings.TrimSpace(nickname),
		ShortName: strings.TrimSpace(shortName),
	}

	if strings.HasPrefix(strings.ToLower(j.Name), "jm ") {
		j.Name = strings.TrimSpace(j.Name[3:])
	}
	if strings.HasPrefix(strings.ToLower(j.Nickname), "jm ") {
		j.Nickname = strings.TrimSpace(j.Nickname[3:])
	}
	if strings.ToLower(j.ShortName) == "vs" {
		j.ShortName = "Video"
		j.IsVideographer = true
	}

	return j
}

func (j *Jumper) AddGroupMember(member *Jumper) {
	j.GroupMembers = append(j.GroupMembers, member)
	if (j.IsTandem || j.IsStudent) && !member.IsVideographer {
		member.IsInstructor = true
	}
}

type Load struct {
	ID             int64     `json:"id"`
	AircraftName   string    `json:"aircraft_name"`
	IsFueling      bool      `json:"is_fueling"`
	IsTurning      bool      `json:"is_turning"`
	IsNoTime       bool      `json:"is_no_time"`
	SlotsAvailable int64     `json:"slots_available"`
	CallMinutes    int64     `json:"call_minutes"`
	LoadNumber     string    `json:"load_number"`
	Tandems        []*Jumper `json:"tandems"`
	Students       []*Jumper `json:"students"`
	SportJumpers   []*Jumper `json:"sport_jumpers"`
}
