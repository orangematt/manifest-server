// (c) Copyright 2017-2021 Matt Messier

package metar

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// FahrenheitFromCelsius converts a temperature from Celsius to Fahrenheit.
func FahrenheitFromCelsius(c float64) float64 {
	return ((c * 9.0) / 5.0) + 32.0
}

var cardinalDirections = []string{
	"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
	"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW",
}

// MPHFromKnots converts a speed from knots to miles per hour.
func MPHFromKnots(kts float64) float64 {
	return kts * 1.151
}

// CardinalDirection returns the cardinal direction expressed as a string
// from a value in degrees.
func CardinalDirection(degrees float64) string {
	for degrees < 0.0 {
		degrees += 360.0
	}
	n := math.Floor(math.Mod(degrees+11.25, 360.0) / 22.5)
	return cardinalDirections[int(n)]
}

var descriptors = map[string]string{
	"MI": "shallow ",
	"PR": "partial ",
	"BC": "patches of ",
	"DR": "low drifting ",
	"BL": "blowing ",
	"SH": "showers ",
	"TS": "thunderstorm ",
	"FZ": "freezing ",
}

var conditions = map[string]string{
	"RA": "rain",
	"DZ": "drizzle",
	"SN": "snow",
	"SG": "snow grains",
	"IC": "ice crystals",
	"PL": "ice pellets",
	"GR": "hail",
	"GS": "small hail and/or snow pellets",
	"FG": "fog",
	"VA": "volcanic ash",
	"BR": "mist",
	"HZ": "haze",
	"DU": "widespread dust",
	"FU": "smoke",
	"SA": "sand",
	"PY": "spray",
	"SQ": "squall",
	"PO": "dust or sand whirls",
	"DS": "dust storm",
	"SS": "sandstorm",
	"FC": "funnel cloud",
	"UP": "unknown precipitation",
}

func weatherCondition(wx string) string {
	var results []string

	parts := strings.Fields(wx)
	i := 0
	for i < len(parts) {
		var intensity, suffix string

		bit := parts[i]
		switch {
		case strings.HasPrefix(bit, "-"):
			intensity = "light "
			bit = bit[1:]
		case strings.HasPrefix(bit, "+"):
			intensity = "heavy "
			bit = bit[1:]
		case bit == "VC":
			suffix = " in the vicinity"
			i++
			bit = parts[i]
		}

		descriptor, ok := descriptors[bit]
		if ok {
			i++
			if i >= len(parts) {
				results = append(results,
					intensity+descriptor+suffix)
				break
			}
			bit = parts[i]
		}

		condition, ok := conditions[bit]
		if !ok {
			i++
			continue
		}

		i++
		results = append(results, intensity+descriptor+condition+suffix)
	}

	if len(results) == 0 {
		return "clear"
	}
	return strings.Join(results, ", ")
}

type Controller struct {
	station string

	lock        sync.Mutex
	fields      map[string]interface{}
	skyCover    string
	wxCondition string
}

func NewController(station string) *Controller {
	return &Controller{
		station: station,
	}
}

const metarURL = "https://aviationweather.gov/adds/dataserver_current/httpparam?datasource=metars&requesttype=retrieve&format=csv&hoursBeforeNow=24&mostRecent=true"

// Refresh retrieves and parses weather data.
func (c *Controller) Refresh() (bool, error) {
	url := fmt.Sprintf("%s&stationString=%s", metarURL, c.station)
	resp, err := http.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	// There should be at least 5 lines. Any less is invalid data.
	// Line 0: "No errors"
	// Line 1: "No warnings"
	// Line 2: "%d ms"
	// Line 3: "data source=metars"
	// Line 4: "%d results"
	// Line 5: <csv keywords>
	// Line 6: <csv data>
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 5 {
		for i, l := range lines {
			l = strings.TrimSpace(l)
			fmt.Printf("Line %d: %s\n", i, l)
		}
		return false, fmt.Errorf("Too few lines (expected >= 5; got %d)",
			len(lines))
	}

	nresults, err := strconv.Atoi(strings.Fields(strings.TrimSpace(lines[4]))[0])
	if err != nil {
		return false, fmt.Errorf("Error parsing # results: %v", err)
	}
	if nresults < 1 {
		return false, errors.New("No results")
	}

	var (
		lowClouds, highClouds []string
		wxCondition           string
	)

	parsedFields := make(map[string]interface{})
	names := strings.Split(strings.TrimSpace(lines[5]), ",")
	fields := strings.Split(strings.TrimSpace(lines[len(lines)-1]), ",")
	for i, name := range names {
		switch name {
		case "wx_string":
			wxCondition = weatherCondition(fields[i])
		case "sky_cover":
			if i+1 < len(names) && names[i+1] == "cloud_base_ft_agl" {
				var base int
				base, err = strconv.Atoi(fields[i+1])
				if err != nil {
					break
				}
				switch fields[i] {
				case "FEW":
					lowClouds = append(lowClouds, fmt.Sprintf("few at %d", base))
				case "SCT":
					lowClouds = append(lowClouds, fmt.Sprintf("scattered at %d", base))
				case "BKN":
					highClouds = append(highClouds, fmt.Sprintf("broken at %d", base))
				case "OVC":
					highClouds = append(highClouds, fmt.Sprintf("overcast deck at %d", base))
				case "OVX":
					highClouds = append(highClouds, "overcast")
				case "SKC", "CLR":
					break
				}
			}
		case "cloud_base_ft_agl":
			// Always skip; used by "sky_cover"
			break
		default:
			var intValue int64
			if intValue, err = strconv.ParseInt(fields[i], 0, 64); err == nil {
				parsedFields[name] = intValue
				break
			}
			var floatValue float64
			if floatValue, err = strconv.ParseFloat(fields[i], 64); err == nil {
				parsedFields[name] = floatValue
				break
			}
			var boolValue bool
			if boolValue, err = strconv.ParseBool(fields[i]); err == nil {
				parsedFields[name] = boolValue
				break
			}
			parsedFields[name] = fields[i]
		}
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	changed := false
	if !reflect.DeepEqual(c.fields, parsedFields) {
		c.fields = parsedFields
		changed = true
	}
	skyCover := "clear"
	if len(highClouds) > 0 {
		skyCover = strings.Join(highClouds, ", ")
	} else if len(lowClouds) > 0 {
		skyCover = strings.Join(lowClouds, ", ")
	}
	if c.skyCover != skyCover {
		c.skyCover = skyCover
		changed = true
	}
	if c.wxCondition != wxCondition {
		c.wxCondition = wxCondition
		changed = true
	}

	return changed, nil
}

// WindSpeedMPH returns the current wind speed in MPH.
func (c *Controller) WindSpeedMPH() float64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	var speed float64
	switch v := c.fields["wind_speed_kt"].(type) {
	case float64:
		speed = v
	case int64:
		speed = float64(v)
	default:
		return 0.0
	}
	return MPHFromKnots(speed)
}

// WindGustSpeedMPH returns current wind gust speed in MPH.
func (c *Controller) WindGustSpeedMPH() float64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	var gusting float64
	switch v := c.fields["wind_gust_kt"].(type) {
	case float64:
		gusting = v
	case int64:
		gusting = float64(v)
	default:
		return 0.0
	}
	return MPHFromKnots(gusting)
}

// WindDirectionDegrees returns the current wind direction in degrees.
func (c *Controller) WindDirectionDegrees() float64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	var windDirectionDegrees float64
	switch v := c.fields["wind_dir_degrees"].(type) {
	case float64:
		windDirectionDegrees = v
	case int64:
		windDirectionDegrees = float64(v)
	default:
		return 0.0
	}
	// Adjust true north to magnetic north. Magnetic deviance for Orange, MA is -14.7
	return float64((int(windDirectionDegrees) - 14 + 360) % 360)
}

// WindConditions returns the current wind conditions as a human-readable string.
func (c *Controller) WindConditions() string {
	speed := c.WindSpeedMPH()
	if speed <= 0 {
		return "light and variable"
	}

	windDirectionDegrees := c.WindDirectionDegrees()
	windDirection := CardinalDirection(windDirectionDegrees)

	gusting := c.WindGustSpeedMPH()
	if gusting > 0 {
		return fmt.Sprintf("%d MPH gusting to %d MPH from %d° (%s)",
			int64(speed), int64(gusting),
			int64(windDirectionDegrees), windDirection)
	}
	return fmt.Sprintf("%d MPH from %d° (%s)",
		int64(speed), int64(windDirectionDegrees), windDirection)
}

// WeatherConditions returns a human-readable description of current weather
// conditions (raining, snowing, clear, etc.)
func (c *Controller) WeatherConditions() string {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.wxCondition == "" {
		return "data error"
	}
	return c.wxCondition
}

// SkyCover returns a human-readable description of the current sky cover.
func (c *Controller) SkyCover() string {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.skyCover == "" {
		return "data error"
	}
	return c.skyCover
}

// TemperatureString returns a human-readable temperature string
func (c *Controller) TemperatureString() string {
	c.lock.Lock()
	defer c.lock.Unlock()
	var temp float64
	switch v := c.fields["temp_c"].(type) {
	case float64:
		temp = v
	case int64:
		temp = float64(v)
	default:
		return "data error"
	}

	return fmt.Sprintf("%d℃ / %d℉",
		int64(temp), int64(FahrenheitFromCelsius(temp)))
}

func (c *Controller) Location() (float64, float64, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	latitude, ok := c.fields["latitude"].(float64)
	if !ok {
		return 0, 0, false
	}
	longitude, ok := c.fields["longitude"].(float64)
	if !ok {
		return 0, 0, false
	}
	return latitude, longitude, true
}
