// (c) Copyright 2017-2021 Matt Messier

package burble

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/jumptown-skydiving/manifest-server/pkg/decode"
	"github.com/jumptown-skydiving/manifest-server/pkg/settings"
)

const (
	burbleBaseURL     = "https://dzm.burblesoft.com"
	burblePublicURL   = burbleBaseURL + "/jmp"
	burbleManifestURL = burbleBaseURL + "/ajax_dzm2_frontend_jumpermanifestpublic"
)

func parseGroupName(s string) string {
	// Strip off suffixes of the form '-##'
	for {
		x := strings.LastIndexByte(s, '-')
		if x == -1 {
			break
		}
		for _, c := range s[x+1:] {
			if !unicode.IsDigit(c) {
				return s
			}
		}
		s = s[:x]
	}
	return s
}

func jumperFromJSON(json map[string]interface{}) *Jumper {
	name := json["name"].(string)
	id := decode.Int("id", json["id"])
	shortName := json["jump"].(string)
	if s, ok := json["handycam_jump"].(string); ok && s != "" {
		shortName = "Handycam"
	}

	jumper := NewJumper(id, name, shortName)
	if gn, ok := json["group_number"].(string); ok {
		jumper.GroupName = parseGroupName(gn)
	}

	// use rig_name if it's present, but fallback to broken rig_id instead
	// rig_id is inconsistent with other name/id fields in the Burble data.
	// The field used to be rig_name until Burble did a major data revision
	// and it became rig_id.
	// Update: Looks like Burble fixed this at some point over the summer.
	//         Leave all of this here for now until we can verify the fix,
	//         but add an additional "0" check for "rig_id"
	if rigName, ok := json["rig_name"].(string); ok && rigName != "" {
		jumper.RigName = rigName
	} else if rigName, ok = json["rig_id"].(string); ok && rigName != "" && rigName != "0" {
		jumper.RigName = rigName
	}
	return jumper
}

type Controller struct {
	settings    *settings.Settings
	columnCount int
	loads       []*Load

	lock sync.Mutex
}

func NewController(settings *settings.Settings) *Controller {
	return &Controller{
		settings: settings,
	}
}

// RefreshCookies makes a throw-away request to get cookies from Burble so that
// data refreshes will work.
func (c *Controller) RefreshCookies() error {
	// Create and use our own request rather than use http.DefaultClient.Get
	// so that we can keep up the charade that we're a browser and not a
	// server app scraping data!
	dzid := c.settings.BurbleDropzoneID()
	urlWithDZID := fmt.Sprintf("%s?dz_id=%d", burblePublicURL, dzid)
	request, err := c.settings.NewHTTPRequest(http.MethodPost, urlWithDZID, nil)
	if err != nil {
		return err
	}

	if _, err = http.DefaultClient.Do(request); err != nil {
		return err
	}

	// All we want are the cookies. They've been set in the cookie jar, so
	// we can throw away the response body.

	return nil
}

// Refresh retrieves new data from Burble
func (c *Controller) Refresh() (bool, error) {
	u, err := url.Parse(burbleManifestURL)
	if err != nil {
		return false, err
	}
	if len(http.DefaultClient.Jar.Cookies(u)) == 0 {
		if err = c.RefreshCookies(); err != nil {
			return false, err
		}
	}

	// Ask Burble for the number of columns we want to display + 1
	// Do this so that we can filter out loads older tha min call minutes,
	// but still be able to determine which jumpers are turning.
	burbleNumColumns := c.settings.DisplayColumns() + 1

	dzid := c.settings.BurbleDropzoneID()
	bodyString := fmt.Sprintf("aircraft=0&columns=%d&display_tandem=1&display_student=1&display_sport=1&display_menu=0&font_size=0&action=getLoads&dz_id=%d&date_format=m%%2Fd%%2FY&acl_application=Burble%%20DZM", burbleNumColumns, dzid)
	body := bytes.NewReader([]byte(bodyString))

	request, err := c.settings.NewHTTPRequest(http.MethodPost, burbleManifestURL, body)
	if err != nil {
		return false, err
	}
	request.Header.Set("Origin", burbleBaseURL)
	request.Header.Set("Referer", burblePublicURL)
	request.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	// It would be nicer to parse the data into structs, but Burble returns
	// JSON data that makes that impossible. Sometimes fields are ints as
	// strings, sometimes they're ints, for empty loads, it's an empty
	// array instead of null or an empty map, etc.

	var rawBurbleData interface{}
	if err = json.Unmarshal(data, &rawBurbleData); err != nil {
		return false, err
	}

	var loads []*Load
	burbleData := rawBurbleData.(map[string]interface{})
	if _, ok := burbleData["loads"]; !ok {
		return false, errors.New("Burble data is missing load information")
	}

	organizerStrings := c.settings.OrganizerStrings()
	sourceLoads := burbleData["loads"].([]interface{})
	columnCount := burbleNumColumns - 1
	for _, rawLoadData := range sourceLoads {
		loadData, ok := rawLoadData.(map[string]interface{})
		if !ok {
			continue
		}

		// Ignore loads that are not public. The old format had this
		// field, but the new format does not. Honor it if it comes
		// back.
		if isPublic, ok := loadData["is_public"]; ok {
			if !decode.Bool("is_public", isPublic) {
				continue
			}
		}

		l := Load{
			ID:           decode.Int("id", loadData["id"]),
			AircraftName: loadData["aircraft_name"].(string),
			IsFueling:    decode.Bool("is_fueling", loadData["is_fueling"]),
			IsTurning:    decode.Bool("is_turning", loadData["is_turning"]),
			CallMinutes:  decode.Int("time_left", loadData["time_left"]),
		}
		if l.CallMinutes >= 120 {
			l.IsNoTime = true
		}

		// aircraft_name seems to always be "" in the new format
		name := loadData["name"].(string)
		if l.AircraftName == "" {
			if x := strings.LastIndex(name, " "); x != -1 {
				l.AircraftName = name[:x]
			}
		}
		l.LoadNumber = strings.TrimSpace(name[len(l.AircraftName)+1:])

		// Reporting of available slots seems to be something Burble has
		// had ongoing difficulties with. How it's reported and its own
		// code for rendering it has evolved over the years, but in the
		// spring of 2021 it was completely broken, so rather than trust
		// the summary data we're given, we now compute it ourselves.
		// Burble has since fixed its spring 2021 breakage, but using
		// our own computation has continued to work and I'm feeling
		// more trusting of it given the troubled history here.
		var privateSlots, publicSlots int64
		maxSlots := decode.Int("max_slots", loadData["max_slots"])
		reserveSlots := decode.Int("reserve_slots", loadData["reserve_slots"])

		groups := loadData["groups"].([]interface{})
		for _, rawGroupData := range groups {
			members := rawGroupData.([]interface{})
			memberData := members[0].(map[string]interface{})
			primaryJumper := jumperFromJSON(memberData)

			jump := strings.ToLower(primaryJumper.ShortName)
			for _, o := range organizerStrings {
				if jump == o {
					primaryJumper.IsOrganizer = true
					break
				}
			}

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
				switch {
				case decode.Bool("is_public", memberData["is_public"]):
					publicSlots++
				case decode.Bool("is_private", memberData["is_private"]):
					privateSlots++
				}
				if i < 1 {
					continue
				}
				jumper := jumperFromJSON(memberData)
				primaryJumper.AddGroupMember(jumper)
			}
		}

		// Group sport jumpers by organizer. Start by building a map of
		// jumpers that are in the same group.
		groupNames := make(map[string][]*Jumper)
		for _, j := range l.SportJumpers {
			groupName := j.GroupName
			if j.GroupName == "" {
				groupName = j.Name
			}
			groupNames[groupName] = append(groupNames[groupName], j)
		}

		// Empty the SportJumpers list to rebuild it. Iterate over each
		// unique group to find groups with organizers. Any group that
		// has no organizer is not treated as a group and all members
		// are added to the manifest individually.
		l.SportJumpers = l.SportJumpers[:0]
	outerLoop:
		for _, members := range groupNames {
			for _, member := range members {
				if !member.IsOrganizer {
					continue
				}
				organizer := member
				l.SportJumpers = append(l.SportJumpers, organizer)
				for _, m := range members {
					if m != organizer {
						organizer.AddGroupMember(m)
					}
				}
				sort.Sort(JumpersByName(organizer.GroupMembers))
				continue outerLoop
			}
			l.SportJumpers = append(l.SportJumpers, members...)
		}

		// Sort tandems, students, and sport jumpers by name. Burble
		// used to do this for us, but no longer does. The sort is a
		// simple lexicographical sort by full name rather than by
		// locale aware surname.
		sort.Sort(JumpersByName(l.Tandems))
		sort.Sort(JumpersByName(l.Students))
		sort.Sort(JumpersByName(l.SportJumpers))

		// Make private slots count against reserve slots. It
		// would seem to be the case that PrivateSlots mean
		// ReserveSlots that are manifested. The zero caps here
		// shouldn't be needed, but are included for "safety"
		reserveSlots -= privateSlots
		if reserveSlots < 0 {
			reserveSlots = 0
		}
		l.SlotsAvailable = maxSlots - publicSlots - privateSlots - reserveSlots
		if l.SlotsAvailable < 0 {
			l.SlotsAvailable = 0
		}

		loads = append(loads, &l)
	}

	for i := 0; i < len(loads)-1; i++ {
		thisLoad := loads[i]
		nextLoad := loads[i+1]
		thisLoad.ForEachJumper(func(thisJumper *Jumper) {
			nextLoad.ForEachJumper(func(nextJumper *Jumper) {
				if thisJumper.Name == nextJumper.Name {
					nextJumper.IsTurning = true
				}
			})
		})
	}

	// Delete loads with CallMinutes older than our minimum setting
	minCallMinutes := c.settings.MinCallMinutes()
	var finalLoads []*Load
	for _, load := range loads {
		if int(load.CallMinutes) >= minCallMinutes {
			finalLoads = append(finalLoads, load)
			if len(finalLoads) >= columnCount {
				break
			}
		}
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	changed := false
	if c.columnCount != columnCount {
		c.columnCount = columnCount
		changed = true
	}
	if !reflect.DeepEqual(c.loads, finalLoads) {
		c.loads = finalLoads
		changed = true
	}

	return changed, nil
}

func (c *Controller) Loads() []*Load {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.loads
}

func (c *Controller) ColumnCount() int {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.columnCount
}
