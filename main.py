import cv2
import time
import os
import datetime
import subprocess
from collections import deque

# --- CONFIG ---
RTSP_URL = os.getenv("RTSP_URL")
CAMERA_IP = os.getenv("CAMERA_IP", "192.168.8.234")
RECORDING_PATH = "/recordings"
MOTION_THRESHOLD = int(os.getenv("MOTION_SENSITIVITY", "500"))
PRE_ROLL_SECONDS = 10
POST_ROLL_SECONDS = 20
# Total clip target: PRE_ROLL_SECONDS of pre-roll + live recording.
# If motion sustains past this, the writer rotates and the next clip's
# pre-roll overlaps the previous clip's tail by PRE_ROLL_SECONDS.
MAX_CLIP_SECONDS = 40
FPS_SAMPLE_SECONDS = 5

def get_cap():
    while True:
        cap = cv2.VideoCapture(RTSP_URL)
        if cap.isOpened():
            print(f"[{datetime.datetime.now()}] Connected to {CAMERA_IP}")
            return cap
        time.sleep(10)

def measure_fps(cap, sample_seconds=FPS_SAMPLE_SECONDS):
    # The camera's reported FPS is unreliable over RTSP — measure the rate
    # we actually pull frames at, since that's what the writer must match
    # for playback duration to equal capture duration.
    start = time.time()
    count = 0
    while time.time() - start < sample_seconds:
        ret, _ = cap.read()
        if not ret:
            break
        count += 1
    elapsed = time.time() - start
    return max(count / elapsed, 1.0) if elapsed > 0 else 1.0

def open_writer(filename, fps, w, h):
    # Pipe raw BGR frames to ffmpeg so the resulting MP4 is H.264/yuv420p
    # with +faststart — playable in Chrome/Safari/Firefox without re-mux.
    # opencv-python-headless ships without an H.264 encoder, hence ffmpeg.
    return subprocess.Popen(
        [
            "ffmpeg",
            "-loglevel", "error",
            "-y",
            "-f", "rawvideo",
            "-vcodec", "rawvideo",
            "-pix_fmt", "bgr24",
            "-s", f"{w}x{h}",
            "-r", f"{fps:.4f}",
            "-i", "-",
            "-c:v", "libx264",
            "-pix_fmt", "yuv420p",
            "-preset", "ultrafast",
            "-movflags", "+faststart",
            filename,
        ],
        stdin=subprocess.PIPE,
    )

def close_writer(proc):
    if proc is None:
        return
    try:
        proc.stdin.close()
    except BrokenPipeError:
        pass
    try:
        proc.wait(timeout=30)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()

def write_frame(proc, frame):
    try:
        proc.stdin.write(frame.tobytes())
    except BrokenPipeError:
        pass

def main():
    if not os.access(RECORDING_PATH, os.W_OK):
        print(f"ERROR: {RECORDING_PATH} is not writable. Check NFS mount.")
        return

    cap = get_cap()
    fps = measure_fps(cap)
    print(f"Measured capture rate: {fps:.2f} FPS")

    buffer = deque()  # (timestamp, frame), trimmed to last PRE_ROLL_SECONDS
    back_sub = cv2.createBackgroundSubtractorMOG2(history=500, varThreshold=50, detectShadows=True)

    out = None
    clip_opened_at = 0.0
    recording_until = 0.0

    while True:
        ret, frame = cap.read()
        now = time.time()
        if not ret:
            if out is not None:
                close_writer(out)
                out = None
                print("Recording saved (stream dropped).")
            cap.release()
            cap = get_cap()
            fps = measure_fps(cap)
            print(f"Re-measured capture rate: {fps:.2f} FPS")
            buffer.clear()
            continue

        # Trim buffer first; current frame goes in at the end of the loop.
        cutoff = now - PRE_ROLL_SECONDS
        while buffer and buffer[0][0] < cutoff:
            buffer.popleft()

        mask = back_sub.apply(cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY))
        motion = cv2.countNonZero(mask) > MOTION_THRESHOLD

        if motion and out is None:
            day = datetime.datetime.now().strftime("%Y-%m-%d")
            ts = datetime.datetime.now().strftime("%Y-%m-%d-%H-%M-%S")
            day_dir = f"{RECORDING_PATH}/{day}"
            os.makedirs(day_dir, exist_ok=True)
            filename = f"{day_dir}/{ts}-{CAMERA_IP}.mp4"
            print(f"Motion detected! Recording to {filename}")
            h, w, _ = frame.shape
            out = open_writer(filename, fps, w, h)
            clip_opened_at = now
            # Pre-roll: write up to PRE_ROLL_SECONDS of buffered frames.
            # If a previous clip ended recently those frames overlap with
            # its tail — that's intentional; every clip gets its own pre-roll.
            for _, f in buffer:
                write_frame(out, f)

        # Every motion frame pushes the deadline forward, so a sustained
        # event keeps the writer open until POST_ROLL_SECONDS after the
        # last motion frame.
        if motion and out is not None:
            recording_until = now + POST_ROLL_SECONDS

        # Rotate if the clip would exceed MAX_CLIP_SECONDS but motion is
        # still active. The new clip's pre-roll buffer is the last
        # PRE_ROLL_SECONDS of frames — i.e. the tail of the closing clip,
        # giving the requested overlap.
        if (
            out is not None
            and now - clip_opened_at >= MAX_CLIP_SECONDS - PRE_ROLL_SECONDS
            and now < recording_until
        ):
            close_writer(out)
            print("Clip rotated (motion still active).")
            day = datetime.datetime.now().strftime("%Y-%m-%d")
            ts = datetime.datetime.now().strftime("%Y-%m-%d-%H-%M-%S")
            day_dir = f"{RECORDING_PATH}/{day}"
            os.makedirs(day_dir, exist_ok=True)
            filename = f"{day_dir}/{ts}-{CAMERA_IP}.mp4"
            print(f"Continuing to {filename}")
            h, w, _ = frame.shape
            out = open_writer(filename, fps, w, h)
            clip_opened_at = now
            for _, f in buffer:
                write_frame(out, f)

        if out is not None:
            write_frame(out, frame)
            if now >= recording_until:
                close_writer(out)
                out = None
                print("Recording saved.")

        buffer.append((now, frame))

if __name__ == "__main__":
    main()
