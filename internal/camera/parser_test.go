package camera

import (
	"io"
	"log"
	"strings"
	"testing"
)

func TestParseStderr_EmitsMotion(t *testing.T) {
	input := strings.Join([]string{
		"ffmpeg version 7.0 ...",
		"[Parsed_showinfo_1 @ 0x55] n:0 pts:90000 pts_time:1.000000 pos:1234 fmt:yuv420p sar:0/1",
		"frame=  120 fps= 30 q=-1.0 size=N/A time=00:00:04.00 bitrate=N/A speed=1.0x",
		"[Parsed_showinfo_1 @ 0x55] n:1 pts:180000 pts_time:2.0 pos:5678",
	}, "\n")

	events := make(chan MotionEvent, 4)
	lg := log.New(io.Discard, "", 0)
	if err := ParseStderr(strings.NewReader(input), "cam1", events, lg); err != nil {
		t.Fatalf("ParseStderr: %v", err)
	}
	close(events)

	var got []float64
	for ev := range events {
		got = append(got, ev.PTSTime)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(got), got)
	}
	if got[0] != 1.0 || got[1] != 2.0 {
		t.Errorf("pts_time mismatch: %v", got)
	}
}

func TestParseStderr_ForwardsLogs(t *testing.T) {
	input := "ffmpeg version 7.0\nStream #0:0: Video: h264, yuv420p, 1920x1080, 15 fps\n"
	events := make(chan MotionEvent, 4)
	var sb strings.Builder
	lg := log.New(&sb, "", 0)
	if err := ParseStderr(strings.NewReader(input), "front", events, lg); err != nil {
		t.Fatalf("ParseStderr: %v", err)
	}
	if !strings.Contains(sb.String(), "front: ffmpeg version") || !strings.Contains(sb.String(), "front: Stream #0:0") {
		t.Errorf("expected forwarded logs with prefix, got: %q", sb.String())
	}
}
