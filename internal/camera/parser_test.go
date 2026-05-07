package camera

import (
	"io"
	"log"
	"strings"
	"testing"
)

func TestParseStderr_EmitsScores(t *testing.T) {
	input := strings.Join([]string{
		"ffmpeg version 7.0 ...",
		"frame:0    pts:0       pts_time:0",
		"lavfi.signalstats.YMIN=0",
		"lavfi.signalstats.YAVG=2.345678",
		"lavfi.signalstats.YMAX=42",
		"frame:1    pts:90000   pts_time:1.0",
		"lavfi.signalstats.YAVG=10.0",
	}, "\n")

	scores := make(chan MotionScore, 4)
	lg := log.New(io.Discard, "", 0)
	if err := ParseStderr(strings.NewReader(input), "cam1", scores, lg); err != nil {
		t.Fatalf("ParseStderr: %v", err)
	}
	close(scores)

	var got []float64
	for s := range scores {
		got = append(got, s.YAVG)
	}
	if len(got) != 2 || got[0] != 2.345678 || got[1] != 10.0 {
		t.Errorf("scores: %v", got)
	}
}

func TestParseStderr_SuppressesMetadataNoise(t *testing.T) {
	input := strings.Join([]string{
		"ffmpeg version 7.0",
		"Stream #0:0: Video: h264, 1920x1080, 15 fps",
		"frame:0    pts:0       pts_time:0",
		"lavfi.signalstats.YMIN=0",
		"lavfi.signalstats.YAVG=1.0",
	}, "\n")

	scores := make(chan MotionScore, 4)
	var sb strings.Builder
	lg := log.New(&sb, "", 0)
	if err := ParseStderr(strings.NewReader(input), "front", scores, lg); err != nil {
		t.Fatalf("ParseStderr: %v", err)
	}
	got := sb.String()

	// real ffmpeg lines should be forwarded
	if !strings.Contains(got, "front: ffmpeg version") || !strings.Contains(got, "front: Stream #0:0") {
		t.Errorf("expected real log lines forwarded, got: %q", got)
	}
	// metadata-block lines should NOT be forwarded
	for _, ban := range []string{"frame:0", "lavfi.signalstats.YMIN", "lavfi.signalstats.YAVG"} {
		if strings.Contains(got, ban) {
			t.Errorf("expected %q to be suppressed, got: %q", ban, got)
		}
	}
}
