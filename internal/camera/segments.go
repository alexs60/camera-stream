package camera

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Segment is one .ts file produced by ffmpeg's segment muxer.
type Segment struct {
	Path  string
	MTime time.Time // close-time of the segment as observed from the filesystem
}

// ListSegments returns the .ts files in dir sorted ascending by mtime. The
// segment muxer names them seg_00000.ts, seg_00001.ts, … so name order and
// mtime order coincide; we sort by mtime explicitly to be robust against
// clock skew, manual cleanup, or reconnects that restart numbering.
func ListSegments(dir string) ([]Segment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Segment, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Segment{
			Path:  filepath.Join(dir, e.Name()),
			MTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MTime.Before(out[j].MTime) })
	return out, nil
}

// SegmentsCovering returns the segments needed to satisfy the [start, end]
// wall-clock range. It includes the segment whose mtime is the latest one
// at-or-before start (because that segment's *content* spans up to its
// close-time), and every segment with mtime <= end.
//
// includeBufferAhead controls whether to also include the segment with
// the smallest mtime > end. Pass true for normal clip close (the next
// segment's content begins shortly after our end timestamp and contains
// the last frames of the post-roll). Pass false for salvage clips where
// ffmpeg has died — the next segment may have been mid-write at the
// moment of death and is likely truncated/corrupt.
func SegmentsCovering(segs []Segment, start, end time.Time, includeBufferAhead bool) []Segment {
	if len(segs) == 0 {
		return nil
	}
	first := 0
	for i, s := range segs {
		if s.MTime.After(start) {
			break
		}
		first = i
	}
	last := first
	for i := first; i < len(segs); i++ {
		if segs[i].MTime.After(end) {
			break
		}
		last = i
	}
	if includeBufferAhead && last+1 < len(segs) {
		last++
	}
	return segs[first : last+1]
}

// PruneSegments deletes segments older than `keepBefore`. Returns the number
// of files removed. Errors deleting individual files are swallowed — a
// transient EBUSY/ENOENT shouldn't crash the supervisor; the next prune
// pass will retry.
func PruneSegments(segs []Segment, keepBefore time.Time) int {
	n := 0
	for _, s := range segs {
		if s.MTime.Before(keepBefore) {
			if err := os.Remove(s.Path); err == nil {
				n++
			}
		}
	}
	return n
}
