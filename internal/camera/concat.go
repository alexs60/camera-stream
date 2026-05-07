package camera

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FinalizeClip remuxes the supplied .ts segments into a single MP4 at
// finalPath. We use ffmpeg's concat demuxer with -c copy (no re-encode);
// the only cost is rewriting the container, which is essentially I/O.
//
// The output is H.264 in MP4 with +faststart so Chrome on macOS/iOS plays
// it natively without download. We write to "<finalPath>.partial" and
// rename atomically on the destination filesystem so partially-written
// clips never appear in `ls`.
func FinalizeClip(ctx context.Context, segs []Segment, finalPath string) error {
	if len(segs) == 0 {
		return fmt.Errorf("no segments to finalize")
	}

	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// ffmpeg's concat demuxer needs an on-disk listing file. Place it next
	// to the output so it's on the same filesystem, then delete on exit.
	listFile, err := writeConcatList(dir, segs)
	if err != nil {
		return err
	}
	defer os.Remove(listFile)

	partial := finalPath + ".partial"
	// Clean up any leftover from a previous crashed run.
	_ = os.Remove(partial)

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-nostdin",
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listFile,
		"-map", "0:v:0",
		"-c", "copy",
		"-movflags", "+faststart",
		"-f", "mp4",
		partial,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("ffmpeg concat: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	if err := os.Rename(partial, finalPath); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("rename %s -> %s: %w", partial, finalPath, err)
	}
	return nil
}

func writeConcatList(dir string, segs []Segment) (string, error) {
	f, err := os.CreateTemp(dir, "concat-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close()
	for _, s := range segs {
		// The concat demuxer's "file" directive needs paths quoted with
		// single quotes; literal single quotes in paths are escaped as
		// '\''. We don't use single quotes in our segment paths, but
		// guard against it anyway.
		path := strings.ReplaceAll(s.Path, "'", `'\''`)
		if _, err := fmt.Fprintf(f, "file '%s'\n", path); err != nil {
			return "", err
		}
	}
	return f.Name(), nil
}
