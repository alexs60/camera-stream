import cv2
import time
import os
import datetime
from collections import deque

# --- CONFIG ---
RTSP_URL = os.getenv("RTSP_URL")
CAMERA_IP = os.getenv("CAMERA_IP", "192.168.8.234")
RECORDING_PATH = "/recordings"  # This matches the volume in docker-compose

def get_cap():
    while True:
        cap = cv2.VideoCapture(RTSP_URL)
        if cap.isOpened():
            print(f"[{datetime.datetime.now()}] Connected to {CAMERA_IP}")
            return cap
        time.sleep(10)

def main():
    # Sanity check: is the NFS drive actually mounted?
    if not os.access(RECORDING_PATH, os.W_OK):
        print(f"ERROR: {RECORDING_PATH} is not writable. Check NFS mount.")
        return

    cap = get_cap()
    fps = int(cap.get(cv2.CAP_PROP_FPS)) or 20
    buffer = deque(maxlen=10 * fps) # 10s pre-roll
    back_sub = cv2.createBackgroundSubtractorMOG2(history=500, varThreshold=50, detectShadows=True)
    
    recording = False
    frames_left = 0
    out = None

    while True:
        ret, frame = cap.read()
        if not ret:
            if out: out.release()
            recording = False
            cap.release()
            cap = get_cap()
            continue

        buffer.append(frame)
        
        # Motion detection
        mask = back_sub.apply(cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY))
        if cv2.countNonZero(mask) > 500 and not recording:
            now = datetime.datetime.now().strftime("%Y-%m-%d-%H-%M-%S")
            # Format: YYYY-MM-DD-HH-MM-SS-IPADDRESS.mp4
            filename = f"{RECORDING_PATH}/{now}-{CAMERA_IP}.mp4"
            
            print(f"Motion detected! Recording to {filename}")
            fourcc = cv2.VideoWriter_fourcc(*'mp4v')
            h, w, _ = frame.shape
            out = cv2.VideoWriter(filename, fourcc, fps, (w, h))
            
            for f in buffer: out.write(f)
            recording = True
            frames_left = 20 * fps # 20s post-roll

        if recording:
            out.write(frame)
            frames_left -= 1
            if frames_left <= 0:
                out.release()
                recording = False
                print("Recording saved.")

if __name__ == "__main__":
    main()
