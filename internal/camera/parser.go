package camera

import (
	"bufio"
	"io"
	"log"
	"regexp"
	"time"
)

// MotionEvent is emitted when ffmpeg's showinfo filter logs a frame that
// passed the scene-change threshold. The Wall field is filled at parse time
// (wall clock when we observed the line); we don't trust ffmpeg's pts_time
// for clip alignment because the stream pts can be reset on reconnect.
type MotionEvent struct {
	Wall    time.Time
	PTSTime float64 // raw pts_time from showinfo, useful for diagnostics
}

// showinfo emits lines like:
//
//	[Parsed_showinfo_1 @ 0x55…] n:0 pts:90000 pts_time:1.0 ...
//
// We only need pts_time; the rest is noise.
var showinfoRE = regexp.MustCompile(`Parsed_showinfo.*pts_time:([\d.]+)`)

// ParseStderr reads ffmpeg stderr line by line. Lines from the showinfo
// filter become MotionEvents on the events channel; everything else is
// forwarded to the supplied logger so users can debug the stream.
//
// Returns when r reaches EOF or an unrecoverable read error occurs. Closing
// the events channel is the caller's responsibility — multiple parsers may
// share one channel in tests.
func ParseStderr(r io.Reader, prefix string, events chan<- MotionEvent, logger *log.Logger) error {
	sc := bufio.NewScanner(r)
	// ffmpeg can emit long lines (full filter graph dumps on init).
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if m := showinfoRE.FindStringSubmatch(line); m != nil {
			pts, _ := parseFloat64(m[1])
			select {
			case events <- MotionEvent{Wall: time.Now(), PTSTime: pts}:
			default:
				// Drop the event if the supervisor is too slow. A flood of
				// scene changes shouldn't block ffmpeg's stderr drain — that
				// would back-pressure the encoder and stall the segmenter.
				logger.Printf("%s: motion event dropped (channel full)", prefix)
			}
			continue
		}
		// Forward everything else for visibility. ffmpeg's startup banner,
		// SDP info, and reconnect messages all land here.
		logger.Printf("%s: %s", prefix, line)
	}
	return sc.Err()
}

func parseFloat64(s string) (float64, error) {
	var f float64
	var sign float64 = 1
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		if s[i] == '-' {
			sign = -1
		}
		i++
	}
	seenDigit := false
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		f = f*10 + float64(s[i]-'0')
		seenDigit = true
	}
	if i < len(s) && s[i] == '.' {
		i++
		div := 10.0
		for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
			f += float64(s[i]-'0') / div
			div *= 10
			seenDigit = true
		}
	}
	if !seenDigit {
		return 0, errParseNumber
	}
	return sign * f, nil
}

var errParseNumber = &parseErr{"not a number"}

type parseErr struct{ msg string }

func (e *parseErr) Error() string { return e.msg }
