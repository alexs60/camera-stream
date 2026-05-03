import cv2
import time
import os
import datetime
from collections import deque

# --- CONFIG ---
RTSP_URL = os.getenv("RTSP_URL")
CAMERA_IP = os.getenv("CAMERA_IP", "192.168.8.234")
RECORDING_PATH = "/recordings"
MOTION_THRESHOLD = int(os.getenv("MOTION_SENSITIVITY", "500"))
PRE_ROLL_SECONDS = 10
POST_ROLL_SECONDS = 20
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
    recording_until = 0.0
    last_written_ts = 0.0  # timestamp of last frame written to any file

    while True:
        ret, frame = cap.read()
        now = time.time()
        if not ret:
            if out is not None:
                out.release()
                out = None
                print("Recording saved (stream dropped).")
            cap.release()
            cap = get_cap()
            fps = measure_fps(cap)
            print(f"Re-measured capture rate: {fps:.2f} FPS")
            buffer.clear()
            continue

        buffer.append((now, frame))
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
            fourcc = cv2.VideoWriter_fourcc(*'mp4v')
            h, w, _ = frame.shape
            out = cv2.VideoWriter(filename, fourcc, fps, (w, h))
            # Only write pre-roll frames newer than the last frame we wrote
            # to a previous file — avoids overlap when motion is continuous
            # across back-to-back clips.
            for ts_f, f in buffer:
                if ts_f > last_written_ts:
                    out.write(f)
                    last_written_ts = ts_f
            # Fixed 30s clip = pre-roll already written + POST_ROLL_SECONDS more.
            # If motion continues past this deadline, the next iteration will
            # see out=None and open a new file (skipping the now-redundant pre-roll).
            recording_until = now + POST_ROLL_SECONDS

        if out is not None:
            out.write(frame)
            last_written_ts = now
            if now >= recording_until:
                out.release()
                out = None
                print("Recording saved.")

if __name__ == "__main__":
    main()
