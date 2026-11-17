# Compaction evidence and cancellation

`compact.Engine` chunks transcripts at user boundaries, summarizes chunks with
bounded parallelism and merges them in history order. The embedding host owns
storage, session authorization, final validation and completion decisions.

`Transcript` is one bounded summarizer input, capped at `TranscriptMaxRunes`
(48000) unless `TextOptions.TranscriptBudget` is smaller. While that budget
still fits, user and assistant messages are copied whole. A fixed per-role
length is not applied first, so a date, amount, or revocation sentence in the
middle of a message stays in the input. Tool result bodies keep their own
excerpt cap and replay pointer. That cap is the externalized-dump contract.

`Generate` calls `SplitChunksChecked` before `Transcript`. A single message
that is still over the chunk token budget is cut on whitespace or punctuation.
The cut moves back to the start of a continuous span when that span fits in
the budget, so a path is not sliced and then repaired. `ChunkOverlapRunes`
(128) may be repeated only while the next window still fits the token budget,
and each window still advances. A continuous span that itself exceeds the
chunk budget, including a long separator-free prose run, returns
`ErrTranscriptBudget` before any model call. The original message is left
whole. This does not raise the budget and does not promise to compact
unbounded input. `SplitChunks` keeps the old signature and returns the unsplit
turn in that case. A direct `Transcript` call on an oversized slice still
returns bounded text and names the omission; that text is not a successful
whole-source summary.

The host keeps required-fact checks, net-benefit checks, and usage accounting.
This package does not decide which prose facts are required.

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
