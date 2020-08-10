// (c) Copyright 2017-2020 Matt Messier

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// WindsAloftSample represents a wind sample, which is composed of direction
// (degrees relative to true north), speed (knots), and temperature (Celsius)
type WindsAloftSample struct {
	Altitude         int  `json:"altitude"`
	Heading          int  `json:"heading"`
	Speed            int  `json:"speed"`
	Temperature      int  `json:"temperature"`
	LightAndVariable bool `json:"is_variable,omitempty"`
}

// WindsAloft contains all of the information parsed from an NWS winds aloft
// report.
type WindsAloft struct {
	// samples is a simple array of information for each altitude from 0 to
	// len(Samples) * 1000 feet. Each index position is 1000 feet.
	samples []WindsAloftSample

	// validTime is the time at which the sample data becomes valid. It's
	// valid for an hour.
	validTime time.Time

	// url is the full url used to request winds aloft data.
	url string

	lock sync.Mutex
}

const windsAloftURL = "https://markschulze.net/winds/winds.php?hourOffset=0"

// NewWindsAloft creates a new WindsAloft instance to track winds aloft data.
func NewWindsAloft(latitude, longitude string) *WindsAloft {
	wa := &WindsAloft{
		url: fmt.Sprintf("%s&lat=%s&lon=%s", windsAloftURL, latitude,
			longitude),
	}

	return wa
}

// Refresh retrieves and parses winds aloft data.
func (wa *WindsAloft) Refresh() error {
	request, err := http.NewRequest(http.MethodGet, wa.url, nil)
	if err != nil {
		return err
	}
	setRequestDefaults(request)
	request.Header.Set("Referer", "https://markschulze.net/winds/")

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil || len(data) == 0 {
		return err
	}

	// It would be nicer to parse the data into structs, but it's actually
	// easier to just work it out manually because JSON sucks.

	var rawWindsAloftData interface{}
	if err = json.Unmarshal(data, &rawWindsAloftData); err != nil {
		// If we get unparseable data, dump it to a file so we can
		// review it later to see what the problem is.
		_ = ioutil.WriteFile("winds.json", data, 0644)
		return err
	}
	windsAloftData, ok := rawWindsAloftData.(map[string]interface{})
	if !ok {
		return errors.New("winds aloft data is invalid")
	}

	now := time.Now()
	validHour := int(decodeInt("validtime", windsAloftData["validtime"]))
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
		return errors.New("direction information missing from winds aloft data")
	}
	if speed, ok = windsAloftData["speed"].(map[string]interface{}); !ok {
		return errors.New("speed data missing from winds aloft data")
	}
	if temp, ok = windsAloftData["temp"].(map[string]interface{}); !ok {
		return errors.New("temperature data missing from winds aloft data")
	}

	maxAltitude := len(direction)
	if len(speed) < maxAltitude {
		maxAltitude = len(speed)
	}
	if len(temp) < maxAltitude {
		maxAltitude = len(speed)
	}

	samples := make([]WindsAloftSample, maxAltitude)
	for i := 0; i < maxAltitude; i++ {
		key := strconv.FormatInt(int64(i*1000), 10)
		samples[i].Altitude = i * 1000
		samples[i].Heading = int(decodeInt(key, direction[key]))
		samples[i].Speed = int(decodeInt(key, speed[key]))
		samples[i].Temperature = int(decodeInt(key, temp[key]))
		samples[i].LightAndVariable = (samples[i].Speed <= 0)
	}

	wa.lock.Lock()
	wa.samples = samples
	wa.validTime = validTime
	wa.lock.Unlock()

	return nil
}

// Samples returns the samples most recently loaded from the data source.
func (wa *WindsAloft) Samples() []WindsAloftSample {
	wa.lock.Lock()

	var samples []WindsAloftSample
	if len(wa.samples) > 0 {
		samples = append(samples, wa.samples...)
	}

	wa.lock.Unlock()
	return samples
}

// ValidTime returns the time that the samples are valid until.
func (wa *WindsAloft) ValidTime() time.Time {
	wa.lock.Lock()
	validTime := wa.validTime
	wa.lock.Unlock()
	return validTime
}
