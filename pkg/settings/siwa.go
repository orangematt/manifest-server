// (c) Copyright 2017-2023 Matt Messier

package settings

import (
	"errors"

	"github.com/orangematt/siwa"
)

func (s *Settings) NewSignInWithAppleManager() (*siwa.Manager, error) {
	if s.config.Get("siwa") == nil {
		return nil, nil
	}

	bundleID := s.config.GetString("siwa.bundle_id")
	if bundleID == "" {
		return nil, errors.New("Missing bundle_id for siwa configuration")
	}
	teamID := s.config.GetString("siwa.team_id")
	if teamID == "" {
		return nil, errors.New("Missing team_id for siwa configuration")
	}
	keyID := s.config.GetString("siwa.key_id")
	if keyID == "" {
		return nil, errors.New("Missing key_id for siwa configuration")
	}
	keyFile := s.config.GetString("siwa.key_file")
	if keyFile == "" {
		return nil, errors.New("missing key_file for siwa configuration")
	}

	return siwa.NewManagerFromKeyFile(bundleID, teamID, keyID, keyFile)
}
