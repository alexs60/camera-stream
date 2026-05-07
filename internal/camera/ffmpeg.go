package camera

import (
	"fmt"
	"path/filepath"
	"time"
)

// FFmpegArgs builds the argv for the long-running ffmpeg per camera.
//
// One RTSP input fans out to two outputs in a single connection:
//
//  1. Segment ring buffer: -c copy MPEG-TS segments of ~segmentSeconds each,
//     written to segmentDir/seg_%05d.ts. No re-encode — preserves source
//     codec, bitrate, and fps. We rely on the supervisor goroutine to prune
//     old segments so disk usage stays bounded.
//
//  2. Motion-detection null sink: decode at 2 fps, downscale to 160px wide,
//     convert to grayscale, frame-difference vs the previous frame, then
//     signalstats writes the average difference (YAVG) as metadata. The
//     metadata filter prints it to stderr, where the Go supervisor reads
//     and thresholds it.
//
//     Why frame-difference instead of select=gt(scene,X): scene detects
//     *transitions* (fires once when motion starts, then nothing while it
//     continues). Frame-difference produces a score on every sampled frame
//     while motion is happening, which is what we want for post-roll
//     extension.
//
// We deliberately keep this a pure function returning a []string so it can
// be unit-tested without spawning ffmpeg.
type FFmpegArgsParams struct {
	RTSPURL        string
	SegmentDir     string
	SegmentSeconds time.Duration
}

func FFmpegArgs(p FFmpegArgsParams) []string {
	if p.SegmentSeconds <= 0 {
		p.SegmentSeconds = 2 * time.Second
	}

	segPattern := filepath.Join(p.SegmentDir, "seg_%05d.ts")

	return []string{
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "info", // info is needed for showinfo lines
		"-nostdin",

		// RTSP input. We don't pass any I/O-timeout flag here: -stimeout
		// was removed in newer ffmpeg, -rw_timeout is rejected by the
		// rtsp demuxer ("Option rw_timeout not found"), and -timeout has
		// ambiguous semantics across formats. Connection refusal is
		// detected at the TCP layer in ~hundreds of ms; silent hangs
		// (broken connection that never RSTs) are caught by the
		// supervisor's stall watchdog.
		"-rtsp_transport", "tcp",
		"-i", p.RTSPURL,

		// Output 1: segment ring buffer (codec copy)
		"-map", "0:v:0",
		"-c", "copy",
		"-f", "segment",
		"-segment_time", fmt.Sprintf("%.3f", p.SegmentSeconds.Seconds()),
		"-segment_format", "mpegts",
		"-reset_timestamps", "1",
		"-strftime", "0",
		segPattern,

		// Output 2: motion detector → null sink. metadata=print writes
		// "lavfi.signalstats.YAVG=N.NNNNNN" lines to /dev/stderr; the Go
		// supervisor parses those and applies the threshold.
		"-map", "0:v:0",
		"-vf", "fps=2,scale=160:-2,format=gray,tblend=all_mode=difference," +
			"signalstats,metadata=mode=print:file=/dev/stderr",
		"-an",
		"-f", "null",
		"-",
	}
}
