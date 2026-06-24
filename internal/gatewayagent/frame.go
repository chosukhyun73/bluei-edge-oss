package gatewayagent

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
)

type SensorFrame struct {
	ReadingID  string         `json:"reading_id,omitempty"`
	SensorID   string         `json:"sensor_id,omitempty"`
	DeviceID   string         `json:"device_id,omitempty"`
	Metric     string         `json:"metric,omitempty"`
	Value      *float64       `json:"value,omitempty"`
	Unit       string         `json:"unit,omitempty"`
	Quality    string         `json:"quality,omitempty"`
	ObservedAt string         `json:"observed_at,omitempty"`
	TankID     string         `json:"tank_id,omitempty"`
	AreaID     string         `json:"area_id,omitempty"`
	Raw        map[string]any `json:"raw,omitempty"`
}

func ParseFrame(line string, defaults FrameDefaults) (events.SensorReadingPayload, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return events.SensorReadingPayload{}, fmt.Errorf("empty frame")
	}
	if strings.HasPrefix(line, "{") {
		return parseJSONFrame(line, defaults)
	}
	return parseCSVFrame(line, defaults)
}

func parseJSONFrame(line string, defaults FrameDefaults) (events.SensorReadingPayload, error) {
	var frame SensorFrame
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		return events.SensorReadingPayload{}, fmt.Errorf("parse json frame: %w", err)
	}
	return frame.toPayload(defaults, map[string]any{"frame_format": "json"})
}

func parseCSVFrame(line string, defaults FrameDefaults) (events.SensorReadingPayload, error) {
	r := csv.NewReader(strings.NewReader(line))
	r.TrimLeadingSpace = true
	fields, err := r.Read()
	if err != nil {
		return events.SensorReadingPayload{}, fmt.Errorf("parse csv frame: %w", err)
	}
	if len(fields) < 3 {
		return events.SensorReadingPayload{}, fmt.Errorf("csv frame requires at least sensor_id,metric,value")
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
	if err != nil {
		return events.SensorReadingPayload{}, fmt.Errorf("parse csv value: %w", err)
	}
	frame := SensorFrame{SensorID: strings.TrimSpace(fields[0]), Metric: strings.TrimSpace(fields[1]), Value: &value}
	if len(fields) > 3 {
		frame.Unit = strings.TrimSpace(fields[3])
	}
	if len(fields) > 4 {
		frame.Quality = strings.TrimSpace(fields[4])
	}
	if len(fields) > 5 {
		frame.ObservedAt = strings.TrimSpace(fields[5])
	}
	return frame.toPayload(defaults, map[string]any{"frame_format": "csv", "raw_line": line})
}

func (f SensorFrame) toPayload(defaults FrameDefaults, raw map[string]any) (events.SensorReadingPayload, error) {
	if f.SensorID == "" {
		f.SensorID = defaults.SensorID
	}
	if f.DeviceID == "" {
		f.DeviceID = defaults.DeviceID
	}
	if f.Metric == "" {
		f.Metric = defaults.Metric
	}
	if f.Unit == "" {
		f.Unit = defaults.Unit
	}
	if f.Quality == "" {
		f.Quality = defaults.Quality
	}
	if f.ObservedAt == "" {
		f.ObservedAt = common.FormatTime(common.NowUTC())
	} else if _, err := time.Parse(time.RFC3339Nano, f.ObservedAt); err != nil {
		return events.SensorReadingPayload{}, fmt.Errorf("observed_at must be RFC3339/RFC3339Nano: %w", err)
	}
	if f.TankID == "" {
		f.TankID = defaults.TankID
	}
	if f.Raw != nil {
		for k, v := range f.Raw {
			raw[k] = v
		}
	}
	payload := events.SensorReadingPayload{
		ReadingID:  f.ReadingID,
		SensorID:   f.SensorID,
		DeviceID:   f.DeviceID,
		Metric:     f.Metric,
		Value:      f.Value,
		Unit:       f.Unit,
		Quality:    f.Quality,
		ObservedAt: f.ObservedAt,
		Location: events.Location{
			AreaID: f.AreaID,
			TankID: f.TankID,
		},
		Raw: raw,
	}
	if payload.ReadingID == "" {
		payload.ReadingID = common.NewID("reading")
	}
	if err := payload.Validate(); err != nil {
		return events.SensorReadingPayload{}, err
	}
	return payload, nil
}
