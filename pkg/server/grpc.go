// (c) Copyright 2017-2021 Matt Messier

package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/jumptown-skydiving/manifest-server/pkg/burble"
	"github.com/jumptown-skydiving/manifest-server/pkg/core"
	"github.com/jumptown-skydiving/manifest-server/pkg/settings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type addClientResponse struct {
	id uint64
}

type addClientRequest struct {
	reply   chan addClientResponse
	updates chan *ManifestUpdate
}

type removeClientResponse struct{}

type removeClientRequest struct {
	reply chan removeClientResponse
	id    uint64
}

type manifestServiceServer struct {
	UnimplementedManifestServiceServer

	app     *core.Controller
	options settings.Options
	wg      sync.WaitGroup
	cancel  context.CancelFunc

	addClientChan    chan addClientRequest
	removeClientChan chan removeClientRequest
}

func newManifestServiceServer(controller *core.Controller) *manifestServiceServer {
	return &manifestServiceServer{
		app:              controller,
		addClientChan:    make(chan addClientRequest, 16),
		removeClientChan: make(chan removeClientRequest, 16),
	}
}

func (s *manifestServiceServer) translateJumper(j *burble.Jumper, leader *Jumper) *Jumper {
	var (
		color  uint32
		prefix string
	)
	shortName := j.ShortName
	if leader != nil {
		color = leader.Color
	} else {
		switch {
		case j.IsTandem:
			color = 0xffff00 // yellow
			if leader == nil {
				prefix = "Tandem"
				shortName = ""
			}
		case j.IsStudent || strings.HasSuffix(j.ShortName, " + Gear"):
			color = 0x00ff00 // green
			if strings.HasSuffix(j.ShortName, " H/P") {
				prefix = "Hop & Pop"
			}
		case strings.HasPrefix(j.ShortName, "3-5k") || strings.HasPrefix(j.ShortName, "3.5k"):
			color = 0xff00ff // magenta
			prefix = "Hop & Pop"
		default:
			color = 0xffffff // white
		}
	}

	var repr string
	if rigName := j.RigName; rigName != "" {
		shortName = fmt.Sprintf("%s / %s", shortName, rigName)
	}
	if shortName != "" {
		shortName = " (" + shortName + ")"
	}
	if prefix != "" {
		repr = fmt.Sprintf("%s: %s%s", prefix, j.Name, shortName)
	} else {
		repr = fmt.Sprintf("%s%s", j.Name, shortName)
	}
	if leader != nil {
		repr = "\t" + repr
	}

	t := JumperType_EXPERIENCED
	if j.IsVideographer {
		t = JumperType_VIDEOGRAPHER
	} else if leader != nil {
		switch leader.Type {
		case JumperType_TANDEM_STUDENT:
			if j.IsInstructor {
				t = JumperType_TANDEM_INSTRUCTOR
			}
		case JumperType_AFF_STUDENT:
			if j.IsInstructor {
				t = JumperType_AFF_INSTRUCTOR
			}
		case JumperType_COACH_STUDENT:
			if j.IsInstructor {
				t = JumperType_COACH
			}
		}
	} else {
		switch {
		case j.IsTandem:
			t = JumperType_TANDEM_STUDENT
		case j.IsStudent:
			// TODO how to distinguish between AFF / Coach?
			t = JumperType_AFF_STUDENT
		}
	}

	return &Jumper{
		Id:        uint64(j.ID),
		Type:      t,
		Name:      j.Name,
		ShortName: j.ShortName,
		Color:     color,
		Repr:      repr,
		RigName:   j.RigName,
	}
}

func (s *manifestServiceServer) slotFromJumper(j *burble.Jumper) *LoadSlot {
	if len(j.GroupMembers) == 0 {
		return &LoadSlot{
			Slot: &LoadSlot_Jumper{
				Jumper: s.translateJumper(j, nil),
			},
		}
	}

	g := &JumperGroup{
		Leader: s.translateJumper(j, nil),
	}
	for _, member := range j.GroupMembers {
		g.Members = append(g.Members, s.translateJumper(member, g.Leader))
	}

	return &LoadSlot{
		Slot: &LoadSlot_Group{
			Group: g,
		},
	}
}

func (s *manifestServiceServer) constructUpdate(source core.DataSource) *ManifestUpdate {
	u := &ManifestUpdate{}

	const sunriseSources = core.PreSunriseDataSource | core.SunriseDataSource
	const sunsetSources = core.PreSunsetDataSource | core.SunsetDataSource
	const optionsSources = core.OptionsDataSource | sunriseSources | sunsetSources
	if source&optionsSources != 0 {
		s.options = s.app.Settings().Options()
		o := s.options
		u.Options = &Options{
			DisplayWeather: o.DisplayWeather,
			DisplayWinds:   o.DisplayWinds,
			Message:        o.Message,
			MessageColor:   0xffffff,
		}
		if source&sunriseSources != 0 {
			u.Options.Sunrise = s.app.SunriseMessage()
		}
		if source&sunsetSources != 0 {
			u.Options.Sunset = s.app.SunsetMessage()
		}
	}

	const statusSources = core.METARDataSource | core.WindsAloftDataSource
	if source&statusSources != 0 {
		var (
			separationColor  uint32
			separationString string
		)
		if s.app.WindsAloftSource() != nil {
			separationColor, separationString = s.app.SeparationStrings()
		} else {
			separationColor = 0xffffff
		}

		var winds, clouds, weather, temperature string
		if m := s.app.METARSource(); m != nil {
			winds = m.WindConditions()
			clouds = m.SkyCover()
			weather = m.WeatherConditions()
			temperature = m.TemperatureString()
		}

		u.Status = &Status{
			Winds:            winds,
			WindsColor:       0xffffff,
			Clouds:           clouds,
			CloudsColor:      0xffffff,
			Weather:          weather,
			WeatherColor:     0xffffff,
			Separation:       separationString,
			SeparationColor:  separationColor,
			Temperature:      temperature,
			TemperatureColor: 0xffffff,
		}
	}

	const jumprunSources = core.JumprunDataSource
	if source&jumprunSources != 0 {
		j := s.app.Jumprun().Jumprun()
		u.Jumprun = &Jumprun{
			Origin: &JumprunOrigin{
				Latitude:          j.Latitude,
				Longitude:         j.Longitude,
				MagneticDeviation: int32(j.MagneticDeclination),
				CameraHeight:      int32(j.CameraHeight),
			},
		}
		if j.IsSet {
			p := &JumprunPath{
				Heading:        int32(j.Heading),
				ExitDistance:   int32(j.ExitDistance),
				OffsetHeading:  int32(j.OffsetHeading),
				OffsetDistance: int32(j.OffsetDistance),
			}
			for _, t := range j.HookTurns {
				if t.Distance == 0 && t.Heading == 0 {
					break
				}
				p.Turns = append(p.Turns, &JumprunTurn{
					Distance: int32(t.Distance),
					Heading:  int32(t.Heading),
				})
			}
			u.Jumprun.Path = p
		}
	}

	const windsAloftSources = core.WindsAloftDataSource
	if source&windsAloftSources != 0 {
		w := s.app.WindsAloftSource()
		u.WindsAloft = &WindsAloft{}
		for _, sample := range w.Samples() {
			u.WindsAloft.Samples = append(u.WindsAloft.Samples,
				&WindsAloftSample{
					Altitude:    int32(sample.Altitude),
					Heading:     int32(sample.Heading),
					Speed:       int32(sample.Speed),
					Temperature: int32(sample.Temperature),
					Variable:    sample.LightAndVariable,
				})
		}
	}

	const loadsSources = core.BurbleDataSource | core.OptionsDataSource
	if source&loadsSources != 0 {
		b := s.app.BurbleSource()
		u.Loads = &Loads{
			ColumnCount: int32(b.ColumnCount()),
		}
		for _, l := range b.Loads() {
			var callMinutes string
			if !l.IsNoTime {
				if l.CallMinutes == 0 {
					callMinutes = "NOW"
				} else {
					callMinutes = strconv.FormatInt(l.CallMinutes, 10)
				}
			}

			load := &Load{
				Id:                uint64(l.ID),
				AircraftName:      l.AircraftName,
				LoadNumber:        l.LoadNumber,
				CallMinutes:       int32(l.CallMinutes),
				CallMinutesString: callMinutes,
				SlotsAvailable:    int32(l.SlotsAvailable),
				IsFueling:         l.IsFueling,
				IsTurning:         l.IsTurning,
				IsNoTime:          l.IsNoTime,
			}
			for _, j := range l.Tandems {
				load.Slots = append(load.Slots, s.slotFromJumper(j))
			}
			for _, j := range l.Students {
				load.Slots = append(load.Slots, s.slotFromJumper(j))
			}
			for _, j := range l.SportJumpers {
				load.Slots = append(load.Slots, s.slotFromJumper(j))
			}

			var slotsAvailable string
			if l.CallMinutes <= 5 {
				// Burble doesn't give us unique Jumper IDs in
				// the loads even though it surely tracks them
				// internally. So we have to do the next best
				// thing and just count unique names. This
				// should generally work out fine since mostly
				// duplicate names really only come up when
				// there is one coach with multiple hop/pop
				// students
				names := make(map[string]struct{})
				for _, slot := range load.Slots {
					if j := slot.GetJumper(); j != nil {
						names[j.Name] = struct{}{}
					} else if g := slot.GetGroup(); g != nil {
						names[g.Leader.Name] = struct{}{}
						for _, member := range g.GetMembers() {
							names[member.Name] = struct{}{}
						}
					}
				}
				slotsAvailable = fmt.Sprintf("%d aboard", len(names))
			} else if l.SlotsAvailable == 1 {
				slotsAvailable = "1 slot"
			} else {
				slotsAvailable = fmt.Sprintf("%d slots", l.SlotsAvailable)
			}
			load.SlotsAvailableString = slotsAvailable

			u.Loads.Loads = append(u.Loads.Loads, load)
		}
	}

	return u
}

func (x *ManifestUpdate) diff(y *ManifestUpdate) bool {
	if proto.Equal(x.Status, y.Status) {
		x.Status = nil
	}
	if proto.Equal(x.Options, y.Options) {
		x.Options = nil
	}
	if proto.Equal(x.Jumprun, y.Jumprun) {
		x.Jumprun = nil
	}
	if proto.Equal(x.WindsAloft, y.WindsAloft) {
		x.WindsAloft = nil
	}
	if proto.Equal(x.Loads, y.Loads) {
		x.Loads = nil
	}
	return x.Status != nil || x.Options != nil || x.Jumprun != nil ||
		x.WindsAloft != nil || x.Loads != nil
}

func (s *manifestServiceServer) processUpdates(ctx context.Context) {
	c := make(chan core.DataSource, 128)
	id := s.app.AddListener(c)
	defer func() {
		s.app.RemoveListener(id)
	}()

	clientID := uint64(0)
	clients := make(map[uint64]chan *ManifestUpdate)

	// Create and send the initial baseline ManifestUpdate
	source := core.BurbleDataSource | core.OptionsDataSource
	if s.app.Jumprun() != nil {
		source |= core.JumprunDataSource
	}
	if s.app.METARSource() != nil {
		source |= core.METARDataSource
	}
	if s.app.WindsAloftSource() != nil {
		source |= core.WindsAloftDataSource
	}
	lastUpdate := s.constructUpdate(source)

	for {
		select {
		case <-ctx.Done():
			return

		case req := <-s.addClientChan:
			clientID++
			clients[clientID] = req.updates
			req.reply <- addClientResponse{
				id: clientID,
			}
			update := proto.Clone(lastUpdate).(*ManifestUpdate)
			req.updates <- update

		case req := <-s.removeClientChan:
			delete(clients, req.id)
			req.reply <- removeClientResponse{}

		case source = <-c:
		drain:
			for {
				select {
				case s := <-c:
					source |= s
				default:
					break drain
				}
			}
			if u := s.constructUpdate(source); u.diff(lastUpdate) {
				for _, client := range clients {
					update := proto.Clone(u).(*ManifestUpdate)
					client <- update
				}
				// We cannot use proto.Merge here because we
				// attribute meaning to nil on optional fields,
				// but proto.Merge ignores nil when merging in,
				// not clearing the field in the destination.
				// This is what we want at the top-level, but
				// not the lower levels.
				//proto.Merge(lastUpdate, u)
				if u.Status != nil {
					lastUpdate.Status = u.Status
				}
				if u.Options != nil {
					lastUpdate.Options = u.Options
				}
				if u.Jumprun != nil {
					lastUpdate.Jumprun = u.Jumprun
				}
				if u.WindsAloft != nil {
					lastUpdate.WindsAloft = u.WindsAloft
				}
				if u.Loads != nil {
					lastUpdate.Loads = u.Loads
				}
			}
		}
	}
}

func (s *manifestServiceServer) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.processUpdates(ctx)
	}()
}

func (s *manifestServiceServer) Stop() {
	s.cancel()
	s.wg.Wait()
}

func (s *manifestServiceServer) addClient(c chan *ManifestUpdate) uint64 {
	request := addClientRequest{
		reply:   make(chan addClientResponse),
		updates: c,
	}
	s.addClientChan <- request
	response := <-request.reply
	return response.id
}

func (s *manifestServiceServer) removeClient(id uint64) {
	request := removeClientRequest{
		reply: make(chan removeClientResponse),
		id:    id,
	}
	s.removeClientChan <- request
	<-request.reply
}

func (s *manifestServiceServer) StreamUpdates(
	_ *emptypb.Empty,
	stream ManifestService_StreamUpdatesServer,
) error {
	c := make(chan *ManifestUpdate, 16)
	id := s.addClient(c)
	defer s.removeClient(id)

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-s.app.Done():
			return nil
		case u := <-c:
			if err := stream.Send(u); err != nil {
				return err
			}
		}
	}
}
