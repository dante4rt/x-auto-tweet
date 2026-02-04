package gemini

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/genai"
)

// Client wraps the Gemini generative AI SDK and exposes a
// simple text generation method for tweet content.
type Client struct {
	client *genai.Client
	model  string
}

// NewClient creates a Gemini API client authenticated with apiKey.
// model specifies which generative model to use (e.g. "gemini-2.5-flash").
func NewClient(apiKey string, model string) (*Client, error) {
	ctx := context.Background()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("creating genai client: %w", err)
	}

	slog.Info("gemini client initialized", "model", model)

	return &Client{
		client: client,
		model:  model,
	}, nil
}

// Generate sends a prompt to the Gemini model and returns the generated text.
// systemPrompt sets the model's behavior context, userPrompt is the actual request,
// and temperature controls randomness (higher = more creative).
func (c *Client) Generate(ctx context.Context, systemPrompt string, userPrompt string, temperature float32) (string, error) {
	config := &genai.GenerateContentConfig{
		Temperature: genai.Ptr(temperature),
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(systemPrompt)},
		},
	}

	resp, err := c.client.Models.GenerateContent(
		ctx,
		c.model,
		[]*genai.Content{genai.NewContentFromText(userPrompt, genai.RoleUser)},
		config,
	)
	if err != nil {
		return "", fmt.Errorf("generating content: %w", err)
	}

	result := strings.TrimSpace(resp.Text())

	slog.Info("content generated",
		"model", c.model,
		"temperature", temperature,
		"response_length", len(result),
	)

	return result, nil
}

// Close is a no-op included for interface consistency.
// The current genai SDK does not require explicit cleanup.
func (c *Client) Close() {}
