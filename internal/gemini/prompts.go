package gemini

import (
	"math/rand"
	"regexp"
	"strings"
	"unicode"
)

// Tweet category identifiers used for prompt selection.
const (
	CategoryFunny     = "funny_meme"
	CategoryTechnical = "technical"
	CategoryPersonal  = "personal"
)

// SystemPrompt defines Rama's persona for tweet generation.
const SystemPrompt = `You are ghostwriting tweets for Rama, a web3 developer at Holdex, Arbitrum Ambassador, based in Jakarta. You ARE Rama - write in first person.

VOICE:
- You're texting a dev friend, not writing an essay
- Use fragments, run-on sentences, sudden topic shifts
- Sprinkle in: ngl, fr, tbh, lowkey, highkey, imo, icl (i can't lie)
- Mix English with occasional Bahasa slang: wkwk, anjir, ngab, gila, cuy
- Never capitalize the first letter unless it's a proper noun or acronym
- Use lowercase "i" sometimes
- Typos are ok occasionally

X ALGORITHM OPTIMIZATION:
- ~30% of tweets should end with a question to drive replies
- Make quotable observations people want to RT
- Be specific (name actual protocols, languages, tools) not generic
- Mild controversy/hot takes get engagement

FORMATTING:
- X is PLAIN TEXT ONLY - no rendering of any kind
- NEVER use backticks, asterisks, underscores, markdown, or code blocks
- NEVER write actual code syntax in tweets (no require(), no function(), no semicolons)
- If referencing code concepts, describe them in plain words
- Keep tweets SHORT and punchy - 1-2 sentences max
- Use line breaks to separate thoughts when needed (just a newline)

HARD RULES:
- NEVER use hashtags
- NEVER cluster more than 1 emoji per tweet (0 emojis preferred)
- NEVER write motivational platitudes
- NEVER say "As a", "It's important to", "In the world of", "Delve", "Landscape"
- NEVER use phrases like "just mass adoption things" or "ah yes, blockchain" sarcastically
- Keep under 200 characters (short tweets perform better)
- Output ONLY the tweet text, nothing else`

// CategoryPrompts maps each tweet category to its generation prompt.
var CategoryPrompts = map[string]string{
	CategoryFunny:     "Write a funny tweet about web3/crypto/dev life. Think: absurd Solidity observation at 11pm, the pain of gas fees, or frontend dev discovering smart contracts. Self-deprecating humor > mocking others. Can be slightly unhinged.",
	CategoryTechnical: "Write a technical tweet sharing a non-obvious insight about EVM, Solidity, TypeScript, or web3 infra. Write it like a comment in a Discord dev channel - casual but genuinely useful. Mention specific tools/concepts.",
	CategoryPersonal:  "Write a personal/lifestyle tweet about dev life in Jakarta. Could be about: late night coding, coffee shop productivity, conference travel, or a random observation. Keep it relatable and authentic.",
}

// GIFQueryPrompt is the prompt template for generating Tenor GIF search queries.
// Use with fmt.Sprintf to inject the tweet text at %s.
const GIFQueryPrompt = `Based on this tweet, generate a 2-3 word search query for finding a relevant funny GIF on Tenor. Output ONLY the search query, nothing else.

Tweet: %s`

// Pre-compiled patterns for AI tell-phrase removal, ordered longest-first
// to avoid partial matches when a shorter phrase is a substring of a longer one.
var aiTellRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bIt's important to\b`),
	regexp.MustCompile(`(?i)\bIn the world of\b`),
	regexp.MustCompile(`(?i)\bjust to be clear\b`),
	regexp.MustCompile(`(?i)\bDelve into\b`),
	regexp.MustCompile(`(?i)\bAs a\b`),
	regexp.MustCompile(`(?i)\bdelve\b`),
	regexp.MustCompile(`(?i)\blandscape\b`),
}

// emojiRe matches individual emoji codepoints across the major Unicode emoji blocks.
var emojiRe = regexp.MustCompile(
	`[\x{1F600}-\x{1F64F}` + // Emoticons
		`\x{1F300}-\x{1F5FF}` + // Miscellaneous Symbols and Pictographs
		`\x{1F680}-\x{1F6FF}` + // Transport and Map Symbols
		`\x{1F1E0}-\x{1F1FF}` + // Regional Indicator Symbols (flags)
		`\x{2600}-\x{26FF}` + // Miscellaneous Symbols
		`\x{2700}-\x{27BF}` + // Dingbats
		`\x{1F900}-\x{1F9FF}` + // Supplemental Symbols and Pictographs
		`\x{1FA00}-\x{1FAFF}]`, // Symbols and Pictographs Extended-A
)

// spaceRe collapses runs of whitespace into a single space.
var spaceRe = regexp.MustCompile(`\s{2,}`)

// maxTweetLen is the hard character limit for tweets.
const maxTweetLen = 280

// Humanize post-processes AI-generated tweet text to remove common
// artifacts and make the output feel more natural and on-brand.
func Humanize(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}

	// Remove surrounding quotes that the model sometimes wraps output in.
	if len(text) >= 2 {
		if (text[0] == '"' && text[len(text)-1] == '"') ||
			(text[0] == '\'' && text[len(text)-1] == '\'') {
			text = strings.TrimSpace(text[1 : len(text)-1])
		}
	}

	// Strip all markdown formatting - X is plain text only.
	text = strings.ReplaceAll(text, "```", "")
	text = strings.ReplaceAll(text, "`", "")
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "*", "")
	text = strings.ReplaceAll(text, "__", "")
	text = strings.ReplaceAll(text, "~~", "")

	// Strip AI tell phrases.
	for _, re := range aiTellRes {
		text = re.ReplaceAllString(text, "")
	}
	text = strings.TrimSpace(spaceRe.ReplaceAllString(text, " "))

	// Remove excess emojis: keep only the first occurrence.
	emojiCount := 0
	text = emojiRe.ReplaceAllStringFunc(text, func(match string) string {
		emojiCount++
		if emojiCount == 1 {
			return match
		}
		return ""
	})
	text = strings.TrimSpace(spaceRe.ReplaceAllString(text, " "))

	// Randomly lowercase the first character (30% chance).
	runes := []rune(text)
	if len(runes) > 0 && unicode.IsUpper(runes[0]) && rand.Intn(100) < 30 {
		runes[0] = unicode.ToLower(runes[0])
		text = string(runes)
	}

	// Remove trailing period (but preserve ? and !).
	if len(text) > 0 && text[len(text)-1] == '.' {
		text = text[:len(text)-1]
	}

	// Truncate to 280 characters at the last space before the limit.
	runes = []rune(text)
	if len(runes) > maxTweetLen {
		truncated := string(runes[:maxTweetLen])
		if idx := strings.LastIndex(truncated, " "); idx > 0 {
			text = truncated[:idx]
		} else {
			text = truncated
		}
	}

	return strings.TrimSpace(text)
}
