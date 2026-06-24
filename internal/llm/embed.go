package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// defaultEmbedModel is the embedding model used when none is given.
const defaultEmbedModel = "nomic-embed-text"

// embedRequest is the body sent to /api/embeddings.
type embedRequest struct {
	// Model is the embedding model name.
	Model string `json:"model"`
	// Prompt is the text to embed.
	Prompt string `json:"prompt"`
}

// embedResponse is the body returned by /api/embeddings.
type embedResponse struct {
	// Embedding is the returned vector.
	Embedding []float32 `json:"embedding"`
	// Error holds a server error message when present.
	Error string `json:"error"`
}

// Embed returns the embedding vector for text using the configured embed model.
func (o *Ollama) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{Model: o.embedModel, Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("llm: encode embed request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: new embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: embed request to %s: %w", o.baseURL, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: read embed body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm: %w: status %d: %s", ErrModel, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var er embedResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("llm: decode embed: %w", err)
	}
	if er.Error != "" {
		return nil, fmt.Errorf("llm: %w: %s", ErrModel, er.Error)
	}
	if len(er.Embedding) == 0 {
		return nil, ErrEmptyResponse
	}
	return er.Embedding, nil
}
