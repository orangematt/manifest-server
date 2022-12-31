// (c) Copyright 2017-2022 Matt Messier

package settings

func (s *Settings) DatabaseDriver() string {
	return s.config.GetString("database.driver")
}

func (s *Settings) DatabaseFilename() string {
	return s.config.GetString("database.filename")
}
