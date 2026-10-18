# Orchestration adapters

The independent `github.com/yurika0211/luckyagent-orchestrator` module owns the
task, collaboration, autonomy and ordinary-delegation engines. This SDK owns
their model-visible tool adapters.

| SDK surface | Engine owner |
| --- | --- |
| `subagent`, legacy delegate tools and root `DelegateManager` facade | `orchestrator/delegate` |
| `autonomy` and legacy `autonomy_*` tools | `orchestrator/autonomy` |
| `execpolicy` task policy values | `orchestrator/task` |
| Legacy SDK `task`, `collab`, `autonomy` package paths | Deprecated aliases to orchestrator packages |

Tool schemas, JSON arguments/results, permission metadata and execution-context
handling remain in the SDK. The `DelegateManager` facade embeds the independent
manager and retains `DelegateParallelTool`. SDK root DTOs remain aliases to
engine types. There is no second implementation behind compatibility paths.

The SDK supplies its existing filesystem validator and path expander through
`delegate.WorkspacePolicy`; extraction does not change the builtin sandbox.
The embedding host still supplies execution callbacks, sessions, storage paths,
model selection and lifecycle. The orchestrator never imports this SDK.

Engine unit tests moved with their implementations. Tool registration,
permission and context-adapter tests remain here. The initial orchestrator
dependency is `v0.1.0`; module requirements and checksums pin downloadable source.

For local development, add the orchestrator checkout to an ignored `go.work`:

```bash
go work init . /path/to/luckyagent-orchestrator
go build ./...
```

The repository split introduces no runtime configuration keys and preserves
the existing task/queue storage formats. `subagent mode=autonomy_queue` retains
its previous metadata-only behavior; background work uses `autonomy action=add`.
