package pipeline

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/slaghuis/indexer/internal/chunker"
	"github.com/slaghuis/indexer/internal/embedder"
	"github.com/slaghuis/indexer/internal/store"
)

type Config struct {
	Repo        string
	Root        string
	Concurrency int
	Ignore      []string
}

type Pipeline struct {
	cfg Config
	emb *embedder.Ollama
	st  *store.Qdrant
	log *slog.Logger
}

func New(cfg Config, emb *embedder.Ollama, st *store.Qdrant, log *slog.Logger) *Pipeline {
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 4
	}
	return &Pipeline{cfg: cfg, emb: emb, st: st, log: log}
}

type stats struct {
	mu                            sync.Mutex
	files, chunks, skipped, errs  int
}

func (s *stats) add(f, c, sk, e int) {
	s.mu.Lock()
	s.files += f
	s.chunks += c
	s.skipped += sk
	s.errs += e
	s.mu.Unlock()
}

// IndexAll walks the repo and indexes every .go file.
func (p *Pipeline) IndexAll(ctx context.Context) error {
	start := time.Now()
	st := &stats{}

	paths := make(chan string, 128)
	g, gctx := errgroup.WithContext(ctx)

	// walker
	g.Go(func() error {
		defer close(paths)
		return filepath.WalkDir(p.cfg.Root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p.shouldSkipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			select {
			case paths <- path:
			case <-gctx.Done():
				return gctx.Err()
			}
			return nil
		})
	})

	// workers
	for i := 0; i < p.cfg.Concurrency; i++ {
		g.Go(func() error {
			for path := range paths {
				if err := p.indexFile(gctx, path, st); err != nil {
					p.log.Warn("index file failed", "path", path, "err", err)
					st.add(0, 0, 0, 1)
				}
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}
	p.log.Info("index complete",
		"files", st.files, "chunks", st.chunks,
		"skipped", st.skipped, "errors", st.errs,
		"duration", time.Since(start))
	return nil
}

func (p *Pipeline) indexFile(ctx context.Context, absPath string, st *stats) error {
	rel, err := filepath.Rel(p.cfg.Root, absPath)
	if err != nil {
		return err
	}
	chunks, err := chunker.ChunkFile(p.cfg.Repo, absPath, rel)
	if err != nil {
		return err
	}
	st.add(1, 0, 0, 0)

	keep := make(map[string]bool, len(chunks))
	var newCount, skipCount int

	for _, c := range chunks {
		id := c.StableID()
		keep[id] = true

		existing, _ := p.st.ExistingHash(ctx, id)
		if existing == c.Hash {
			skipCount++
			continue
		}

		vec, err := p.emb.Embed(ctx, c.Text())
		if err != nil {
			return fmt.Errorf("embed %s: %w", c.Symbol, err)
		}
		if err := p.st.Upsert(ctx, c, vec); err != nil {
			return fmt.Errorf("upsert %s: %w", c.Symbol, err)
		}
		newCount++
	}

	if err := p.st.DeleteStale(ctx, p.cfg.Repo, rel, keep); err != nil {
		p.log.Warn("delete stale failed", "path", rel, "err", err)
	}

	st.add(0, newCount, skipCount, 0)
	p.log.Debug("file indexed", "path", rel, "new", newCount, "skipped", skipCount)
	return nil
}

// IndexFile is exposed for the watcher.
func (p *Pipeline) IndexFile(ctx context.Context, absPath string) error {
	return p.indexFile(ctx, absPath, &stats{})
}

// RemoveFile is called when a file is deleted.
func (p *Pipeline) RemoveFile(ctx context.Context, absPath string) error {
	rel, err := filepath.Rel(p.cfg.Root, absPath)
	if err != nil {
		return err
	}
	return p.st.DeleteByPath(ctx, p.cfg.Repo, rel)
}

func (p *Pipeline) shouldSkipDir(name string) bool {
	skip := map[string]bool{
		".git": true, "vendor": true, "node_modules": true,
		".idea": true, ".vscode": true, "dist": true, "bin": true,
	}
	if skip[name] {
		return true
	}
	for _, pat := range p.cfg.Ignore {
		if pat == name {
			return true
		}
	}
	return false
}