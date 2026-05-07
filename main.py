import cv2
import time
import os
import datetime
import subprocess
import threading
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
# Process and encode at this fixed rate, regardless of the camera's native
# FPS. Writer fps == this rate, so playback duration always tracks wall-clock
# duration. Lower this if libx264 can't keep up (files would otherwise play
# in slow motion). Two containers on a modest CPU should each run 10-15 fps.
TARGET_FPS = float(os.getenv("TARGET_FPS", "15"))
FRAME_INTERVAL = 1.0 / TARGET_FPS


class FrameGrabber:
    # OpenCV/FFmpeg's RTSP backend queues unread frames; when the consumer
    # falls behind (CPU contention from a second container, encoder load),
    # cap.read() returns increasingly stale frames and the recording lags
    # real time. This thread drains the queue continuously and exposes only
    # the most recent frame; the main loop reads it at TARGET_FPS and the
    # in-between camera frames are dropped.
    def __init__(self, cap):
        self.cap = cap
        self._frame = None
        self._ts = 0.0
        self._lock = threading.Lock()
        self._stop = threading.Event()
        self._alive = True
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    def _run(self):
        while not self._stop.is_set():
            ret, frame = self.cap.read()
            if not ret:
                self._alive = False
                return
            with self._lock:
                self._frame = frame
                self._ts = time.time()

    def read(self):
        with self._lock:
            return self._frame, self._ts

    def alive(self):
        return self._alive

    def stop(self):
        self._stop.set()
        self._thread.join(timeout=2)


def get_cap():
    while True:
        cap = cv2.VideoCapture(RTSP_URL)
        if cap.isOpened():
            print(f"[{datetime.datetime.now()}] Connected to {CAMERA_IP}")
            return cap
        time.sleep(10)


def open_writer(filename, w, h):
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
            "-r", f"{TARGET_FPS:.4f}",
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


def open_clip(buffer, w, h):
    day = datetime.datetime.now().strftime("%Y-%m-%d")
    ts = datetime.datetime.now().strftime("%Y-%m-%d-%H-%M-%S")
    day_dir = f"{RECORDING_PATH}/{day}"
    os.makedirs(day_dir, exist_ok=True)
    filename = f"{day_dir}/{ts}-{CAMERA_IP}.mp4"
    print(f"Recording to {filename}")
    out = open_writer(filename, w, h)
    for _, f in buffer:
        write_frame(out, f)
    return out


def main():
    if not os.access(RECORDING_PATH, os.W_OK):
        print(f"ERROR: {RECORDING_PATH} is not writable. Check NFS mount.")
        return

    print(f"Target processing/encoding rate: {TARGET_FPS:.1f} FPS")

    cap = get_cap()
    grabber = FrameGrabber(cap)

    buffer = deque()  # (timestamp, frame), trimmed to last PRE_ROLL_SECONDS
    back_sub = cv2.createBackgroundSubtractorMOG2(history=500, varThreshold=50, detectShadows=True)

    out = None
    clip_opened_at = 0.0
    recording_until = 0.0
    last_tick = 0.0

    while True:
        if not grabber.alive():
            if out is not None:
                close_writer(out)
                out = None
                print("Recording saved (stream dropped).")
            grabber.stop()
            cap.release()
            cap = get_cap()
            grabber = FrameGrabber(cap)
            buffer.clear()
            last_tick = 0.0
            continue

        # Pace the loop at TARGET_FPS. If processing took longer than the
        # interval, sleep_for is negative and we run flat-out (slow-motion
        # files in that case, but never wrong-speed).
        now = time.time()
        sleep_for = (last_tick + FRAME_INTERVAL) - now
        if sleep_for > 0.001:
            time.sleep(sleep_for)
        last_tick = time.time()
        now = last_tick

        frame, _ = grabber.read()
        if frame is None:
            continue

        cutoff = now - PRE_ROLL_SECONDS
        while buffer and buffer[0][0] < cutoff:
            buffer.popleft()

        mask = back_sub.apply(cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY))
        motion = cv2.countNonZero(mask) > MOTION_THRESHOLD

        if motion and out is None:
            print("Motion detected!")
            h, w, _ = frame.shape
            out = open_clip(buffer, w, h)
            clip_opened_at = now

        if motion and out is not None:
            recording_until = now + POST_ROLL_SECONDS

        if (
            out is not None
            and now - clip_opened_at >= MAX_CLIP_SECONDS - PRE_ROLL_SECONDS
            and now < recording_until
        ):
            close_writer(out)
            print("Clip rotated (motion still active).")
            h, w, _ = frame.shape
            out = open_clip(buffer, w, h)
            clip_opened_at = now

        if out is not None:
            write_frame(out, frame)
            if now >= recording_until:
                close_writer(out)
                out = None
                print("Recording saved.")

        buffer.append((now, frame))


if __name__ == "__main__":
    main()
