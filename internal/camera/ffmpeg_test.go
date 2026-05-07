package camera

import (
	"strings"
	"testing"
	"time"
)

func TestFFmpegArgs_Shape(t *testing.T) {
	args := FFmpegArgs(FFmpegArgsParams{
		RTSPURL:        "rtsp://u:p@1.2.3.4/x",
		SegmentDir:     "/tmp/seg/front",
		SegmentSeconds: 2 * time.Second,
	})

	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-rtsp_transport tcp",
		"-i rtsp://u:p@1.2.3.4/x",
		"-c copy",
		"-f segment",
		"/tmp/seg/front/seg_%05d.ts",
		"fps=2,scale=160:-2,format=gray,tblend=all_mode=difference,signalstats,metadata=mode=print:file=/dev/stderr",
		"-f null",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q\nfull: %s", want, joined)
		}
	}

	for _, dontWant := range []string{"-stimeout", "-reconnect ", "-rw_timeout", "select='gt(scene"} {
		if strings.Contains(joined, dontWant) {
			t.Errorf("argv should not contain %q (was removed): %s", dontWant, joined)
		}
	}

	if args[0] != "ffmpeg" {
		t.Errorf("argv[0] = %q, want ffmpeg", args[0])
	}
}

func TestFFmpegArgs_Defaults(t *testing.T) {
	args := FFmpegArgs(FFmpegArgsParams{
		RTSPURL:    "rtsp://x",
		SegmentDir: "/d",
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-segment_time 2.000") {
		t.Errorf("expected default segment_time 2.000 in argv: %s", joined)
	}
}
