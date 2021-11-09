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
	"strings"
	"sync"

	"github.com/orangematt/manifest-server/pkg/decode"
	"github.com/orangematt/manifest-server/pkg/settings"
)

const (
	burbleBaseURL     = "https://dzm.burblesoft.com"
	burblePublicURL   = burbleBaseURL + "/jmp"
	burbleManifestURL = burbleBaseURL + "/ajax_staff_jumpermanifestpublic"
	burbleNumColumns  = 6 // # columns to ask Burble for
)

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

	sourceLoads := burbleData["loads"].([]interface{})
	columnCount := len(sourceLoads)
	for _, rawLoadData := range sourceLoads {
		loadData, ok := rawLoadData.(map[string]interface{})
		if !ok {
			continue
		}

		// Ignore loads that are not public
		if !decode.Bool("is_public", loadData["is_public"]) {
			continue
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

		name := loadData["name"].(string)
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
			name = memberData["first_name"].(string) + " " +
				memberData["last_name"].(string)
			nickname := memberData["name"].(string)
			primaryID := decode.Int("id", memberData["id"])
			primaryJumper := NewJumper(
				primaryID,
				name,
				nickname,
				memberData["short_name"].(string))
			if rigName, ok := memberData["rig_name"].(string); ok {
				primaryJumper.RigName = rigName
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
				name = fmt.Sprintf("%s %s",
					memberData["first_name"].(string),
					memberData["last_name"].(string))
				if primaryJumper.IsStudent || primaryJumper.IsTandem {
					nickname = name
				} else {
					nickname = memberData["name"].(string)
				}
				id := decode.Int("id", memberData["id"])
				jumper := NewJumper(id, name, nickname,
					memberData["short_name"].(string))
				if _, ok = memberData["handycam_jump"].(string); ok {
					jumper.ShortName = "Handycam"
				}
				if rigName, ok := memberData["rig_name"].(string); ok {
					jumper.RigName = rigName
				}
				primaryJumper.AddGroupMember(jumper)
			}
		}

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

	c.lock.Lock()
	defer c.lock.Unlock()

	changed := false
	if c.columnCount != columnCount {
		c.columnCount = columnCount
		changed = true
	}
	if !reflect.DeepEqual(c.loads, loads) {
		c.loads = loads
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
