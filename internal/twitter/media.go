package twitter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	mediaUploadEndpoint = "https://upload.twitter.com/1.1/media/upload.json"
	chunkSize           = 5 * 1024 * 1024 // 5 MB
)

// mediaInitResponse is the JSON returned by the INIT command.
type mediaInitResponse struct {
	MediaIDString string `json:"media_id_string"`
}

// mediaFinalizeResponse is the JSON returned by the FINALIZE command.
type mediaFinalizeResponse struct {
	MediaIDString  string          `json:"media_id_string"`
	ProcessingInfo *processingInfo `json:"processing_info,omitempty"`
}

// mediaStatusResponse is the JSON returned by the STATUS command.
type mediaStatusResponse struct {
	ProcessingInfo *processingInfo `json:"processing_info,omitempty"`
}

// processingInfo describes the async processing state of uploaded media.
type processingInfo struct {
	State          string             `json:"state"`
	CheckAfterSecs int                `json:"check_after_secs"`
	Error          *processingError   `json:"error,omitempty"`
}

// processingError holds the error detail when processing fails.
type processingError struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

// UploadGIF uploads a GIF to Twitter using the chunked media upload flow
// (INIT -> APPEND -> FINALIZE) and returns the media_id_string.
func (c *Client) UploadGIF(ctx context.Context, gifData []byte) (string, error) {
	mediaID, err := c.mediaInit(ctx, len(gifData))
	if err != nil {
		return "", fmt.Errorf("media INIT: %w", err)
	}

	if err := c.mediaAppend(ctx, mediaID, gifData); err != nil {
		return "", fmt.Errorf("media APPEND: %w", err)
	}

	mediaID, processingState, err := c.mediaFinalize(ctx, mediaID)
	if err != nil {
		return "", fmt.Errorf("media FINALIZE: %w", err)
	}

	if processingState != nil {
		if err := c.waitForProcessing(ctx, mediaID, processingState); err != nil {
			return "", fmt.Errorf("media STATUS polling: %w", err)
		}
	}

	slog.Info("gif upload completed", "media_id", mediaID)
	return mediaID, nil
}

// mediaInit sends the INIT command and returns the allocated media_id_string.
func (c *Client) mediaInit(ctx context.Context, totalBytes int) (string, error) {
	slog.Info("media upload INIT", "total_bytes", totalBytes)

	form := url.Values{}
	form.Set("command", "INIT")
	form.Set("total_bytes", strconv.Itoa(totalBytes))
	form.Set("media_type", "image/gif")
	form.Set("media_category", "tweet_gif")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mediaUploadEndpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating INIT request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending INIT request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading INIT response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("media INIT failed", "status_code", resp.StatusCode, "response", string(respBody))
		return "", fmt.Errorf("INIT returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var initResp mediaInitResponse
	if err := json.Unmarshal(respBody, &initResp); err != nil {
		return "", fmt.Errorf("unmarshaling INIT response: %w", err)
	}

	slog.Info("media INIT succeeded", "media_id", initResp.MediaIDString)
	return initResp.MediaIDString, nil
}

// mediaAppend uploads the GIF data in chunks via the APPEND command.
func (c *Client) mediaAppend(ctx context.Context, mediaID string, gifData []byte) error {
	totalSize := len(gifData)
	segmentIndex := 0

	for offset := 0; offset < totalSize; offset += chunkSize {
		end := offset + chunkSize
		if end > totalSize {
			end = totalSize
		}
		chunk := gifData[offset:end]

		slog.Info("media upload APPEND",
			"media_id", mediaID,
			"segment_index", segmentIndex,
			"chunk_bytes", len(chunk),
		)

		if err := c.appendChunk(ctx, mediaID, segmentIndex, chunk); err != nil {
			return fmt.Errorf("segment %d: %w", segmentIndex, err)
		}

		segmentIndex++
	}

	return nil
}

// appendChunk sends a single APPEND chunk as base64-encoded form data.
func (c *Client) appendChunk(ctx context.Context, mediaID string, segmentIndex int, chunk []byte) error {
	form := url.Values{}
	form.Set("command", "APPEND")
	form.Set("media_id", mediaID)
	form.Set("segment_index", strconv.Itoa(segmentIndex))
	form.Set("media_data", base64.StdEncoding.EncodeToString(chunk))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mediaUploadEndpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating APPEND request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending APPEND request: %w", err)
	}
	defer resp.Body.Close()

	// APPEND returns empty body on success; drain it.
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("media APPEND failed",
			"status_code", resp.StatusCode,
			"segment_index", segmentIndex,
			"response", string(respBody),
		)
		return fmt.Errorf("APPEND returned status %d: %s", resp.StatusCode, string(respBody))
	}

	slog.Info("media APPEND succeeded", "media_id", mediaID, "segment_index", segmentIndex)
	return nil
}

// mediaFinalize sends the FINALIZE command and returns the media_id_string
// along with any processing_info that requires polling.
func (c *Client) mediaFinalize(ctx context.Context, mediaID string) (string, *processingInfo, error) {
	slog.Info("media upload FINALIZE", "media_id", mediaID)

	form := url.Values{}
	form.Set("command", "FINALIZE")
	form.Set("media_id", mediaID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mediaUploadEndpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", nil, fmt.Errorf("creating FINALIZE request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("sending FINALIZE request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("reading FINALIZE response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("media FINALIZE failed", "status_code", resp.StatusCode, "response", string(respBody))
		return "", nil, fmt.Errorf("FINALIZE returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var finalResp mediaFinalizeResponse
	if err := json.Unmarshal(respBody, &finalResp); err != nil {
		return "", nil, fmt.Errorf("unmarshaling FINALIZE response: %w", err)
	}

	slog.Info("media FINALIZE succeeded",
		"media_id", finalResp.MediaIDString,
		"has_processing_info", finalResp.ProcessingInfo != nil,
	)
	return finalResp.MediaIDString, finalResp.ProcessingInfo, nil
}

// waitForProcessing polls the STATUS endpoint until the media finishes
// processing or the context is cancelled.
func (c *Client) waitForProcessing(ctx context.Context, mediaID string, info *processingInfo) error {
	for {
		switch info.State {
		case "succeeded":
			slog.Info("media processing succeeded", "media_id", mediaID)
			return nil

		case "failed":
			msg := "unknown error"
			if info.Error != nil {
				msg = info.Error.Message
			}
			slog.Error("media processing failed", "media_id", mediaID, "error", msg)
			return fmt.Errorf("media processing failed: %s", msg)

		case "pending", "in_progress":
			waitSecs := info.CheckAfterSecs
			if waitSecs <= 0 {
				waitSecs = 1
			}
			slog.Info("media processing in progress, waiting",
				"media_id", mediaID,
				"state", info.State,
				"check_after_secs", waitSecs,
			)

			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while waiting for processing: %w", ctx.Err())
			case <-time.After(time.Duration(waitSecs) * time.Second):
			}

			statusInfo, err := c.mediaStatus(ctx, mediaID)
			if err != nil {
				return fmt.Errorf("polling STATUS: %w", err)
			}
			info = statusInfo

		default:
			return fmt.Errorf("unknown processing state: %s", info.State)
		}
	}
}

// mediaStatus queries the STATUS endpoint for the current processing state.
func (c *Client) mediaStatus(ctx context.Context, mediaID string) (*processingInfo, error) {
	params := url.Values{}
	params.Set("command", "STATUS")
	params.Set("media_id", mediaID)

	endpoint := mediaUploadEndpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating STATUS request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending STATUS request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading STATUS response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("media STATUS failed", "status_code", resp.StatusCode, "response", string(respBody))
		return nil, fmt.Errorf("STATUS returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var statusResp mediaStatusResponse
	if err := json.Unmarshal(respBody, &statusResp); err != nil {
		return nil, fmt.Errorf("unmarshaling STATUS response: %w", err)
	}

	if statusResp.ProcessingInfo == nil {
		// No processing_info means processing is complete.
		return &processingInfo{State: "succeeded"}, nil
	}

	slog.Info("media STATUS polled", "media_id", mediaID, "state", statusResp.ProcessingInfo.State)
	return statusResp.ProcessingInfo, nil
}
