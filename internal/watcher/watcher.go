package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/slaghuis/indexer/internal/pipeline"
)

type Watcher struct {
	root   string
	ignore []string
	pipe   *pipeline.Pipeline
	log    *slog.Logger

	mu      sync.Mutex
	pending map[string]time.Time
}

func New(root string, ignore []string, p *pipeline.Pipeline, log *slog.Logger) *Watcher {
	return &Watcher{
		root: root, ignore: ignore, pipe: p, log: log,
		pending: make(map[string]time.Time),
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()

	err = filepath.Walk(w.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if path == w.root {
				return err
			}
			w.log.Warn("watch walk skipped", "path", path, "err", err)
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if pipeline.SkipDir(info.Name(), w.ignore) {
			return filepath.SkipDir
		}
		return fw.Add(path)
	})
	if err != nil {
		return err
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					if !pipeline.SkipDir(info.Name(), w.ignore) {
						if err := fw.Add(ev.Name); err != nil {
							w.log.Warn("watch add dir", "path", ev.Name, "err", err)
						}
					}
				}
			}
			if !pipeline.IsGoSource(ev.Name) {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			w.mu.Lock()
			w.pending[ev.Name] = time.Now()
			w.mu.Unlock()
		case <-ticker.C:
			w.flush(ctx)
		case err := <-fw.Errors:
			w.log.Warn("watch error", "err", err)
		}
	}
}

func (w *Watcher) flush(ctx context.Context) {
	w.mu.Lock()
	now := time.Now()
	ready := []string{}
	for p, t := range w.pending {
		if now.Sub(t) > 400*time.Millisecond {
			ready = append(ready, p)
			delete(w.pending, p)
		}
	}
	w.mu.Unlock()

	for _, p := range ready {
		if _, err := os.Stat(p); err != nil {
			if err := w.pipe.RemoveFile(ctx, p); err != nil {
				w.log.Warn("remove failed", "path", p, "err", err)
			}
			continue
		}
		if err := w.pipe.IndexFile(ctx, p); err != nil {
			w.log.Warn("reindex failed", "path", p, "err", err)
		} else {
			w.log.Info("reindexed", "path", p)
		}
	}
}
