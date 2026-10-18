# Compaction evidence and cancellation

`compact.Engine` chunks transcripts at user boundaries, summarizes chunks with
bounded parallelism and merges them in history order. The embedding host owns
storage, session authorization, final validation and completion decisions.

`LocalSummary` is a recovery fallback. Assistant statements are placed under
`Uncertain facts` with an unverified label; a statement such as “I will edit the
file” is never promoted into completed work. Tool text remains command/evidence
material. This fallback does not infer that the user's task is complete.

`Validate` checks length, goal/pending sections and required evidence. It also
rejects explicit future-plan prefixes in the English `Completed work` section.
This is a narrow structural check, not a semantic proof that every summary
claim is true. Hosts must validate the final merged summary before persisting
it, and continue to use actual tool results for task acceptance.

`SummaryContext` can give a summary call its own timeout when a parent deadline
is nearly exhausted. While that context is active, an explicit parent
`context.Canceled` still cancels the summary call. A parent deadline expiration
can be extended by design; canceling the returned context releases its linked
cancellation callback. Already canceled parents fail immediately.

The regression tests cover assistant plans, future-plan validation, short
deadline extension and explicit cancellation. They do not measure model quality
or production task success rates.

```bash
GOWORK=off go test -mod=readonly -race ./compact
```
