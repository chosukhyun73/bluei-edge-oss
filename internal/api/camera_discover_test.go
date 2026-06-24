package api

import (
	"net"
	"testing"
)

func TestParseProbeMatch(t *testing.T) {
	// 실제 ONVIF ProbeMatch 와 유사한 응답 (네임스페이스 prefix 가 기기마다 다름).
	sample := `<?xml version="1.0"?><SOAP-ENV:Envelope><SOAP-ENV:Body>
<d:ProbeMatches xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
<d:ProbeMatch>
<d:Scopes>onvif://www.onvif.org/name/Balus%205MP onvif://www.onvif.org/hardware/IPC-B5 onvif://www.onvif.org/location/country/kr</d:Scopes>
<d:XAddrs>http://192.168.0.123/onvif/device_service</d:XAddrs>
</d:ProbeMatch>
</d:ProbeMatches>
</SOAP-ENV:Body></SOAP-ENV:Envelope>`

	cam := parseProbeMatch(sample)
	if cam.IP != "192.168.0.123" {
		t.Fatalf("IP: got %q", cam.IP)
	}
	if cam.Name != "Balus 5MP" {
		t.Fatalf("Name(url-decoded): got %q", cam.Name)
	}
	if cam.Model != "IPC-B5" {
		t.Fatalf("Model: got %q", cam.Model)
	}
	if cam.RTSPPort != 554 {
		t.Fatalf("RTSPPort default: got %d", cam.RTSPPort)
	}
	if cam.Source != "onvif" {
		t.Fatalf("Source: got %q", cam.Source)
	}
}

func TestParseProbeMatchWithPort(t *testing.T) {
	cam := parseProbeMatch(`<d:XAddrs>http://10.0.0.5:8080/onvif/device_service</d:XAddrs>`)
	if cam.IP != "10.0.0.5" || cam.HTTPPort != 8080 {
		t.Fatalf("got ip=%q http=%d", cam.IP, cam.HTTPPort)
	}
}

func TestParseProbeMatchEmpty(t *testing.T) {
	if cam := parseProbeMatch("garbage no xaddrs"); cam.IP != "" {
		t.Fatalf("expected empty IP, got %q", cam.IP)
	}
}

func TestOUIVendor(t *testing.T) {
	if v := ouiVendor("c0:56:e3:11:22:33"); v != "Hikvision" {
		t.Fatalf("Hikvision OUI: got %q", v)
	}
	if v := ouiVendor("AA:BB:CC:00:11:22"); v != "" {
		t.Fatalf("unknown OUI should be empty, got %q", v)
	}
	if v := ouiVendor("short"); v != "" {
		t.Fatalf("short MAC should be empty, got %q", v)
	}
}

func TestIsExcludedSubnetIP(t *testing.T) {
	cases := map[string]bool{
		"192.168.0.114": false,
		"10.0.0.5":      false,
		"172.18.0.1":    true, // docker
		"100.108.226.1": true, // tailscale CGNAT
	}
	for ip, want := range cases {
		if got := isExcludedSubnetIP(net.ParseIP(ip)); got != want {
			t.Fatalf("%s: got %v want %v", ip, got, want)
		}
	}
}
