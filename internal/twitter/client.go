package twitter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/dghubble/oauth1"
)

const (
	maxPostRetries = 5
	baseRetryDelay = 2 * time.Second
)

const tweetsEndpoint = "https://api.twitter.com/2/tweets"

// Client wraps an OAuth1-signed HTTP client for posting tweets
// via the X (Twitter) API v2.
type Client struct {
	httpClient *http.Client
}

// tweetRequest is the JSON body sent to the create-tweet endpoint.
type tweetRequest struct {
	Text  string      `json:"text"`
	Media *mediaField `json:"media,omitempty"`
}

// mediaField holds media attachment IDs for a tweet.
type mediaField struct {
	MediaIDs []string `json:"media_ids"`
}

// tweetResponse is the JSON body returned from the create-tweet endpoint.
type tweetResponse struct {
	Data struct {
		ID string `json:"id"`
	} `json:"data"`
}

// NewClient creates a Twitter API client authenticated with OAuth 1.0a
// user-context credentials.
func NewClient(apiKey, apiSecret, accessToken, accessTokenSecret string) *Client {
	config := oauth1.NewConfig(apiKey, apiSecret)
	token := oauth1.NewToken(accessToken, accessTokenSecret)
	httpClient := config.Client(oauth1.NoContext, token)

	slog.Info("twitter client initialized")

	return &Client{httpClient: httpClient}
}

// isRetryableStatus returns true for transient HTTP errors worth retrying.
func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusInternalServerError ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

// PostTweet publishes a tweet and returns the new tweet's ID.
// If mediaID is non-empty it is attached as a media reference.
// Retries up to maxPostRetries times on transient server errors with exponential backoff.
func (c *Client) PostTweet(ctx context.Context, text string, mediaID string) (string, error) {
	body := tweetRequest{Text: text}
	if mediaID != "" {
		body.Media = &mediaField{MediaIDs: []string{mediaID}}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshaling tweet request: %w", err)
	}

	slog.Info("posting tweet", "text_length", len(text), "has_media", mediaID != "")

	var lastErr error
	for attempt := 0; attempt < maxPostRetries; attempt++ {
		if attempt > 0 {
			delay := baseRetryDelay * (1 << (attempt - 1))
			slog.Warn("retrying tweet post",
				"attempt", attempt+1,
				"delay", delay.String(),
				"last_error", lastErr,
			)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tweetsEndpoint, bytes.NewReader(payload))
		if err != nil {
			return "", fmt.Errorf("creating tweet request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("sending tweet request: %w", err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("reading tweet response: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusCreated {
			var tweetResp tweetResponse
			if err := json.Unmarshal(respBody, &tweetResp); err != nil {
				return "", fmt.Errorf("unmarshaling tweet response: %w", err)
			}
			slog.Info("tweet posted successfully", "tweet_id", tweetResp.Data.ID)
			return tweetResp.Data.ID, nil
		}

		lastErr = fmt.Errorf("twitter API returned status %d: %s", resp.StatusCode, string(respBody))

		if !isRetryableStatus(resp.StatusCode) {
			slog.Error("tweet post failed with non-retryable status",
				"status_code", resp.StatusCode,
				"response", string(respBody),
			)
			return "", lastErr
		}

		slog.Warn("tweet post got retryable error",
			"status_code", resp.StatusCode,
			"attempt", attempt+1,
			"max_retries", maxPostRetries,
		)
	}

	slog.Error("tweet post failed after all retries", "attempts", maxPostRetries)
	return "", fmt.Errorf("tweet post failed after %d retries: %w", maxPostRetries, lastErr)
}
