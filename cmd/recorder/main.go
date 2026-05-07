package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"camera-stream/internal/camera"
	"camera-stream/internal/config"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := config.Load(os.Environ())
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	log.Printf("loaded %d camera(s); recordings -> %s, segments -> %s, scene_threshold=%g",
		len(cfg.Cameras), cfg.RecordingPath, cfg.TmpfsPath, cfg.SceneThreshold)
	log.Printf("clip params: pre_roll=%s post_roll=%s max_clip=%s segment=%s",
		cfg.PreRoll, cfg.PostRoll, cfg.MaxClipDuration, cfg.SegmentDuration)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var wg sync.WaitGroup
	for _, cam := range cfg.Cameras {
		cam := cam
		sup := &camera.Supervisor{
			Cfg:    cfg,
			Cam:    cam,
			Logger: log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds),
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sup.Run(ctx); err != nil {
				log.Printf("[%s] supervisor exited: %v", cam.Name, err)
			}
		}()
	}

	<-ctx.Done()
	log.Printf("signal received, stopping supervisors")
	wg.Wait()
	log.Printf("all supervisors finished")
}
