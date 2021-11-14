// (c) Copyright 2017-2021 Matt Messier

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/orangematt/manifest-server/pkg/burble"
	"github.com/orangematt/manifest-server/pkg/core"
	"github.com/orangematt/manifest-server/pkg/settings"
)

type Manifest struct {
	Settings    *settings.Settings `json:"settings"`
	JumprunTime string             `json:"jumprun_time,omitempty"`
	WindsTime   string             `json:"winds_time,omitempty"`
	ColumnCount int                `json:"column_count"`
	Temperature string             `json:"temperature"`
	Winds       string             `json:"winds"`
	Clouds      string             `json:"clouds"`
	Weather     string             `json:"weather"`
	Separation  string             `json:"separation"`
	Message     string             `json:"message,omitempty"`
	Loads       []*burble.Load     `json:"loads"`
}

func (s *WebServer) addToManifest(slots []string, jumper *burble.Jumper) []string {
	var color, prefix string
	if jumper.IsTandem {
		color = "#ffff00" // yellow
	} else if jumper.IsStudent || strings.HasSuffix(jumper.ShortName, " + Gear") {
		color = "#00ff00" // green
		if jumper.ShortName == "3500 H/P" {
			prefix = "Hop & Pop"
		} else if jumper.ShortName == "5500 H/P" {
			prefix = "Hop & Pop"
		}
	} else if strings.HasPrefix(jumper.ShortName, "3-5k") || strings.HasPrefix(jumper.ShortName, "3.5k") {
		color = "#ff00ff" // magenta
		prefix = "Hop & Pop"
	} else {
		color = "#ffffff" // white
	}

	displayNicknames := s.app.Settings().DisplayNicknames()
	jumperName := jumper.Name
	if displayNicknames && jumper.Nickname != "" {
		jumperName = jumper.Nickname
	}
	shortName := jumper.ShortName
	if rigName := jumper.RigName; rigName != "" {
		shortName = fmt.Sprintf("%s / %s", shortName, rigName)
	}
	if jumper.IsTandem {
		slots = append(slots, fmt.Sprintf("%s Tandem: %s", color,
			jumperName))
	} else if prefix != "" {
		slots = append(slots, fmt.Sprintf("%s %s: %s (%s)", color,
			prefix, jumperName, shortName))
	} else {
		slots = append(slots, fmt.Sprintf("%s %s (%s)", color,
			jumperName, shortName))
	}

	for _, member := range jumper.GroupMembers {
		memberName := member.Name
		if displayNicknames && member.Nickname != "" {
			memberName = member.Nickname
		}
		shortName = member.ShortName
		if rigName := member.RigName; rigName != "" {
			shortName = fmt.Sprintf("%s / %s", shortName, rigName)
		}
		slots = append(slots, fmt.Sprintf("%s \t%s (%s)", color,
			memberName, shortName))
	}

	return slots
}

func (s *WebServer) messageString() string {
	if message := s.app.Settings().Message(); message != "" {
		return message
	}

	if s := s.app.SunriseMessage(); s != "" {
		return s
	}
	if s := s.app.SunsetMessage(); s != "" {
		return s
	}

	return ""
}

func (s *WebServer) updateManifestStaticData() {
	burbleSource := s.app.BurbleSource()
	metarSource := s.app.METARSource()
	settings := s.app.Settings()

	m := Manifest{
		Settings:    settings,
		ColumnCount: burbleSource.ColumnCount(),
		Message:     s.messageString(),
		Loads:       burbleSource.Loads(),
	}
	if t, ok := s.ContentModifyTime("/jumprun.json"); ok {
		m.JumprunTime = t.Format(http.TimeFormat)
	}
	if t, ok := s.ContentModifyTime("/winds"); ok {
		m.WindsTime = t.Format(http.TimeFormat)
	}
	if metarSource != nil {
		m.Temperature = metarSource.TemperatureString()
		m.Winds = metarSource.WindConditions()
		m.Clouds = metarSource.SkyCover()
		m.Weather = metarSource.WeatherConditions()
	}
	if b, err := json.Marshal(m); err == nil {
		s.SetContent("/manifest.json", b, "application/json; charset=utf-8")
	}
	aloftColor, aloftString := s.app.SeparationStrings()
	m.Separation = aloftString

	// There are five lines of information that are shown on the upper
	// right of the display. Each line output is prefixed with a color to
	// use for rendering (of the form "#rrggbb") They are:
	//
	//   1. Time (temperature)
	//   2. Winds
	//   3. Clouds
	//   4. Weather
	//   5. Winds Aloft
	//
	// The next line is the message line that is displayed regardless of
	// whether there are any loads manifesting. There's no interface to set
	// it arbitrarily yet, but it's used to show a sunset/sunrise alert.
	// This line is also prefixed with the color to use to render it.
	//
	//   6. Message (arbitrary, time to sunset/sunrise)
	//
	// The remainder of the lines sent is variable, and all have to do with
	// the manifesting loads.
	//
	//   7. Integer: # of loads manifesting
	//
	// For each load that is manifesting, the following lines are present:
	//
	//   n. ID
	//   n+1. AircraftName
	//   n+2. LoadNumber
	//   n+3. CallMinutes
	//   n+4. SlotsFilled
	//   n+5. SlotsAvailable
	//   n+6. IsTurning
	//   n+7. IsFueling
	//   n+8..n+SlotsFilled+8. #rrggbb Manifest entry

	windsColor := "#ffffff"
	/*
		windSpeed := metarSource.WindSpeedMPH()
		windGusts := metarSource.WindGustSpeedMPH()
			if windSpeed >= 17.0 || windGusts >= 17.0 {
				windsColor = "#ff0000" // red
			} else if windGusts-windSpeed >= 7 {
				windsColor = "#ffff00" // yellow
			}
	*/

	lines := make([]string, 7)
	lines[0] = fmt.Sprintf("#ffffff %s", metarSource.TemperatureString())
	lines[1] = fmt.Sprintf("%s %s", windsColor, metarSource.WindConditions())
	lines[2] = fmt.Sprintf("#ffffff %s", metarSource.SkyCover())
	lines[3] = fmt.Sprintf("#ffffff %s", metarSource.WeatherConditions())
	lines[4] = fmt.Sprintf("#%06x %s", aloftColor, aloftString)
	lines[5] = fmt.Sprintf("#ffffff %s", s.messageString())

	loads := burbleSource.Loads()
	lines[6] = fmt.Sprintf("%d", len(loads))
	for _, load := range loads {
		var slots []string

		for _, j := range load.Tandems {
			slots = s.addToManifest(slots, j)
		}
		for _, j := range load.Students {
			slots = s.addToManifest(slots, j)
		}
		for _, j := range load.SportJumpers {
			slots = s.addToManifest(slots, j)
		}

		lines = append(lines, fmt.Sprintf("%d", load.ID))
		lines = append(lines, load.AircraftName)
		lines = append(lines, fmt.Sprintf("Load %s", load.LoadNumber))
		if load.IsNoTime {
			lines = append(lines, "")
		} else {
			lines = append(lines, fmt.Sprintf("%d", load.CallMinutes))
		}
		lines = append(lines, fmt.Sprintf("%d", len(slots)))
		if load.CallMinutes <= 5 {
			lines = append(lines, fmt.Sprintf("%d aboard", len(slots)))
		} else {
			slotsStr := "slots"
			if load.SlotsAvailable == 1 {
				slotsStr = "slot"
			}
			lines = append(lines, fmt.Sprintf("%d %s", load.SlotsAvailable, slotsStr))
		}
		if load.IsTurning {
			lines = append(lines, "1")
		} else {
			lines = append(lines, "0")
		}
		if load.IsFueling {
			lines = append(lines, "1")
		} else {
			lines = append(lines, "0")
		}
		lines = append(lines, slots...)
	}

	// Deprecated
	b := []byte(strings.Join(lines, "\n") + "\n")
	now := time.Now()
	s.SetContentFunc("/manifest",
		func(w http.ResponseWriter, req *http.Request) {
			h := w.Header()

			if t := m.JumprunTime; t != "" {
				h.Set("X-Jumprun-Time", t)
			}
			if t := m.WindsTime; t != "" {
				h.Set("X-Winds-Time", t)
			}
			o := settings.Options()
			h.Set("X-Display-Weather", strconv.FormatBool(o.DisplayWeather))
			h.Set("X-Display-Winds", strconv.FormatBool(o.DisplayWinds))
			h.Set("X-Display-Nicknames", strconv.FormatBool(o.DisplayNicknames))
			h.Set("X-Column-Count", strconv.FormatInt(int64(m.ColumnCount), 10))

			h.Set("Content-Type", "text/plain; charset=utf-8")
			http.ServeContent(w, req, "", now, bytes.NewReader(b))
		})
}

func (s *WebServer) updateWindsStaticData() {
	var lines []string
	samples := s.app.WindsAloftSource().Samples()
	for _, sample := range samples {
		line := fmt.Sprintf("%d %d %d %d %v",
			sample.Altitude, sample.Heading, sample.Speed,
			sample.Temperature, sample.LightAndVariable)
		lines = append(lines, line)
	}

	s.SetContent("/winds",
		[]byte(strings.Join(lines, "\n")+"\n"),
		"text/plain; charset=utf-8")

	if b, err := json.Marshal(samples); err == nil {
		s.SetContent("/winds.json", b, "application/json; charset=utf-8")
	}
}

func (s *WebServer) updateJumprunStaticData() {
	jumprun := s.app.Jumprun()
	if jumprun == nil {
		return
	}
	j := jumprun.Jumprun()

	modifyTime := time.Unix(j.TimeStamp, 0)
	s.SetContentWithTime("/jumprun",
		j.LegacyContent(), "text/plain; charset=utf-8", modifyTime)
	if jsonContent, err := json.Marshal(j); err != nil {
		s.SetContentWithTime("/jumprun.json",
			jsonContent, "application/json; charset=utf-8", modifyTime)
	}
}

func (s *WebServer) EnableLegacySupport() {
	// Initial legacy endpoint data
	s.SetContent("/manifest", []byte("\n\n\n\n\n\n0\n"), "text/plain; charset=utf-8")
	if s.app.Settings().WindsEnabled() {
		s.SetContent("/winds", []byte{}, "text/plain; charset=utf-8")
		s.SetContent("/winds.json", []byte("{}"), "application/json; charset=utf-8")
	}

	c := make(chan core.DataSource, 64)
	s.app.AddListener(c)

	// Spawn a goroutine to listen for events from the controller and update
	// the static content that's returned for legacy clients.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.app.Done():
				return
			case source := <-c:
			drain:
				for {
					select {
					case s := <-c:
						source |= s
					default:
						break drain
					}
				}
				if source&core.WindsAloftDataSource != 0 {
					s.updateWindsStaticData()
				}
				if source&core.JumprunDataSource != 0 {
					s.updateJumprunStaticData()
				}
				s.updateManifestStaticData()
			}
		}
	}()
}
