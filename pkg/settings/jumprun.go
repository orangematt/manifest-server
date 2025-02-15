// (c) Copyright 2017-2021 Matt Messier

package settings

func (s *Settings) JumprunEnabled() bool {
	return s.config.GetBool("jumprun.enabled")
}

func (s *Settings) JumprunLatitude() string {
	return s.config.GetString("jumprun.latitude")
}

func (s *Settings) JumprunLongitude() string {
	return s.config.GetString("jumprun.longitude")
}

func (s *Settings) JumprunStateFile() string {
	return s.config.GetString("jumprun.state_file")
}

func (s *Settings) JumprunMagneticDeclination() int {
	return s.config.GetInt("jumprun.magnetic_declination")
}

func (s *Settings) JumprunCameraHeight() int {
	return s.config.GetInt("jumprun.camera_height")
}
