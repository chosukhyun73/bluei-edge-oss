package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// 카메라 자동 탐지 — GET /v1/cameras/discover
//
// 1) ONVIF WS-Discovery: 멀티캐스트 probe(239.255.255.250:3702, NetworkVideoTransmitter)
//    → ONVIF 카메라가 IP/모델/벤더로 응답. "진짜 카메라만" 에 가장 근접.
// 2) 포트 스캔 fallback: LAN /24 의 RTSP(554)/HTTP(80) 열린 호스트 + /proc/net/arp MAC.
//    ONVIF 미지원 카메라까지 커버하지만 오탐(NVR 등) 가능.
//
// 둘 다 같은 L2 서브넷만 닿는다(멀티캐스트/스캔은 라우터 너머 X). 호스트(GX10)에서 실행.
// ──────────────────────────────────────────────────────────────────────────────

type discoveredCamera struct {
	IP       string `json:"ip"`
	Vendor   string `json:"vendor,omitempty"`
	Model    string `json:"model,omitempty"`
	Name     string `json:"name,omitempty"`
	MAC      string `json:"mac,omitempty"`
	RTSPPort int    `json:"rtsp_port,omitempty"`
	HTTPPort int    `json:"http_port,omitempty"`
	Source   string `json:"source"` // "onvif" | "scan"
}

// GET /v1/cameras/discover?scan=true — ONVIF + (옵션)포트스캔.
func (s *Server) handleCameraDiscover(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
	defer cancel()

	byIP := map[string]*discoveredCamera{}
	merge := func(c discoveredCamera) {
		if c.IP == "" {
			return
		}
		ex, ok := byIP[c.IP]
		if !ok {
			cp := c
			byIP[c.IP] = &cp
			return
		}
		// ONVIF 정보(vendor/model/name)는 우선, scan 은 MAC 보강.
		if ex.Vendor == "" {
			ex.Vendor = c.Vendor
		}
		if ex.Model == "" {
			ex.Model = c.Model
		}
		if ex.Name == "" {
			ex.Name = c.Name
		}
		if ex.MAC == "" {
			ex.MAC = c.MAC
		}
		if ex.RTSPPort == 0 {
			ex.RTSPPort = c.RTSPPort
		}
		if ex.HTTPPort == 0 {
			ex.HTTPPort = c.HTTPPort
		}
		if ex.Source == "onvif" || c.Source == "onvif" {
			ex.Source = "onvif"
		}
	}

	for _, c := range onvifDiscover(ctx) {
		merge(c)
	}
	if r.URL.Query().Get("scan") != "false" {
		for _, c := range scanLAN(ctx) {
			merge(c)
		}
	}

	out := make([]discoveredCamera, 0, len(byIP))
	for _, c := range byIP {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return ipLess(out[i].IP, out[j].IP) })
	writeJSON(w, http.StatusOK, map[string]any{"cameras": out, "count": len(out)})
}

// ── ONVIF WS-Discovery ────────────────────────────────────────────────────────

const wsDiscoveryAddr = "239.255.255.250:3702"

func onvifProbe() []byte {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	msgID := "uuid:" + hex.EncodeToString(b)
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" ` +
		`xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" ` +
		`xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" ` +
		`xmlns:dn="http://www.onvif.org/ver10/network/wsdl">` +
		`<e:Header><w:MessageID>` + msgID + `</w:MessageID>` +
		`<w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>` +
		`<w:Action e:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action>` +
		`</e:Header><e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body></e:Envelope>`)
}

func onvifDiscover(ctx context.Context) []discoveredCamera {
	group, err := net.ResolveUDPAddr("udp4", wsDiscoveryAddr)
	if err != nil {
		return nil
	}
	body := onvifProbe()

	var mu sync.Mutex
	var out []discoveredCamera
	var wg sync.WaitGroup

	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || !ip4.IsPrivate() || isExcludedSubnetIP(ip4) {
				continue
			}
			src := append(net.IP(nil), ip4...)
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: src, Port: 0})
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = conn.WriteToUDP(body, group)
				deadline := time.Now().Add(2500 * time.Millisecond)
				_ = conn.SetReadDeadline(deadline)
				buf := make([]byte, 65535)
				for {
					n, _, err := conn.ReadFromUDP(buf)
					if err != nil {
						return
					}
					if cam := parseProbeMatch(string(buf[:n])); cam.IP != "" {
						mu.Lock()
						out = append(out, cam)
						mu.Unlock()
					}
				}
			}()
		}
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return out
}

var (
	xaddrRe     = regexp.MustCompile(`https?://(\d{1,3}(?:\.\d{1,3}){3})(?::(\d+))?`)
	scopeNameRe = regexp.MustCompile(`onvif://www\.onvif\.org/name/([^\s<]+)`)
	scopeHwRe   = regexp.MustCompile(`onvif://www\.onvif\.org/hardware/([^\s<]+)`)
)

// parseProbeMatch — ONVIF ProbeMatch SOAP 응답에서 IP/포트/이름/모델 추출.
// 네임스페이스가 기기마다 달라 정규식으로 견고하게 파싱.
func parseProbeMatch(xml string) discoveredCamera {
	cam := discoveredCamera{Source: "onvif", RTSPPort: 554, HTTPPort: 80}
	if m := xaddrRe.FindStringSubmatch(xml); m != nil {
		cam.IP = m[1]
		if m[2] != "" {
			if p, err := strconv.Atoi(m[2]); err == nil {
				cam.HTTPPort = p
			}
		}
	}
	if m := scopeNameRe.FindStringSubmatch(xml); m != nil {
		if v, err := url.QueryUnescape(m[1]); err == nil {
			cam.Name = v
		} else {
			cam.Name = m[1]
		}
	}
	if m := scopeHwRe.FindStringSubmatch(xml); m != nil {
		if v, err := url.QueryUnescape(m[1]); err == nil {
			cam.Model = v
		} else {
			cam.Model = m[1]
		}
	}
	// ONVIF scope 에 vendor 가 없는 기기가 많아 name/model 텍스트에서 벤더 추정.
	if cam.Vendor == "" {
		cam.Vendor = vendorFromText(cam.Name + " " + cam.Model)
	}
	return cam
}

// vendorFromText — 카메라 이름/모델 문자열에서 알려진 벤더 키워드 추출 (소문자).
func vendorFromText(s string) string {
	l := strings.ToLower(s)
	for _, v := range []string{"hikvision", "dahua", "hanwha", "samsung", "axis", "reolink", "balus"} {
		if strings.Contains(l, v) {
			return v
		}
	}
	return ""
}

// ── 포트 스캔 fallback ──────────────────────────────────────────────────────────

func scanLAN(ctx context.Context) []discoveredCamera {
	subnets := lanSubnets()
	if len(subnets) == 0 {
		return nil
	}
	sem := make(chan struct{}, 64)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var out []discoveredCamera

	for _, sn := range subnets {
		for _, ip := range hostsIn(sn) {
			select {
			case <-ctx.Done():
				break
			default:
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(ip string) {
				defer wg.Done()
				defer func() { <-sem }()
				d := net.Dialer{Timeout: 350 * time.Millisecond}
				c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "554"))
				if err != nil {
					return
				}
				_ = c.Close()
				cam := discoveredCamera{IP: ip, RTSPPort: 554, Source: "scan"}
				if c2, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "80")); err == nil {
					_ = c2.Close()
					cam.HTTPPort = 80
				}
				mu.Lock()
				out = append(out, cam)
				mu.Unlock()
			}(ip)
		}
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}

	arp := readARP()
	for i := range out {
		if mac, ok := arp[out[i].IP]; ok {
			out[i].MAC = mac
			if v := ouiVendor(mac); v != "" {
				out[i].Vendor = v
			}
		}
	}
	return out
}

// lanSubnets — 스캔할 사설 IPv4 /24 서브넷 (docker/tailscale 제외, 최대 3개).
func lanSubnets() []*net.IPNet {
	var out []*net.IPNet
	seen := map[string]bool{}
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || !ip4.IsPrivate() || isExcludedSubnetIP(ip4) {
				continue
			}
			ones, _ := ipnet.Mask.Size()
			if ones < 24 { // /24 보다 큰 서브넷은 호스트가 너무 많아 스캔 제한
				ones = 24
			}
			mask := net.CIDRMask(ones, 32)
			network := ip4.Mask(mask)
			key := network.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, &net.IPNet{IP: network, Mask: mask})
			if len(out) >= 3 {
				return out
			}
		}
	}
	return out
}

// isExcludedSubnetIP — docker 브리지(172.16/12)·tailscale CGNAT(100.64/10) 제외.
func isExcludedSubnetIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return true
	}
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return true
	}
	if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return true
	}
	return false
}

// hostsIn — /24 기준 .1 ~ .254 호스트 IP 목록 (최대 254개).
func hostsIn(sn *net.IPNet) []string {
	base := sn.IP.To4()
	if base == nil {
		return nil
	}
	out := make([]string, 0, 254)
	for h := 1; h <= 254; h++ {
		ip := net.IPv4(base[0], base[1], base[2], byte(h))
		if sn.Contains(ip) {
			out = append(out, ip.String())
		}
	}
	return out
}

// readARP — /proc/net/arp 에서 IP→MAC 매핑 (00:00:00:00:00:00 제외).
func readARP() map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return out
	}
	lines := strings.Split(string(data), "\n")
	for i, ln := range lines {
		if i == 0 {
			continue // header
		}
		f := strings.Fields(ln)
		if len(f) < 4 {
			continue
		}
		mac := strings.ToUpper(f[3])
		if mac == "00:00:00:00:00:00" || mac == "" {
			continue
		}
		out[f[0]] = mac
	}
	return out
}

// ouiVendor — MAC OUI prefix → 카메라 벤더 추정 (best-effort, 일부만).
var ouiVendors = map[string]string{
	"C0:56:E3": "Hikvision", "4C:BD:8F": "Hikvision", "44:19:B6": "Hikvision",
	"3C:EF:8C": "Dahua", "90:02:A9": "Dahua", "14:A7:8B": "Dahua",
	"00:09:18": "Hanwha/Samsung", "00:16:6C": "Hanwha/Samsung",
	"00:40:8C": "Axis", "AC:CC:8E": "Axis",
	"EC:71:DB": "Reolink",
}

func ouiVendor(mac string) string {
	if len(mac) < 8 {
		return ""
	}
	return ouiVendors[strings.ToUpper(mac[:8])]
}

// ipLess — 정렬용 IPv4 비교.
func ipLess(a, b string) bool {
	ia, ib := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if ia == nil || ib == nil {
		return a < b
	}
	for i := 0; i < 4; i++ {
		if ia[i] != ib[i] {
			return ia[i] < ib[i]
		}
	}
	return false
}
