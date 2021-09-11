// (c) Copyright 2017-2021 Matt Messier

package settings

func (s *Settings) WebServerAddress() string {
	return s.config.GetString("server.http_address")
}

func (s *Settings) WebServerSecureAddress() string {
	return s.config.GetString("server.https_address")
}

func (s *Settings) WebServerGRPCAddress() string {
	return s.config.GetString("server.grpc_address")
}

func (s *Settings) ServerCertFile() string {
	return s.config.GetString("server.cert_file")
}

func (s *Settings) ServerKeyFile() string {
	return s.config.GetString("server.key_file")
}
