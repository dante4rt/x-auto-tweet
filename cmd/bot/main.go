package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/dantezy/x-auto-tweet/internal/config"
	"github.com/dantezy/x-auto-tweet/internal/gemini"
	"github.com/dantezy/x-auto-tweet/internal/history"
	"github.com/dantezy/x-auto-tweet/internal/scheduler"
	"github.com/dantezy/x-auto-tweet/internal/klipy"
	"github.com/dantezy/x-auto-tweet/internal/twitter"
)

func main() {
	postNow := flag.Bool("now", false, "post one tweet immediately and exit")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("starting x-auto-tweet bot")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	geminiClient, err := gemini.NewClient(cfg.GeminiAPIKey, cfg.Gemini.Model)
	if err != nil {
		slog.Error("failed to create gemini client", "error", err)
		os.Exit(1)
	}
	defer geminiClient.Close()

	twitterClient := twitter.NewClient(
		cfg.Twitter.APIKey,
		cfg.Twitter.APISecret,
		cfg.Twitter.AccessToken,
		cfg.Twitter.AccessTokenSecret,
	)

	klipyClient := klipy.NewClient(cfg.KlipyAPIKey)

	historyStore, err := history.NewStore(
		"data/tweet_history.json",
		cfg.History.MaxEntries,
		cfg.History.SimilarityThreshold,
	)
	if err != nil {
		slog.Error("failed to create history store", "error", err)
		os.Exit(1)
	}

	sched, err := scheduler.New(cfg, geminiClient, twitterClient, klipyClient, historyStore)
	if err != nil {
		slog.Error("failed to create scheduler", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if *postNow {
		slog.Info("posting one tweet immediately (--now)")
		if err := sched.PostNow(ctx); err != nil {
			slog.Error("tweet failed", "error", err)
			os.Exit(1)
		}
		slog.Info("tweet posted, exiting")
		return
	}

	slog.Info("bot initialized, starting scheduler")

	if err := sched.Run(ctx); err != nil {
		if ctx.Err() != nil {
			slog.Info("bot shut down gracefully")
			return
		}
		slog.Error("scheduler exited with error", "error", err)
		os.Exit(1)
	}
}
