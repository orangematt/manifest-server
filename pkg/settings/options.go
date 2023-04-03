// (c) Copyright 2017-2021 Matt Messier

package settings

type Options struct {
	DisplayWeather bool   `json:"display_weather"`
	DisplayWinds   bool   `json:"display_winds"`
	DisplayColumns int    `json:"display_columns"`
	MinCallMinutes int    `json:"min_call_minutes"`
	Message        string `json:"message"`
	FuelRequested  bool   `json:"fuel_requested"`
}

func (s *Settings) Message() string {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.options.Message
}

func (s *Settings) DisplayWeather() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.options.DisplayWeather
}

func (s *Settings) DisplayWinds() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.options.DisplayWinds
}

func (s *Settings) DisplayColumns() int {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.options.DisplayColumns
}

func (s *Settings) MinCallMinutes() int {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.options.MinCallMinutes
}

func (s *Settings) FuelRequested() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.options.FuelRequested
}

func (s *Settings) SetFuelRequested(b bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.options.FuelRequested = b
}
