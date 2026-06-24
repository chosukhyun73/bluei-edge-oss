package events

import (
	"testing"
	"time"
)

func TestSensorReadingPayloadValidate(t *testing.T) {
	v := 7.2
	p := SensorReadingPayload{
		ReadingID:  "reading_01",
		SensorID:   "sensor_do_tank_01",
		DeviceID:   "water_probe_01",
		Metric:     MetricDissolvedOxygen,
		Value:      &v,
		Unit:       "mg/L",
		Quality:    QualityOK,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Location:   Location{TankID: "tank_01", AreaID: "ras_room_a", PlatformTankID: "00000000-0000-0000-0000-000000000001"},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestSensorReadingPayloadValidateMissingValueAllowedForMissingQuality(t *testing.T) {
	p := SensorReadingPayload{
		ReadingID:  "reading_01",
		SensorID:   "sensor_do_tank_01",
		DeviceID:   "water_probe_01",
		Metric:     MetricDissolvedOxygen,
		Value:      nil,
		Unit:       "mg/L",
		Quality:    QualityMissing,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestSensorReadingPayloadValidateRejectsInvalidMetric(t *testing.T) {
	v := 1.0
	p := SensorReadingPayload{
		ReadingID:  "reading_01",
		SensorID:   "sensor_01",
		DeviceID:   "device_01",
		Metric:     "not_a_metric",
		Value:      &v,
		Unit:       "raw",
		Quality:    QualityOK,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected invalid metric error")
	}
}

func TestDeviceHealthPayloadValidate(t *testing.T) {
	p := DeviceHealthPayload{
		DeviceID:   "water_probe_01",
		DeviceType: DeviceTypeWaterQualitySensor,
		TankID:     "tank_01",
		Status:     DeviceStatusOnline,
		Quality:    QualityOK,
		LastSeenAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestCameraHealthPayloadValidate(t *testing.T) {
	p := CameraHealthPayload{
		CameraID:       "cam_tank_01_front",
		TankID:         "tank_01",
		Status:         DeviceStatusOnline,
		IngestFPS:      29.8,
		LastFrameAt:    time.Now().UTC().Format(time.RFC3339Nano),
		ReconnectCount: 1,
		DroppedFrames:  3,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestVisionObservationPayloadValidate(t *testing.T) {
	p := VisionObservationPayload{
		ObservationID: "vis_obs_01",
		CameraID:      "cam_tank_01_front",
		TankID:        "tank_01",
		Mode:          "lightweight",
		Phase:         "feeding_start",
		ObservedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		FrameTS:       time.Now().UTC().Format(time.RFC3339Nano),
		ModelVersion:  "behavior-v1",
		Confidence:    ptrFloat64(0.82),
		Scores:        map[string]float64{"activity_score": 0.72, "feeding_response": 0.81},
		Candidates:    []string{"feeding_response_onset"},
		Quality:       QualityOK,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestVisionObservationPayloadValidateRejectsBadScore(t *testing.T) {
	p := VisionObservationPayload{
		ObservationID: "vis_obs_01",
		CameraID:      "cam_tank_01_front",
		Mode:          "enhanced",
		Phase:         "feeding_stop",
		ObservedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		ModelVersion:  "behavior-v1",
		Confidence:    ptrFloat64(0.82),
		Scores:        map[string]float64{"feeding_response": 1.2},
		Quality:       QualityOK,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected invalid score error")
	}
}

func TestAlertPayloadValidate(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	p := AlertPayload{
		AlertID:   "alert_01",
		AlertType: "water_quality.do_low",
		Severity:  SeverityWarning,
		Status:    AlertStatusOpen,
		Subject:   AlertSubject{Kind: "tank", ID: "tank_01"},
		RuleID:    "rule_do_low",
		Message:   "DO is below threshold",
		RaisedAt:  now,
		UpdatedAt: now,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestAlertPayloadValidateRejectsInvalidSeverity(t *testing.T) {
	p := AlertPayload{
		AlertID:   "alert_01",
		AlertType: "water_quality.do_low",
		Severity:  "urgent",
		Status:    AlertStatusOpen,
		Subject:   AlertSubject{Kind: "tank", ID: "tank_01"},
		Message:   "DO is below threshold",
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected invalid severity error")
	}
}

func TestFeedingRecordedPayloadValidate(t *testing.T) {
	p := FeedingRecordedPayload{
		FeedingID:   "feeding_01",
		TankID:      "tank_01",
		FeederID:    "feeder_01",
		Source:      FeedingSourceManual,
		FeedAmountG: 450,
		FeedType:    "pellet_3mm",
		FedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		RecordedBy:  "operator",
		Quality:     QualityOK,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestFeedingRecordedPayloadValidateRejectsInvalidSource(t *testing.T) {
	p := FeedingRecordedPayload{
		FeedingID:   "feeding_01",
		TankID:      "tank_01",
		Source:      "robot_guess",
		FeedAmountG: 450,
		FedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		Quality:     QualityOK,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected invalid feeding source error")
	}
}

func TestFeedingRecommendationPayloadValidate(t *testing.T) {
	now := time.Now().UTC()
	p := FeedingRecommendationPayload{
		RecommendationID:   "feed_rec_01",
		TankID:             "tank_01",
		RecommendedAmountG: 1200,
		RecommendedAt:      now.Format(time.RFC3339Nano),
		ValidUntil:         now.Add(30 * time.Minute).Format(time.RFC3339Nano),
		Confidence:         0.72,
		ReasonCodes:        []string{"water_quality_ok", "get_window_ready"},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestVisionObservationPayloadValidateRequiresProvenanceForOK(t *testing.T) {
	p := VisionObservationPayload{
		ObservationID: "vis_obs_01",
		CameraID:      "cam_tank_01_front",
		TankID:        "tank_01",
		Mode:          "lightweight",
		Phase:         "feeding_start",
		ObservedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		FrameTS:       time.Now().UTC().Format(time.RFC3339Nano),
		ModelVersion:  "behavior-v1",
		Confidence:    ptrFloat64(0.72),
		Quality:       QualityOK,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestVisionObservationPayloadValidateRejectsOKWithoutModelVersion(t *testing.T) {
	p := VisionObservationPayload{
		ObservationID: "vis_obs_01",
		CameraID:      "cam_tank_01_front",
		Mode:          "lightweight",
		Phase:         "feeding_start",
		ObservedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		FrameTS:       time.Now().UTC().Format(time.RFC3339Nano),
		Confidence:    ptrFloat64(0.72),
		Quality:       QualityOK,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected missing model_version error")
	}
}

func TestVisionObservationDisputedPayloadValidate(t *testing.T) {
	p := VisionObservationDisputedPayload{
		DisputeID:     "vis_dispute_01",
		ObservationID: "vis_obs_01",
		CameraID:      "cam_tank_01_front",
		TankID:        "tank_01",
		OperatorID:    "operator_01",
		Verdict:       "wrong",
		Reason:        "operator saw normal feeding response",
		DisputedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestVisionObservationDisputedPayloadValidate_OperatorScoreInRange(t *testing.T) {
	score := 0.65
	p := VisionObservationDisputedPayload{
		DisputeID: "d1", ObservationID: "o1", CameraID: "c1", OperatorID: "op1",
		Verdict: "correct", OperatorScore: &score,
		DisputedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("0.65 should be valid: %v", err)
	}
}

func TestVisionObservationDisputedPayloadValidate_OperatorScoreOutOfRange(t *testing.T) {
	for _, v := range []float64{-0.1, 1.1, 2.0, -1.0} {
		score := v
		p := VisionObservationDisputedPayload{
			DisputeID: "d1", ObservationID: "o1", CameraID: "c1", OperatorID: "op1",
			Verdict: "correct", OperatorScore: &score,
			DisputedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := p.Validate(); err == nil {
			t.Fatalf("score=%v should be rejected", v)
		}
	}
}

func ptrFloat64(v float64) *float64 { return &v }

func TestMediaClipStoredPayloadValidate(t *testing.T) {
	p := MediaClipStoredPayload{
		ClipID:     "clip_01",
		CameraID:   "cam_tank_01_front",
		TankID:     "tank_01",
		Reason:     "feeding_session",
		StartedAt:  time.Now().UTC().Add(-10 * time.Second).Format(time.RFC3339Nano),
		EndedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		URI:        "file:///var/lib/bluei-edge/media/clip_01.mjpeg",
		MimeType:   "multipart/x-mixed-replace",
		SizeBytes:  1024,
		FrameCount: 50,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestMediaClipStoredPayloadRejectsInvalidTimeRange(t *testing.T) {
	now := time.Now().UTC()
	p := MediaClipStoredPayload{
		ClipID:    "clip_01",
		CameraID:  "cam_tank_01_front",
		Reason:    "feeding_session",
		StartedAt: now.Format(time.RFC3339Nano),
		EndedAt:   now.Add(-time.Second).Format(time.RFC3339Nano),
		URI:       "file:///tmp/clip_01.mjpeg",
		MimeType:  "multipart/x-mixed-replace",
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected invalid clip time range")
	}
}
