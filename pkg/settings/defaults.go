// (c) Copyright 2017-2021 Matt Messier

package settings

var defaults = map[string]interface{}{
	"display_nicknames": true,
	"display_weather":   true,
	"winds":             true,
	"message_file":      "/var/lib/manifest-server/message",
	"timezone":          "America/New_York",

	"server.http_address":  ":http",
	"server.https_address": ":https",
	"server.cert_file":     nil,
	"server.key_file":      nil,

	"burble.dzid": 417,

	"jumprun.enabled":    false,
	"jumprun.latitude":   "42.5700",
	"jumprun.longitude":  "-72.2885",
	"jumprun.state_file": "/var/lib/manifest-server/jumprun.json",

	"metar.enabled": true,
	"metar.station": "KORE",

	"winds.enabled":   true,
	"winds.latitude":  "42.5700",
	"winds.longitude": "-72.2885",
}
