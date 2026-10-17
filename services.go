package tool

import (
	"github.com/yurika0211/luckyagent-tools/memory"
	"github.com/yurika0211/luckyagent-tools/multimodal"
	"github.com/yurika0211/luckyagent-tools/rag"
)

// Services groups tool-layer business services and owns their registration wiring.
type Services struct {
	Builtin           *BuiltinToolService
	SearchCfg         *WebSearchConfig
	OpenCLI           *OpenCLIConfig
	Memory            *MemoryToolService
	RAG               *RAGToolService
	Delegate          *DelegateManager
	Cron              *CronToolService
	Autonomy          *AutonomyToolService
	Heartbeat         *HeartbeatToolService
	Skills            *SkillToolService
	Codex             *CodexToolService
	Grok              *GrokToolService
	Ephemeral         *EphemeralCredentialToolService
	EphemeralTerminal *EphemeralCredentialTerminalToolService
}

// Close releases optional long-lived tool runtimes such as Codex App Server
// and the Grok agent process.
func (s *Services) Close() error {
	if s == nil {
		return nil
	}
	var first error
	if s.Ephemeral != nil {
		if err := s.Ephemeral.Close(); err != nil && first == nil {
			first = err
		}
	}
	if s.EphemeralTerminal != nil {
		if err := s.EphemeralTerminal.Close(); err != nil && first == nil {
			first = err
		}
	}
	if s.Codex != nil {
		if err := s.Codex.Close(); err != nil && first == nil {
			first = err
		}
	}
	if s.Grok != nil {
		if err := s.Grok.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// SetComputerUseService attaches the optional desktop automation tools to the
// builtin registration set. It is separate from NewServices so existing
// callers remain source-compatible while computer backends stay optional.
func (s *Services) SetComputerUseService(service *ComputerUseToolService) *Services {
	if s != nil && s.Builtin != nil {
		s.Builtin.SetComputerUseService(service)
	}
	return s
}

// ReloadMediaTools updates the three media tools without rebuilding unrelated
// registry entries such as skills, MCP servers, or scheduled-task tools.
func (s *Services) ReloadMediaTools(r *Registry, defaultImageProvider string, mediaProcessor *multimodal.Processor, imageGenerator multimodal.ImageGenerator, imageGenDefaults ImageGenerationDefaults, speechSynthesizer multimodal.SpeechSynthesizer, ttsDefaults TTSDefaults) {
	if s == nil || s.Builtin == nil {
		return
	}
	s.Builtin.ReloadMedia(r, defaultImageProvider, mediaProcessor, imageGenerator, imageGenDefaults, speechSynthesizer, ttsDefaults)
}

// NewServices creates a tool service container.
func NewServices(searchCfg *WebSearchConfig, opencliCfg *OpenCLIConfig, defaultImageProvider string, mediaProcessor *multimodal.Processor, imageGenerator multimodal.ImageGenerator, imageGenDefaults ImageGenerationDefaults, speechSynthesizer multimodal.SpeechSynthesizer, ttsDefaults TTSDefaults, mem memory.ToolBackend, ragMgr rag.Backend, delegate *DelegateManager, policies ...FilesystemPolicy) *Services {
	policy := filesystemPolicyFromOptional(policies)
	return &Services{
		Builtin:   NewBuiltinToolService(searchCfg, opencliCfg, defaultImageProvider, mediaProcessor, imageGenerator, imageGenDefaults, speechSynthesizer, ttsDefaults, policy),
		SearchCfg: searchCfg,
		OpenCLI:   opencliCfg,
		Memory:    NewMemoryToolService(mem),
		RAG:       NewRAGToolService(ragMgr),
		Delegate:  delegate,
	}
}

// RegisterCoreTools registers builtins and delegate tools through the service container.
func (s *Services) RegisterCoreTools(r *Registry) {
	if r == nil {
		return
	}

	if s.Builtin != nil {
		s.Builtin.RegisterTools(r)
	}

	if s.Memory != nil {
		memoryTool := MemoryTool(s.Memory.HandleRemember, s.Memory.HandleHygiene, s.Memory.HandleRecall, s.Memory.HandleRecallDetailed)
		memoryTool.ContextDetailedHandler = s.Memory.HandleContext
		r.Register(memoryTool)
	}
	if s.RAG != nil {
		r.Register(RAGSearchTool(s.RAG.HandleSearch))
		r.Register(RAGIndexTool(s.RAG.HandleIndex))
	}
	if s.Delegate != nil {
		r.Register(SubagentTool(s.Delegate))
	}
	if s.Cron != nil {
		s.Cron.RegisterTools(r)
	}
	if s.Autonomy != nil {
		s.Autonomy.RegisterTools(r)
	}
	if s.Heartbeat != nil {
		s.Heartbeat.RegisterTools(r)
	}
	if s.Skills != nil {
		s.Skills.RegisterSkillTools(r)
	}
	if s.Codex != nil {
		s.Codex.RegisterTools(r)
	}
	if s.Grok != nil {
		s.Grok.RegisterTools(r)
	}
	if s.Ephemeral != nil {
		s.Ephemeral.RegisterTools(r)
	}
	if s.EphemeralTerminal != nil {
		s.EphemeralTerminal.RegisterTools(r)
	}
}

// RevokeEphemeralSession removes temporary credential tools for one session.
func (s *Services) RevokeEphemeralSession(sessionID string) {
	if s != nil && s.Ephemeral != nil {
		s.Ephemeral.RevokeSession(sessionID)
	}
	if s != nil && s.EphemeralTerminal != nil {
		s.EphemeralTerminal.RevokeSession(sessionID)
	}
}
