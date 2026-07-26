package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	DefaultModel     = "claude-sonnet-4-5"
	maxRetries       = 1
	maxTokensDefault = 1024
)

type LLMProvider string

const (
	ProviderGroq      LLMProvider = "groq"
	ProviderGemini    LLMProvider = "gemini"
	ProviderOpenAI    LLMProvider = "openai"
	ProviderAnthropic LLMProvider = "anthropic"
)

// Client wraps LLM completion calls supporting Groq, Gemini, OpenAI, and Anthropic.
type Client struct {
	provider LLMProvider
	apiKey   string
	model    string
	ac       *anthropic.Client
	hc       *http.Client
}

// NewClient creates an LLM API client.
// It automatically detects GROQ_API_KEY, GEMINI_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY.
func NewClient(apiKey, model string) *Client {
	hc := &http.Client{Timeout: 30 * time.Second}

	// 1. Check if apiKey explicitly passed or detect from environment
	groqKey := os.Getenv("GROQ_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	openaiKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := apiKey
	if anthropicKey == "" {
		anthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	if groqKey != "" {
		m := model
		if m == "" {
			m = "llama-3.3-70b-versatile"
		}
		return &Client{provider: ProviderGroq, apiKey: groqKey, model: m, hc: hc}
	}

	if geminiKey != "" {
		m := model
		if m == "" {
			m = "gemini-2.0-flash"
		}
		return &Client{provider: ProviderGemini, apiKey: geminiKey, model: m, hc: hc}
	}

	if openaiKey != "" {
		m := model
		if m == "" {
			m = "gpt-4o-mini"
		}
		return &Client{provider: ProviderOpenAI, apiKey: openaiKey, model: m, hc: hc}
	}

	// Default to Anthropic Claude
	m := model
	if m == "" {
		m = DefaultModel
	}
	ac := anthropic.NewClient(option.WithAPIKey(anthropicKey))
	return &Client{provider: ProviderAnthropic, apiKey: anthropicKey, model: m, ac: &ac, hc: hc}
}

// ProviderName returns the active LLM provider name for logging.
func (c *Client) ProviderName() string {
	switch c.provider {
	case ProviderGroq:
		return fmt.Sprintf("Groq API (%s)", c.model)
	case ProviderGemini:
		return fmt.Sprintf("Google Gemini API (%s)", c.model)
	case ProviderOpenAI:
		return fmt.Sprintf("OpenAI API (%s)", c.model)
	default:
		return fmt.Sprintf("Anthropic Claude (%s)", c.model)
	}
}

// jsonResponse runs a prompt and returns parsed JSON into dest.
// It retries once with a stricter prompt if the first response is unparseable.
func (c *Client) jsonResponse(ctx context.Context, systemPrompt, userPrompt string, dest any) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		prompt := userPrompt
		if attempt > 0 {
			prompt = userPrompt + "\n\nIMPORTANT: Your previous response could not be parsed as JSON. " +
				"Reply with ONLY valid JSON — no markdown, no prose, no code fences."
		}

		raw, err := c.complete(ctx, systemPrompt, prompt)
		if err != nil {
			return fmt.Errorf("%s LLM error: %w", c.provider, err)
		}

		extracted := extractJSON(raw)
		if err := json.Unmarshal([]byte(extracted), dest); err != nil {
			lastErr = fmt.Errorf("JSON parse error (attempt %d): %w — raw: %.200s", attempt+1, err, raw)
			continue
		}
		return nil
	}
	return lastErr
}

// complete sends a single message to the active LLM provider.
func (c *Client) complete(ctx context.Context, system, user string) (string, error) {
	switch c.provider {
	case ProviderGroq:
		return c.completeGroq(ctx, system, user)
	case ProviderGemini:
		return c.completeGemini(ctx, system, user)
	case ProviderOpenAI:
		return c.completeOpenAI(ctx, system, user)
	default:
		return c.completeAnthropic(ctx, system, user)
	}
}

// completeGroq calls Groq Cloud API (OpenAI compatible format).
func (c *Client) completeGroq(ctx context.Context, system, user string) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type reqBody struct {
		Model       string    `json:"model"`
		Messages    []message `json:"messages"`
		Temperature float64   `json:"temperature"`
	}

	payload := reqBody{
		Model: c.model,
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("groq http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("groq API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", err
	}
	if len(apiResp.Choices) == 0 {
		return "", errors.New("groq returned empty choices")
	}
	return apiResp.Choices[0].Message.Content, nil
}

// completeGemini calls Google Gemini API (AI Studio).
func (c *Client) completeGemini(ctx context.Context, system, user string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", c.model, c.apiKey)

	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	type sysInstruction struct {
		Parts []part `json:"parts"`
	}
	type reqBody struct {
		SystemInstruction sysInstruction `json:"system_instruction"`
		Contents          []content      `json:"contents"`
	}

	payload := reqBody{
		SystemInstruction: sysInstruction{Parts: []part{{Text: system}}},
		Contents:          []content{{Role: "user", Parts: []part{{Text: user}}}},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", err
	}
	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("gemini returned empty content")
	}
	return apiResp.Candidates[0].Content.Parts[0].Text, nil
}

// completeOpenAI calls OpenAI API.
func (c *Client) completeOpenAI(ctx context.Context, system, user string) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type reqBody struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}

	payload := reqBody{
		Model: c.model,
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("openai API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", err
	}
	if len(apiResp.Choices) == 0 {
		return "", errors.New("openai returned empty choices")
	}
	return apiResp.Choices[0].Message.Content, nil
}

// completeAnthropic calls Anthropic Claude SDK.
func (c *Client) completeAnthropic(ctx context.Context, system, user string) (string, error) {
	if c.ac == nil {
		return "", errors.New("anthropic client not initialized")
	}
	msg, err := c.ac.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: maxTokensDefault,
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", err
	}

	if len(msg.Content) == 0 {
		return "", errors.New("claude returned empty content")
	}

	var sb strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String(), nil
}

// jsonFenceRe matches a JSON code block (```json ... ``` or ``` ... ```).
var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(.+?)\\n?```")

// extractJSON strips markdown code fences from a response and returns the raw JSON.
func extractJSON(raw string) string {
	if m := jsonFenceRe.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(raw)
}
