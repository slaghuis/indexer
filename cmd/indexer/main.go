package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gopkg.in/yaml.v3"

	"github.com/slaghuis/indexer/internal/embedder"
	"github.com/slaghuis/indexer/internal/pipeline"
	"github.com/slaghuis/indexer/internal/store"
	"github.com/slaghuis/indexer/internal/watcher"
)

type Config struct {
	Repo        string   `yaml:"repo"`
	Root        string   `yaml:"root"`
	Concurrency int      `yaml:"concurrency"`
	Ignore      []string `yaml:"ignore"`

	Qdrant struct {
		Host       string `yaml:"host"`
		Port       int    `yaml:"port"`
		Collection string `yaml:"collection"`
		Dim        uint64 `yaml:"dim"`
	} `yaml:"qdrant"`

	Ollama struct {
		BaseURL string `yaml:"base_url"`
		Model   string `yaml:"model"`
	} `yaml:"ollama"`
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func main() {
	cfgPath := flag.String("config", "config.yaml", "config path")
	watchMode := flag.Bool("watch", false, "watch for changes after initial index")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	emb := embedder.NewOllama(cfg.Ollama.BaseURL, cfg.Ollama.Model)
	st, err := store.New(cfg.Qdrant.Host, cfg.Qdrant.Port,
		cfg.Qdrant.Collection, cfg.Qdrant.Dim)
	if err != nil {
		log.Error("qdrant", "err", err)
		os.Exit(1)
	}

	p := pipeline.New(pipeline.Config{
		Repo:        cfg.Repo,
		Root:        cfg.Root,
		Concurrency: cfg.Concurrency,
		Ignore:      cfg.Ignore,
	}, emb, st, log)

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := p.IndexAll(ctx); err != nil {
		log.Error("index all", "err", err)
		os.Exit(1)
	}

	if *watchMode {
		log.Info("entering watch mode")
		w := watcher.New(cfg.Root, p, log)
		if err := w.Run(ctx); err != nil {
			log.Error("watcher", "err", err)
			os.Exit(1)
		}
	}
}