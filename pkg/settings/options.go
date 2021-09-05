// (c) Copyright 2017-2021 Matt Messier

package settings

type Options struct {
	DisplayNicknames bool   `json:"display_nicknames"`
	DisplayWeather   bool   `json:"display_weather"`
	DisplayWinds     bool   `json:"display_winds"`
	Message          string `json:"message"`
}

func (s *Settings) Message() string {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.options.Message
}

func (s *Settings) DisplayNicknames() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.options.DisplayNicknames
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
