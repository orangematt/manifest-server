// (c) Copyright 2017-2021 Matt Messier

package jumprun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/orangematt/manifest-server/pkg/settings"
)

type UpdateFunc func()

type Controller struct {
	settings      *settings.Settings
	stateFilename string
	update        UpdateFunc

	lock     sync.Mutex
	jumprun  Jumprun
	template *template.Template
}

func NewController(
	settings *settings.Settings,
	update UpdateFunc,
) *Controller {
	c := &Controller{
		settings:      settings,
		stateFilename: settings.JumprunStateFile(),
		update:        update,
	}
	if err := c.restore(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot restore jumprun state: %v\n", err)
	} else {
		latitude := settings.JumprunLatitude()
		longitude := settings.JumprunLongitude()
		if c.jumprun.Latitude != latitude || c.jumprun.Longitude != longitude {
			c.Reset()
		}
	}
	return c
}

func (c *Controller) Jumprun() Jumprun {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.jumprun
}

func (c *Controller) Reset() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.jumprun = Jumprun{
		TimeStamp: time.Now().Unix(),
	}
	c.updateStaticData()
}

func (c *Controller) SetFromURLValues(values url.Values) error {
	var (
		err error
		v   int
	)

	newj := Jumprun{
		TimeStamp: time.Now().Unix(),
		IsSet:     true,
	}
	if v, err = newj.getIntValue(values, "main_heading", 0); err != nil {
		return err
	}
	if v < 0 || v > 359 {
		return fmt.Errorf("main heading out of range: %d", v)
	}
	newj.Heading = v

	if v, err = newj.getIntValue(values, "exit_distance", 0); err != nil {
		return err
	}
	newj.ExitDistance = v

	if v, err = newj.getIntValue(values, "offset_heading", 0); err != nil {
		return err
	}
	newj.OffsetHeading = v

	if v, err = newj.getIntValue(values, "offset_distance", 0); err != nil {
		return err
	}
	newj.OffsetDistance = v

	for i := 0; i < len(newj.HookTurns); i++ {
		var turn Turn
		key := fmt.Sprintf("hook_heading_%d", i)
		value := values.Get(key)
		if value == "" {
			break
		}

		var v64 int64
		if v64, err = strconv.ParseInt(value, 10, 32); err != nil {
			return fmt.Errorf("cannot parse hook heading %d: %v", i, err)
		}
		if v < 0 || v > 359 {
			return fmt.Errorf("hook heading %d out of range: %d", i, v64)
		}
		turn.Heading = int(v64)

		key = fmt.Sprintf("hook_distance_%d", i)
		if v64, err = strconv.ParseInt(values.Get(key), 10, 32); err != nil {
			return fmt.Errorf("cannot parse hook distance %d: %v", i, err)
		}
		turn.Distance = int(v64)
		newj.HookTurns[i] = turn
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.jumprun = newj
	c.updateStaticData()

	return nil
}

func (c *Controller) updateStaticData() {
	if c.update != nil {
		c.update()
	}
}

func (c *Controller) restore() error {
	dataBytes, err := ioutil.ReadFile(c.stateFilename)
	if err != nil {
		return err
	}

	var newj Jumprun
	if err = json.Unmarshal(dataBytes, &newj); err != nil {
		return err
	}

	c.lock.Lock()
	c.jumprun = newj
	c.lock.Unlock()

	c.updateStaticData()
	return nil
}

func (c *Controller) Write() error {
	c.lock.Lock()
	j := c.jumprun
	c.lock.Unlock()

	dataBytes, err := json.Marshal(&j)
	if err != nil {
		return err
	}

	tempFilename := c.stateFilename + ".tmp"
	if err = ioutil.WriteFile(tempFilename, dataBytes, 0600); err == nil {
		_ = os.Rename(tempFilename, c.stateFilename)
	}
	return err
}

func (c *Controller) initializeTemplate() *template.Template {
	if c.template == nil {
		var err error
		c.template, err = template.New("jumprun").Parse(jumprunHTML)
		if err != nil {
			// This should never fail -- the HTML is hard-coded, so
			// panic if this happens, because it means it's an error
			// that will never not happen.
			panic(err)
		}
	}
	return c.template
}

func (c *Controller) HTML(w http.ResponseWriter, req *http.Request) {
	c.lock.Lock()
	j := c.jumprun
	tmpl := c.initializeTemplate()
	c.lock.Unlock()

	if tmpl == nil {
		http.NotFound(w, req)
		return
	}

	b := &bytes.Buffer{}
	if err := tmpl.Execute(b, &j); err != nil {
		http.NotFound(w, req)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r := bytes.NewReader(b.Bytes())
	http.ServeContent(w, req, "", time.Now(), r)
}

func (c *Controller) FormHandler(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.NotFound(w, req)
		return
	}
	if err := c.SetFromURLValues(req.Form); err == nil {
		_ = c.Write()
	}
}

const jumprunHTML = `<html>
	<head>
		<title>Manifest - Set Jump Run</title>
	</head>
	<body>
		<form action="/setjumprun" method="post">
			<div>
				All headings are relative to magentic north. All distances are
				specified in tenths of a mile (e.g. 1 is 1/10 mile, 5 is
				1/2 mile, 10 is 1 mile, etc.).  The exit distance is the offset
				from center where the pilot will turn on the green light. A
				negative value means that the exit point is before center. A
				positive value means that the exit point is after center.
			</div>
			<div>
				<hr>
				<h3>Jump Run</h3>
			</div>
			<div>
				<label>Heading:</label>
				<input type="text" name="main_heading" value="{{.Heading}}">
			</div>
			<div>
				<label>Exit Distance:<label>
				<input type="text" name="exit_distance" value="{{.ExitDistance}}">
			</div>
			<div>
				<h4>Offset:</h4>
				<label>Heading:</label>
				<input type="text" name="offset_heading" value="{{.OffsetHeading}}">
				<label>Distance:</label>
				<input type="text" name="offset_distance" value="{{.OffsetDistance}}">
			</div>
			<div>
			<hr>
			<h3>Hook Points</h3>
			Leave these blank if there is to be no hook.
			<p>
			</div>
			{{range $index, $element := .HookTurns}}
			<div>
				<label>Distance:<label>
				<input type="text" name="hook_distance_{{$index}}" value="{{$element.Distance}}">
				<label>Heading:<label>
				<input type="text" name="hook_heading_{{$index}}" value="{{$element.Heading}}">
			</div>
			{{end}}
			<div>
				<hr>
				<button type="reset">Reset</button>
				<button type="submit">Submit</button>
			</div>
		</form>
	</body>
</html>
`
