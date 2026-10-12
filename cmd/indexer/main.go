package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
		APIKey     string `yaml:"api_key"`
		UseTLS     bool   `yaml:"tls"`
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
	if c.Concurrency < 1 {
		c.Concurrency = 4
	}
	if c.Qdrant.APIKey == "" {
		c.Qdrant.APIKey = os.Getenv("QDRANT_API_KEY")
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	var missing []string
	if strings.TrimSpace(c.Repo) == "" {
		missing = append(missing, "repo")
	}
	if strings.TrimSpace(c.Root) == "" {
		missing = append(missing, "root")
	}
	if strings.TrimSpace(c.Qdrant.Host) == "" {
		missing = append(missing, "qdrant.host")
	}
	if c.Qdrant.Port == 0 {
		missing = append(missing, "qdrant.port")
	}
	if strings.TrimSpace(c.Qdrant.Collection) == "" {
		missing = append(missing, "qdrant.collection")
	}
	if c.Qdrant.Dim == 0 {
		missing = append(missing, "qdrant.dim")
	}
	if strings.TrimSpace(c.Ollama.BaseURL) == "" {
		missing = append(missing, "ollama.base_url")
	}
	if strings.TrimSpace(c.Ollama.Model) == "" {
		missing = append(missing, "ollama.model")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	info, err := os.Stat(c.Root)
	if err != nil {
		return fmt.Errorf("root %q: %w", c.Root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("root %q is not a directory", c.Root)
	}
	abs, err := filepath.Abs(c.Root)
	if err != nil {
		return err
	}
	c.Root = abs
	return nil
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	cfgPath := flag.String("config", "config.yaml", "config path")
	watchMode := flag.Bool("watch", false, "watch for changes after initial index")
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parseLogLevel(*logLevel)}))

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	emb := embedder.NewOllama(cfg.Ollama.BaseURL, cfg.Ollama.Model)
	st, err := store.New(cfg.Qdrant.Host, cfg.Qdrant.Port,
		cfg.Qdrant.Collection, cfg.Qdrant.Dim, cfg.Qdrant.APIKey, cfg.Qdrant.UseTLS)
	if err != nil {
		log.Error("qdrant", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Warn("qdrant close", "err", err)
		}
	}()

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
		w := watcher.New(cfg.Root, cfg.Ignore, p, log)
		if err := w.Run(ctx); err != nil {
			log.Error("watcher", "err", err)
			os.Exit(1)
		}
	}
}
