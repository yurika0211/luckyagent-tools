# LuckyAgent Tools

An independent Go module for tool registration, execution contracts, generic
builtins, Skills and MCP clients. It does not import the LuckyAgent runtime.

```text
github.com/yurika0211/luckyagent-tools
```

The root package is named `tool`. Its implementations were extracted from
LuckyAgent and retain the original MIT license and attribution.

```bash
go get github.com/yurika0211/luckyagent-tools@v0.2.2
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
| Root runtime services | Memory/RAG handlers, delegation/autonomy tool adapters, cron/heartbeat tools, media/computer tools, temporary credential tools and coding-agent bridges |
| `memory`, `rag`, `credentials`, `chat` | Neutral backend ports, shared data and credential identifier validation |
| `task`, `collab`, `autonomy` | Deprecated compatibility aliases to the independent `luckyagent-orchestrator` engines |
| `cron` | Generic scheduled job engine |
| `computer`, `multimodal`, `codex`, `grok` | Optional desktop/media providers and external coding-agent clients |
| `approval`, `hitl` | Neutral approval values and runtime-scoped human approval/input waits |
| `skillroute` | Matching immutable skill descriptions to natural-language requests |
| `execpolicy` | Turn-local intent constraints and tool-call decisions using the task policy decision contract |
| `compact` | Chunking, text-only parallel summaries, validation, local fallback and restoration evidence |
| `workflow` | DAG execution with host-supplied action handlers and no HTTP framework dependency |

The embedding host owns tool selection, approval prompts, Agent execution,
memory/RAG persistence, credential storage, provider selection and lifecycle.
Reusable execution, validation and rendering live here. Permission metadata
and deny/sandbox checks are preserved; dispatch itself does not ask a human for
approval. The host resolves `PermApprove` before calling a handler.

Task storage, collaboration, autonomy and ordinary-delegation management now
live in `github.com/yurika0211/luckyagent-orchestrator`. This SDK retains tool
definitions and public compatibility names. See [orchestration adapters](docs/orchestration.md)
for ownership, the `v0.1.0` engine dependency and local development.

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

The execution packages above do not import the root tool catalog, the host, or
API/gateway modules. `hitl.New` creates one broker per runtime identity and
notifies the caller with a neutral pending value; the caller renders it and
resolves replies. Credential input still requires a masked form. Existing root
`ApprovalOption` and `PendingApproval` remain aliases of the shared values.

`skillroute.Match` accepts skill descriptors and returns an original-slice
index, score, explicit-name flag and reason, or nil for weak/ambiguous matches.
`execpolicy.New` accepts per-turn disabled tools/actions; `Evaluate` reuses
`task.PolicyDecision`, while approvals, quotas and actual dispatch stay in the
runtime. Neither package loads configuration or executes tools.

`compact.New` accepts an estimator, limits, text-rendering callbacks and a
text-only `ChatFunc`. `Generate` preserves user-turn boundaries, limits parallel
calls and merges in history order. Progress callbacks are serialized. Hosts
validate the final summary before saving their own backups/checkpoints.
`Validate`, `LocalSummary`, `SelectInput` and `RestoreAttachments` can also be
used independently. `workflow.NewWorkflowEngine` accepts an executor with
registered action callbacks; HTTP adapters live in `luckyagent-api/workflow`.

These execution APIs are additions after v0.2.2. Downstream modules must require
a commit pseudo-version or a newer release containing them. Use matching
checkouts in a Go workspace during joint development, and merge tools before
upgrading API and host dependencies. No runtime JSON configuration, tool wire
name or stored session format is changed by the extraction.

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

## Desktop capture and tool schemas

The X11 backend has a bounded native screenshot fallback when ImageMagick is
missing or fails; see [X11 capture](docs/x11-capture.md). Tool conversion sorts
the JSON Schema `required` array so map iteration does not reorder required
parameter names between otherwise identical requests.
