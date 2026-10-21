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
			ContextWindow:  4096,
		})
	}
	return out, nil
}

// Chat sends a chat completion request (non-streaming for now) to
// /chat/completions (OpenAI-compatible).
func (l *LMStudio) Chat(ctx context.Context, req ChatRequest) (<-chan ChatResult, error) {
	// Build OpenAI-compatible request.
	type oaiMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type oaiReq struct {
		Model       string   `json:"model"`
		Messages    []oaiMsg `json:"messages"`
		Stream      bool     `json:"stream"`
		Temperature float64  `json:"temperature"`
		MaxTokens   int      `json:"max_tokens,omitempty"`
	}

	msgs := make([]oaiMsg, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, oaiMsg{Role: m.Role, Content: m.Content})
	}

	payload := oaiReq{
		Model:       req.Model,
		Messages:    msgs,
		Stream:      true,
		Temperature: 0.7,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("lmstudio marshal request: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", l.baseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := l.transport.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("lmstudio chat: %v", err)
	}

	results := make(chan ChatResult, 8)
	go func() {
		defer resp.Body.Close()
		defer close(results)

		scanner := NewSSEReader(ctx, resp.Body)
		for {
			data, ok := scanner.NextData()
			if !ok {
				return
			}
			if data == "[DONE]" {
				return
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				results <- ChatResult{Err: fmt.Errorf("lmstudio decode chunk: %v", err)}
				continue
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content != "" {
					results <- ChatResult{Content: c.Delta.Content}
				}
			}
		}
	}()

	return results, nil
}
