package opencli

// Config describes the optional OpenCLI content extractor.
type Config struct {
	Enabled            bool     `json:"enabled,omitempty"`               // 是否启用 OpenCLI 抽取器
	Command            string   `json:"command,omitempty"`               // 可执行文件名，默认 opencli
	Args               []string `json:"args,omitempty"`                  // 参数模板，支持 {url} / {max_chars} 占位符
	TimeoutSeconds     int      `json:"timeout_seconds,omitempty"`       // 超时时间（秒）
	MaxChars           int      `json:"max_chars,omitempty"`             // 默认最大返回字符数
	FallbackToWebFetch bool     `json:"fallback_to_web_fetch,omitempty"` // OpenCLI 失败后是否回退到 web_fetch
}
