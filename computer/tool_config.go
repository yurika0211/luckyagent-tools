package computer

// ComputerUseToolConfig controls the optional local desktop automation tools.
// Computer use is disabled by default because it can observe and control the
// interactive desktop, including applications outside LuckyAgent.
type ToolConfig struct {
	Enabled             bool     `json:"enabled,omitempty"`
	Mode                string   `json:"mode,omitempty"`    // observe, assist, control
	Backend             string   `json:"backend,omitempty"` // auto, x11, wayland, wslg, windows, darwin
	CaptureDir          string   `json:"capture_dir,omitempty"`
	AllowedSources      []string `json:"allowed_sources,omitempty"`
	AllowedWindows      []string `json:"allowed_windows,omitempty"`
	RequireApproval     bool     `json:"require_approval,omitempty"`
	MaxSteps            int      `json:"max_steps,omitempty"`
	TimeoutSeconds      int      `json:"timeout_seconds,omitempty"`
	StepTimeoutSeconds  int      `json:"step_timeout_seconds,omitempty"`
	SettleMS            int      `json:"settle_ms,omitempty"`
	SettleMilliseconds  int      `json:"settle_milliseconds,omitempty"`
	MaxObservationBytes int      `json:"max_observation_bytes,omitempty"`
	MaxScreenshotWidth  int      `json:"max_screenshot_width,omitempty"`
	MaxBatchActions     int      `json:"max_batch_actions,omitempty"`
	// MaxConsecutiveObserveOnly stops screenshot-only computer-use loops after N
	// consecutive observe batches without a computer_act. Default 2.
	MaxConsecutiveObserveOnly int    `json:"max_consecutive_observe_only,omitempty"`
	SettleMode                string `json:"settle_mode,omitempty"`
	KeepFrames                int    `json:"keep_frames,omitempty"`
	FrameTTLSeconds           int    `json:"frame_ttl_seconds,omitempty"`
	RetainFrames              int    `json:"retain_frames,omitempty"`
	AllowTextInput            bool   `json:"allow_text_input,omitempty"`
	AllowHighRiskActions      bool   `json:"allow_high_risk_actions,omitempty"`
}
