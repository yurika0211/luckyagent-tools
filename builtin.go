package tool

// RegisterBuiltinTools registers the standalone tool catalog. Application
// operations such as media processing can be registered by the embedding host.
func RegisterBuiltinTools(r *Registry) {
	RegisterBuiltinToolsWithConfig(r, nil, nil)
}

// RegisterBuiltinToolsWithConfig supplies search and OpenCLI options. Memory
// and RAG definitions retain their callback-based, unconfigured defaults.
func RegisterBuiltinToolsWithConfig(r *Registry, searchCfg *WebSearchConfig, opencliCfg *OpenCLIConfig) {
	policy := DefaultFilesystemPolicy()
	r.Register(TerminalTool())
	r.Register(FileReadTool(policy))
	r.Register(DocumentReadTool(policy))
	r.Register(FileWriteTool())
	r.Register(FileMkdirTool())
	r.Register(FileMoveTool())
	r.Register(FileDeleteTool())
	r.Register(FilePatchTool())
	r.Register(FileListTool(policy))
	r.Register(WebSearchTool(searchCfg))
	r.Register(WebFetchTool(searchCfg))
	r.Register(OpenCLITool(opencliCfg, searchCfg))
	r.Register(CurrentTimeTool())
	r.Register(CalculateTool())
	r.Register(DataTool())
	r.Register(HTTPRequestTool())
	r.Register(MemoryTool(nil, nil, nil, nil))
	r.Register(RAGSearchTool(nil))
	r.Register(RAGIndexTool(nil))
}
