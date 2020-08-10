// (c) Copyright 2017-2020 Matt Messier

package main

import (
	"bytes"
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// Settings are configurable options that may be changed via the web interface
// while the server is running.
type Settings struct {
	DisplayNicknames bool
	DisplayWeather   bool
	DisplayWinds     bool

	lock     sync.Mutex
	config   *viper.Viper
	template *template.Template
}

func NewSettings(v *viper.Viper) *Settings {
	s := &Settings{}
	s.config = v

	config.SetDefault("display_nicknames", true)
	s.DisplayNicknames = config.GetBool("display_nicknames")
	config.SetDefault("display_weather", true)
	s.DisplayWeather = config.GetBool("display_weather")
	config.SetDefault("display_winds", true)
	s.DisplayWinds = config.GetBool("display_winds")

	return s
}

func (s *Settings) Write() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	config.Set("display_nicknames", s.DisplayNicknames)
	config.Set("display_weather", s.DisplayWeather)
	config.Set("display_winds", s.DisplayWinds)

	if err := config.WriteConfig(); err != nil {
		return err
	}
	return nil
}

func (s *Settings) SetFromURLValues(values url.Values) bool {
	changed := false
	sv := reflect.ValueOf(s).Elem()
	for k, v := range values {
		if len(v) != 1 {
			continue
		}
		fv := sv.FieldByName(k)
		switch fv.Kind() {
		case reflect.Bool:
			o := fv.Bool()
			n := parseBool(v[0])
			if o != n {
				changed = true
				fv.SetBool(n)
			}
		}
	}
	return changed
}

func (s *Settings) initializeTemplate() *template.Template {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.template == nil {
		var err error
		s.template, err = template.New("settings").Parse(settingsHTML)
		if err != nil {
			// This should never fail -- the HTML is hard-coded, so
			// panic if this happens, because it means it's an error
			// that will never not happen.
			panic(err)
		}
	}
	return s.template
}

func (s *Settings) HTML(w http.ResponseWriter, req *http.Request) {
	tmpl := s.initializeTemplate()
	if tmpl == nil {
		http.NotFound(w, req)
		return
	}

	b := &bytes.Buffer{}
	if err := tmpl.Execute(b, s); err != nil {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", mimetypeHTML)
	r := bytes.NewReader(b.Bytes())
	http.ServeContent(w, req, "", time.Now(), r)
}

const settingsHTML = `<html>
<head>
	<title>Manifest Settings</title>
	<script>
	function change(id) {
		var v = document.getElementById(id).checked
		var xmlhttp = new XMLHttpRequest();
		xmlhttp.open("GET", "/setconfig?" + id + "=" + v, true);
		xmlhttp.send();
	}
	</script>
</head>
<body>
	<form>
		<div>
			<h3>Settings</h3>
			<hr>
			<br>
		</div>
		<div>
			<input type="checkbox" id="DisplayNicknames" onchange="change('DisplayNicknames');" {{if .DisplayNicknames}}checked{{end}}><label>&nbsp;Display nicknames instead of real names</label>
		</div>
		<div>
			<input type="checkbox" id="DisplayWeather" onchange="change('DisplayWeather');" {{if .DisplayWeather}}checked{{end}}><label>&nbsp;Display weather information</label>
		</div>
		<div>
			<input type="checkbox" id="DisplayWinds" onchange="change('DisplayWinds');" {{if .DisplayWinds}}checked{{end}}><label>&nbsp;Display winds aloft information<label>
		</div>
	</form>
</body>
</html>
`
