package gatewayagent

import "testing"

func TestParseJSONFrameWithDefaults(t *testing.T) {
	v := 7820.0
	_ = v
	payload, err := ParseFrame(`{"value":7820}`, FrameDefaults{TankID: "tank_01", DeviceID: "feeder_01", SensorID: "scale_01", Metric: "feed_weight", Unit: "g", Quality: "ok"})
	if err != nil {
		t.Fatalf("ParseFrame() error = %v", err)
	}
	if payload.SensorID != "scale_01" || payload.DeviceID != "feeder_01" || payload.Metric != "feed_weight" || payload.Location.TankID != "tank_01" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Value == nil || *payload.Value != 7820 {
		t.Fatalf("unexpected value: %v", payload.Value)
	}
}

func TestParseCSVFrame(t *testing.T) {
	payload, err := ParseFrame(`scale_01,feed_weight,7800,g,ok`, FrameDefaults{TankID: "tank_01", DeviceID: "feeder_01"})
	if err != nil {
		t.Fatalf("ParseFrame() error = %v", err)
	}
	if payload.SensorID != "scale_01" || payload.Unit != "g" || payload.Location.TankID != "tank_01" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}
