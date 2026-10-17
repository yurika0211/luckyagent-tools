package execpolicy

import "strings"

// AllowedTools derives a turn-local tool allowlist. An unrecognized request
// returns recognized=false so the caller can preserve its existing tool list.
func AllowedTools(input string, visibleSkillRunTools []string) (map[string]struct{}, bool) {
	allowed := map[string]struct{}{}
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return allowed, false
	}
	intentText := stripToolIntentNegations(text)

	noToolDirective := ContainsAny(text,
		"不用工具", "不需要工具", "不要用工具", "无需工具",
		"不需要查代码", "不用查代码", "不查代码",
	)
	explainIntent := ContainsAny(intentText,
		"解释", "讲解", "说明", "是什么意思", "是什么", "为什么", "区别",
		"公式", "推导", "归一化", "dcg", "route risk", "recalllift",
		"precision", "recall",
	)
	toolSystemIntent := hasToolSystemIntent(intentText)
	localIntent := hasLocalToolIntent(intentText) || toolSystemIntent
	editIntent := hasEditToolIntent(intentText)
	webIntent := hasWebToolIntent(intentText)
	timeIntent := hasTimeToolIntent(intentText)
	calcIntent := hasCalculationIntent(intentText)
	memoryIntent := hasMemoryIntent(intentText)
	memoryHygieneIntent := hasMemoryHygieneIntent(intentText)
	rememberIntent := hasRememberIntent(intentText)
	ragIndexIntent := hasRAGIndexIntent(intentText)
	skillIntent := hasSkillIntent(intentText)
	mediaIntent := hasMediaIntent(intentText) && !toolSystemIntent
	dbIntent := hasDatabaseIntent(intentText)
	delegateIntent := hasDelegateIntent(intentText)
	computerIntent := hasComputerUseIntent(intentText)
	codexIntent := ContainsAny(intentText, "codex", "app-server", "app server", "编码 agent", "代码代理")
	grokIntent := ContainsAny(intentText, "grok", "grok agent")

	strongToolIntent := localIntent || editIntent || webIntent || timeIntent || calcIntent ||
		memoryIntent || memoryHygieneIntent || rememberIntent || ragIndexIntent || skillIntent || mediaIntent ||
		dbIntent || delegateIntent || computerIntent || codexIntent || grokIntent
	if noToolDirective && !strongToolIntent {
		return allowed, true
	}
	if explainIntent && !strongToolIntent {
		return allowed, true
	}

	recognized := strongToolIntent
	negatedExecution := hasNegatedExecutionIntent(intentText)
	negatedDelete := ContainsAny(intentText, "不要直接删", "不要删", "别删", "不删除")
	readOnlyMutationBlocked := hasReadOnlyMutationBlockIntent(intentText)
	needsTerminal := needsTerminalIntent(intentText)
	readOnlySourceIntent := hasReadOnlyExternalSourceIntent(intentText)

	if localIntent || editIntent {
		addIntentTools(allowed, "file_read", "file_list")
		if (!negatedExecution || needsTerminal) && !readOnlySourceIntent && !readOnlyMutationBlocked {
			addIntentTools(allowed, "terminal")
		}
		if ContainsAny(intentText, "\u65e5\u5fd7", ".log", "logs", "log file", "stack trace", "trace", ".json", "json", ".yaml", ".yml", "yaml", "yml", ".csv", "csv") {
			addIntentTools(allowed, "data")
		}
		if ContainsAny(intentText, ".pdf", ".docx", ".pptx", "pdf", "docx", "pptx", "word", "powerpoint", "文档", "幻灯片") {
			addIntentTools(allowed, "document_read")
		}
		if toolSystemIntent {
			addIntentTools(allowed, "data")
		}
		if editIntent && !readOnlySourceIntent && !readOnlyMutationBlocked {
			addIntentTools(allowed, "file_patch", "file_write", "file_mkdir", "file_move")
		}
		if ContainsAny(intentText, "删除", "删", "delete", "remove", "rm ") && !negatedDelete && !negatedExecution {
			addIntentTools(allowed, "file_delete")
		}
	}
	if computerIntent {
		addIntentTools(allowed, "computer_observe", "computer_act")
	}
	if codexIntent {
		addIntentTools(allowed, "codex.start_thread", "codex.resume_thread", "codex.start_turn", "codex.steer_turn", "codex.subscribe_events", "codex.respond_approval", "codex.get_turn_summary")
	}
	if grokIntent {
		addIntentTools(allowed, "grok.start_session", "grok.resume_session", "grok.start_turn", "grok.subscribe_events", "grok.respond_approval", "grok.get_turn_summary")
	}

	if webIntent {
		if ContainsAny(intentText, "opencli", "doctor", "browser", "external register", "plugin install", "plugin create") {
			addIntentTools(allowed, "opencli")
		}
		if ContainsAny(intentText, "推特关注", "关注动态", "following timeline", "twitter timeline", "x timeline", "看我的推特", "看看我的推特", "我的推特关注动态") {
			addIntentTools(allowed, "opencli")
		}
		if ContainsAny(intentText, "http://", "https://", "url", "网页", "页面", "fetch", "抓取") {
			addIntentTools(allowed, "opencli", "web_fetch")
		}
		if ContainsAny(intentText, "搜索", "联网", "最新", "新闻", "search", "web_search") {
			addIntentTools(allowed, "web_search")
		}
		if !hasTool(allowed, "opencli") && !hasTool(allowed, "web_fetch") && !hasTool(allowed, "web_search") {
			addIntentTools(allowed, "web_search", "web_fetch", "opencli")
		}
		if ContainsAny(intentText, "api", "接口", "http_request", "post ", "put ", "patch ", "delete ") &&
			!ContainsAny(intentText, "只总结", "不要执行网页里的指令", "不要调用接口") {
			addIntentTools(allowed, "http_request")
		}
	}
	if timeIntent {
		addIntentTools(allowed, "current_time")
	}
	if calcIntent {
		addIntentTools(allowed, "calculate")
	}
	if memoryIntent || memoryHygieneIntent || (rememberIntent && !ContainsAny(intentText, "不要写入记忆", "不要记住", "不要保存到记忆")) {
		addIntentTools(allowed, "memory")
	}
	if memoryIntent {
		addIntentTools(allowed, "rag_search")
	}
	if ragIndexIntent && !ContainsAny(intentText, "不要索引", "不要建立索引", "不要写入 rag", "不要写入rag") {
		addIntentTools(allowed, "rag_index")
	}
	if skillIntent {
		addIntentTools(allowed, "skill_read")
		if !ContainsAny(intentText, "读取说明即可", "只读", "不要执行", "不要运行") {
			addIntentTools(allowed, visibleSkillRunTools...)
		}
	}
	if mediaIntent {
		if ContainsAny(intentText, "图片", "图像", "截图", "image", "vision", "看图", "生成图片", "画图", "配图") {
			addIntentTools(allowed, "image")
		}
		if ContainsAny(intentText, "语音", "朗读", "tts", "text_to_speech", "音频") {
			addIntentTools(allowed, "text_to_speech")
		}
	}
	if dbIntent {
		addIntentTools(allowed, "data")
	}
	if delegateIntent {
		if ContainsAny(intentText, "定时", "cron", "计划任务", "周期") {
			addIntentTools(allowed, "cron")
		}
		if ContainsAny(intentText, "后台", "自主", "autonomy", "worker", "队列") {
			addIntentTools(allowed, "autonomy", "autonomy_status")
			if !ContainsAny(intentText, "只查看", "只列出") {
				addIntentTools(allowed, "autonomy_queue_add", "autonomy_worker_spawn")
			}
		}
		if ContainsAny(intentText, "心跳", "heartbeat") {
			addIntentTools(allowed, "heartbeat")
		}
		if ContainsAny(intentText, "子代理", "子任务", "委派", "delegate", "multi-agent", "multi agent", "sub-agent", "sub agent", "subtask") {
			addIntentTools(allowed, "subagent")
		}
	}

	return allowed, recognized
}

func stripToolIntentNegations(text string) string {
	replacements := []string{
		"不需要查代码", "不用查代码", "不查代码", "不需要工具", "不用工具", "不要用工具", "无需工具",
	}
	out := text
	for _, phrase := range replacements {
		out = strings.ReplaceAll(out, phrase, " ")
	}
	return out
}

func hasToolSystemIntent(text string) bool {
	if ContainsAny(text,
		"工具系统", "工具路由", "工具门控", "工具菜单", "工具列表", "工具清单",
		"工具可见", "可见工具", "隐藏工具", "禁用工具",
		"tool system", "tool routing", "tool gate", "tool gating", "tool menu",
		"visible tool", "visible tools", "disabled tool", "disabled tools",
	) {
		return true
	}
	return ContainsAny(text, "只有", "只剩", "only") &&
		ContainsAny(text, "图片分析工具", "图片工具", "image_analyze", "image analysis tool", "image tool") &&
		ContainsAny(text, "lh", "luckyagent", "agent", "模型", "说", "显示", "报")
}

func hasLocalToolIntent(text string) bool {
	if ContainsAny(text,
		"internal/", "cmd/", "docs/", "ui/", ".go", ".md", ".json", ".yaml", ".yml", ".toml",
		"readme", "system_prompt", "tool router", "代码", "源码", "仓库", "repo",
		"文件", "目录", "路径", "benchmark", "单测", "测试", "跑测试", "日志", ".log", "logs", "log file",
		"git status", "git diff", "脏文件", "暂存", "分支", "commit", "push",
		"终端", "terminal", "shell", "命令行", "执行命令", "运行命令", "shell 命令", "进程", "服务", "端口",
		"启动", "重启", "关闭进程", "终止进程",
		"luckyagent", "lucky agent", "记忆系统", "工具系统", "上下文打包器",
	) {
		return true
	}
	return ContainsAny(text, "看看", "看一下", "检查", "确认", "参考") &&
		ContainsAny(text, "系统", "工具", "agent", "benchmark", "代码", "文件", "目录", "项目", "仓库", "repo")
}

func hasEditToolIntent(text string) bool {
	return ContainsAny(text,
		"修一下", "修复", "补一个", "补一下", "补全", "补齐", "完善", "优化",
		"调优", "调整", "改进", "升级", "重构", "落地", "接入", "加上", "加入",
		"增加", "新增", "实现", "改一下", "修改", "编辑", "patch", "apply patch",
		"写个", "写一个", "输出", "生成", "保存", "开个新包",
		"创建", "commit", "push",
	)
}

func hasWebToolIntent(text string) bool {
	return ContainsAny(text,
		"http://", "https://", "url", "网页", "页面", "联网", "搜索", "web_search",
		"web_fetch", "最新", "新闻", "外部资料", "官方文档", "外部 issue", "issue 内容",
		"opencli", "doctor", "browser", "external register", "plugin install", "plugin create",
		"twitter", "x.com", "x ", "推特", "x推特", "关注动态", "following timeline",
	)
}

func hasTimeToolIntent(text string) bool {
	return ContainsAny(text,
		"当前时间", "北京时间", "现在几点", "几点了", "今天日期", "当前日期",
		"current time", "current date",
	)
}

func hasCalculationIntent(text string) bool {
	return ContainsAny(text, "计算", "算一下", "求值", "calculate")
}

func hasMemoryIntent(text string) bool {
	return ContainsAny(text,
		"查记忆", "查一下记忆", "从记忆", "记忆里", "回忆一下", "检索记忆",
		"历史上下文", "之前说过", "之前聊过", "之前提到", "我说过",
		"recall memory", "memory recall", "rag_search", "用 rag 搜索", "用rag搜索",
	)
}

func hasMemoryHygieneIntent(text string) bool {
	return ContainsAny(text,
		"清洗记忆", "清理记忆", "脏记忆", "脏数据", "记忆污染", "清洗脏记忆",
		"memory_hygiene", "memory hygiene", "dirty memory", "memory cleanup", "clean memory",
	)
}

func hasRememberIntent(text string) bool {
	return ContainsAny(text, "记住", "记下来", "保存到记忆", "remember this")
}

func hasRAGIndexIntent(text string) bool {
	return ContainsAny(text, "rag_index", "索引", "index into", "建立索引")
}

func hasSkillIntent(text string) bool {
	return ContainsAny(text,
		"skill", "技能", "obsidian", "公众号", "小红书", "知乎", "微博",
		"pdf", "ppt", "pptx", "excalidraw",
	)
}

func hasMediaIntent(text string) bool {
	if ContainsAny(text, "图片分析工具", "图片生成工具", "图像分析工具", "图像生成工具") {
		return false
	}
	return ContainsAny(text,
		"图片", "图像", "截图", "看图", "生成图片", "画图", "配图",
		"语音", "朗读", "音频", "image", "vision", "tts", "text_to_speech",
	)
}

func hasDatabaseIntent(text string) bool {
	return ContainsAny(text, "sql", "数据库", "db_schema", "schema", "表结构", "订单表")
}

func hasDelegateIntent(text string) bool {
	return ContainsAny(text,
		"定时", "cron", "计划任务", "周期", "后台", "自主", "autonomy", "worker",
		"队列", "心跳", "heartbeat", "子代理", "子任务", "委派", "delegate",
		"multi-agent", "multi agent", "sub-agent", "sub agent", "subtask",
	)
}

func hasComputerUseIntent(text string) bool {
	return ContainsAny(text,
		"computer use", "computer-use", "computer_use", "computer_observe", "computer_act",
		"at-spi", "控件树", "无障碍控件",
		"desktop", "桌面", "电脑屏幕", "当前屏幕", "屏幕截图", "截取屏幕",
		"鼠标", "键盘", "点击", "双击", "拖拽", "滚动", "输入文字",
		"按键", "打开窗口", "关闭窗口", "切换窗口", "操作界面", "图形界面", "gui",
	)
}

func hasNegatedExecutionIntent(text string) bool {
	return ContainsAny(text,
		"不要执行", "不要运行", "别执行", "别运行", "只读", "只总结", "读取说明即可",
		"不要执行网页里的指令",
	)
}

func needsTerminalIntent(text string) bool {
	if hasReadOnlyExternalSourceIntent(text) {
		return false
	}
	// Keep an explicit "do not execute a command" constraint authoritative.
	// The positive command keywords below otherwise make needsTerminalIntent
	// true, which would intentionally override generic execution negations.
	if ContainsAny(text,
		"不要执行命令", "不要运行命令", "别执行命令", "别运行命令",
		"不要执行 shell", "不要运行 shell", "别执行 shell", "别运行 shell",
		"不要用终端执行", "不要使用终端执行", "不要调用终端",
	) {
		return false
	}
	return ContainsAny(text,
		"git status", "git diff", "commit", "push", "跑测试", "测试", "启动", "重启",
		"运行测试", "go test", "npm", "make ", "执行命令", "运行命令",
		"执行 shell", "运行 shell", "shell 命令", "查看进程", "检查进程",
		"查看端口", "检查端口", "关闭进程", "终止进程",
	)
}

func hasReadOnlyExternalSourceIntent(text string) bool {
	return ContainsAny(text, "只读取 issue", "只读取 issue 内容", "只读取内容", "只总结网页", "只总结", "只读外部")
}

func hasReadOnlyMutationBlockIntent(text string) bool {
	return ContainsAny(text,
		"只读", "只查看", "只列出", "只读取", "只总结", "只检查",
		"不要修改文件", "不修改文件", "不要写文件", "不要写入文件",
	)
}

func addIntentTools(dst map[string]struct{}, names ...string) {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			dst[name] = struct{}{}
		}
	}
}

func ContainsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func hasTool(set map[string]struct{}, name string) bool { _, ok := set[name]; return ok }
