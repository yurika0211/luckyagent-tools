package compact

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// The 1800-rune case is the published minimal repro: one synthetic path in the
// middle of a user message, under TranscriptMaxRunes.
func TestTranscriptKeepsMiddlePathWhenGlobalBudgetFits(t *testing.T) {
	const path = "/synthetic/example/required-entry.ndjson"
	raw := strings.Repeat("background ", 80) + path + strings.Repeat(" background", 80)
	if got := utf8.RuneCountInString(raw); got != 1800 {
		t.Fatalf("fixture runes=%d, want 1800", got)
	}
	out := Transcript([]Message{{Role: "user", Content: raw}}, TextOptions{})
	if !strings.Contains(out, path) {
		t.Fatalf("path lost while global budget had room:\n%s", truncate(out, 240))
	}
	if strings.Contains(out, "omitted") {
		t.Fatalf("in-budget message was cut:\n%s", truncate(out, 240))
	}
	if utf8.RuneCountInString(out) > TranscriptMaxRunes {
		t.Fatalf("transcript runes=%d", utf8.RuneCountInString(out))
	}
}

// The 16699-rune ledger is the published synthetic fixture, loaded unchanged.
// Date and the revocation sentence are ordinary prose, not a path pattern.
func TestTranscriptKeepsReal16699FixtureFacts(t *testing.T) {
	raw := loadLedgerFixture(t)
	if got := utf8.RuneCountInString(raw); got != 16699 {
		t.Fatalf("fixture runes=%d, want 16699", got)
	}
	facts := []string{
		"2026-04-17",
		"18420.65 LUM",
		"/synthetic/ledger/kx4471/batch-19.ndjson",
		"旧承运决定 VESSEL-RED-3 已撤销",
		"当前有效承运改为 BARGE-QUILL-8",
		"night-window only, 22:10-23:40 local",
	}
	for _, fact := range facts {
		if !strings.Contains(raw, fact) {
			t.Fatalf("fixture no longer contains %q; do not edit the fixture to match the test", fact)
		}
	}
	out := Transcript([]Message{{Role: "user", Content: raw}}, TextOptions{})
	if utf8.RuneCountInString(out) > TranscriptMaxRunes {
		t.Fatalf("in-budget fixture exceeded the transcript cap: %d", utf8.RuneCountInString(out))
	}
	for _, fact := range facts {
		if !strings.Contains(out, fact) {
			t.Fatalf("in-budget transcript dropped %q", fact)
		}
	}
}

func TestTranscriptUTF8MiddleSentence(t *testing.T) {
	const sentence = "旧承运决定 VESSEL-RED-3 已撤销，生效日期 2026-04-17"
	raw := strings.Repeat("背景🙂 ", 30) + sentence + strings.Repeat(" 结尾café", 30)
	out := Transcript([]Message{{Role: "user", Content: raw}}, TextOptions{})
	if !strings.Contains(out, sentence) {
		t.Fatalf("utf-8 sentence lost:\n%s", out)
	}
}

func TestEngineSeesMiddleFactsAcrossChunkWindows(t *testing.T) {
	const (
		date = "生效日期定为 2026-04-17"
		revoke = "旧承运决定 VESSEL-RED-3 已撤销，不要再把它当有效值"
	)
	// One message larger than both the chunk token budget and one transcript.
	// The facts sit in the middle and are ordinary sentences.
	body := strings.Repeat("噪声段 synthetic noise。", 4000) + "\n" + date + "\n" + revoke + "\n" + strings.Repeat("噪声段 synthetic noise。", 4000)
	if utf8.RuneCountInString(body) < TranscriptMaxRunes {
		t.Fatalf("fixture is not oversized: %d", utf8.RuneCountInString(body))
	}
	var mu sync.Mutex
	var prompts []string
	engine := New(Config{
		MaxChunkTokens: 3000,
		MaxParallel:    2,
		Estimator:      EstimatorFunc(func(text string) int { return (utf8.RuneCountInString(text) + 3) / 4 }),
		Chat: func(_ context.Context, request ChatRequest) (string, error) {
			if request.Kind == "segment" {
				mu.Lock()
				prompts = append(prompts, request.UserPrompt)
				mu.Unlock()
			}
			return testSummary("continue"), nil
		},
	})
	if _, _, err := engine.Generate(context.Background(), []Message{{Role: "user", Content: body}}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(prompts, "\n")
	if !strings.Contains(joined, date) || !strings.Contains(joined, revoke) {
		t.Fatalf("engine prompts missed a middle sentence; prompts=%d", len(prompts))
	}
	est := EstimatorFunc(func(text string) int { return (utf8.RuneCountInString(text) + 3) / 4 })
	chunks := SplitChunks([]Message{{Role: "user", Content: body}}, est, 3000)
	if stitched := stitchWindows(chunks); stitched != body {
		t.Fatalf("windows do not cover the source: got %d runes, want %d", utf8.RuneCountInString(stitched), utf8.RuneCountInString(body))
	}
	for _, prompt := range prompts {
		if utf8.RuneCountInString(prompt) > TranscriptMaxRunes+4000 {
			t.Fatalf("one segment prompt is unbounded: %d", utf8.RuneCountInString(prompt))
		}
	}
}

func TestEngineSeesRealFixtureWithoutCuttingIt(t *testing.T) {
	raw := loadLedgerFixture(t)
	var prompt string
	engine := New(Config{
		Chat: func(_ context.Context, request ChatRequest) (string, error) {
			prompt = request.UserPrompt
			return testSummary("ledger"), nil
		},
	})
	if _, _, err := engine.Generate(context.Background(), []Message{{Role: "user", Content: raw}}); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"2026-04-17", "旧承运决定 VESSEL-RED-3 已撤销", "18420.65 LUM"} {
		if !strings.Contains(prompt, fact) {
			t.Fatalf("engine prompt dropped %q", fact)
		}
	}
}

func TestEngineOverBudgetInputIsChunkedBeforeTranscript(t *testing.T) {
	// Two long user messages. Each fits one transcript; together they do not.
	// The engine must show both middles to the summarizer, not drop the first.
	one := strings.Repeat("a", 30000) + " FIRST-DATE 2026-04-17 " + strings.Repeat("b", 30000)
	two := strings.Repeat("c", 30000) + " SECOND-REVOKE 旧承运决定已撤销 " + strings.Repeat("d", 30000)
	var mu sync.Mutex
	var prompts []string
	engine := New(Config{
		MaxChunkTokens: 20000,
		Estimator:      EstimatorFunc(func(text string) int { return utf8.RuneCountInString(text) }),
		Chat: func(_ context.Context, request ChatRequest) (string, error) {
			if request.Kind == "segment" {
				mu.Lock()
				prompts = append(prompts, request.UserPrompt)
				mu.Unlock()
			}
			return testSummary("goal"), nil
		},
	})
	if _, _, err := engine.Generate(context.Background(), []Message{{Role: "user", Content: one}, {Role: "user", Content: two}}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(prompts, "\n")
	if !strings.Contains(joined, "FIRST-DATE 2026-04-17") || !strings.Contains(joined, "SECOND-REVOKE 旧承运决定已撤销") {
		t.Fatalf("a message middle was not shown to the summarizer")
	}
}

func TestEngineReportsBudgetWhenAChunkCannotFit(t *testing.T) {
	// Tool body is still excerpted, so a giant tool alone is allowed.
	// A user message whose one window still cannot be rendered whole is an error.
	// Force that by a transcript budget smaller than the minimum window.
	body := strings.Repeat("x", 5000) + " date 2026-04-17"
	engine := New(Config{
		MaxChunkTokens: 100000,
		Text:           TextOptions{TranscriptBudget: 64},
		Chat: func(context.Context, ChatRequest) (string, error) {
			t.Fatal("oversized transcript was summarized as a whole source")
			return "", nil
		},
	})
	_, _, err := engine.Generate(context.Background(), []Message{{Role: "user", Content: body}})
	if !errors.Is(err, ErrTranscriptBudget) {
		t.Fatalf("err=%v", err)
	}
}

func TestEngineCancelStillSkipsModelOnOversizedInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := strings.Repeat("段", 60000)
	engine := New(Config{MaxChunkTokens: 2000, Chat: func(context.Context, ChatRequest) (string, error) {
		t.Fatal("canceled compact called the model")
		return "", nil
	}})
	_, _, err := engine.Generate(ctx, []Message{{Role: "user", Content: body}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestToolExcerptContractStillBoundsDumps(t *testing.T) {
	dump := strings.Repeat("stack dump line ", 400)
	out := Transcript([]Message{{Role: "tool", Name: "terminal", Index: 9, Content: dump + "\nerror: open /tmp/build/out.log"}}, TextOptions{})
	if strings.Contains(out, strings.Repeat("stack dump line ", 40)) {
		t.Fatal("tool dump was copied whole")
	}
	if !strings.Contains(out, "#9") {
		t.Fatalf("tool excerpt lost its replay pointer:\n%s", out)
	}
	if !strings.Contains(out, "/tmp/build/out.log") && !strings.Contains(out, "omitted") {
		t.Fatalf("tool excerpt dropped the error without a record:\n%s", out)
	}
}

func TestSplitKeepsToolCallPairedAndBoundedOverlap(t *testing.T) {
	est := EstimatorFunc(func(text string) int { return (utf8.RuneCountInString(text) + 3) / 4 })
	body := strings.Repeat("段", 20000)
	msg := Message{Role: "user", Content: body, ToolCalls: []ToolCall{{Name: "read", Arguments: `{"path":"/synthetic/example/required-entry.ndjson"}`}}}
	chunks := SplitChunks([]Message{msg}, est, 2000)
	if len(chunks) < 2 {
		t.Fatalf("oversized message stayed one chunk: %d", len(chunks))
	}
	var covered int
	for i, chunk := range chunks {
		if len(chunk) != 1 || len(chunk[0].ToolCalls) != 1 || chunk[0].ToolCalls[0].Name != "read" {
			t.Fatalf("chunk %d detached the tool call", i)
		}
		if EstimateMessages(est, chunk) > 2000+ChunkOverlapRunes {
			t.Fatalf("chunk %d tokens=%d", i, EstimateMessages(est, chunk))
		}
		covered += utf8.RuneCountInString(chunk[0].Content)
	}
	// Overlap copies are bounded by the fixed overlap, not by a detected span.
	extra := covered - len([]rune(body))
	maxExtra := (len(chunks) - 1) * ChunkOverlapRunes
	if extra > maxExtra {
		t.Fatalf("overlap extra=%d max=%d", extra, maxExtra)
	}
}

// stitchWindows joins chunk windows on their fixed overlap. The longest
// suffix/prefix match at or below ChunkOverlapRunes is the copied edge.
func stitchWindows(chunks [][]Message) string {
	var out []rune
	var prev []rune
	for i, chunk := range chunks {
		if len(chunk) != 1 {
			return ""
		}
		part := []rune(chunk[0].Content)
		if i == 0 {
			out = append(out, part...)
			prev = part
			continue
		}
		k := ChunkOverlapRunes
		if k > len(prev) {
			k = len(prev)
		}
		if k > len(part) {
			k = len(part)
		}
		found := -1
		for n := k; n >= 0; n-- {
			if runeSuffixPrefix(prev, part, n) {
				found = n
				break
			}
		}
		if found < 0 {
			return ""
		}
		out = append(out, part[found:]...)
		prev = part
	}
	return string(out)
}

func runeSuffixPrefix(prev, part []rune, n int) bool {
	if n == 0 {
		return true
	}
	a := prev[len(prev)-n:]
	b := part[:n]
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func loadLedgerFixture(t *testing.T) string {
	t.Helper()
	const path = "/tmp/luckyagent-compact-e2e-fixture-20261117.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var doc struct {
		LedgerUserMessage string `json:"ledger_user_message"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return doc.LedgerUserMessage
}
