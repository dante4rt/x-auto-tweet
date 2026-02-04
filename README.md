# x-auto-tweet

Go bot that auto-tweets 1-2x/day on X with AI-generated content via Gemini, optional GIF attachments via Klipy, tuned for the X algorithm.

## How It Works

```text
Category (weighted random) → Gemini generates tweet → Humanize (strip AI artifacts)
→ Duplicate check (Jaccard similarity) → Optional GIF (Klipy) → Post to X
```

- **60%** funny/meme, **30%** technical, **10%** personal tweets
- Tweets scheduled at random times within a configurable daily window
- GIFs attached to ~40% of funny tweets
- History stored in JSON, checked for duplicates before posting

## Setup

### 1. API Keys

| Service   | Get Key At                                                          |
| --------- | ------------------------------------------------------------------- |
| X/Twitter | [developer.x.com](https://developer.x.com) (OAuth 1.0a, Read+Write) |
| Gemini    | [aistudio.google.com](https://aistudio.google.com/apikey)           |
| Klipy     | [klipy.com/docs](https://klipy.com/docs)                            |

### 2. Configure

```bash
cp .env.example .env
# Fill in your API keys
```

> [!IMPORTANT]
> Your X app must have **Read and Write** permissions. After changing permissions, **regenerate** your Access Token and Secret.

### 3. Run

```bash
# Docker (recommended)
docker compose up -d

# Local
go build -o bot ./cmd/bot && ./bot
```

### Test with a single tweet

```bash
# Docker
docker compose run bot /app/bot --now

# Local
./bot --now
```

## Configuration

Edit `config.yaml` to tune behavior:

```yaml
schedule:
  tweets_per_day: 2
  window_start: "09:00"
  window_end: "22:00"
  timezone: "Asia/Jakarta"
  min_gap_minutes: 120

content_mix:
  funny_meme: 60
  technical: 30
  personal: 10

gif:
  attach_probability: 40
  max_size_bytes: 5242880

gemini:
  model: "gemini-2.5-flash"
  temperature: 1.2
```

> [!TIP]
> Higher `temperature` = more creative/unpredictable tweets. Lower = safer/repetitive.

## Project Structure

```text
cmd/bot/main.go          # Entry point, --now flag, signal handling
internal/
  config/                # Viper config (YAML + env vars)
  scheduler/             # Daily planning, random times, pipeline orchestration
  gemini/                # Gemini client + persona prompts + Humanize()
  twitter/               # OAuth 1.0a tweet posting + chunked GIF upload
  klipy/                 # Klipy GIF API search + download
  history/               # JSON persistence + Jaccard similarity check
```

## License

MIT
