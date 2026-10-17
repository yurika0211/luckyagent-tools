# LuckyAgent Tools

An independent Go module for tool registration, execution contracts, generic
builtins, Skills and MCP clients. It does not import the LuckyAgent runtime.

```text
github.com/yurika0211/luckyagent-tools
```

The root package is named `tool`. Its implementations were extracted from
LuckyAgent and retain the original MIT license and attribution.

```bash
go get github.com/yurika0211/luckyagent-tools@v0.2.0
```

## Embed a tool registry

```go
package main

import (
    "fmt"

    tool "github.com/yurika0211/luckyagent-tools"
)

func main() {
    registry := tool.NewRegistry()
    registry.Register(&tool.Tool{
        Name:       "echo",
        Permission: tool.PermAuto,
        Parameters: map[string]tool.Param{"text": {Type: "string", Required: true}},
        Handler: func(args map[string]any) (string, error) {
            return fmt.Sprint(args["text"]), nil
        },
    })
    output, err := registry.Call("echo", map[string]any{"text": "hello"})
    if err != nil {
        panic(err)
    }
    fmt.Println(output)
}
```

`RegisterBuiltinTools` registers the standalone catalog. Applications may also
register individual factories, use `RegisterBuiltinToolsWithConfig` for search
and OpenCLI options, and provide callbacks to memory/RAG tool definitions.
Unconfigured callbacks keep their existing unavailable-service responses.

## Ownership

| Package | Responsibilities |
| --- | --- |
| Root `tool` | Tool/Param contracts, registry and function schemas, routing, execution context, gateway, subscriptions and usage tracking |
| Root builtin factories | Shell, files and Office documents, search/fetch, HTTP, time, calculation, structured data and SQLite queries, OpenCLI |
| Root Skill APIs | Discovery, parsing, lifecycle, execution, documentation reads, archive staging, scanning and installation confirmation |
| Root MCP APIs | MCP server configuration, tool discovery, registration and calls |
| `search` | Search engines and result aggregation |
| `sandbox` | Execution mode, task snapshots, path checks and isolated shell commands |
| `opencli` | Host-independent OpenCLI configuration DTO |
| `textutil` | Shared text, HTML and URL formatting helpers |
| Root runtime services | Memory/RAG handlers, delegation, cron/autonomy/heartbeat tools, media/computer tools, temporary credential tools and coding-agent bridges |
| `memory`, `rag`, `credentials`, `chat` | Neutral backend ports, shared data and credential identifier validation |
| `task`, `collab`, `cron`, `autonomy` | Optional task storage, collaboration, scheduling and durable queue engines with injected executors |
| `computer`, `multimodal`, `codex`, `grok` | Optional desktop/media providers and external coding-agent clients |

The embedding host owns tool selection, approval prompts, Agent execution,
memory/RAG persistence, credential storage, provider selection and lifecycle.
Reusable execution, validation and rendering live here. Permission metadata
and deny/sandbox checks are preserved; dispatch itself does not ask a human for
approval. The host resolves `PermApprove` before calling a handler.

Defaults preserve LuckyAgent-compatible file roots and environment behavior.
Filesystem read policies and task sandbox snapshots remain explicit inputs;
this extraction does not broaden file or network access. The SQLite builtin
uses `go-sqlite3` and therefore requires CGO for database operations.

## Development

```bash
go test ./...
go test -race ./...
```

Tests moved with the generic implementations. Application adapter tests remain
in LuckyAgent's `internal/toolapp`; a boundary test rejects imports from the
LuckyAgent host, and the embedding example is executed as a Go example test.

LuckyAgent retains aliases at `internal/tool`, `internal/tool/search`,
`internal/sandbox` and the moved formatting helpers in `internal/utils`.
Platform and model application services use the same shared tool types.

## Inject application capabilities

`NewMemoryToolService` consumes `memory.ToolBackend`; `NewRAGToolService`
consumes the two-method `rag.Backend` search/index port. The module never opens
LuckyAgent's memory or RAG store. `CredentialProvider.WithCredential` grants
access for a single callback; credential loading, zeroing and cleanup belong to
the provider. Pass it to `NewEphemeralCredentialToolService` or its terminal
variant. Temporary tool expiry, session scope, shell quoting and redaction stay
in this module.

Delegate managers accept executor functions; media tools accept providers;
desktop tools accept `ComputerManager`. Hosts instantiate only the capabilities
they enable and close owned services during shutdown. `NewServices` assembles
the runtime catalog. `RegisterRuntimeBuiltinTools[WithConfig]` adds the optional
media tools; the existing `RegisterBuiltinTools[WithConfig]` remains the generic
standalone catalog for source compatibility.
