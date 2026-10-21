package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// LMStudio is a client for the LM Studio API (OpenAI-compatible).
type LMStudio struct {
	baseURL   string
	transport *Transport
	client    *http.Client
}

// NewLMStudio creates an LM Studio client.
func NewLMStudio(baseURL string, transport *Transport) *LMStudio {
	return &LMStudio{
		baseURL:   strings.TrimRight(baseURL, "/"),
		transport: transport,
		client:    &http.Client{Timeout: TotalDeadline},
	}
}

// modelsResponse is the /api/v1/models response.
type modelsResponse struct {
	Data []lmstudioModel `json:"data"`
}

type lmstudioModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// ListModels calls /api/v1/models and returns discovered models.
func (l *LMStudio) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", l.baseURL+"/api/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.transport.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lmstudio list models: %v", err)
	}
	defer resp.Body.Close()

	var models modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("lmstudio decode models: %v", err)
	}

	out := make([]ModelInfo, 0, len(models.Data))
	for _, m := range models.Data {
		out = append(out, ModelInfo{
			ID:             m.ID,
			Name:           m.ID,
			Backend:        "lmstudio",
			URL:            l.baseURL,
			ToolsSupported: true,
			ContextWindow:  DefaultContextWindow,
		})
	}
	return out, nil
}

// Chat streams a chat completion with tools from LM Studio's
// OpenAI-compatible endpoint.
func (l *LMStudio) Chat(ctx context.Context, req ChatRequest) (<-chan ChatResult, error) {
	temp := 0.7
	c := &OpenAICompat{
		backend:     "lmstudio",
		baseURL:     l.baseURL + "/v1",
		transport:   l.transport,
		temperature: &temp,
	}
	return c.Chat(ctx, req)
}
