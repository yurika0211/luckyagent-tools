package compact

import (
	"regexp"
	"sort"
	"strings"
)

// Identifiers are the parts of a message the next turn cannot rebuild from
// prose: a path, a URL, a commit, a numeric value, and the first line of an
// error. A summary that drops one of them has lost evidence, even when it
// still reads like a summary.
var (
	identifierPathPattern   = regexp.MustCompile(`(?:^|[\s"'(=:\[,])((?:/[\w.\-]+){2,}/?|(?:\.{1,2}/)[^\s"')\]]+|~/[^\s"')\]]+|[A-Za-z]:\\[^\s"')\]]+)`)
	identifierURLPattern    = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"')\]]+`)
	identifierCommitPattern = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	identifierValuePattern  = regexp.MustCompile(`(?:\b0x[0-9a-fA-F]+|\b\d+\.\d+|\b\d+%|\b\d{4,})`)
)

// MaxIdentifiers bounds one extraction so a large tool dump cannot turn one
// summary into thousands of required substrings.
const MaxIdentifiers = 40

// maxIdentifierErrorHeadRunes bounds one captured error line.
const maxIdentifierErrorHeadRunes = 160

// Identifiers returns the identifiers of the messages a summary covers.
// Paths, URLs, commits, and values are mutually exclusive shapes, and the
// result is deduplicated and ordered so a failure can name a stable identifier.
//
// This is the only extractor. Callers that already check identifiers themselves
// should call it instead of keeping a second copy.
func Identifiers(messages []Message) []string {
	seen := make(map[string]struct{}, MaxIdentifiers)
	out := make([]string, 0, MaxIdentifiers)
	add := func(candidate string) {
		candidate = strings.TrimSpace(strings.Trim(candidate, "\"'(),[]<>."))
		if len(candidate) < 3 || len([]rune(candidate)) > 200 {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	for _, message := range messages {
		for _, line := range strings.Split(message.Content, "\n") {
			// The first line of an error is what a follow-up turn re-reads, so
			// it counts as an identifier even without a path or a value on it.
			if !errorPattern.MatchString(line) {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if runes := []rune(trimmed); len(runes) > maxIdentifierErrorHeadRunes {
				trimmed = string(runes[:maxIdentifierErrorHeadRunes])
			}
			add(trimmed)
		}
		for _, match := range identifierURLPattern.FindAllString(message.Content, -1) {
			add(match)
		}
		for _, match := range identifierPathPattern.FindAllStringSubmatch(message.Content, -1) {
			if len(match) > 1 {
				add(match[1])
			}
		}
		for _, match := range identifierCommitPattern.FindAllString(strings.ToLower(message.Content), -1) {
			if len(match) >= 7 && len(match) <= 40 {
				add(match)
			}
		}
		for _, match := range identifierValuePattern.FindAllString(message.Content, -1) {
			add(match)
		}
	}
	sort.Strings(out)
	if len(out) > MaxIdentifiers {
		out = out[:MaxIdentifiers]
	}
	return out
}

// MissingIdentifiers reports which required identifiers the summary does not
// mention. Matching is case-insensitive because a summary may reflow casing.
func MissingIdentifiers(summary string, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	lower := strings.ToLower(summary)
	missing := make([]string, 0, len(required))
	for _, identifier := range required {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		if !strings.Contains(lower, strings.ToLower(identifier)) {
			missing = append(missing, identifier)
		}
	}
	return missing
}
