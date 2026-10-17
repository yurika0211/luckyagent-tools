// Package skillroute matches natural-language requests to immutable skill descriptions.
// It does not load files, select model tools, or execute a matched skill.
package skillroute

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type Descriptor struct {
	Name        string
	Aliases     []string
	Description string
	Summary     string
}

type Result struct {
	Skill    Descriptor
	Index    int
	Score    int
	Explicit bool
	Reason   string
}

// Match returns nil when no strong, unambiguous match exists. Index refers to
// the caller's original descriptor slice, including empty descriptors.
func Match(userInput string, skills []Descriptor) *Result {
	input := strings.TrimSpace(strings.ToLower(userInput))
	if input == "" || len(skills) == 0 {
		return nil
	}
	tokens := expandSkillRouteTokens(tokenizeSkillRouteText(input))
	if len(tokens) == 0 {
		return nil
	}
	var matches []*Result
	for i, skill := range skills {
		if strings.TrimSpace(skill.Name) == "" {
			continue
		}
		if match := scoreSkillRoute(skill, input, tokens); match != nil {
			match.Index = i
			matches = append(matches, match)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	slices.SortFunc(matches, func(a, b *Result) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if a.Explicit != b.Explicit {
			if a.Explicit {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Skill.Name, b.Skill.Name)
	})
	best := matches[0]
	if best.Explicit {
		return best
	}
	if best.Score < 3 {
		return nil
	}
	if len(matches) > 1 && best.Score <= matches[1].Score {
		return nil
	}
	return best
}

func filterNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, s := range values {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

var skillRouteTokenRe = regexp.MustCompile(`[a-zA-Z0-9_+\-]+|[\p{Han}]{2,}`)

var skillRouteStopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "from": {}, "into": {}, "this": {}, "that": {},
	"use": {}, "using": {}, "need": {}, "want": {}, "help": {}, "please": {}, "about": {}, "then": {},
	"user": {}, "task": {}, "workflow": {}, "skill": {}, "when": {}, "apply": {}, "through": {}, "should": {},
	"一个": {}, "这个": {}, "那个": {}, "一下": {}, "帮我": {}, "需要": {}, "想要": {}, "使用": {}, "技能": {},
	"工作流": {}, "一下子": {}, "然后": {}, "什么": {}, "怎么": {}, "可以": {},
}

var skillRouteConceptMap = map[string][]string{
	"research":      {"调研", "研究", "资料", "来源", "报告"},
	"deep-research": {"调研", "深度调研", "研究", "资料", "来源", "报告"},
	"report":        {"报告", "汇报", "总结"},
	"evidence":      {"证据", "依据", "来源"},
	"source":        {"来源", "资料"},
	"obsidian":      {"笔记", "知识库", "vault", "markdown", "wikilink", "标签"},
	"note":          {"笔记", "便签"},
	"notes":         {"笔记", "便签"},
	"vault":         {"知识库", "仓库", "库"},
	"tag":           {"标签", "tag"},
	"tags":          {"标签", "tag"},
	"wikilink":      {"双链", "wikilink", "链接"},
	"telegram":      {"电报", "tg", "telegram", "机器人", "bot"},
	"bot":           {"机器人", "bot"},
	"workflow":      {"工作流", "流程"},
	"markdown":      {"markdown", "md"},
	"task":          {"任务", "待办"},
	"tasks":         {"任务", "待办"},
	"vikunja":       {"vikunja", "任务", "待办"},
	"calendar":      {"日历", "calendar"},
	"email":         {"邮件", "邮箱", "email"},
	"pdf":           {"pdf", "文档"},
	"image":         {"图片", "图像", "配图"},
	"images":        {"图片", "图像", "配图"},
	"video":         {"视频", "video"},
	"audio":         {"音频", "声音", "audio"},
	"weather":       {"天气", "气温", "预报"},
}

func scoreSkillRoute(skill Descriptor, loweredInput string, inputTokens []string) *Result {
	names := []string{skill.Name}
	names = append(names, skill.Aliases...)

	explicitHit := ""
	for _, name := range names {
		norm := strings.ToLower(strings.TrimSpace(name))
		if norm == "" {
			continue
		}
		if strings.Contains(loweredInput, norm) {
			explicitHit = norm
			break
		}
		normHyphen := strings.ReplaceAll(norm, "_", "-")
		if normHyphen != norm && strings.Contains(loweredInput, normHyphen) {
			explicitHit = normHyphen
			break
		}
	}

	summary := skill.Summary
	corpus := strings.Join(filterNonEmpty([]string{
		skill.Name,
		strings.Join(skill.Aliases, " "),
		skill.Description,
		summary,
	}), " ")
	corpusTokens := expandSkillRouteTokens(tokenizeSkillRouteText(strings.ToLower(corpus)))
	if len(corpusTokens) == 0 && explicitHit == "" {
		return nil
	}

	score := 0
	reasons := make([]string, 0, 3)
	if explicitHit != "" {
		score += 100
		reasons = append(reasons, fmt.Sprintf("input explicitly mentions %q", explicitHit))
	}

	seenReason := make(map[string]struct{})
	corpusSet := make(map[string]struct{}, len(corpusTokens))
	for _, token := range corpusTokens {
		corpusSet[token] = struct{}{}
	}
	for _, token := range inputTokens {
		if _, ok := corpusSet[token]; !ok {
			continue
		}
		if len([]rune(token)) >= 4 {
			score += 2
		} else {
			score += 1
		}
		if _, ok := seenReason[token]; !ok && len(reasons) < 3 {
			reasons = append(reasons, fmt.Sprintf("keyword overlap on %q", token))
			seenReason[token] = struct{}{}
		}
	}

	if score == 0 {
		return nil
	}

	return &Result{
		Skill:    skill,
		Score:    score,
		Explicit: explicitHit != "",
		Reason:   strings.Join(reasons, "; "),
	}
}

func tokenizeSkillRouteText(input string) []string {
	raw := skillRouteTokenRe.FindAllString(strings.ToLower(input), -1)
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if _, stop := skillRouteStopwords[token]; stop {
			continue
		}
		if len([]rune(token)) < 2 {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	return tokens
}

func expandSkillRouteTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokens)*2)
	expanded := make([]string, 0, len(tokens)*2)
	for _, token := range tokens {
		token = strings.TrimSpace(strings.ToLower(token))
		if token == "" {
			continue
		}
		if _, ok := seen[token]; !ok {
			seen[token] = struct{}{}
			expanded = append(expanded, token)
		}
		if related, ok := skillRouteConceptMap[token]; ok {
			for _, alt := range related {
				alt = strings.TrimSpace(strings.ToLower(alt))
				if alt == "" {
					continue
				}
				if _, ok := seen[alt]; ok {
					continue
				}
				seen[alt] = struct{}{}
				expanded = append(expanded, alt)
			}
		}
		for concept, related := range skillRouteConceptMap {
			if token == concept || !strings.Contains(token, concept) {
				continue
			}
			if _, ok := seen[concept]; !ok {
				seen[concept] = struct{}{}
				expanded = append(expanded, concept)
			}
			for _, alt := range related {
				alt = strings.TrimSpace(strings.ToLower(alt))
				if alt == "" {
					continue
				}
				if _, ok := seen[alt]; ok {
					continue
				}
				seen[alt] = struct{}{}
				expanded = append(expanded, alt)
			}
		}
		for concept, related := range skillRouteConceptMap {
			matched := false
			for _, alt := range related {
				alt = strings.TrimSpace(strings.ToLower(alt))
				if alt == "" || !strings.Contains(token, alt) {
					continue
				}
				matched = true
				if _, ok := seen[concept]; !ok {
					seen[concept] = struct{}{}
					expanded = append(expanded, concept)
				}
			}
			if !matched {
				continue
			}
			for _, alt := range related {
				alt = strings.TrimSpace(strings.ToLower(alt))
				if alt == "" {
					continue
				}
				if _, ok := seen[alt]; ok {
					continue
				}
				seen[alt] = struct{}{}
				expanded = append(expanded, alt)
			}
		}
	}
	return expanded
}
