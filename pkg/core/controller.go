// (c) Copyright 2017-2021 Matt Messier

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/jumptown-skydiving/manifest-server/pkg/burble"
	"github.com/jumptown-skydiving/manifest-server/pkg/db"
	"github.com/jumptown-skydiving/manifest-server/pkg/jumprun"
	"github.com/jumptown-skydiving/manifest-server/pkg/metar"
	"github.com/jumptown-skydiving/manifest-server/pkg/settings"
	"github.com/jumptown-skydiving/manifest-server/pkg/winds"
	"github.com/kelvins/sunrisesunset"
	"github.com/orangematt/siwa"
)

type DataSource uint64

const (
	BurbleDataSource     DataSource = 1 << 0
	JumprunDataSource               = 1 << 1
	METARDataSource                 = 1 << 2
	WindsAloftDataSource            = 1 << 3
	OptionsDataSource               = 1 << 4
	PreSunriseDataSource            = 1 << 5 // Fires once per minute for an hour prior to sunrise
	SunriseDataSource               = 1 << 6
	PreSunsetDataSource             = 1 << 7 // Fires once per minute for an hour prior to sunset
	SunsetDataSource                = 1 << 8
)

type Controller struct {
	mutex sync.Mutex

	db               db.Connection
	location         *time.Location
	burbleSource     *burble.Controller
	jumprun          *jumprun.Controller
	metarSource      *metar.Controller
	windsAloftSource *winds.Controller

	siwa *siwa.Manager

	settings   *settings.Settings
	listeners  map[int]chan DataSource
	listenerID int
	done       chan struct{}
	wg         sync.WaitGroup
}

func NewController(settings *settings.Settings) (*Controller, error) {
	c := &Controller{
		settings:  settings,
		listeners: make(map[int]chan DataSource),
		done:      make(chan struct{}),
	}

	var err error
	c.siwa, err = settings.NewSignInWithAppleManager()
	if err != nil {
		return nil, err
	}
	c.siwa.SetDelegate(c)

	c.db, err = db.Connect(settings)
	if err != nil {
		return nil, fmt.Errorf("Failed to initialize database: %w", err)
	}

	loc, err := settings.Location()
	if err != nil {
		return nil, fmt.Errorf("Invalid timezone: %w", err)
	}
	c.location = loc

	c.burbleSource = burble.NewController(c.settings)
	c.launchDataSource(
		func() time.Time { return time.Now().Add(10 * time.Second) },
		"Burble",
		c.burbleSource.Refresh,
		func() { c.WakeListeners(BurbleDataSource) })

	if c.settings.METAREnabled() {
		c.metarSource = metar.NewController(c.settings)
		c.launchDataSource(
			func() time.Time { return time.Now().Add(5 * time.Minute) },
			"METAR",
			c.metarSource.Refresh,
			func() { c.WakeListeners(METARDataSource) })
	}

	if c.settings.WindsEnabled() {
		c.windsAloftSource = winds.NewController(c.settings)
		c.launchDataSource(
			func() time.Time { return time.Now().Add(15 * time.Minute) },
			"Winds Aloft",
			c.windsAloftSource.Refresh,
			func() { c.WakeListeners(WindsAloftDataSource) })
	}

	if c.settings.JumprunEnabled() {
		c.jumprun = jumprun.NewController(c.settings,
			func() { c.WakeListeners(JumprunDataSource) })
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runAtSunriseSunset()
	}()

	return c, nil
}

func (c *Controller) Done() <-chan struct{} {
	return c.done
}

func (c *Controller) Close() {
	close(c.done)
	c.wg.Wait()
	c.db.Close()
}

func (c *Controller) Settings() *settings.Settings {
	return c.settings
}

func (c *Controller) Location() *time.Location {
	return c.location
}

func (c *Controller) BurbleSource() *burble.Controller {
	return c.burbleSource
}

func (c *Controller) Jumprun() *jumprun.Controller {
	return c.jumprun
}

func (c *Controller) METARSource() *metar.Controller {
	return c.metarSource
}

func (c *Controller) WindsAloftSource() *winds.Controller {
	return c.windsAloftSource
}

func (c *Controller) SignInWithAppleManager() *siwa.Manager {
	return c.siwa
}

func (c *Controller) CurrentTime() time.Time {
	return time.Now().In(c.Location())
}

func (c *Controller) NewRequestWithContext(
	ctx context.Context,
	method string,
	url string,
	body io.Reader,
) (*http.Request, error) {
	return c.settings.NewRequestWithContext(ctx, method, url, body)
}

func (c *Controller) SeparationDelay(speed int) int {
	msec := (1852.0 * float64(speed)) / 3600.0
	ftsec := msec / 0.3048
	return int(math.Ceil(1000.0 / ftsec))
}

func (c *Controller) SeparationStrings() (uint32, string) {
	windsAloftSource := c.WindsAloftSource()

	color := uint32(0xffffff)
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
		str, t string
		speed  int
	)
	if sample.LightAndVariable {
		speed = 85
	} else {
		speed = 85 - sample.Speed
	}
	if speed <= 0 {
		color = 0xff0000
		str = fmt.Sprintf("Winds are %d knots",
			sample.Speed)
	} else {
		str = fmt.Sprintf("Separation is %d seconds",
			c.SeparationDelay(speed))
	}

	t = fmt.Sprintf("(%d℃ / %d℉)", sample.Temperature,
		int64(metar.FahrenheitFromCelsius(float64(sample.Temperature))))

	if str != "" && t != "" {
		return color, fmt.Sprintf("%s %s", str, t)
	}
	if str == "" {
		return color, t
	}
	if t == "" {
		return color, str
	}

	return color, ""
}

func (c *Controller) launchDataSource(
	nextRefresh func() time.Time,
	sourceName string,
	refresh func() (bool, error),
	update func(),
) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			if changed, err := refresh(); err != nil {
				fmt.Fprintf(os.Stderr, "Error refreshing %s: %v\n", sourceName, err)
			} else if changed {
				update()
			}

			nextTime := nextRefresh()
			refreshPeriod := time.Until(nextTime)
			t := time.NewTicker(refreshPeriod)

			select {
			case <-c.Done():
				t.Stop()
				return
			case <-t.C:
				t.Stop()
				break
			}
		}
	}()
}

func (c *Controller) Coordinates() (latitude float64, longitude float64, err error) {
	if c.Jumprun() != nil {
		j := c.Jumprun().Jumprun()
		if j.IsSet && j.Latitude != "" && j.Longitude != "" {
			latitude, err = strconv.ParseFloat(j.Latitude, 64)
			if err == nil {
				longitude, err = strconv.ParseFloat(j.Longitude, 64)
				if err == nil {
					return
				}
			}
		}
	}
	if c.WindsAloftSource() != nil {
		settings := c.Settings()
		latitude, err = strconv.ParseFloat(settings.WindsLatitude(), 64)
		if err == nil {
			longitude, err = strconv.ParseFloat(settings.WindsLongitude(), 64)
			if err == nil {
				return
			}
		}
	}
	var ok bool
	if latitude, longitude, ok = c.METARSource().Location(); ok {
		return latitude, longitude, nil
	}
	err = errors.New("location is unknown")
	return
}

func (c *Controller) SunriseAndSunsetTimes() (sunrise time.Time, sunset time.Time, err error) {
	dzTimeNow := c.CurrentTime()
	_, utcOffset := dzTimeNow.Zone()

	var latitude, longitude float64
	latitude, longitude, err = c.Coordinates()
	if err != nil {
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

func (c *Controller) SunriseMessage() string {
	sunrise, _, err := c.SunriseAndSunsetTimes()
	if err != nil {
		return ""
	}

	dzTimeNow := c.CurrentTime()
	if dzTimeNow.Before(sunrise) {
		delta := int(sunrise.Sub(dzTimeNow).Minutes())
		switch {
		case delta == 1:
			return "Sunrise is in 1 minute"
		case delta == 60:
			return "Sunrise is in 1 hour"
		case delta > 1 && delta < 60:
			return fmt.Sprintf("Sunrise is in %d minutes", delta)
		}
	}
	return ""
}

func (c *Controller) SunsetMessage() string {
	_, sunset, err := c.SunriseAndSunsetTimes()
	if err != nil {
		return ""
	}

	dzTimeNow := c.CurrentTime()
	if dzTimeNow.Before(sunset) {
		delta := int(sunset.Sub(dzTimeNow).Minutes())
		switch {
		case delta == 1:
			return "Sunset is in 1 minute"
		case delta == 60:
			return "Sunset is in 1 hour"
		case delta > 1 && delta < 60:
			return fmt.Sprintf("Sunset is in %d minutes", delta)
		}
	}
	return ""
}

func (c *Controller) AddListener(l chan DataSource) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.listenerID++
	id := c.listenerID
	c.listeners[id] = l
	return id
}

func (c *Controller) RemoveListener(id int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.listeners, id)
}

func (c *Controller) WakeListeners(source DataSource) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, l := range c.listeners {
		l <- source
	}
}

func (c *Controller) sunrise() {
	// Clear the active jumprun at sunrise
	if c.Jumprun() != nil {
		if sunrise, _, err := c.SunriseAndSunsetTimes(); err == nil {
			dzTimeNow := c.CurrentTime()
			activeJumprunTime := time.Unix(c.jumprun.Jumprun().TimeStamp, 0).In(c.Location())
			if activeJumprunTime.Before(sunrise) && dzTimeNow.After(sunrise) {
				c.Jumprun().Reset()
				if err = c.Jumprun().Write(); err != nil {
					fmt.Fprintf(os.Stderr, "cannot save jumprun state: %v\n", err)
				}
			}
		}
	}
	c.WakeListeners(SunriseDataSource)
}

func (c *Controller) sunset() {
	c.WakeListeners(SunsetDataSource)
}

func (c *Controller) runAtSunriseSunset() {
	lastPre := []int{-1, -1}
	lastSunrise := []int{0, 0, 0}
	lastSunset := []int{0, 0, 0}
	t := time.NewTicker(1 * time.Second)
	for {
		sunrise, sunset, err := c.SunriseAndSunsetTimes()
		if err != nil {
			fmt.Fprintf(os.Stderr, "SunriseAndSunsetTimes ERROR: %v\n", err)
			return
		}

		now := c.CurrentTime()
		if now.Equal(sunset) || now.After(sunset) {
			y, m, d := sunset.Date()
			thisSunset := []int{y, int(m), d}
			if !reflect.DeepEqual(lastSunset, thisSunset) {
				c.sunset()
				lastSunset = thisSunset
			}
		} else if sunset.After(now) && sunset.Sub(now).Hours() <= 1 {
			thisPre := []int{now.Hour(), now.Minute()}
			if !reflect.DeepEqual(lastPre, thisPre) {
				c.WakeListeners(PreSunsetDataSource)
				lastPre = thisPre
			}
		}
		if now.Equal(sunrise) || now.After(sunrise) {
			y, m, d := sunrise.Date()
			thisSunrise := []int{y, int(m), d}
			if !reflect.DeepEqual(lastSunrise, thisSunrise) {
				c.sunrise()
				lastSunrise = thisSunrise
			}
		} else if sunrise.After(now) && sunrise.Sub(now).Hours() <= 1 {
			thisPre := []int{now.Hour(), now.Minute()}
			if !reflect.DeepEqual(lastPre, thisPre) {
				c.WakeListeners(PreSunriseDataSource)
				lastPre = thisPre
			}
		}

		select {
		case <-c.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
