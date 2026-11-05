package compact

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// A transcript line is evidence. The next turn may need a path, an error
// string, a commit, or one numeric value that happens to sit in the middle of
// a long result, and a positional head or tail cut drops exactly those lines.
// Compaction therefore drops the most redundant lines and never drops a line
// that carries an identifier.
//
// The redundancy score is a deterministic stand-in for a conditional-entropy
// ranking. This package has no frozen proxy model, and the rules below can
// only remove blank, repeated, and progress lines, so they cannot remove
// evidence by mistake.
type lineKind int

const (
	lineRedundant lineKind = iota
	lineNormal
	lineProtected
)

type scoredLine struct {
	index int
	text  string
	kind  lineKind
	score float64
	runes int
}

var (
	diffHunkPattern = regexp.MustCompile(`^(?:@@ -\d|\+\+\+ |--- |diff --git )`)
	commitPattern   = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	valuePattern    = regexp.MustCompile(`(?:\b0x[0-9a-fA-F]+|\b\d+\.\d+|\b\d+%|\b\d{4,})`)
	pathPattern     = regexp.MustCompile(`(?:^|[\s"'(=:\[,])(?:/[\w.\-]+/[^\s"')\]]*|(?:\.{1,2}/)[^\s"')\]]+|~/[^\s"')\]]+|[A-Za-z]:\\[^\s"')\]]+)`)
	urlPattern      = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"')\]]+`)
	errorPattern    = regexp.MustCompile(`(?i)\b(?:error|panic|exception|traceback|fatal|failed|failure|denied|not found|exit (?:status|code))\b`)
	progressPattern = regexp.MustCompile(`(?i)^\s*(?:\[[^\]]{0,40}\]\s*)?(?:\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2})?\s*)?(?:\d+%|\.{3,}|…|\[[#=>. -]{3,}\]|loading|fetching|processing|working|thinking|done\.?)?\s*$`)
)

// commandVerbs are first tokens that mark a line as a command. A command is
// reproducible evidence: it records what was run.
var commandVerbs = map[string]bool{
	"go": true, "git": true, "npm": true, "npx": true, "pnpm": true, "yarn": true,
	"docker": true, "make": true, "curl": true, "wget": true, "ssh": true, "scp": true,
	"python": true, "python3": true, "pip": true, "node": true, "cargo": true, "rustc": true,
	"bash": true, "sh": true, "zsh": true, "pwsh": true, "powershell": true,
	"kubectl": true, "systemctl": true, "journalctl": true, "rg": true, "grep": true,
	"sed": true, "awk": true, "find": true, "ls": true, "cat": true, "rm": true,
	"mv": true, "cp": true, "mkdir": true, "chmod": true, "chown": true, "tar": true,
	"psql": true, "sqlite3": true, "redis-cli": true, "terraform": true, "helm": true,
}

// isRepetitionLine reports whether a line only repeats one drawing character,
// as progress bars and separators do.
func isRepetitionLine(line string) bool {
	if len(line) < 3 {
		return false
	}
	first := line[0]
	if !strings.ContainsRune("#=*-_+", rune(first)) {
		return false
	}
	for i := 1; i < len(line); i++ {
		if line[i] != first {
			return false
		}
	}
	return true
}

// classifyLine decides whether a line may be dropped. Redundancy wins over
// identifier detection so a timestamped progress line stays droppable even
// though it contains digits.
func classifyLine(line string) lineKind {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || isRepetitionLine(trimmed) {
		return lineRedundant
	}
	if progressPattern.MatchString(trimmed) {
		return lineRedundant
	}
	if lineHasIdentifier(line) {
		return lineProtected
	}
	return lineNormal
}

// lineHasIdentifier reports whether the line carries something the next turn
// cannot reconstruct from the remaining context.
func lineHasIdentifier(line string) bool {
	if urlPattern.MatchString(line) || pathPattern.MatchString(line) {
		return true
	}
	if diffHunkPattern.MatchString(strings.TrimSpace(line)) {
		return true
	}
	if errorPattern.MatchString(line) {
		return true
	}
	if valuePattern.MatchString(line) {
		return true
	}
	if commitPattern.MatchString(strings.ToLower(line)) {
		return true
	}
	return lineIsCommand(line)
}

// lineIsCommand reports whether the line starts with a shell command.
func lineIsCommand(line string) bool {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "$ ")
	trimmed = strings.TrimPrefix(trimmed, "> ")
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	return commandVerbs[strings.ToLower(fields[0])]
}

// lineRedundancy scores a droppable line; lower scores are dropped first. A
// repeated line keeps its first occurrence and makes later copies cheap to
// drop, which is what removes repeated table headers and echoed progress.
func lineRedundancy(line string, seen map[string]int, position, total int) float64 {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return 0
	}
	seen[trimmed]++
	if seen[trimmed] > 1 {
		return 0.1
	}
	if progressPattern.MatchString(trimmed) || isRepetitionLine(trimmed) {
		return 0.2
	}
	score := 1.0
	if length := float64(utf8.RuneCountInString(trimmed)); length > 0 {
		score += minFloat(length/240.0, 1.0)
	}
	if position == 0 || position == total-1 {
		score += 0.5
	}
	return score
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// minShrinkableLine is the size below which a line is not worth cutting again.
const minShrinkableLine = 64

// fitLines shortens text to at most limit runes. Lines that carry identifiers
// are never dropped, redundant lines are dropped first, at least one original
// line survives, retained lines keep their original order, and a single line
// that is still too long is cut in the middle rather than discarded.
//
// When protected lines alone exceed the budget the block stays intact and
// overBudget is true, so the caller can say so instead of losing evidence the
// next turn cannot rebuild.
func fitLines(text string, limit int) (string, bool) {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text, false
	}
	rawLines := strings.Split(text, "\n")
	lines := make([]scoredLine, len(rawLines))
	seen := make(map[string]int, len(rawLines))
	for i, line := range rawLines {
		kind := classifyLine(line)
		// A line that only repeats an earlier one carries nothing new, so it
		// is redundant even when the first copy looked like ordinary content.
		// The first copy stays, which is what keeps one table header.
		if kind != lineProtected && seen[strings.TrimSpace(line)] > 0 {
			kind = lineRedundant
		}
		score := 0.0
		if kind != lineProtected {
			score = lineRedundancy(line, seen, i, len(rawLines))
		}
		lines[i] = scoredLine{index: i, text: line, kind: kind, score: score, runes: utf8.RuneCountInString(line)}
	}

	kept := make([]bool, len(lines))
	nonProtected := 0
	for _, line := range lines {
		kept[line.index] = true
		if line.kind != lineProtected {
			nonProtected++
		}
	}

	dropped := 0
	firstDropped, lastDropped := -1, -1
	blockRunes := func() int {
		total, first := 0, true
		for _, line := range lines {
			if !kept[line.index] {
				continue
			}
			if !first {
				total++
			}
			first = false
			total += line.runes
		}
		if dropped > 0 {
			total += 1 + utf8.RuneCountInString(omissionRecord(firstDropped, lastDropped, dropped))
		}
		return total
	}
	drop := func(index int) {
		if !kept[index] {
			return
		}
		kept[index] = false
		if lines[index].kind != lineProtected {
			nonProtected--
		}
		dropped++
		if firstDropped < 0 || index < firstDropped {
			firstDropped = index
		}
		if index > lastDropped {
			lastDropped = index
		}
	}

	// Redundant lines go first: blank, repeated, and progress lines carry no
	// evidence, so dropping them costs nothing the next turn can use. They are
	// dropped whenever the block is over budget, not only until a reserve is
	// met, because leaving them in forces a later cut through a real line.
	for _, line := range cheapestFirst(lines) {
		if blockRunes() <= limit {
			break
		}
		if line.kind != lineRedundant || nonProtected <= 1 {
			continue
		}
		drop(line.index)
	}

	// A single long line can still dominate the budget. Shrink it in place so
	// the shorter lines around it survive intact. A protected line is shrunk
	// from the middle rather than dropped whole, and only once: shrinking it
	// again would eat the identifier the protection exists to keep.
	shrunkOnce := make(map[int]bool, len(lines))
	for blockRunes() > limit {
		longest, index := -1, -1
		for _, line := range lines {
			if !kept[line.index] {
				continue
			}
			if line.runes > longest {
				longest, index = line.runes, line.index
			}
		}
		if index < 0 || longest <= minShrinkableLine || shrunkOnce[index] {
			break
		}
		// A protected line is kept whole. Cutting it to meet the budget would
		// remove the identifier the protection exists for, and a line that is
		// itself one identifier has no redundant middle to cut.
		if lines[index].kind == lineProtected {
			break
		}
		shrunkOnce[index] = true
		target := longest / 2
		if over := blockRunes() - limit; over > 0 && longest-over-1 > target {
			target = longest - over - 1
		}
		if target < minShrinkableLine/2 {
			target = minShrinkableLine / 2
		}
		shrunk := shrinkLine(lines[index].text, target)
		if utf8.RuneCountInString(shrunk) >= lines[index].runes {
			break
		}
		lines[index].text = shrunk
		lines[index].runes = utf8.RuneCountInString(shrunk)
	}

	// Only then drop ordinary content lines, cheapest first, keeping one.
	for _, line := range cheapestFirst(lines) {
		if blockRunes() <= limit {
			break
		}
		if line.kind == lineProtected || nonProtected <= 1 {
			continue
		}
		drop(line.index)
	}

	parts := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if kept[line.index] {
			parts = append(parts, line.text)
		}
	}
	compacted := strings.Join(parts, "\n")
	if dropped > 0 {
		compacted += "\n" + omissionRecord(firstDropped, lastDropped, dropped)
	}
	over := utf8.RuneCountInString(compacted) > limit
	return compacted, over
}

// omissionRecord is the pointer a folded block keeps to its original text.
// The range is the original line span, so a reader can ask for those lines
// back instead of guessing what was removed.
func omissionRecord(first, last, dropped int) string {
	if first < 0 || last < first {
		return fmt.Sprintf("... (%d lines omitted for context; the full output stays in the session transcript) ...", dropped)
	}
	return fmt.Sprintf("... (%d lines omitted for context, original lines %d-%d; the full output stays in the session transcript) ...", dropped, first+1, last+1)
}

// fitTranscript shortens a rendered transcript to limit runes. Unlike fitLines
// it does not protect lines, because the lines here are already rendered
// messages and a message that mentions a path is not itself an identifier.
// The tail is kept: the most recent messages are the ones the next turn needs.
func fitTranscript(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	raw := strings.Split(text, "\n")
	runes := make([]int, len(raw))
	total := 0
	for i, line := range raw {
		runes[i] = utf8.RuneCountInString(line)
		total += runes[i]
		if i > 0 {
			total++
		}
	}
	dropped := 0
	for total > limit && dropped < len(raw)-1 {
		total -= runes[dropped]
		if dropped+1 < len(raw) {
			total--
		}
		dropped++
	}
	if dropped == 0 {
		return truncateMiddle(text, limit)
	}
	kept := strings.Join(raw[dropped:], "\n")
	note := fmt.Sprintf("... (%d earlier transcript lines omitted for context) ...\n", dropped)
	if utf8.RuneCountInString(note+kept) > limit {
		kept = truncateMiddle(kept, limit-utf8.RuneCountInString(note))
	}
	return note + kept
}

// cheapestFirst orders lines by redundancy score, then by original position so
// the result is deterministic.
func cheapestFirst(lines []scoredLine) []scoredLine {
	ordered := make([]scoredLine, len(lines))
	copy(ordered, lines)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score < ordered[j].score
		}
		return ordered[i].index < ordered[j].index
	})
	return ordered
}

// shrinkLine shortens one line. A line that carries an identifier keeps the
// head and the tail, because the identifier is usually at one end and a cut
// that keeps only the middle would drop it. A plain line is cut in the middle
// so both ends of a long sentence survive.
func shrinkLine(line string, limit int) string {
	if lineHasIdentifier(line) {
		return truncateEnds(line, limit)
	}
	return truncateMiddle(line, limit)
}

// truncateEnds keeps the head and tail of one line and marks the cut.
func truncateEnds(line string, limit int) string {
	runes := []rune(line)
	if limit <= 0 || len(runes) <= limit {
		return line
	}
	const marker = " ... "
	markerRunes := []rune(marker)
	if len(markerRunes) >= limit {
		return string(runes[:limit])
	}
	room := limit - len(markerRunes)
	head := room / 2
	tail := room - head
	return string(runes[:head]) + marker + string(runes[len(runes)-tail:])
}

// truncateMiddle keeps the head and tail of an oversized block. It is only
// reached when line-level dropping cannot meet the budget, so the marker stays
// explicit about what was cut.
func truncateMiddle(out string, limit int) string {
	runes := []rune(out)
	if limit <= 0 || len(runes) <= limit {
		return out
	}
	marker := fmt.Sprintf(
		"\n\n... (middle omitted for context: %d chars; do not rely on omitted details without another tool check) ...\n\n",
		len(runes)-limit,
	)
	markerRunes := []rune(marker)
	if len(markerRunes) >= limit {
		return string(runes[:limit])
	}
	room := limit - len(markerRunes)
	head := room * 2 / 3
	tail := room - head
	return string(runes[:head]) + marker + string(runes[len(runes)-tail:])
}
