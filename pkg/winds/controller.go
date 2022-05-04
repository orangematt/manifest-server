// (c) Copyright 2017-2021 Matt Messier

package winds

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/jumptown-skydiving/manifest-server/pkg/decode"
	"github.com/jumptown-skydiving/manifest-server/pkg/settings"
)

type Controller struct {
	settings *settings.Settings

	// samples is a simple array of information for each altitude from 0 to
	// len(Samples) * 1000 feet. Each index position is 1000 feet.
	samples []Sample

	// validTime is the time at which the sample data becomes valid. It's
	// valid for an hour.
	validTime time.Time

	// url is the full url used to request winds aloft data.
	url string

	lock sync.Mutex
}

const windsAloftURL = "https://markschulze.net/winds/winds.php?hourOffset=0"

func NewController(settings *settings.Settings) *Controller {
	latitude := settings.WindsLatitude()
	longitude := settings.WindsLongitude()
	wa := &Controller{
		settings: settings,
		url: fmt.Sprintf("%s&lat=%s&lon=%s", windsAloftURL, latitude,
			longitude),
	}

	return wa
}

func (c *Controller) Refresh() (bool, error) {
	request, err := c.settings.NewHTTPRequest(http.MethodGet, c.url, nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Referer", "https://markschulze.net/winds/")

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil || len(data) == 0 {
		return false, err
	}

	// It would be nicer to parse the data into structs, but it's actually
	// easier to just work it out manually because JSON sucks.

	var rawWindsAloftData interface{}
	if err = json.Unmarshal(data, &rawWindsAloftData); err != nil {
		// If we get unparseable data, dump it to a file so we can
		// review it later to see what the problem is.
		_ = ioutil.WriteFile("winds.json", data, 0644)
		return false, err
	}
	windsAloftData, ok := rawWindsAloftData.(map[string]interface{})
	if !ok {
		return false, errors.New("winds aloft data is invalid")
	}

	now := time.Now()
	validHour := int(decode.Int("validtime", windsAloftData["validtime"]))
	validTime := time.Date(now.Year(), now.Month(), now.Day(),
		validHour, 0, 0, 0, time.UTC)
	if validHour < now.Hour() {
		validTime = validTime.Add(24 * time.Hour)
	}

	// Parse out the data that we want. We care about "direction", "speed",
	// and "temp".
	var (
		direction map[string]interface{}
		speed     map[string]interface{}
		temp      map[string]interface{}
	)
	if direction, ok = windsAloftData["direction"].(map[string]interface{}); !ok {
		return false, errors.New("direction information missing from winds aloft data")
	}
	if speed, ok = windsAloftData["speed"].(map[string]interface{}); !ok {
		return false, errors.New("speed data missing from winds aloft data")
	}
	if temp, ok = windsAloftData["temp"].(map[string]interface{}); !ok {
		return false, errors.New("temperature data missing from winds aloft data")
	}

	maxAltitude := len(direction)
	if len(speed) < maxAltitude {
		maxAltitude = len(speed)
	}
	if len(temp) < maxAltitude {
		maxAltitude = len(speed)
	}

	samples := make([]Sample, maxAltitude)
	for i := 0; i < maxAltitude; i++ {
		key := strconv.FormatInt(int64(i*1000), 10)
		samples[i].Altitude = i * 1000
		samples[i].Heading = int(decode.Int(key, direction[key]))
		samples[i].Speed = int(decode.Int(key, speed[key]))
		samples[i].Temperature = int(decode.Int(key, temp[key]))
		samples[i].LightAndVariable = (samples[i].Speed <= 0)
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	changed := false
	if !reflect.DeepEqual(c.samples, samples) {
		c.samples = samples
		changed = true
	}
	if c.validTime != validTime {
		c.validTime = validTime
		changed = true
	}

	return changed, nil
}

// Samples returns the samples most recently loaded from the data source.
func (c *Controller) Samples() []Sample {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.samples
}

// ValidTime returns the time that the samples are valid until.
func (c *Controller) ValidTime() time.Time {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.validTime
}
