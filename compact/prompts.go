package compact

import (
	"github.com/yurika0211/luckyagent-tools/textutil"
	"strings"
)

func SegmentPrompt(transcript, agentName string) string {
	return "Summarize the conversation below so another " + agentLabel(agentName) + " instance can continue the current task.\n" +
		"Output plain text only under these headings:\n" +
		"Current user goal:\nCompleted work:\nPending work:\nKey files and functions:\nCommands and test results:\nUser constraints:\nUncertain facts:\n" +
		"Rules:\n" +
		"- Preserve exact file paths, commands, config keys, errors, decisions, and unresolved items.\n" +
		"- Do not invent commands, test results, files, or user preferences.\n" +
		"- Do not include generic advice or commentary.\n" +
		"- Do not request or use tools; this compaction must only produce text.\n" +
		"- If the transcript notes older lines were omitted, focus on the retained recent evidence.\n\n" +
		"Conversation:\n" + transcript
}

func MergePrompt(segmentBody, agentName string) string {
	return "Merge the segment summaries below into one compact summary for another " + agentLabel(agentName) + " instance.\n" +
		"Output plain text only under these headings:\n" +
		"Current user goal:\nCompleted work:\nPending work:\nKey files and functions:\nCommands and test results:\nUser constraints:\nUncertain facts:\n" +
		"Rules:\n" +
		"- Prefer the latest user goal when segments disagree.\n" +
		"- Keep concrete paths, commands, errors, decisions, and unresolved items.\n" +
		"- Deduplicate repeated facts; do not invent new work.\n" +
		"- Do not request or use tools; produce text only.\n\n" +
		"Segment summaries:\n" + segmentBody
}

func MergeLocal(segmentSummaries []string, opts TextOptions) string {
	if len(segmentSummaries) == 0 {
		return ""
	}
	if len(segmentSummaries) == 1 {
		// Strip optional ### Segment header.
		body := segmentSummaries[0]
		if idx := strings.Index(body, "\n"); idx >= 0 && strings.HasPrefix(strings.TrimSpace(body), "### ") {
			body = strings.TrimSpace(body[idx+1:])
		}
		return body
	}

	var goals, completed, pending, files, commands, constraints, uncertain []string
	for _, seg := range segmentSummaries {
		body := seg
		if idx := strings.Index(body, "\n"); idx >= 0 && strings.Contains(body[:idx], "### ") {
			body = strings.TrimSpace(body[idx+1:])
		}
		goals = append(goals, sectionBullets(body, "Current user goal")...)
		completed = append(completed, sectionBullets(body, "Completed work")...)
		pending = append(pending, sectionBullets(body, "Pending work")...)
		files = append(files, sectionBullets(body, "Key files and functions")...)
		commands = append(commands, sectionBullets(body, "Commands and test results")...)
		constraints = append(constraints, sectionBullets(body, "User constraints")...)
		uncertain = append(uncertain, sectionBullets(body, "Uncertain facts")...)
	}

	pickGoal := opts.mergeFallbackGoal()
	if len(goals) > 0 {
		pickGoal = strings.TrimPrefix(strings.TrimSpace(goals[len(goals)-1]), "- ")
	}

	var b strings.Builder
	b.WriteString("Current user goal:\n- " + pickGoal + "\n")
	writeLocalSection(&b, "Completed work", textutil.DedupStringsLimit(completed, 6), "- Prior segment work was recorded without reliable detail.")
	writeLocalSection(&b, "Pending work", textutil.DedupStringsLimit(pending, 4), "- Continue from the latest user goal and verify unfinished work.")
	writeLocalSection(&b, "Key files and functions", textutil.DedupStringsLimit(files, 12), "- No explicit file path was found in segment summaries.")
	writeLocalSection(&b, "Commands and test results", textutil.DedupStringsLimit(commands, 8), "- No explicit command or tool result was found in segment summaries.")
	writeLocalSection(&b, "User constraints", textutil.DedupStringsLimit(constraints, 4), "- Preserve the latest user instructions.")
	writeLocalSection(&b, "Uncertain facts", textutil.DedupStringsLimit(uncertain, 4), "- Segment merge used local rules; verify repository state before treating progress as complete.")
	return strings.TrimSpace(b.String())
}

func writeLocalSection(b *strings.Builder, title string, lines []string, fallback string) {
	b.WriteString(title + ":\n")
	if len(lines) == 0 {
		b.WriteString(fallback + "\n")
		return
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "-") {
			line = "- " + line
		}
		b.WriteString(line + "\n")
	}
}

func sectionBullets(summary, heading string) []string {
	summary = strings.TrimSpace(summary)
	if summary == "" || heading == "" {
		return nil
	}
	lower := strings.ToLower(summary)
	headLower := strings.ToLower(heading)
	idx := strings.Index(lower, headLower)
	if idx < 0 {
		return nil
	}
	rest := summary[idx:]
	// Skip heading line.
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:]
	} else {
		return nil
	}
	var out []string
	for _, line := range strings.Split(rest, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		// Next section heading (Title case with trailing colon).
		if strings.HasSuffix(trim, ":") && !strings.HasPrefix(trim, "-") && len(trim) < 80 {
			break
		}
		out = append(out, trim)
	}
	return out
}
