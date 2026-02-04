package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"time"

	"github.com/dantezy/x-auto-tweet/internal/config"
	"github.com/dantezy/x-auto-tweet/internal/gemini"
	"github.com/dantezy/x-auto-tweet/internal/history"
	"github.com/dantezy/x-auto-tweet/internal/klipy"
	"github.com/dantezy/x-auto-tweet/internal/twitter"
)

const maxRetries = 3

// Scheduler plans and executes daily tweets at randomized times.
type Scheduler struct {
	cfg     *config.Config
	gemini  *gemini.Client
	twitter *twitter.Client
	klipy   *klipy.Client
	history *history.Store
	loc     *time.Location
}

// New creates a Scheduler with all required dependencies.
func New(
	cfg *config.Config,
	geminiClient *gemini.Client,
	twitterClient *twitter.Client,
	klipyClient *klipy.Client,
	historyStore *history.Store,
) (*Scheduler, error) {
	loc, err := time.LoadLocation(cfg.Schedule.Timezone)
	if err != nil {
		return nil, fmt.Errorf("loading timezone %s: %w", cfg.Schedule.Timezone, err)
	}

	return &Scheduler{
		cfg:     cfg,
		gemini:  geminiClient,
		twitter: twitterClient,
		klipy:   klipyClient,
		history: historyStore,
		loc:     loc,
	}, nil
}

// Run starts the scheduling loop and blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	slog.Info("scheduler started",
		"timezone", s.cfg.Schedule.Timezone,
		"tweets_per_day", s.cfg.Schedule.TweetsPerDay,
		"window", s.cfg.Schedule.WindowStart+"-"+s.cfg.Schedule.WindowEnd,
	)

	for {
		times := s.planDay()
		slog.Info("daily plan created", "tweet_times", formatTimes(times))

		for _, t := range times {
			delay := time.Until(t)
			if delay < 0 {
				slog.Info("skipping past tweet time", "time", t.Format(time.RFC3339))
				continue
			}

			slog.Info("waiting for next tweet", "scheduled_at", t.Format(time.RFC3339), "delay", delay.Round(time.Second))

			select {
			case <-ctx.Done():
				slog.Info("scheduler stopped")
				return ctx.Err()
			case <-time.After(delay):
			}

			if err := s.executeTweet(ctx); err != nil {
				slog.Error("tweet execution failed", "error", err)
			}
		}

		// Wait until midnight in the configured timezone to plan the next day.
		now := time.Now().In(s.loc)
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, s.loc)
		sleepUntilMidnight := time.Until(nextMidnight)

		slog.Info("daily tweets done, sleeping until midnight", "sleep", sleepUntilMidnight.Round(time.Second))

		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return ctx.Err()
		case <-time.After(sleepUntilMidnight):
		}
	}
}

// planDay generates random tweet times for today within the configured window.
func (s *Scheduler) planDay() []time.Time {
	now := time.Now().In(s.loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)

	startH, startM := parseTime(s.cfg.Schedule.WindowStart)
	endH, endM := parseTime(s.cfg.Schedule.WindowEnd)

	windowStart := today.Add(time.Duration(startH)*time.Hour + time.Duration(startM)*time.Minute)
	windowEnd := today.Add(time.Duration(endH)*time.Hour + time.Duration(endM)*time.Minute)

	if windowEnd.Sub(windowStart) <= 0 {
		slog.Warn("invalid tweet window, using defaults")
		windowStart = today.Add(9 * time.Hour)
		windowEnd = today.Add(22 * time.Hour)
	}

	// If we're already past the window start, only schedule from now onward.
	if now.After(windowStart) {
		windowStart = now
	}

	windowDuration := windowEnd.Sub(windowStart)
	if windowDuration <= 0 {
		slog.Info("tweet window has already passed for today")
		return nil
	}

	minGap := time.Duration(s.cfg.Schedule.MinGapMinutes) * time.Minute
	count := s.cfg.Schedule.TweetsPerDay

	var times []time.Time
	for attempt := 0; attempt < 100 && len(times) < count; attempt++ {
		candidate := windowStart.Add(time.Duration(rand.Int63n(int64(windowDuration))))

		valid := true
		for _, existing := range times {
			if candidate.Sub(existing).Abs() < minGap {
				valid = false
				break
			}
		}
		if valid {
			times = append(times, candidate)
		}
	}

	sort.Slice(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})

	return times
}

// PostNow immediately executes one tweet. Useful for testing.
func (s *Scheduler) PostNow(ctx context.Context) error {
	return s.executeTweet(ctx)
}

// executeTweet runs the full tweet generation pipeline.
func (s *Scheduler) executeTweet(ctx context.Context) error {
	category := s.pickCategory()
	slog.Info("tweet category selected", "category", category)

	prompt, ok := gemini.CategoryPrompts[category]
	if !ok {
		return fmt.Errorf("unknown category: %s", category)
	}

	var tweetText string
	for i := 0; i < maxRetries; i++ {
		raw, err := s.gemini.Generate(ctx, gemini.SystemPrompt, prompt, s.cfg.Gemini.Temperature)
		if err != nil {
			return fmt.Errorf("generating tweet: %w", err)
		}

		text := gemini.Humanize(raw)

		if text == "" {
			slog.Warn("empty tweet after humanization, retrying", "attempt", i+1)
			continue
		}

		if s.history.IsTooSimilar(text) {
			slog.Warn("generated tweet too similar, retrying", "attempt", i+1)
			continue
		}

		tweetText = text
		break
	}

	if tweetText == "" {
		return fmt.Errorf("failed to generate unique tweet after %d retries", maxRetries)
	}

	var mediaID string
	hasGIF := false
	if category == gemini.CategoryFunny && rand.Intn(100) < s.cfg.GIF.AttachProbability {
		gifMediaID, err := s.attachGIF(ctx, tweetText)
		if err != nil {
			slog.Warn("gif attachment failed, posting without gif", "error", err)
		} else {
			mediaID = gifMediaID
			hasGIF = true
		}
	}

	tweetID, err := s.twitter.PostTweet(ctx, tweetText, mediaID)
	if err != nil {
		return fmt.Errorf("posting tweet: %w", err)
	}

	entry := history.Entry{
		ID:       tweetID,
		Text:     tweetText,
		Category: category,
		HasGIF:   hasGIF,
		PostedAt: time.Now(),
	}

	if err := s.history.Add(entry); err != nil {
		slog.Error("failed to record tweet in history", "error", err)
	}

	slog.Info("tweet posted",
		"event", "tweet_posted",
		"tweet_id", tweetID,
		"category", category,
		"has_gif", hasGIF,
		"text", tweetText,
	)

	return nil
}

// attachGIF generates a search query, finds a GIF, downloads it, and uploads to Twitter.
func (s *Scheduler) attachGIF(ctx context.Context, tweetText string) (string, error) {
	queryPrompt := fmt.Sprintf(gemini.GIFQueryPrompt, tweetText)
	query, err := s.gemini.Generate(ctx, "You generate short GIF search queries.", queryPrompt, 0.8)
	if err != nil {
		return "", fmt.Errorf("generating gif query: %w", err)
	}

	slog.Info("gif search query generated", "query", query)

	results, err := s.klipy.SearchGIF(ctx, query, 5)
	if err != nil {
		return "", fmt.Errorf("searching gifs: %w", err)
	}

	if len(results) == 0 {
		return "", fmt.Errorf("no gifs found for query: %s", query)
	}

	// Pick a random GIF from results that's under the size limit.
	var selected *klipy.GIFResult
	for _, r := range results {
		if r.Size <= s.cfg.GIF.MaxSizeBytes {
			r := r
			selected = &r
			break
		}
	}
	if selected == nil {
		return "", fmt.Errorf("all gifs exceed size limit of %d bytes", s.cfg.GIF.MaxSizeBytes)
	}

	gifData, err := s.klipy.DownloadGIF(ctx, selected.URL)
	if err != nil {
		return "", fmt.Errorf("downloading gif: %w", err)
	}

	mediaID, err := s.twitter.UploadGIF(ctx, gifData)
	if err != nil {
		return "", fmt.Errorf("uploading gif to twitter: %w", err)
	}

	return mediaID, nil
}

// pickCategory selects a tweet category based on the configured weights.
func (s *Scheduler) pickCategory() string {
	total := s.cfg.ContentMix.FunnyMeme + s.cfg.ContentMix.Technical + s.cfg.ContentMix.Personal
	if total <= 0 {
		return gemini.CategoryFunny
	}

	roll := rand.Intn(total)
	switch {
	case roll < s.cfg.ContentMix.FunnyMeme:
		return gemini.CategoryFunny
	case roll < s.cfg.ContentMix.FunnyMeme+s.cfg.ContentMix.Technical:
		return gemini.CategoryTechnical
	default:
		return gemini.CategoryPersonal
	}
}

// parseTime parses an "HH:MM" string into hours and minutes.
func parseTime(s string) (int, int) {
	var h, m int
	fmt.Sscanf(s, "%d:%d", &h, &m)
	return h, m
}

// formatTimes returns a slice of RFC3339 strings for logging.
func formatTimes(times []time.Time) []string {
	strs := make([]string, len(times))
	for i, t := range times {
		strs[i] = t.Format(time.RFC3339)
	}
	return strs
}
