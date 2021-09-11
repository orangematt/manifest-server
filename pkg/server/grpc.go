// (c) Copyright 2017-2021 Matt Messier

package server

import (
	"fmt"
	reflect "reflect"
	"strconv"
	"strings"

	"github.com/orangematt/manifest-server/pkg/burble"
	"github.com/orangematt/manifest-server/pkg/core"
	"github.com/orangematt/manifest-server/pkg/settings"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

type manifestServiceServer struct {
	UnimplementedManifestServiceServer

	app     *core.Controller
	options settings.Options
}

func newManifestServiceServer(controller *core.Controller) *manifestServiceServer {
	return &manifestServiceServer{
		app: controller,
	}
}

func (s *manifestServiceServer) translateJumper(j *burble.Jumper, leader *Jumper) *Jumper {
	var color, prefix string
	if leader != nil {
		color = leader.Color
	} else {
		switch {
		case j.IsTandem:
			color = "#ffff00" // yellow
		case j.IsStudent || strings.HasSuffix(j.ShortName, " + Gear"):
			color = "#00ff00" // green
			if strings.HasSuffix(j.ShortName, " H/P") {
				prefix = "Hop & Pop"
			}
		case strings.HasPrefix(j.ShortName, "3-5k") || strings.HasPrefix(j.ShortName, "3.5k"):
			color = "#ff00ff" // magenta
			prefix = "Hop & Pop"
		default:
			color = "#ffffff" // white
		}
	}

	var name, repr string
	if s.options.DisplayNicknames && j.Nickname != "" {
		name = j.Nickname
	} else {
		name = j.Name
	}
	if leader == nil {
		switch {
		case j.IsTandem:
			repr = "Tandem: " + name
		case prefix != "":
			repr = fmt.Sprintf("%s: %s (%s)", prefix, name, j.ShortName)
		default:
			repr = fmt.Sprintf("%s (%s)", name, j.ShortName)
		}
	} else {
		repr = fmt.Sprintf("\t%s (%s)", name, j.ShortName)
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
		Nickname:  j.Nickname,
		Color:     color,
		Repr:      repr,
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

	const statusSources = core.METARDataSource | core.WindsAloftDataSource
	if source&statusSources != 0 {
		var separationColor, separationString string
		if s.app.WindsAloftSource() != nil {
			separationColor, separationString = s.app.SeparationStrings()
		} else {
			separationColor = "#ffffff"
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
			WindsColor:       "#ffffff",
			Clouds:           clouds,
			CloudsColor:      "#ffffff",
			Weather:          weather,
			WeatherColor:     "#ffffff",
			Separation:       separationString,
			SeparationColor:  separationColor,
			Temperature:      temperature,
			TemperatureColor: "#ffffff",
		}
	}

	const optionsSources = core.OptionsDataSource
	if source&optionsSources != 0 {
		o := s.app.Settings().Options()
		u.Options = &Options{
			DisplayNicknames: o.DisplayNicknames,
			DisplayWeather:   o.DisplayWeather,
			DisplayWinds:     o.DisplayWinds,
			Message:          o.Message,
			MessageColor:     "#ffffff",
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

	const loadsSources = core.BurbleDataSource
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

			var slotsAvailable string
			if l.CallMinutes <= 5 {
				slotsAvailable = fmt.Sprintf("%d aboard", l.SlotsAvailable)
			} else if l.SlotsAvailable == 1 {
				slotsAvailable = "1 slot"
			} else {
				slotsAvailable = fmt.Sprintf("%d slots", l.SlotsAvailable)
			}

			load := &Load{
				Id:                   uint64(l.ID),
				AircraftName:         l.AircraftName,
				LoadNumber:           l.LoadNumber,
				CallMinutes:          int32(l.CallMinutes),
				CallMinutesString:    callMinutes,
				SlotsAvailable:       int32(l.SlotsAvailable),
				SlotsAvailableString: slotsAvailable,
				IsFueling:            l.IsFueling,
				IsTurning:            l.IsTurning,
				IsNoTime:             l.IsNoTime,
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

			u.Loads.Loads = append(u.Loads.Loads, load)
		}
	}

	return u
}

func (s *manifestServiceServer) diffUpdates(new, old *ManifestUpdate) bool {
	if new.Status != nil && reflect.DeepEqual(new.Status, old.Status) {
		new.Status = nil
	}
	if new.Options != nil && reflect.DeepEqual(new.Options, old.Options) {
		new.Options = nil
	}
	if new.Jumprun != nil && reflect.DeepEqual(new.Jumprun, old.Jumprun) {
		new.Jumprun = nil
	}
	if new.WindsAloft != nil && reflect.DeepEqual(new.WindsAloft, old.WindsAloft) {
		new.WindsAloft = nil
	}
	if new.Loads != nil && reflect.DeepEqual(new.Loads, old.Loads) {
		new.Loads = nil
	}
	return new.Status != nil || new.Options != nil || new.Jumprun != nil ||
		new.WindsAloft != nil || new.Loads != nil
}

func (s *manifestServiceServer) mergeUpdates(to, from *ManifestUpdate) {
	if from.Status != nil {
		to.Status = from.Status
	}
	if from.Options != nil {
		to.Options = from.Options
	}
	if from.Jumprun != nil {
		to.Jumprun = from.Jumprun
	}
	if from.WindsAloft != nil {
		to.WindsAloft = from.WindsAloft
	}
	if from.Loads != nil {
		to.Loads = from.Loads
	}
}

func (s *manifestServiceServer) StreamUpdates(
	_ *emptypb.Empty,
	stream ManifestService_StreamUpdatesServer,
) error {
	// Start listening for changes before sending out the initial baseline
	// ManifestUpdate so that we don't lose any updates.
	c := make(chan core.DataSource, 128)
	id := s.app.AddListener(c)
	defer func() {
		s.app.RemoveListener(id)
	}()

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
	if err := stream.Send(lastUpdate); err != nil {
		return err
	}

	for {
		select {
		case <-s.app.Done():
			return nil
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
			u := s.constructUpdate(source)
			if s.diffUpdates(u, lastUpdate) {
				if err := stream.Send(u); err != nil {
					return err
				}
				s.mergeUpdates(lastUpdate, u)
			}
		}
	}
}
