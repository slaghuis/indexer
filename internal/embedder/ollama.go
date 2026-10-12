package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Ollama struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

type embedReq struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResp struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (o *Ollama) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedReq{Model: o.Model, Input: text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.BaseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("ollama embed status %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}

	var out embedResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("empty embeddings")
	}
	return out.Embeddings[0], nil
}
