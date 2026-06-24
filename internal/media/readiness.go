package media

import "os/exec"

var RequiredTools = []string{"gst-launch-1.0"}

var RequiredGStreamerPlugins = []string{
	"rtspsrc",
	"rtph264depay",
	"h264parse",
	"avdec_h264",
	"videoconvert",
	"videoscale",
	"videorate",
	"jpegenc",
	"multipartmux",
	"fdsink",
	"mpegtsmux",
	"hlssink",
}

type RuntimeReadiness struct {
	Tools   map[string]bool `json:"tools"`
	Plugins map[string]bool `json:"plugins"`
	Ready   bool            `json:"ready"`
}

type RuntimeProbe interface {
	Available(name string) bool
}

type RuntimeProbeFunc func(name string) bool

func (f RuntimeProbeFunc) Available(name string) bool { return f(name) }

type ExecRuntimeProbe struct{}

func (ExecRuntimeProbe) Available(name string) bool {
	if name == "gst-launch-1.0" {
		_, err := exec.LookPath(name)
		return err == nil
	}
	return exec.Command("gst-inspect-1.0", name).Run() == nil
}

func CheckRuntime(probe RuntimeProbe) RuntimeReadiness {
	out := RuntimeReadiness{Tools: map[string]bool{}, Plugins: map[string]bool{}, Ready: true}
	for _, tool := range RequiredTools {
		ok := probe.Available(tool)
		out.Tools[tool] = ok
		if !ok {
			out.Ready = false
		}
	}
	for _, plugin := range RequiredGStreamerPlugins {
		ok := probe.Available(plugin)
		out.Plugins[plugin] = ok
		if !ok {
			out.Ready = false
		}
	}
	return out
}
