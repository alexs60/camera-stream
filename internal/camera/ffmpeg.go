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
//  2. Scene-change null sink: select=gt(scene,sceneThreshold),showinfo. We
//     don't write the output anywhere (-f null), we just want ffmpeg to log
//     a "[Parsed_showinfo_…] pts_time:NNN" line on stderr each time the
//     scene-change score crosses the threshold. The Go supervisor parses
//     those lines and treats them as motion events.
//
// We deliberately keep this a pure function returning a []string so it can
// be unit-tested without spawning ffmpeg.
type FFmpegArgsParams struct {
	RTSPURL        string
	SegmentDir     string
	SegmentSeconds time.Duration
	SceneThreshold float64
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

		// Output 2: scene-change detector → null sink, lines on stderr
		"-map", "0:v:0",
		"-vf", fmt.Sprintf("select='gt(scene,%g)',showinfo", p.SceneThreshold),
		"-an",
		"-f", "null",
		"-",
	}
}
