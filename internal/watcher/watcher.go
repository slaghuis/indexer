package watcher

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/slaghuis/indexer/internal/pipeline"
)

type Watcher struct {
	root string
	pipe *pipeline.Pipeline
	log  *slog.Logger

	mu      sync.Mutex
	pending map[string]time.Time
}

func New(root string, p *pipeline.Pipeline, log *slog.Logger) *Watcher {
	return &Watcher{
		root: root, pipe: p, log: log,
		pending: make(map[string]time.Time),
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()

	// Add recursive watches
	err = filepath.Walk(w.root, func(path string, info interface {
		IsDir() bool
		Name() string
	}, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		if skipDir(info.Name()) {
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
			if !strings.HasSuffix(ev.Name, ".go") || strings.HasSuffix(ev.Name, "_test.go") {
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
		if _, err := statFile(p); err != nil {
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

func skipDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules", ".idea", ".vscode", "dist", "bin":
		return true
	}
	return false
}