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
	// segments closing at t+0, t+2, t+4, t+6, t+8 seconds
	segs := mkSegs(base, 0, 2*time.Second, 4*time.Second, 6*time.Second, 8*time.Second)

	// motion at t+5: pre-roll 5s -> start = t+0; post-roll 25s -> end = t+30
	// (capped to what we have available — the test only seeds up to t+8s)
	got := SegmentsCovering(segs, base.Add(0), base.Add(8*time.Second))
	if len(got) != 5 {
		t.Errorf("expected all 5 segments, got %d", len(got))
	}

	// pre-roll window only: start=t+1, end=t+1 should pull seg @t+0 (its
	// content covers t+0..t+2) plus the next segment as buffer.
	got = SegmentsCovering(segs, base.Add(1*time.Second), base.Add(1*time.Second))
	if len(got) < 2 {
		t.Errorf("expected at least 2 segments to cover t+1..t+1, got %d", len(got))
	}
	if got[0].MTime != base {
		t.Errorf("first segment should be the t+0 segment (covers start), got %v", got[0].MTime.Sub(base))
	}
}

func TestSegmentsCovering_Empty(t *testing.T) {
	got := SegmentsCovering(nil, time.Now(), time.Now())
	if got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}
