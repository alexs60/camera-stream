package camera

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"camera-stream/internal/config"
)

// Supervisor manages one camera end-to-end: spawns and respawns ffmpeg,
// listens for motion events, runs the clip state machine, and finalizes
// clips by remuxing TS segments into MP4.
type Supervisor struct {
	Cfg    *config.Config
	Cam    config.Camera
	Logger *log.Logger

	segDir string // tmpfs/<name>
	outDir string // recordings/<name>

	cur        *clipState
	lastMotion time.Time

	// finalizeWG tracks in-flight FinalizeClip calls so Run() can wait for
	// them on shutdown — otherwise SIGTERM would orphan a half-written
	// clip.
	finalizeWG sync.WaitGroup
}

// Run is the supervisor's main loop. It returns when ctx is cancelled or
// when ffmpeg has failed unrecoverably (we currently always retry).
func (s *Supervisor) Run(ctx context.Context) error {
	s.segDir = filepath.Join(s.Cfg.TmpfsPath, s.Cam.Name)
	s.outDir = filepath.Join(s.Cfg.RecordingPath, s.Cam.Name)

	if err := os.MkdirAll(s.segDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", s.segDir, err)
	}
	if err := os.MkdirAll(s.outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", s.outDir, err)
	}

	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			break
		}
		// If ffmpeg ran successfully for a while before dying, treat this
		// as a transient blip and reset the backoff. Otherwise a camera
		// that flaps once an hour would sit at the 30s ceiling forever.
		if time.Since(started) > 30*time.Second {
			backoff = time.Second
		}
		s.Logger.Printf("ffmpeg exited after %s: %v; restarting in %s",
			time.Since(started).Truncate(time.Millisecond), err, backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}

	// runOnce's defer already finalized the in-flight clip and waited for
	// finalize goroutines on its way out. Nothing left to do.
	s.Logger.Printf("supervisor stopped")
	return nil
}

// runOnce launches one ffmpeg invocation and runs the state machine until
// the process exits or ctx is cancelled. Returns the reason for exit.
//
// On exit, any in-progress clip is finalized truncated at the disconnect
// moment so we don't lose pre-disconnect footage. The segment dir is
// wiped before starting and after finalizing — leftover .ts files from
// a previous ffmpeg invocation have stale mtimes that would confuse
// SegmentsCovering on the next motion event after a reconnect.
func (s *Supervisor) runOnce(ctx context.Context) error {
	s.wipeSegmentDir()
	defer func() {
		// Save whatever we had recorded before ffmpeg died.
		if s.cur != nil {
			if now := time.Now(); now.Before(s.cur.endsAt) {
				s.cur.endsAt = now
			}
			s.spawnFinalize(s.cur)
			s.cur = nil
		}
		// Wait for any in-flight finalize to drain its segments before we
		// nuke the directory; otherwise concat would reference deleted files.
		s.finalizeWG.Wait()
		s.wipeSegmentDir()
	}()

	args := FFmpegArgs(FFmpegArgsParams{
		RTSPURL:        s.Cam.RTSP,
		SegmentDir:     s.segDir,
		SegmentSeconds: s.Cfg.SegmentDuration,
	})

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	cmd.Stdout = nil

	s.Logger.Printf("spawning ffmpeg")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	scores := make(chan MotionScore, 256)
	parseDone := make(chan error, 1)
	go func() {
		parseDone <- ParseStderr(stderr, s.Cam.Name, scores, s.Logger)
		close(scores)
	}()

	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	pruneTick := time.NewTicker(10 * time.Second)
	defer pruneTick.Stop()
	stallTick := time.NewTicker(5 * time.Second)
	defer stallTick.Stop()
	heartbeatTick := time.NewTicker(60 * time.Second)
	defer heartbeatTick.Stop()

	// Stall watchdog: if ffmpeg connects but stops emitting segments
	// (silent network drop, decoder lockup, …) ffmpeg itself won't
	// notice for up to 2 hours under default TCP keepalive. Kill it
	// after stallTimeout of no new segments and let the supervisor
	// respawn.
	const stallTimeout = 30 * time.Second
	startedAt := time.Now()
	lastSegMTime := time.Time{}

	// Per-period stats for the motion heartbeat.
	var (
		periodMaxScore float64
		periodFrames   int
		periodTriggers int
	)

	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
			return ctx.Err()

		case score, ok := <-scores:
			if !ok {
				// stderr closed -> ffmpeg is done; reap and report
				err := cmd.Wait()
				if perr := <-parseDone; perr != nil && err == nil {
					err = perr
				}
				if err == nil {
					err = errors.New("ffmpeg exited with status 0")
				}
				return err
			}
			periodFrames++
			if score.YAVG > periodMaxScore {
				periodMaxScore = score.YAVG
			}
			if score.YAVG >= s.Cfg.MotionThreshold {
				periodTriggers++
				s.handleMotion(score.Wall)
			}

		case now := <-tick.C:
			s.handleTick(now)

		case <-pruneTick.C:
			s.prune()

		case <-heartbeatTick.C:
			s.Logger.Printf("motion stats: max_yavg=%.2f over %d frames, %d triggers (threshold=%.2f)",
				periodMaxScore, periodFrames, periodTriggers, s.Cfg.MotionThreshold)
			periodMaxScore = 0
			periodFrames = 0
			periodTriggers = 0

		case now := <-stallTick.C:
			segs, err := ListSegments(s.segDir)
			if err != nil || len(segs) == 0 {
				// No segments yet. Tolerate this for a while after spawn
				// (RTSP handshake can take a few seconds), but if we never
				// see one, kill ffmpeg so the supervisor retries.
				if now.Sub(startedAt) > stallTimeout {
					s.Logger.Printf("stall: no segments produced in %s, killing ffmpeg", stallTimeout)
					_ = cmd.Process.Kill()
				}
				continue
			}
			latest := segs[len(segs)-1].MTime
			if !latest.Equal(lastSegMTime) {
				lastSegMTime = latest
				continue
			}
			if now.Sub(latest) > stallTimeout {
				s.Logger.Printf("stall: no new segment for %s (last at %s), killing ffmpeg",
					now.Sub(latest).Truncate(time.Second), latest.Format(time.RFC3339))
				_ = cmd.Process.Kill()
			}
		}
	}
}

func (s *Supervisor) handleMotion(at time.Time) {
	prev := s.cur
	s.cur = onMotion(s.cur, at, s.Cfg.PreRoll, s.Cfg.PostRoll, s.Cfg.MaxClipDuration)
	s.lastMotion = at
	if prev == nil {
		s.Logger.Printf("motion: opening clip (start=%s, end=%s, hardCap=%s)",
			s.cur.startedAt.Format(time.RFC3339), s.cur.endsAt.Format(time.RFC3339), s.cur.hardCap.Format(time.RFC3339))
	}
}

func (s *Supervisor) handleTick(now time.Time) {
	fin, sustained := shouldFinalize(s.cur, now, s.lastMotion, s.Cfg.PostRoll)
	if !fin {
		return
	}
	clip := s.cur
	s.cur = nil
	s.spawnFinalize(clip)

	if sustained {
		// Open a fresh clip immediately with a new pre-roll window — the
		// "split on sustained motion" behavior. Treat `now` as the trigger.
		s.cur = onMotion(nil, now, s.Cfg.PreRoll, s.Cfg.PostRoll, s.Cfg.MaxClipDuration)
		s.Logger.Printf("split: motion still active, starting new clip")
	}
}

func (s *Supervisor) spawnFinalize(clip *clipState) {
	s.finalizeWG.Add(1)
	go func() {
		defer s.finalizeWG.Done()
		// Wait briefly so the segment that closes shortly after endsAt
		// is actually flushed to disk before we list the directory.
		time.Sleep(s.Cfg.SegmentDuration + 500*time.Millisecond)

		segs, err := ListSegments(s.segDir)
		if err != nil {
			s.Logger.Printf("finalize: list segments: %v", err)
			return
		}
		picked := SegmentsCovering(segs, clip.startedAt, clip.endsAt)
		if len(picked) == 0 {
			s.Logger.Printf("finalize: no segments cover %s..%s; dropping clip",
				clip.startedAt.Format(time.RFC3339), clip.endsAt.Format(time.RFC3339))
			return
		}

		day := clip.startedAt.Format("2006-01-02")
		ts := clip.startedAt.Format("2006-01-02-15-04-05")
		dayDir := filepath.Join(s.outDir, day)
		filename := fmt.Sprintf("%s-%s.mp4", ts, s.Cam.IP)
		final := filepath.Join(dayDir, filename)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := FinalizeClip(ctx, picked, final); err != nil {
			s.Logger.Printf("finalize: %v", err)
			return
		}
		s.Logger.Printf("clip saved: %s (%d segments)", final, len(picked))
	}()
}

// prune deletes segments older than what any future or in-flight clip
// could still need. The retention window has to be at least PreRoll
// (so a motion event happening *now* still has its 5s of history) plus
// a safety margin of two segment durations.
func (s *Supervisor) prune() {
	segs, err := ListSegments(s.segDir)
	if err != nil {
		s.Logger.Printf("prune: list: %v", err)
		return
	}
	keepBefore := time.Now().Add(-(s.Cfg.PreRoll + 2*s.Cfg.SegmentDuration))
	// If a clip is in flight, never delete segments it still needs.
	if s.cur != nil && s.cur.startedAt.Before(keepBefore) {
		keepBefore = s.cur.startedAt
	}
	if n := PruneSegments(segs, keepBefore); n > 0 {
		s.Logger.Printf("prune: removed %d segments older than %s", n, keepBefore.Format(time.RFC3339))
	}
}

func (s *Supervisor) wipeSegmentDir() {
	entries, err := os.ReadDir(s.segDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(s.segDir, e.Name()))
	}
}
