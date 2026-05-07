package camera

import (
	"testing"
	"time"
)

func mkSegs(base time.Time, offsets ...time.Duration) []Segment {
	out := make([]Segment, len(offsets))
	for i, off := range offsets {
		out[i] = Segment{Path: "/x", MTime: base.Add(off)}
	}
	return out
}

func TestSegmentsCovering_Basic(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	segs := mkSegs(base, 0, 2*time.Second, 4*time.Second, 6*time.Second, 8*time.Second)

	got := SegmentsCovering(segs, base.Add(0), base.Add(8*time.Second), true)
	if len(got) != 5 {
		t.Errorf("expected all 5 segments, got %d", len(got))
	}

	got = SegmentsCovering(segs, base.Add(1*time.Second), base.Add(1*time.Second), true)
	if len(got) < 2 {
		t.Errorf("expected at least 2 segments to cover t+1..t+1, got %d", len(got))
	}
	if got[0].MTime != base {
		t.Errorf("first segment should be the t+0 segment (covers start), got %v", got[0].MTime.Sub(base))
	}
}

func TestSegmentsCovering_BufferAheadFlag(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	segs := mkSegs(base, 0, 2*time.Second, 4*time.Second, 6*time.Second)

	// Salvage: end at t+3s, includeBufferAhead=false -> should NOT include
	// the segment at t+4s (ffmpeg may have been mid-writing it).
	got := SegmentsCovering(segs, base, base.Add(3*time.Second), false)
	if len(got) != 2 {
		t.Errorf("salvage path: expected 2 segments (t+0, t+2), got %d", len(got))
	}

	// Normal close at the same range with includeBufferAhead=true: pulls
	// in the segment at t+4s.
	got = SegmentsCovering(segs, base, base.Add(3*time.Second), true)
	if len(got) != 3 {
		t.Errorf("normal path: expected 3 segments (t+0, t+2, t+4), got %d", len(got))
	}
}

func TestSegmentsCovering_Empty(t *testing.T) {
	if got := SegmentsCovering(nil, time.Now(), time.Now(), true); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}
