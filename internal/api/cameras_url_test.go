package api

import "testing"

func TestDefaultRTSPPath(t *testing.T) {
	cases := []struct{ vendor, tier, want string }{
		{"hikvision", "sub", "/Streaming/Channels/102"},
		{"hikvision", "main", "/Streaming/Channels/101"},
		{"Hikvision", "", "/Streaming/Channels/102"}, // tier "" → sub, 대소문자 무관
		{"dahua", "main", "/cam/realmonitor?channel=1&subtype=0"},
		{"unknown_vendor", "sub", ""},
		{"", "sub", ""},
	}
	for _, c := range cases {
		if got := defaultRTSPPath(c.vendor, c.tier); got != c.want {
			t.Errorf("defaultRTSPPath(%q,%q)=%q want %q", c.vendor, c.tier, got, c.want)
		}
	}
}

func TestBuildRTSPURL(t *testing.T) {
	// 평범한 경로
	if got := buildRTSPURL("192.168.0.94", 554, "/Streaming/Channels/102", "admin", "pw"); got != "rtsp://admin:pw@192.168.0.94:554/Streaming/Channels/102" {
		t.Errorf("plain: %q", got)
	}
	// 쿼리 포함 경로 (Dahua) — RawQuery 로 분리되어 ? 가 escape 되지 않아야.
	got := buildRTSPURL("10.0.0.5", 0, "/cam/realmonitor?channel=1&subtype=1", "", "")
	if got != "rtsp://10.0.0.5:554/cam/realmonitor?channel=1&subtype=1" {
		t.Errorf("query: %q", got)
	}
	// 슬래시 없는 경로 보정
	if got := buildRTSPURL("h", 554, "live", "", ""); got != "rtsp://h:554/live" {
		t.Errorf("noslash: %q", got)
	}
}

func TestVendorFromText(t *testing.T) {
	if vendorFromText("HIKVISION DS-2CD1021-I") != "hikvision" {
		t.Error("hikvision from name")
	}
	if vendorFromText("some random box") != "" {
		t.Error("unknown should be empty")
	}
}
