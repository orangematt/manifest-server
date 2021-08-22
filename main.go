// (c) Copyright 2017-2020 Matt Messier

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kelvins/sunrisesunset"
	"github.com/spf13/viper"

	"golang.org/x/net/publicsuffix"
)

const (
	messageUpdateFrequency = 5 * time.Second

	mimetypeHTML  = "text/html; charset=utf-8"
	mimetypeJSON  = "application/json; charset=utf-8"
	mimetypePlain = "text/plain; charset=utf-8"
)

var (
	done = make(chan struct{})
	wg   sync.WaitGroup

	config     *viper.Viper
	dzLocation *time.Location
	settings   *Settings
	jumprun    *JumprunManager
	webServer  *WebServer

	metarSource      *METAR
	burbleSource     *Burble
	windsAloftSource *WindsAloft

	messageLock sync.Mutex
	message     string
)

type Manifest struct {
	Settings    *Settings     `json:"settings"`
	JumprunTime string        `json:"jumprun_time,omitempty"`
	WindsTime   string        `json:"winds_time,omitempty"`
	ColumnCount int           `json:"column_count"`
	Temperature string        `json:"temperature"`
	Winds       string        `json:"winds"`
	Clouds      string        `json:"clouds"`
	Weather     string        `json:"weather"`
	Separation  string        `json:"separation"`
	Message     string        `json:"message,omitempty"`
	Loads       []*BurbleLoad `json:"loads"`
}

func currentTime() time.Time {
	return time.Now().In(dzLocation)
}

func separationDelay(speed int) int {
	msec := (1852.0 * float64(speed)) / 3600.0
	ftsec := msec / 0.3048
	return int(math.Ceil(1000.0 / ftsec))
}

func windsAloftString() (string, string) {
	color := "#ffffff"
	if windsAloftSource == nil {
		return color, ""
	}

	// We're only interested in 13000 feet
	samples := windsAloftSource.Samples()
	if len(samples) < 14 {
		return color, ""
	}
	sample := samples[13]

	var (
		s, t  string
		speed int
	)
	if sample.LightAndVariable {
		speed = 85
	} else {
		speed = 85 - sample.Speed
	}
	if speed <= 0 {
		color = "#ff0000"
		s = fmt.Sprintf("Winds are %d knots",
			sample.Speed)
	} else {
		s = fmt.Sprintf("Separation is %d seconds",
			separationDelay(speed))
	}

	t = fmt.Sprintf("(%d℃ / %d℉)", sample.Temperature,
		int64(FahrenheitFromCelsius(float64(sample.Temperature))))

	if s != "" && t != "" {
		return color, fmt.Sprintf("%s %s", s, t)
	}
	if s == "" {
		return color, t
	}
	if t == "" {
		return color, s
	}

	return color, ""
}

func addToManifest(slots []string, jumper *BurbleJumper) []string {
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

	if jumper.IsTandem {
		slots = append(slots, fmt.Sprintf("%s Tandem: %s", color,
			jumper.Name))
	} else if prefix != "" {
		slots = append(slots, fmt.Sprintf("%s %s: %s (%s)", color,
			prefix, jumper.Name, jumper.ShortName))
	} else {
		slots = append(slots, fmt.Sprintf("%s %s (%s)", color,
			jumper.Name, jumper.ShortName))
	}

	for _, member := range jumper.GroupMembers {
		slots = append(slots, fmt.Sprintf("%s \t%s (%s)", color,
			member.Name, member.ShortName))
	}

	return slots
}

func sunriseAndSunsetTimes() (sunrise time.Time, sunset time.Time, err error) {
	dzTimeNow := currentTime()
	_, utcOffset := dzTimeNow.Zone()

	latitude, longitude, ok := metarSource.Location()
	if !ok {
		err = errors.New("location is unknown")
		return
	}

	sunrise, sunset, err = sunrisesunset.GetSunriseSunset(
		latitude, longitude, float64(utcOffset)/3600.0, dzTimeNow)
	if err != nil {
		return
	}

	year, month, day := dzTimeNow.Date()
	sunrise = time.Date(year, month, day, sunrise.Hour(), sunrise.Minute(), sunrise.Second(), 0, dzTimeNow.Location())
	sunset = time.Date(year, month, day, sunset.Hour(), sunset.Minute(), sunset.Second(), 0, dzTimeNow.Location())

	return
}

func messageString() string {
	messageLock.Lock()
	defer messageLock.Unlock()

	if message != "" {
		return message
	}

	dzTimeNow := currentTime()

	sunrise, sunset, err := sunriseAndSunsetTimes()
	if err != nil {
		return ""
	}

	if dzTimeNow.Before(sunset) {
		delta := sunset.Sub(dzTimeNow).Minutes()
		switch {
		case delta == 1:
			return "Sunset is in 1 minute"
		case delta == 60:
			return "Sunset is in 1 hour"
		case delta > 1 && delta < 60:
			return fmt.Sprintf("Sunset is in %d minutes", int(delta))
		}
	}
	if dzTimeNow.Before(sunrise) {
		delta := sunrise.Sub(dzTimeNow).Minutes()
		switch {
		case delta == 1:
			return "Sunrise is in 1 minute"
		case delta == 60:
			return "Sunrise is in 1 hour"
		case delta > 1 && delta < 60:
			return fmt.Sprintf("Sunrise is in %d minutes", int(delta))
		}
	}

	return ""
}

func updateManifestStaticData() {
	// Clear the active jumprun at sunrise
	if jumprun != nil {
		sunrise, _, err := sunriseAndSunsetTimes()
		if err == nil {
			dzTimeNow := currentTime()
			activeJumprunTime := time.Unix(jumprun.Jumprun().TimeStamp, 0).In(dzLocation)
			if activeJumprunTime.Before(sunrise) && dzTimeNow.After(sunrise) {
				jumprun.Reset()
				if err = jumprun.Write(); err != nil {
					fmt.Fprintf(os.Stderr, "cannot save jumprun state: %v\n", err)
				}
			}
		}
	}

	m := Manifest{
		Settings:    settings,
		ColumnCount: burbleSource.ColumnCount(),
		Message:     messageString(),
		Loads:       burbleSource.Loads(),
	}
	if t, ok := webServer.ContentModifyTime("/jumprun.json"); ok {
		m.JumprunTime = t.Format(http.TimeFormat)
	}
	if t, ok := webServer.ContentModifyTime("/winds"); ok {
		m.WindsTime = t.Format(http.TimeFormat)
	}
	if metarSource != nil {
		m.Temperature = metarSource.TemperatureString()
		m.Winds = metarSource.WindConditions()
		m.Clouds = metarSource.SkyCover()
		m.Weather = metarSource.WeatherConditions()
	}
	if b, err := json.Marshal(m); err == nil {
		webServer.SetContent("/manifest.json", b, mimetypeJSON)
	}
	aloftColor, aloftString := windsAloftString()
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
	lines[4] = fmt.Sprintf("%s %s", aloftColor, aloftString)
	lines[5] = fmt.Sprintf("#ffffff %s", messageString())

	loads := burbleSource.Loads()
	lines[6] = fmt.Sprintf("%d", len(loads))
	for _, load := range loads {
		var slots []string

		for _, j := range load.Tandems {
			slots = addToManifest(slots, j)
		}
		for _, j := range load.Students {
			slots = addToManifest(slots, j)
		}
		for _, j := range load.SportJumpers {
			slots = addToManifest(slots, j)
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
	webServer.SetContentFunc("/manifest",
		func(w http.ResponseWriter, req *http.Request) {
			h := w.Header()

			if t := m.JumprunTime; t != "" {
				h.Set("X-Jumprun-Time", t)
			}
			if t := m.WindsTime; t != "" {
				h.Set("X-Winds-Time", t)
			}
			h.Set("X-Display-Weather", strconv.FormatBool(settings.DisplayWeather))
			h.Set("X-Display-Winds", strconv.FormatBool(settings.DisplayWinds))
			h.Set("X-Column-Count", strconv.FormatInt(int64(m.ColumnCount), 10))

			h.Set("Content-Type", mimetypePlain)
			http.ServeContent(w, req, "", now, bytes.NewReader(b))
		})
}

func updateWindsStaticData() {
	var lines []string
	samples := windsAloftSource.Samples()
	for _, sample := range samples {
		line := fmt.Sprintf("%d %d %d %d %v",
			sample.Altitude, sample.Heading, sample.Speed,
			sample.Temperature, sample.LightAndVariable)
		lines = append(lines, line)
	}

	webServer.SetContent("/winds",
		[]byte(strings.Join(lines, "\n")+"\n"),
		mimetypePlain)

	if b, err := json.Marshal(samples); err == nil {
		webServer.SetContent("/winds.json", b, mimetypeJSON)
	}
}

func launchDataSource(
	nextRefresh func() time.Time,
	sourceName string,
	refresh func() error,
	update func(),
) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			if err := refresh(); err != nil {
				fmt.Printf("Error refreshing %s: %v\n", sourceName, err)
			} else {
				update()
			}

			nextTime := nextRefresh()
			//fmt.Printf("%s: next refresh at %s\n", sourceName, nextTime.Format("15:04:05"))
			refreshPeriod := time.Until(nextTime)
			t := time.NewTicker(refreshPeriod)

			select {
			case <-done:
				t.Stop()
				return
			case <-t.C:
				t.Stop()
				break
			}
		}
	}()
}

func setRequestDefaults(request *http.Request) {
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/11.0.3 Safari/605.1.15")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
}

func main() {
	// Set up a cookie jar for the app to use.
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not create cookie jar: %v\n", err)
		os.Exit(1)
	}
	http.DefaultClient.Jar = jar

	config = viper.New()
	config.AddConfigPath("/etc/manifest-server")
	config.AddConfigPath("$HOME/.manifest-server")
	config.AddConfigPath(".")
	if err = config.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Could not read config: %v\n", err)
		os.Exit(1)
	}
	settings = NewSettings(config)

	config.SetDefault("timezone", "America/New_York")
	timezone := config.GetString("timezone")
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid timezone %q: %v\n", timezone, err)
		os.Exit(1)
	}
	dzLocation = loc

	config.SetDefault("server.http_address", ":http")
	httpAddress := config.GetString("server.http_address")
	config.SetDefault("server.https_address", ":https")
	httpsAddress := config.GetString("server.https_address")

	certFile := config.GetString("server.cert_file")
	keyFile := config.GetString("server.key_file")

	webServer, err = NewWebServer(httpAddress, httpsAddress, certFile, keyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot create web server: %v\n", err)
		os.Exit(1)
	}
	webServer.SetContent("/manifest", []byte("\n\n\n\n\n\n0\n"), mimetypePlain)
	webServer.SetContentFunc("/settings.html", settings.HTML)

	config.SetDefault("metar.enabled", true)
	if config.GetBool("metar.enabled") {
		config.SetDefault("metar.station", "KORE")
		metarStation := config.GetString("metar.station")
		metarSource = NewMETAR(metarStation)
		launchDataSource(
			func() time.Time { return time.Now().Add(300 * time.Second) },
			"METAR",
			metarSource.Refresh,
			updateManifestStaticData)
	}

	config.SetDefault("burble.dzid", 417)
	dzid := config.GetInt("burble.dzid")
	burbleSource = NewBurble(dzid)
	launchDataSource(
		func() time.Time { return time.Now().Add(10 * time.Second) },
		"Burble",
		burbleSource.Refresh,
		updateManifestStaticData)

	config.SetDefault("winds.enabled", true)
	if config.GetBool("winds.enabled") {
		webServer.SetContent("/winds", []byte{}, mimetypePlain)
		webServer.SetContent("/winds.json", []byte("{}"), mimetypeJSON)

		config.SetDefault("winds.latitude", "42.5700")
		latitude := config.GetString("winds.latitude")
		config.SetDefault("winds.longitude", "-72.2885")
		longitude := config.GetString("winds.longitude")
		windsAloftSource = NewWindsAloft(latitude, longitude)
		launchDataSource(
			func() time.Time { return time.Now().Add(15 * time.Minute) },
			"Winds Aloft",
			windsAloftSource.Refresh,
			func() {
				updateManifestStaticData()
				updateWindsStaticData()
			})
	}

	config.SetDefault("message_file", "/var/lib/manifest-server/message")
	messageDataFile := config.GetString("message_file")
	wg.Add(1)
	go func() {
		defer wg.Done()
		messageTimer := time.NewTicker(messageUpdateFrequency)
		for {
			if data, readErr := ioutil.ReadFile(messageDataFile); readErr == nil {
				data = bytes.Split(data, []byte{'\n'})[0]
				data = bytes.TrimSpace(data)
				messageLock.Lock()
				update := false
				if newMessage := string(data); newMessage != message {
					message = newMessage
					update = true
				}
				messageLock.Unlock()
				if update {
					updateManifestStaticData()
				}
			}
			select {
			case <-messageTimer.C:
			case <-done:
				return
			}
		}
	}()

	config.SetDefault("jumprun.enabled", false)
	if config.GetBool("jumprun.enabled") {
		config.SetDefault("jumprun.latitude", "42.5700")
		config.SetDefault("jumprun.longitude", "-72.2885")
		config.SetDefault("jumprun.state_file", "/var/lib/manifest-server/jumprun.json")
		jumprun = NewJumprunManager(config.GetString("jumprun.state_file"),
			config.GetString("jumprun.latitude"),
			config.GetString("jumprun.longitude"),
			func(j Jumprun) {
				modifyTime := time.Unix(j.TimeStamp, 0)
				webServer.SetContentWithTime("/jumprun",
					j.LegacyContent(), mimetypePlain, modifyTime)
				if jsonContent, err := json.Marshal(j); err != nil {
					webServer.SetContentWithTime("/jumprun.json",
						jsonContent, mimetypeJSON, modifyTime)
				}
			})
		webServer.SetContentFunc("/jumprun.html", jumprun.HTML)
	}

	if err = webServer.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot start web server: %v\n", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
	signal.Stop(c)

	close(done)
	webServer.Close()
	wg.Wait()
}
