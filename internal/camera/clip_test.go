package camera

import (
	"testing"
	"time"
)

func TestOnMotion_OpensClip(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cur := onMotion(nil, t0, 5*time.Second, 25*time.Second, 2*time.Minute)
	if cur == nil {
		t.Fatal("expected a clip to be opened")
	}
	if cur.startedAt != t0.Add(-5*time.Second) {
		t.Errorf("startedAt: %v", cur.startedAt.Sub(t0))
	}
	if cur.endsAt != t0.Add(25*time.Second) {
		t.Errorf("endsAt: %v", cur.endsAt.Sub(t0))
	}
	if cur.hardCap != t0.Add(2*time.Minute-5*time.Second) {
		t.Errorf("hardCap: %v (want maxClip - preRoll past t0)", cur.hardCap.Sub(t0))
	}
}

func TestOnMotion_ExtendsPostRoll(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cur := onMotion(nil, t0, 5*time.Second, 25*time.Second, 2*time.Minute)
	// motion 10s later should push endsAt to t0+10+25 = t0+35
	cur = onMotion(cur, t0.Add(10*time.Second), 5*time.Second, 25*time.Second, 2*time.Minute)
	if cur.endsAt != t0.Add(35*time.Second) {
		t.Errorf("endsAt after extension: %v, want t0+35s", cur.endsAt.Sub(t0))
	}
}

func TestOnMotion_CapsAtHardCap(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	preRoll, postRoll, maxClip := 5*time.Second, 25*time.Second, 30*time.Second
	cur := onMotion(nil, t0, preRoll, postRoll, maxClip)
	// motion 20s in should *try* to push end to t0+45, but maxClip=30
	// + preRoll=5 means hardCap = t0+25, so endsAt should clamp at t0+25.
	cur = onMotion(cur, t0.Add(20*time.Second), preRoll, postRoll, maxClip)
	if cur.endsAt != cur.hardCap {
		t.Errorf("endsAt should clamp to hardCap; got %v vs cap %v", cur.endsAt.Sub(t0), cur.hardCap.Sub(t0))
	}
}

func TestShouldFinalize(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cur := &clipState{
		startedAt: t0,
		endsAt:    t0.Add(10 * time.Second),
		hardCap:   t0.Add(1 * time.Minute),
	}

	if fin, _ := shouldFinalize(cur, t0.Add(5*time.Second), t0.Add(5*time.Second), 25*time.Second); fin {
		t.Error("not yet at endsAt -> shouldn't finalize")
	}
	if fin, sus := shouldFinalize(cur, t0.Add(11*time.Second), t0.Add(8*time.Second), 25*time.Second); !fin || !sus {
		t.Errorf("past endsAt with recent motion -> finalize+sustained; got fin=%v sus=%v", fin, sus)
	}
	if fin, sus := shouldFinalize(cur, t0.Add(40*time.Second), t0.Add(8*time.Second), 25*time.Second); !fin || sus {
		t.Errorf("past endsAt, motion older than postRoll -> finalize, not sustained; got fin=%v sus=%v", fin, sus)
	}
}
