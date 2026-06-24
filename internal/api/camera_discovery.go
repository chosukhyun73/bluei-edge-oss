package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type cameraDiscoveryRequest struct {
	CIDR      string   `json:"cidr"`
	Hosts     []string `json:"hosts"`
	AutoLocal bool     `json:"auto_local"`
	TimeoutMS int      `json:"timeout_ms"`
}

type cameraDiscoveryCandidate struct {
	Host          string   `json:"host"`
	OpenPorts     []int    `json:"open_ports"`
	LikelyCamera  bool     `json:"likely_camera"`
	VendorHint    string   `json:"vendor_hint,omitempty"`
	RTSPPort      int      `json:"rtsp_port,omitempty"`
	HTTPPort      int      `json:"http_port,omitempty"`
	SuggestedMain string   `json:"suggested_main_path,omitempty"`
	SuggestedSub  string   `json:"suggested_sub_path,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

func (s *Server) handleCameraDiscovery(w http.ResponseWriter, r *http.Request) {
	var req cameraDiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	hosts, err := discoveryHosts(req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_DISCOVERY_REQUEST", err.Error(), "")
		return
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > 3*time.Second {
		timeout = 700 * time.Millisecond
	}
	candidates := scanCameraCandidates(r.Context(), hosts, timeout)
	writeJSON(w, http.StatusOK, map[string]any{"items": candidates, "count": len(candidates), "scanned": len(hosts)})
}

func (s *Server) handleCameraTest(w http.ResponseWriter, r *http.Request, cameraID string) {
	profile, err := s.store.GetCameraProfile(r.Context(), cameraID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if profile == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "camera profile not found", "")
		return
	}
	result := map[string]any{"camera_id": cameraID, "tcp": map[string]any{}, "snapshot_ok": false}
	if profile.Host != "" {
		ports := []int{profile.RTSPPort, profile.HTTPPort, 8000}
		if ports[0] == 0 {
			ports[0] = 554
		}
		if ports[1] == 0 {
			ports[1] = 80
		}
		tcp := map[string]bool{}
		for _, port := range ports {
			if port <= 0 {
				continue
			}
			tcp[fmt.Sprintf("%d", port)] = tcpOpen(r.Context(), profile.Host, port, 900*time.Millisecond)
		}
		result["tcp"] = tcp
	}
	rtspURL, err := s.cameraRTSPURL(r.Context(), profile, r.URL.Query().Get("profile"))
	if err != nil {
		result["snapshot_error"] = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	if _, err := snapshotJPEG(r.Context(), rtspURL); err != nil {
		result["snapshot_error"] = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	result["snapshot_ok"] = true
	profile.Status = "verified"
	_ = s.store.UpsertCameraProfile(r.Context(), profile)
	writeJSON(w, http.StatusOK, result)
}

func discoveryHosts(req cameraDiscoveryRequest) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, h := range req.Hosts {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			continue
		}
		if net.ParseIP(h) == nil {
			return nil, fmt.Errorf("invalid host IP: %s", h)
		}
		seen[h] = true
		out = append(out, h)
	}
	if req.CIDR != "" {
		ips, err := hostsFromCIDR(req.CIDR, 2048)
		if err != nil {
			return nil, err
		}
		for _, h := range ips {
			if !seen[h] {
				seen[h] = true
				out = append(out, h)
			}
		}
	}
	if len(out) == 0 {
		cidrs, err := localPrivateIPv4CIDRs24()
		if err != nil {
			return nil, err
		}
		if len(cidrs) > 1 {
			cidrs = cidrs[:1]
		}
		for _, cidr := range cidrs {
			ips, err := hostsFromCIDR(cidr, 2048)
			if err != nil {
				continue
			}
			for _, h := range ips {
				if !seen[h] {
					seen[h] = true
					out = append(out, h)
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("hosts, cidr, or auto_local is required")
	}
	if len(out) > 2048 {
		return nil, fmt.Errorf("discovery is limited to 2048 hosts per request")
	}
	return out, nil
}

func localPrivateIPv4CIDRs24() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "tailscale") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || !isPrivateIPv4(ip) {
				continue
			}
			ones, bits := ipnet.Mask.Size()
			if bits != 32 || ones < 21 { // avoid accidental huge LAN sweeps; /21 = 2046 hosts max
				continue
			}
			network := ip.Mask(ipnet.Mask)
			cidr := fmt.Sprintf("%s/%d", network.String(), ones)
			if !seen[cidr] {
				seen[cidr] = true
				out = append(out, cidr)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no active private IPv4 interface found for auto discovery")
	}
	sort.Strings(out)
	return out, nil
}

func isPrivateIPv4(ip net.IP) bool {
	return ip[0] == 10 || (ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) || (ip[0] == 192 && ip[1] == 168)
}

func hostsFromCIDR(cidr string, limit int) ([]string, error) {
	ip, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("invalid cidr: %w", err)
	}
	base := ip.To4()
	if base == nil {
		return nil, fmt.Errorf("only IPv4 CIDR discovery is supported")
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("only IPv4 CIDR discovery is supported")
	}
	total := 1 << uint(32-ones)
	if total > limit+2 {
		return nil, fmt.Errorf("cidr too large; limit is %d hosts", limit)
	}
	var out []string
	for i := 1; i < total-1; i++ {
		candidate := append(net.IP(nil), base...)
		for j := 0; j < i; j++ {
			incIP(candidate)
		}
		if ipnet.Contains(candidate) {
			out = append(out, candidate.String())
		}
	}
	return out, nil
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func nextIP(ip net.IP) net.IP {
	incIP(ip)
	return ip
}

func scanCameraCandidates(ctx context.Context, hosts []string, timeout time.Duration) []cameraDiscoveryCandidate {
	ports := []int{554, 80, 443, 8000, 8080}
	jobs := make(chan string)
	results := make(chan cameraDiscoveryCandidate, len(hosts))
	var wg sync.WaitGroup
	workers := 128
	if len(hosts) < workers {
		workers = len(hosts)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for host := range jobs {
				var open []int
				for _, port := range ports {
					if tcpOpen(ctx, host, port, timeout) {
						open = append(open, port)
					}
				}
				if len(open) == 0 {
					continue
				}
				results <- candidateFromPorts(host, open)
			}
		}()
	}
	for _, h := range hosts {
		jobs <- h
	}
	close(jobs)
	wg.Wait()
	close(results)
	var out []cameraDiscoveryCandidate
	for c := range results {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

func tcpOpen(ctx context.Context, host string, port int, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func candidateFromPorts(host string, ports []int) cameraDiscoveryCandidate {
	c := cameraDiscoveryCandidate{Host: host, OpenPorts: ports, SuggestedMain: "/Streaming/Channels/101", SuggestedSub: "/Streaming/Channels/102"}
	for _, p := range ports {
		switch p {
		case 554:
			c.RTSPPort = 554
			c.LikelyCamera = true
		case 80, 443, 8080:
			if c.HTTPPort == 0 {
				c.HTTPPort = p
			}
		case 8000:
			c.VendorHint = "hikvision_candidate"
		}
	}
	if c.RTSPPort == 0 {
		c.Warnings = append(c.Warnings, "RTSP port 554 not open; may be NVR/web device or RTSP disabled")
	}
	if c.VendorHint == "" && c.HTTPPort != 0 && c.RTSPPort != 0 {
		c.VendorHint = "generic_rtsp_onvif_candidate"
	}
	return c
}
