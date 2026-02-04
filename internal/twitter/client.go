package twitter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/dghubble/oauth1"
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

// PostTweet publishes a tweet and returns the new tweet's ID.
// If mediaID is non-empty it is attached as a media reference.
func (c *Client) PostTweet(ctx context.Context, text string, mediaID string) (string, error) {
	body := tweetRequest{Text: text}
	if mediaID != "" {
		body.Media = &mediaField{MediaIDs: []string{mediaID}}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshaling tweet request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tweetsEndpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("creating tweet request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	slog.Info("posting tweet", "text_length", len(text), "has_media", mediaID != "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending tweet request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading tweet response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		slog.Error("tweet post failed",
			"status_code", resp.StatusCode,
			"response", string(respBody),
		)
		return "", fmt.Errorf("twitter API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var tweetResp tweetResponse
	if err := json.Unmarshal(respBody, &tweetResp); err != nil {
		return "", fmt.Errorf("unmarshaling tweet response: %w", err)
	}

	slog.Info("tweet posted successfully", "tweet_id", tweetResp.Data.ID)

	return tweetResp.Data.ID, nil
}
