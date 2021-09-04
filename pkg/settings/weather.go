// (c) Copyright 2017-2021 Matt Messier

package settings

func (s *Settings) WindsEnabled() bool {
	return s.config.GetBool("winds.enabled")
}

func (s *Settings) WindsLatitude() string {
	return s.config.GetString("winds.latitude")
}

func (s *Settings) WindsLongitude() string {
	return s.config.GetString("winds.longitude")
}

func (s *Settings) METAREnabled() bool {
	return s.config.GetBool("metar.enabled")
}

func (s *Settings) METARStation() string {
	return s.config.GetString("metar.station")
}
