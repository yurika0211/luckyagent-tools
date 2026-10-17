package tool

import (
	"github.com/yurika0211/luckyagent-tools/multimodal"
)

// RegisterRuntimeBuiltinTools 注册所有内置工具
func RegisterRuntimeBuiltinTools(r *Registry, mediaProcessor ...*multimodal.Processor) {
	RegisterRuntimeBuiltinToolsWithConfig(r, nil, nil, mediaProcessor...)
}

// RegisterRuntimeBuiltinToolsWithConfig 注册所有内置工具（带搜索配置）
func RegisterRuntimeBuiltinToolsWithConfig(r *Registry, searchCfg *WebSearchConfig, opencliCfg *OpenCLIConfig, mediaProcessor ...*multimodal.Processor) {
	var processor *multimodal.Processor
	if len(mediaProcessor) > 0 {
		processor = mediaProcessor[0]
	}

	RegisterBuiltinToolsWithConfig(r, searchCfg, opencliCfg)
	r.Register(ImageTool(processor, "", nil, ImageGenerationDefaults{}))
	r.Register(TextToSpeechTool(nil, TTSDefaults{}))
}

// RegisterBuiltinComputerTools adds the optional desktop observation/control
// tools to an existing registry. Keeping this separate avoids constructing a
// platform backend when computer use is disabled.
func RegisterBuiltinComputerTools(r *Registry, manager ComputerManager, cfg ComputerUseConfig) {
	if r == nil || manager == nil {
		return
	}
	NewComputerUseToolService(manager, cfg).RegisterTools(r)
}
