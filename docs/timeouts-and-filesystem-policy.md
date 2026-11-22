# Tool timeouts and filesystem policy

This batch addresses tools issues #23, #25 and #24. Runtime config uses the same
`tools.filesystem.allowed_write_roots` key (runtime issue #37).

## Timeouts

| Tool | Default | Maximum | Timeout result |
| --- | --- | --- | --- |
| `terminal` foreground | 120 seconds | 900 seconds | Error text includes `status=timeout`, `requested_timeout`, measured `elapsed_ms`, and `killed`. |
| `http_request` | 30 seconds | 120 seconds | Error and text/JSON output include `timeout_seconds` and measured `elapsed`; partial body bytes are retained. |
| `web_fetch` | 30 seconds | 120 seconds | The `timeout` argument bounds the extraction/fallback chain. Incomplete responses return `status=partial`, `bytes_read`, available content and a non-nil error. A timeout without captured bytes returns `status=timeout`. |
| `web_search` | 8 seconds | unchanged | Empty search results are not evidence of a timeout. |

Use `terminal` with `background=true` for long builds/tests. Background task
polling does not kill a command when its wait ends. The foreground 900-second
cap still clamps larger requests; it is not the default. `killed=true` means
the termination call succeeded. Unix retains process-group termination;
Windows retains its existing shell-process termination behavior.

For `http_request`, the existing `status` field remains the HTTP status (for
example, `200 OK`). `request_status` identifies `success`, `timeout`, `partial`
or `error`. A response header saying `200 OK` does not make a body read failure
successful. Both text and JSON return the error as well as available output.

For `web_fetch`, Defuddle, Jina and Curl receive the shared deadline. A deadline
stops subsequent fallback attempts. Partial or late responses are not cached
as complete pages. Curl/Jina disable stdout buffering so bytes received before
termination remain available. `bytes_read` counts output captured by the
current engine before display limits: response output for Curl/Jina and
extractor output for Defuddle. It does not measure source-network traffic for
every engine.

Deliberate `max_chars`/`max_response_bytes` limits remain normal truncation;
they do not become timeout failures.

## Explicit filesystem roots

The tools API accepts an optional `FilesystemPolicy`. Existing zero-argument
tool constructors keep their defaults. In a runtime config, for example:

```json
{
  "tools": {
    "filesystem": {
      "allowed_read_roots": ["/srv/reference-notes"],
      "allowed_write_roots": ["/srv/luckyagent/workspace"]
    }
  }
}
```

The runtime passes these lists to the tools policy. An empty or omitted write
list adds no permission. It does not inherit read roots and does not grant the
whole disk or another user's home directory. Existing built-in roots remain
unchanged: the runtime's `.luckyagent` home, its temporary directory and the
null device, plus the already supported `.lh-home` case. Existing explicit
dev behavior is retained.

Read/list operations can use built-in, read and write roots. Write, patch,
mkdir, move and delete use built-in and write roots. Denial messages identify
`sandbox`, the requested path and the actual expanded `allowed_read_roots`
and `allowed_write_roots`; sensitive-path denials remain enforced. Relative
paths use their supplied cwd and never silently switch to the process cwd.
Existing symlink ancestors are resolved before checking newly created paths.

Authorization precedes existence checks. A missing path inside an allowed
root is `not_found` and preserves `os.ErrNotExist` in the error chain. A path
outside allowed roots remains a sandbox denial, including when it is absent.

Terminal's explicit cwd and supported literal `cd`/redirection paths use the
same read/write policy before either foreground or background execution.
Literal paths with a symlink followed by `..` are rejected as ambiguous,
because cleaning the path would not match the kernel's redirect traversal.
Literal shell paths with boundary whitespace or a quoted leading `~` are
also rejected rather than applying the file tools' trimming/tilde expansion
to a shell word with different semantics; use an unambiguous absolute path.
These checks do not interpret arbitrary scripts, variable expansions or all
shell control flow. Command substitution/backticks make the entire command
opaque to the literal scanner; complex `cd` control flow cannot establish a
guaranteed base for subsequent relative redirects. Windows checks explicit
cwd only, leaving PowerShell syntax to the existing executor. These are
preflight checks, not an OS jail.

## Restricted task snapshots

`iso` and `workspace-write` snapshots bind every built-in filesystem tool to
the snapshot root for the lifetime of the request. This includes calls made
without a `ShellContext`, relative paths, write/patch/move/delete operations,
and document reads. A caller-supplied `_sandbox_mode=dev` cannot widen a
restricted request. Host trash is disabled in a restricted snapshot because it
would move data outside the managed root. Symlink ancestors are resolved
before the policy check; a path that reaches outside the snapshot is denied.

This still does not claim that the whole LuckyAgent process is jailed. MCP,
plugins, skills and other non-filesystem tools are separately disabled by the
Agent policy in restricted modes, while terminal is the only operation
executed inside bubblewrap. A restricted file denial means the filesystem
boundary was enforced by tool policy, not that a kernel-level OS jail covers
every process in the turn.

## Verification

Run these from this module, independently of sibling checkouts:

```sh
GOWORK=off go build ./...
GOWORK=off go test ./...
```

Network regressions use local streaming HTTP/proxy servers and the real Curl
engine. Filesystem regressions must create an external root outside the
built-in home/tmp policy, then inspect actual file side effects; writing under
`/tmp` alone does not prove that an explicit write root is connected.
