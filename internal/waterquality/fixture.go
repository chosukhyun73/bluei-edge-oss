package waterquality

import (
	"crypto/sha1"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"bluei.kr/edge/internal/events"
)

type SmartSalmonOptions struct {
	SiteID   string
	EdgeID   string
	DeviceID string
	SensorID string
}

// ParseSmartSalmonCSV converts the provided Gangwon Smart Salmon water-quality
// fixture into canonical sensor reading payloads. It intentionally returns
// payloads, not persisted events, so tests and import commands can choose their
// own event envelope/idempotency policy.
func ParseSmartSalmonCSV(r io.Reader, opts SmartSalmonOptions) ([]events.SensorReadingPayload, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return nil, err
	}
	idx := indexHeader(header)
	required := []string{"동 이름", "모듈 번호", "수조 번호", "수온", "pH", "DO", "저장 시점"}
	for _, key := range required {
		if _, ok := idx[key]; !ok {
			return nil, fmt.Errorf("missing required column %q", key)
		}
	}

	var out []events.SensorReadingPayload
	rowNo := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv row %d: %w", rowNo+1, err)
		}
		rowNo++
		observed, err := parseKSTOffsetTime(get(rec, idx, "저장 시점"))
		if err != nil {
			return nil, fmt.Errorf("row %d observed_at: %w", rowNo, err)
		}
		building := get(rec, idx, "동 이름")
		moduleNo := get(rec, idx, "모듈 번호")
		tankNo := get(rec, idx, "수조 번호")
		tankID := "tank_" + strings.TrimSpace(tankNo)
		if tankID == "tank_" {
			tankID = "tank_unknown"
		}

		for _, spec := range []struct {
			column string
			metric string
			unit   string
		}{
			{"수온", events.MetricWaterTemperature, "celsius"},
			{"pH", events.MetricPH, "ph"},
			{"DO", events.MetricDissolvedOxygen, "mg/L"},
		} {
			rawValue := get(rec, idx, spec.column)
			v, err := strconv.ParseFloat(strings.TrimSpace(rawValue), 64)
			if err != nil {
				return nil, fmt.Errorf("row %d %s parse: %w", rowNo, spec.column, err)
			}
			classification := ClassifyReading(spec.metric, &v)
			sensorID := opts.SensorID
			if sensorID == "" {
				sensorID = fmt.Sprintf("%s_%s", strings.ToLower(opts.DeviceID), spec.metric)
			}
			out = append(out, events.SensorReadingPayload{
				ReadingID:  fixtureReadingID(opts, sensorID, tankID, spec.metric, observed),
				SensorID:   sensorID,
				DeviceID:   opts.DeviceID,
				Metric:     spec.metric,
				Value:      &v,
				Unit:       spec.unit,
				Quality:    classification.Quality,
				ObservedAt: observed.Format(time.RFC3339Nano),
				Location: events.Location{
					AreaID: building,
					TankID: tankID,
				},
				Raw: map[string]any{
					"fixture":        "gangwon_smart_salmon_water_quality",
					"building":       building,
					"module_no":      moduleNo,
					"tank_no":        tankNo,
					"source_column":  spec.column,
					"raw_value":      rawValue,
					"quality_reason": classification.Reason,
				},
			})
		}
	}
	return out, nil
}

func indexHeader(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	return idx
}

func get(rec []string, idx map[string]int, key string) string {
	i := idx[key]
	if i < 0 || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

func parseKSTOffsetTime(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05.999 -0700",
		"2006-01-02 15:04:05 -0700",
		time.RFC3339Nano,
	}
	var lastErr error
	for _, layout := range layouts {
		t, err := time.Parse(layout, strings.TrimSpace(s))
		if err == nil {
			return t.UTC(), nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func fixtureReadingID(opts SmartSalmonOptions, sensorID, tankID, metric string, observedAt time.Time) string {
	key := strings.Join([]string{
		opts.SiteID,
		opts.EdgeID,
		opts.DeviceID,
		sensorID,
		tankID,
		metric,
		observedAt.Format(time.RFC3339Nano),
	}, "|")
	sum := sha1.Sum([]byte(key))
	return "reading_fixture_" + hex.EncodeToString(sum[:])[:16]
}
