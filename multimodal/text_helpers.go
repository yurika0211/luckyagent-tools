package multimodal

import utils "github.com/yurika0211/luckyagent-tools/textutil"

func truncateString(s string, maxLen int) string {
	return utils.Truncate(s, maxLen)
}
