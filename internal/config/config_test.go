package config

import "testing"

func TestLoad_TwoCameras(t *testing.T) {
	env := []string{
		"RECORDING_PATH=/rec",
		"TMPFS_PATH=/tmp/seg",
		"SCENE_THRESHOLD=0.07",
		"CAMERA_1_NAME=front",
		"CAMERA_1_RTSP=rtsp://u:p@1.1.1.1/x",
		"CAMERA_1_IP=1.1.1.1",
		"CAMERA_2_NAME=back",
		"CAMERA_2_RTSP=rtsp://u:p@2.2.2.2/x",
		"CAMERA_2_IP=2.2.2.2",
	}
	cfg, err := Load(env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RecordingPath != "/rec" || cfg.TmpfsPath != "/tmp/seg" {
		t.Errorf("paths: %+v", cfg)
	}
	if cfg.SceneThreshold != 0.07 {
		t.Errorf("threshold: %v", cfg.SceneThreshold)
	}
	if len(cfg.Cameras) != 2 || cfg.Cameras[0].Name != "front" || cfg.Cameras[1].Name != "back" {
		t.Errorf("cameras: %+v", cfg.Cameras)
	}
}

func TestLoad_NoCameras(t *testing.T) {
	if _, err := Load([]string{"RECORDING_PATH=/rec"}); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestLoad_PartialCameraIsError(t *testing.T) {
	env := []string{
		"CAMERA_1_NAME=front",
		"CAMERA_1_IP=1.1.1.1",
	}
	if _, err := Load(env); err == nil {
		t.Error("expected error for missing RTSP, got nil")
	}
}

func TestLoad_BadName(t *testing.T) {
	env := []string{
		"CAMERA_1_NAME=front cam",
		"CAMERA_1_RTSP=rtsp://x",
		"CAMERA_1_IP=1.1.1.1",
	}
	if _, err := Load(env); err == nil {
		t.Error("expected error for unsafe name, got nil")
	}
}

func TestLoad_DuplicateName(t *testing.T) {
	env := []string{
		"CAMERA_1_NAME=front", "CAMERA_1_RTSP=rtsp://a", "CAMERA_1_IP=1.1.1.1",
		"CAMERA_2_NAME=front", "CAMERA_2_RTSP=rtsp://b", "CAMERA_2_IP=2.2.2.2",
	}
	if _, err := Load(env); err == nil {
		t.Error("expected error for duplicate name, got nil")
	}
}

func TestLoad_StopsAtFirstGap(t *testing.T) {
	env := []string{
		"CAMERA_1_NAME=front", "CAMERA_1_RTSP=rtsp://a", "CAMERA_1_IP=1.1.1.1",
		// no CAMERA_2_*; CAMERA_3_* should NOT be picked up
		"CAMERA_3_NAME=back", "CAMERA_3_RTSP=rtsp://b", "CAMERA_3_IP=2.2.2.2",
	}
	cfg, err := Load(env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Cameras) != 1 {
		t.Errorf("expected 1 camera (loader stops at gap), got %d", len(cfg.Cameras))
	}
}
