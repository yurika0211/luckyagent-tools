package compact

import (
	"github.com/yurika0211/luckyagent-tools/textutil"
	"strings"
)

var truncate = textutil.Truncate

func (o TextOptions) summarizeTool(name, result string) string {
	if o.SummarizeTool != nil {
		return o.SummarizeTool(name, result)
	}
	return truncate(strings.TrimSpace(result), 120)
}

func (o TextOptions) fallbackGoal() string {
	if strings.TrimSpace(o.FallbackGoal) != "" {
		return o.FallbackGoal
	}
	return "Continue the current conversation from the available prior messages."
}

func (o TextOptions) mergeFallbackGoal() string {
	if strings.TrimSpace(o.MergeFallbackGoal) != "" {
		return o.MergeFallbackGoal
	}
	return "Continue the current conversation from prior segment summaries."
}

func agentLabel(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "agent"
}
