package klipy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const baseURL = "https://api.klipy.com/api/v1"

// GIFResult holds the URL and file size of a single GIF from the Klipy API.
type GIFResult struct {
	URL  string
	Size int64
}

// searchResponse is the top-level JSON returned by the Klipy search endpoint.
type searchResponse struct {
	Result bool       `json:"result"`
	Data   searchData `json:"data"`
}

// searchData holds the paginated result set.
type searchData struct {
	Data    []searchItem `json:"data"`
	HasNext bool         `json:"has_next"`
}

// searchItem represents a single result from the API.
type searchItem struct {
	ID   int        `json:"id"`
	File fileFormats `json:"file"`
}

// fileFormats groups media by size category.
type fileFormats struct {
	HD formatSet `json:"hd"`
	MD formatSet `json:"md"`
	SM formatSet `json:"sm"`
}

// formatSet holds each media type within a size category.
type formatSet struct {
	GIF *mediaFile `json:"gif,omitempty"`
}

// mediaFile is a single rendition with URL and size.
type mediaFile struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int64  `json:"size"`
}

// Client interacts with the Klipy GIF API v1.
type Client struct {
	appKey     string
	httpClient *http.Client
}

// NewClient creates a new Klipy API client with a default 30-second timeout.
func NewClient(appKey string) *Client {
	return &Client{
		appKey: appKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SearchGIF queries the Klipy search endpoint and returns matching GIF results.
func (c *Client) SearchGIF(ctx context.Context, query string, limit int) ([]GIFResult, error) {
	endpoint := fmt.Sprintf("%s/%s/gifs/search", baseURL, c.appKey)

	params := url.Values{}
	params.Set("q", query)
	params.Set("per_page", strconv.Itoa(limit))
	params.Set("customer_id", "x-auto-tweet")
	params.Set("format_filter", "gif")

	reqURL := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("klipy: creating search request: %w", err)
	}

	slog.DebugContext(ctx, "klipy: searching GIFs", slog.String("query", query), slog.Int("limit", limit))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("klipy: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("klipy: search returned status %d: %s", resp.StatusCode, string(body))
	}

	var searchResp searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("klipy: decoding search response: %w", err)
	}

	if !searchResp.Result {
		return nil, fmt.Errorf("klipy: search returned result=false")
	}

	results := make([]GIFResult, 0, len(searchResp.Data.Data))
	for _, item := range searchResp.Data.Data {
		// Prefer md (medium) GIF, fall back to sm, then hd.
		gif := item.File.MD.GIF
		if gif == nil {
			gif = item.File.SM.GIF
		}
		if gif == nil {
			gif = item.File.HD.GIF
		}
		if gif == nil {
			slog.WarnContext(ctx, "klipy: result missing gif format, skipping", slog.Int("id", item.ID))
			continue
		}
		results = append(results, GIFResult{
			URL:  gif.URL,
			Size: gif.Size,
		})
	}

	slog.InfoContext(ctx, "klipy: search completed", slog.String("query", query), slog.Int("results", len(results)))
	return results, nil
}

// DownloadGIF fetches the raw bytes of a GIF from the given URL.
func (c *Client) DownloadGIF(ctx context.Context, gifURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gifURL, nil)
	if err != nil {
		return nil, fmt.Errorf("klipy: creating download request: %w", err)
	}

	slog.DebugContext(ctx, "klipy: downloading GIF", slog.String("url", gifURL))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("klipy: download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("klipy: download returned status %d for url %s", resp.StatusCode, gifURL)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("klipy: reading download body: %w", err)
	}

	slog.InfoContext(ctx, "klipy: download completed", slog.Int("bytes", len(data)))
	return data, nil
}
