// (c) Copyright 2017-2020 Matt Messier

package main

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
)

// Settings stores dynamically configurable information
type JumprunTurn struct {
	Distance int `json:"distance"` // tenths of a mile
	Heading  int `json:"heading"`  // degrees from magnetic north
}

type JumprunUpdate func(Jumprun)

type Jumprun struct {
	TimeStamp      int64          `json:"timestamp"`       // time when set (UTC UnixNano)
	Heading        int            `json:"heading"`         // degrees from magnetic north
	ExitDistance   int            `json:"exit_distance"`   // tenths of a mile
	OffsetHeading  int            `json:"offset_heading"`  // degrees from magnetic north
	OffsetDistance int            `json:"offset_distance"` // tenths of a mile
	HookTurns      [4]JumprunTurn `json:"hook_turns"`      // list of turns if there's a hook
	Latitude       string         `json:"latitude"`        // latitude of jumprun origin
	Longitude      string         `json:"longitude"`       // longitude of jumprun origin
	IsSet          bool           `json:"is_set"`          // true if jumprun is set
}

type JumprunManager struct {
	jumprun       Jumprun
	stateFilename string
	updateFunc    JumprunUpdate

	lock     sync.Mutex
	template *template.Template
}

func NewJumprunManager(
	stateFilename, latitude, longitude string,
	updateFunc JumprunUpdate,
) *JumprunManager {
	j := &JumprunManager{
		jumprun: Jumprun{
			Latitude:  latitude,
			Longitude: longitude,
		},
		stateFilename: stateFilename,
		updateFunc:    updateFunc,
	}
	if err := j.restore(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot restore jumprun state: %v\n", err)
	} else if j.jumprun.Latitude != latitude || j.jumprun.Longitude != longitude {
		j.Reset()
	}
	return j
}

func (j *JumprunManager) Jumprun() Jumprun {
	return j.jumprun
}

func (j *JumprunManager) Reset() {
	j.lock.Lock()
	defer j.lock.Unlock()

	j.jumprun = Jumprun{
		TimeStamp: time.Now().Unix(),
	}
	j.updateStaticData()
}

func (j *Jumprun) LegacyContent() []byte {
	b := bytes.Buffer{}

	if !j.IsSet {
		b.WriteString("unset\n")
	} else {
		b.WriteString(fmt.Sprintf("%d %d %d %d\n", j.Heading, j.ExitDistance, j.OffsetHeading, j.OffsetDistance))
		for _, turn := range j.HookTurns {
			if turn.Heading == 0 && turn.Distance == 0 {
				break
			}
			b.WriteString(fmt.Sprintf("%d %d\n", turn.Heading, turn.Distance))
		}
	}

	return b.Bytes()
}

func (j *Jumprun) getIntValue(values url.Values, key string, defaultValue int) (int, error) {
	v := values.Get(key)
	if v == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return defaultValue, fmt.Errorf("cannot parse %s: %v", key, err)
	}
	return int(value), nil
}

func (j *JumprunManager) SetFromURLValues(values url.Values) error {
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
		var turn JumprunTurn
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

	j.lock.Lock()
	defer j.lock.Unlock()
	j.jumprun = newj
	j.updateStaticData()

	return nil
}

func (j *JumprunManager) updateStaticData() {
	if j.updateFunc != nil {
		j.updateFunc(j.jumprun)
	}
}

func (j *JumprunManager) restore() error {
	dataBytes, err := ioutil.ReadFile(j.stateFilename)
	if err != nil {
		return err
	}

	var newj Jumprun
	if err = json.Unmarshal(dataBytes, &newj); err != nil {
		return err
	}
	j.jumprun = newj

	j.updateStaticData()
	return nil
}

func (j *JumprunManager) Write() error {
	j.lock.Lock()
	defer j.lock.Unlock()
	dataBytes, err := json.Marshal(&j.jumprun)
	if err != nil {
		return err
	}

	tempFilename := j.stateFilename + ".tmp"
	if err = ioutil.WriteFile(tempFilename, dataBytes, 0600); err == nil {
		_ = os.Rename(tempFilename, j.stateFilename)
	}
	return err
}

func (j *JumprunManager) initializeTemplate() *template.Template {
	j.lock.Lock()
	defer j.lock.Unlock()

	if j.template == nil {
		var err error
		j.template, err = template.New("jumprun").Parse(jumprunHTML)
		if err != nil {
			// This should never fail -- the HTML is hard-coded, so
			// panic if this happens, because it means it's an error
			// that will never not happen.
			panic(err)
		}
	}
	return j.template
}

func (j *JumprunManager) HTML(w http.ResponseWriter, req *http.Request) {
	tmpl := j.initializeTemplate()
	if tmpl == nil {
		http.NotFound(w, req)
		return
	}

	b := &bytes.Buffer{}
	if err := tmpl.Execute(b, &j.jumprun); err != nil {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", mimetypeHTML)
	r := bytes.NewReader(b.Bytes())
	http.ServeContent(w, req, "", time.Now(), r)
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
