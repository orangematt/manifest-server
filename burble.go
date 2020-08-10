// (c) Copyright 2017-2020 Matt Messier

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	burbleBaseURL     = "https://dzm.burblesoft.com"
	burblePublicURL   = burbleBaseURL + "/jmp"
	burbleManifestURL = burbleBaseURL + "/ajax_staff_jumpermanifestpublic"
	burbleNumColumns  = 6 // # columns to ask Burble for
)

// BurbleJumper represents a jumper
type BurbleJumper struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	Nickname       string          `json:"nickname"`
	ShortName      string          `json:"short_name"`
	IsInstructor   bool            `json:"is_instructor"`
	IsTandem       bool            `json:"is_tandem"`
	IsStudent      bool            `json:"is_student"`
	IsVideographer bool            `json:"is_videographer"`
	GroupMembers   []*BurbleJumper `json:"group_members"`
}

// NewBurbleJumper creates a new representation of a jumper
func NewBurbleJumper(id int64, name, nickname, shortName string) *BurbleJumper {
	j := &BurbleJumper{
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

// AddGroupMember adds a jumper to a group of jumpers
func (j *BurbleJumper) AddGroupMember(member *BurbleJumper) {
	j.GroupMembers = append(j.GroupMembers, member)
	if (j.IsTandem || j.IsStudent) && !member.IsVideographer {
		member.IsInstructor = true
	}
}

// BurbleLoad represents a load
type BurbleLoad struct {
	ID             int64           `json:"id"`
	AircraftName   string          `json:"aircraft_name"`
	IsFueling      bool            `json:"is_fueling"`
	IsTurning      bool            `json:"is_turning"`
	IsNoTime       bool            `json:"is_no_time"`
	SlotsAvailable int64           `json:"slots_available"`
	CallMinutes    int64           `json:"call_minutes"`
	LoadNumber     string          `json:"load_number"`
	Tandems        []*BurbleJumper `json:"tandems"`
	Students       []*BurbleJumper `json:"students"`
	SportJumpers   []*BurbleJumper `json:"sport_jumpers"`
}

// Burble encapsulates data retrieved from Burble for a dropzone
type Burble struct {
	dropZoneID  int
	columnCount int
	loads       []*BurbleLoad

	lock sync.Mutex
}

// NewBurble creates a new Burble data source with the specified dropzone ID.
func NewBurble(dropZoneID int) *Burble {
	return &Burble{
		dropZoneID: dropZoneID,
	}
}

// RefreshCookies makes a throw-away request to get cookies from Burble so that
// data refreshes will work.
func (b *Burble) RefreshCookies() error {
	// Create and use our own request rather than use http.DefaultClient.Get
	// so that we can keep up the charade that we're a browser and not a
	// server app scraping data!
	urlWithDZID := fmt.Sprintf("%s?dz_id=%d", burblePublicURL, b.dropZoneID)
	request, err := http.NewRequest(http.MethodPost, urlWithDZID, nil)
	if err != nil {
		return err
	}
	setRequestDefaults(request)

	if _, err = http.DefaultClient.Do(request); err != nil {
		return err
	}

	// All we want are the cookies. They've been set in the cookie jar, so
	// we can throw away the response body.

	return nil
}

// Refresh retrieves new data from Burble
func (b *Burble) Refresh() error {
	u, err := url.Parse(burbleManifestURL)
	if err != nil {
		return err
	}
	if len(http.DefaultClient.Jar.Cookies(u)) == 0 {
		if err = b.RefreshCookies(); err != nil {
			return err
		}
	}

	bodyString := fmt.Sprintf("aircraft=0&columns=%d&display_tandem=1&display_student=1&display_sport=1&display_menu=0&font_size=0&action=getLoads&dz_id=%d&date_format=m%%2Fd%%2FY&acl_application=Burble%%20DZM", burbleNumColumns, b.dropZoneID)
	body := bytes.NewReader([]byte(bodyString))

	request, err := http.NewRequest(http.MethodPost, burbleManifestURL, body)
	if err != nil {
		return err
	}
	setRequestDefaults(request)
	request.Header.Set("Origin", burbleBaseURL)
	request.Header.Set("Referer", burblePublicURL)
	request.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// It would be nicer to parse the data into structs, but Burble returns
	// JSON data that makes that impossible. Sometimes fields are ints as
	// strings, sometimes they're ints, for empty loads, it's an empty
	// array instead of null or an empty map, etc.

	var rawBurbleData interface{}
	if err = json.Unmarshal(data, &rawBurbleData); err != nil {
		return err
	}

	var loads []*BurbleLoad
	burbleData := rawBurbleData.(map[string]interface{})
	if _, ok := burbleData["loads"]; !ok {
		return errors.New("Burble data is missing load information")
	}

	sourceLoads := burbleData["loads"].([]interface{})
	columnCount := len(sourceLoads)
	for _, rawLoadData := range sourceLoads {
		loadData, ok := rawLoadData.(map[string]interface{})
		if !ok {
			continue
		}

		// Ignore loads that are not public
		if !decodeBool("is_public", loadData["is_public"]) {
			continue
		}

		l := BurbleLoad{
			ID:           decodeInt("id", loadData["id"]),
			AircraftName: loadData["aircraft_name"].(string),
			IsFueling:    decodeBool("is_fueling", loadData["is_fueling"]),
			IsTurning:    decodeBool("is_turning", loadData["is_turning"]),
			CallMinutes:  decodeInt("time_left", loadData["time_left"]),
		}
		if l.CallMinutes >= 120 {
			l.IsNoTime = true
		}

		name := loadData["name"].(string)
		l.LoadNumber = strings.TrimSpace(name[len(l.AircraftName)+1:])

		slots, ok := loadData["slots"].(map[string]interface{})
		if !ok {
			slots = make(map[string]interface{})
		}
		maxSlots := decodeInt("max_slots", loadData["max_slots"])
		privateSlots := decodeInt("slots.private_slots", slots["private_slots"])
		publicSlots := decodeInt("slots.public_slots", slots["public_slots"])
		reserveSlots := decodeInt("reserve_slots", loadData["reserve_slots"])
		l.SlotsAvailable = maxSlots - (publicSlots + privateSlots) - reserveSlots

		groups := loadData["groups"].([]interface{})
		for _, rawGroupData := range groups {
			members := rawGroupData.([]interface{})
			memberData := members[0].(map[string]interface{})
			name = memberData["first_name"].(string) + " " +
				memberData["last_name"].(string)
			nickname := memberData["name"].(string)
			primaryID := decodeInt("id", memberData["id"])
			primaryJumper := NewBurbleJumper(
				primaryID,
				name,
				nickname,
				memberData["short_name"].(string))
			switch memberData["type"].(string) {
			case "Sport Jumper":
				l.SportJumpers = append(l.SportJumpers, primaryJumper)
			case "Student":
				primaryJumper.IsStudent = true
				l.Students = append(l.Students, primaryJumper)
			case "Tandem":
				primaryJumper.IsTandem = true
				l.Tandems = append(l.Tandems, primaryJumper)
			}
			for i, rawMemberData := range members {
				memberData = rawMemberData.(map[string]interface{})
				if !decodeBool("is_public", memberData["is_public"]) {
					l.SlotsAvailable++
				}
				if i < 1 {
					continue
				}
				name = fmt.Sprintf("%s %s",
					memberData["first_name"].(string),
					memberData["last_name"].(string))
				if primaryJumper.IsStudent || primaryJumper.IsTandem {
					nickname = name
				} else {
					nickname = memberData["name"].(string)
				}
				id := decodeInt("id", memberData["id"])
				jumper := NewBurbleJumper(id, name, nickname,
					memberData["short_name"].(string))
				if _, ok = memberData["handycam_jump"].(string); ok {
					jumper.ShortName = "Handycam"
				}
				primaryJumper.AddGroupMember(jumper)
			}
		}

		loads = append(loads, &l)
	}

	b.lock.Lock()
	b.columnCount = columnCount
	b.loads = loads
	b.lock.Unlock()

	return nil
}

func (b *Burble) Loads() []*BurbleLoad {
	b.lock.Lock()
	loads := b.loads
	b.lock.Unlock()
	return loads
}

func (b *Burble) ColumnCount() int {
	b.lock.Lock()
	columnCount := b.columnCount
	b.lock.Unlock()
	return columnCount
}
