package camera

import (
	"time"
)

// clipState tracks the bounds of the currently-recording clip in wall-clock
// time. A nil pointer in the supervisor means "idle" — no recording in
// progress. State transitions:
//
//	nil --(motion)--> {startedAt, endsAt, hardCap}
//	state --(motion)--> bumps endsAt forward by PostRoll, capped at hardCap
//	state --(now > endsAt)--> finalize, return to nil
//	state --(now >= hardCap && motion still active)--> finalize, immediately
//	  open a fresh state with a new pre-roll window (split)
type clipState struct {
	startedAt time.Time // wall time the clip's pre-roll window begins (motion - PreRoll)
	endsAt    time.Time // wall time the clip should close, moves forward on new motion
	hardCap   time.Time // absolute upper bound; never moves
}

// onMotion either opens a fresh clip or extends the active one's post-roll.
// Returns the state the supervisor should hold afterwards.
func onMotion(cur *clipState, at time.Time, preRoll, postRoll, maxClip time.Duration) *clipState {
	if cur == nil {
		return &clipState{
			startedAt: at.Add(-preRoll),
			endsAt:    at.Add(postRoll),
			hardCap:   at.Add(maxClip - preRoll), // total clip duration capped at maxClip
		}
	}
	newEnd := at.Add(postRoll)
	if newEnd.After(cur.hardCap) {
		newEnd = cur.hardCap
	}
	if newEnd.After(cur.endsAt) {
		cur.endsAt = newEnd
	}
	return cur
}

// shouldFinalize reports whether the supervisor should close the clip now.
// Returns (finalize?, motionStillActive?). When both are true, the supervisor
// should finalize the current clip and immediately open a successor with a
// fresh pre-roll window — that's the "split on sustained motion" behavior.
func shouldFinalize(cur *clipState, now time.Time, lastMotion time.Time, postRoll time.Duration) (bool, bool) {
	if cur == nil {
		return false, false
	}
	if now.Before(cur.endsAt) && now.Before(cur.hardCap) {
		return false, false
	}
	// We've reached endsAt or hardCap. Sustained motion = a motion event
	// observed within the last PostRoll seconds — meaning if we re-armed
	// right now we'd immediately get a fresh trigger.
	sustained := !lastMotion.IsZero() && now.Sub(lastMotion) < postRoll
	return true, sustained
}
