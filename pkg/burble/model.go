// (c) Copyright 2017-2021 Matt Messier

package burble

import "strings"

type ForEachJumperFunc func(j *Jumper)

type Jumper struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	ShortName      string    `json:"short_name"`
	RigName        string    `json:"rig_name"`
	GroupName      string    `json:"group_name"`
	GroupMembers   []*Jumper `json:"group_members"`
	IsInstructor   bool      `json:"is_instructor"`
	IsTandem       bool      `json:"is_tandem"`
	IsStudent      bool      `json:"is_student"`
	IsVideographer bool      `json:"is_videographer"`
	IsOrganizer    bool      `json:"is_organizer"`
	IsTurning      bool      `json:"is_turning"`
	IsPondSwoop    bool      `json:"is_pond_swoop"`
}

func NewJumper(id int64, name, shortName string) *Jumper {
	j := &Jumper{
		ID:        id,
		Name:      strings.TrimSpace(name),
		ShortName: strings.TrimSpace(shortName),
	}

	if strings.HasPrefix(strings.ToLower(j.Name), "jm ") {
		j.Name = strings.TrimSpace(j.Name[3:])
	}
	jump := strings.ToLower(j.ShortName)
	if jump == "vs" {
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

func (j *Jumper) ForEachGroupMember(f ForEachJumperFunc) {
	for _, member := range j.GroupMembers {
		f(member)
		member.ForEachGroupMember(f)
	}
}

type JumpersByName []*Jumper

func (j JumpersByName) Len() int {
	return len(j)
}

func (j JumpersByName) Swap(a, b int) {
	j[a], j[b] = j[b], j[a]
}

func (j JumpersByName) Less(a, b int) bool {
	return strings.ToLower(j[a].Name) < strings.ToLower(j[b].Name)
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

func (l *Load) ForEachJumper(f ForEachJumperFunc) {
	for _, j := range l.Tandems {
		f(j)
		j.ForEachGroupMember(f)
	}
	for _, j := range l.Students {
		f(j)
		j.ForEachGroupMember(f)
	}
	for _, j := range l.SportJumpers {
		f(j)
		j.ForEachGroupMember(f)
	}
}
