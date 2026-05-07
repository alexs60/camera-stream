package camera

import (
	"bufio"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MotionScore is the per-frame output of the signalstats motion-detection
// pipeline: the average per-pixel luma difference between this frame and
// the previous one (range 0-255). The Go supervisor applies the threshold.
type MotionScore struct {
	Wall  time.Time
	YAVG  float64
}

// signalstats prints (in mode=print) a block per frame:
//   frame:0    pts:0       pts_time:0
//   lavfi.signalstats.YMIN=0
//   lavfi.signalstats.YLOW=0
//   lavfi.signalstats.YAVG=2.345678
//   ... (more keys)
//
// We only need YAVG. The frame:/pts: line is silently consumed; other
// lavfi.* keys are silently consumed too so they don't flood the logger.
var (
	yavgRE      = regexp.MustCompile(`lavfi\.signalstats\.YAVG=([\d.]+)`)
	frameHdrRE  = regexp.MustCompile(`^frame:\d+\s+pts:`)
	lavfiKeyRE  = regexp.MustCompile(`^lavfi\.`)
)

// ParseStderr reads ffmpeg stderr line by line, emits a MotionScore for
// every YAVG line on `scores`, and forwards everything else to logger.
// Returns when r reaches EOF.
func ParseStderr(r io.Reader, prefix string, scores chan<- MotionScore, logger *log.Logger) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if m := yavgRE.FindStringSubmatch(line); m != nil {
			v, err := strconv.ParseFloat(m[1], 64)
			if err == nil {
				select {
				case scores <- MotionScore{Wall: time.Now(), YAVG: v}:
				default:
					logger.Printf("%s: motion score dropped (channel full)", prefix)
				}
			}
			continue
		}
		// Suppress the rest of the metadata block so it doesn't drown the log.
		if frameHdrRE.MatchString(line) || lavfiKeyRE.MatchString(strings.TrimSpace(line)) {
			continue
		}
		logger.Printf("%s: %s", prefix, line)
	}
	return sc.Err()
}
