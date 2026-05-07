package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Camera struct {
	Name string
	RTSP string
	IP   string
}

type Config struct {
	RecordingPath   string
	TmpfsPath       string
	SceneThreshold  float64
	PreRoll         time.Duration
	PostRoll        time.Duration
	MaxClipDuration time.Duration
	SegmentDuration time.Duration
	Cameras         []Camera
}

// Load parses configuration from a list of "KEY=VALUE" strings (typically
// os.Environ). Returning an error rather than reading the environment directly
// keeps the loader pure and trivially testable.
func Load(env []string) (*Config, error) {
	m := envMap(env)

	cfg := &Config{
		RecordingPath:   getOr(m, "RECORDING_PATH", "/recordings"),
		TmpfsPath:       getOr(m, "TMPFS_PATH", "/tmp/cam-segments"),
		PreRoll:         5 * time.Second,
		PostRoll:        25 * time.Second,
		MaxClipDuration: 2 * time.Minute,
		SegmentDuration: 2 * time.Second,
	}

	thr, err := parseFloat(getOr(m, "SCENE_THRESHOLD", "0.05"))
	if err != nil {
		return nil, fmt.Errorf("SCENE_THRESHOLD: %w", err)
	}
	if thr <= 0 || thr >= 1 {
		return nil, fmt.Errorf("SCENE_THRESHOLD must be in (0,1), got %v", thr)
	}
	cfg.SceneThreshold = thr

	cams, err := parseCameras(m)
	if err != nil {
		return nil, err
	}
	if len(cams) == 0 {
		return nil, errors.New("no cameras configured: set CAMERA_1_NAME / CAMERA_1_RTSP / CAMERA_1_IP")
	}
	cfg.Cameras = cams

	return cfg, nil
}

// parseCameras scans CAMERA_<N>_* triples starting from N=1 and stops at the
// first N that has none of NAME/RTSP/IP set. A partially-specified slot
// (e.g. NAME but no RTSP) is treated as a configuration error rather than
// silently skipped.
func parseCameras(m map[string]string) ([]Camera, error) {
	var out []Camera
	seenNames := map[string]bool{}
	for n := 1; ; n++ {
		prefix := fmt.Sprintf("CAMERA_%d_", n)
		name := m[prefix+"NAME"]
		rtsp := m[prefix+"RTSP"]
		ip := m[prefix+"IP"]
		if name == "" && rtsp == "" && ip == "" {
			break
		}
		if name == "" || rtsp == "" || ip == "" {
			return nil, fmt.Errorf("camera %d: NAME, RTSP and IP are all required (got name=%q rtsp=%q ip=%q)", n, name, rtsp, ip)
		}
		if !validName(name) {
			return nil, fmt.Errorf("camera %d: NAME %q must be a safe folder name (letters, digits, dash, underscore)", n, name)
		}
		if seenNames[name] {
			return nil, fmt.Errorf("camera %d: duplicate NAME %q", n, name)
		}
		seenNames[name] = true
		out = append(out, Camera{Name: name, RTSP: rtsp, IP: ip})
	}
	return out, nil
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		i := strings.IndexByte(e, '=')
		if i <= 0 {
			continue
		}
		m[e[:i]] = e[i+1:]
	}
	return m
}

func getOr(m map[string]string, k, def string) string {
	if v, ok := m[k]; ok && v != "" {
		return v
	}
	return def
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
