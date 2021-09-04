// (c) Copyright 2017-2021 Matt Messier

package settings

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

func ParseBool(s string) bool {
	s = strings.ToLower(s)
	return s == "on" || s == "true" || s == "t" || s == "y" || s == "yes" || s == "1"
}

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

func NewSettings() (*Settings, error) {
	s := &Settings{
		config: viper.New(),
	}

	for key, value := range defaults {
		v := reflect.ValueOf(value)
		switch v.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
			if v.IsNil() {
				continue
			}
			fallthrough
		default:
			s.config.SetDefault(key, value)
		}
	}

	s.config.AddConfigPath("/etc/manifest-server")
	s.config.AddConfigPath("$HOME/.manifest-server")
	s.config.AddConfigPath(".")
	if err := s.config.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("Could not read config: %w\n", err)
	}

	return s, nil
}

func (s *Settings) Write() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.config.Set("display_nicknames", s.DisplayNicknames)
	s.config.Set("display_weather", s.DisplayWeather)
	s.config.Set("display_winds", s.DisplayWinds)

	if err := s.config.WriteConfig(); err != nil {
		return err
	}
	return nil
}

func (s *Settings) NewHTTPRequest(
	method string,
	url string,
	body io.Reader,
) (*http.Request, error) {
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/11.0.3 Safari/605.1.15")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	return request, err
}

func (s *Settings) Location() (*time.Location, error) {
	timezone := s.config.GetString("timezone")
	return time.LoadLocation(timezone)
}

func (s *Settings) MessageFile() string {
	return s.config.GetString("message_file")
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
			n := ParseBool(v[0])
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r := bytes.NewReader(b.Bytes())
	http.ServeContent(w, req, "", time.Now(), r)
}

func (s *Settings) FormHandler(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot parse form: %v\n", err)
		http.NotFound(w, req)
		return
	}
	if s.SetFromURLValues(req.Form) {
		_ = s.Write()
	}
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
