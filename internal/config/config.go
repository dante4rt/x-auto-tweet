package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/viper"
)

type ScheduleConfig struct {
	TweetsPerDay  int    `mapstructure:"tweets_per_day"`
	WindowStart   string `mapstructure:"window_start"`
	WindowEnd     string `mapstructure:"window_end"`
	Timezone      string `mapstructure:"timezone"`
	MinGapMinutes int    `mapstructure:"min_gap_minutes"`
}

type ContentMixConfig struct {
	FunnyMeme int `mapstructure:"funny_meme"`
	Technical int `mapstructure:"technical"`
	Personal  int `mapstructure:"personal"`
}

type GIFConfig struct {
	AttachProbability int   `mapstructure:"attach_probability"`
	MaxSizeBytes      int64 `mapstructure:"max_size_bytes"`
}

type GeminiConfig struct {
	Model       string  `mapstructure:"model"`
	Temperature float32 `mapstructure:"temperature"`
}

type HistoryConfig struct {
	MaxEntries          int     `mapstructure:"max_entries"`
	SimilarityThreshold float64 `mapstructure:"similarity_threshold"`
}

type TwitterConfig struct {
	APIKey            string `mapstructure:"api_key"`
	APISecret         string `mapstructure:"api_secret"`
	AccessToken       string `mapstructure:"access_token"`
	AccessTokenSecret string `mapstructure:"access_token_secret"`
}

type Config struct {
	Schedule    ScheduleConfig   `mapstructure:"schedule"`
	ContentMix  ContentMixConfig `mapstructure:"content_mix"`
	GIF         GIFConfig        `mapstructure:"gif"`
	Gemini      GeminiConfig     `mapstructure:"gemini"`
	History     HistoryConfig    `mapstructure:"history"`
	Twitter     TwitterConfig    `mapstructure:"twitter"`
	GeminiAPIKey string          `mapstructure:"gemini_api_key"`
	KlipyAPIKey  string          `mapstructure:"klipy_api_key"`
}

func Load() (*Config, error) {
	v := viper.New()

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("/app")

	// Bind environment variables for secrets
	envBindings := map[string]string{
		"twitter.api_key":            "TWITTER_API_KEY",
		"twitter.api_secret":         "TWITTER_API_SECRET",
		"twitter.access_token":       "TWITTER_ACCESS_TOKEN",
		"twitter.access_token_secret": "TWITTER_ACCESS_TOKEN_SECRET",
		"gemini_api_key":             "GEMINI_API_KEY",
		"klipy_api_key":              "KLIPY_API_KEY",
	}

	for key, env := range envBindings {
		if err := v.BindEnv(key, env); err != nil {
			return nil, fmt.Errorf("binding env var %s: %w", env, err)
		}
	}

	// Set defaults
	v.SetDefault("schedule.tweets_per_day", 2)
	v.SetDefault("schedule.window_start", "09:00")
	v.SetDefault("schedule.window_end", "22:00")
	v.SetDefault("schedule.timezone", "Asia/Jakarta")
	v.SetDefault("schedule.min_gap_minutes", 120)

	v.SetDefault("content_mix.funny_meme", 60)
	v.SetDefault("content_mix.technical", 30)
	v.SetDefault("content_mix.personal", 10)

	v.SetDefault("gif.attach_probability", 40)
	v.SetDefault("gif.max_size_bytes", int64(5242880))

	v.SetDefault("gemini.model", "gemini-2.5-flash")
	v.SetDefault("gemini.temperature", float32(1.2))

	v.SetDefault("history.max_entries", 500)
	v.SetDefault("history.similarity_threshold", 0.3)

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
		slog.Info("no config file found, using defaults and env vars")
	} else {
		slog.Info("loaded config file", "path", v.ConfigFileUsed())
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	var missing []string

	if cfg.Twitter.APIKey == "" {
		missing = append(missing, "TWITTER_API_KEY")
	}
	if cfg.Twitter.APISecret == "" {
		missing = append(missing, "TWITTER_API_SECRET")
	}
	if cfg.Twitter.AccessToken == "" {
		missing = append(missing, "TWITTER_ACCESS_TOKEN")
	}
	if cfg.Twitter.AccessTokenSecret == "" {
		missing = append(missing, "TWITTER_ACCESS_TOKEN_SECRET")
	}
	if cfg.GeminiAPIKey == "" {
		missing = append(missing, "GEMINI_API_KEY")
	}
	if cfg.KlipyAPIKey == "" {
		missing = append(missing, "KLIPY_API_KEY")
	}

	if len(missing) > 0 {
		return fmt.Errorf("required env vars not set: %s", strings.Join(missing, ", "))
	}

	return nil
}
