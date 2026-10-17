package memory

import (
	"context"
	"time"

	"github.com/yurika0211/luckyagent-tools/memory/diagnostic"
)

// Reader and Writer are the host's primary memory use-case boundaries.
type Reader interface {
	Recall(context.Context, RecallRequest) ([]SearchResult, error)
}
type Writer interface {
	Remember(context.Context, RememberRequest) (RememberResult, error)
}
type ContentAnalyzer interface{ AnalyzeMemoryContent(string) []HygieneIssue }
type ContextReader interface {
	Recent(int) []Entry
	ByTierWithOptions(Tier, RouteOptions) []Entry
	RouteWithOptions(string, RouteOptions) RouteAnalysis
	RecordActivationFeedback(string, []Entry, string, float64, time.Time)
}

type RecallRequest struct {
	Query   string
	Options SearchOptions
}
type RememberRequest struct {
	Content     string
	Category    string
	Tier        Tier
	Importance  float64
	Options     SaveOptions
	Mode        string
	EntryFilter func(Entry) bool
}
type RememberResult struct {
	SaveResult
	Superseded []string
}

// Inspector is the owner-facing read surface. It cannot mutate facts, configure
// ranking, close the backend or run filesystem migrations.
type Inspector interface {
	Reader
	Count() int
	Stats() map[Tier]int
	Get(string) (*Entry, error)
	Recent(int) []Entry
	GraphTopology(GraphTopologyOptions) GraphTopology
	Source() SourceInfo
	BuildSearchTrace(string, string, string, SearchOptions, []SearchResult, time.Duration) SearchTrace
}

type SourceInfo struct{ Kind, Location string }

type Hygiene interface {
	AuditHygiene(HygieneOptions) HygieneReport
	QuarantineDirty(HygieneOptions) (HygieneReport, error)
	DeleteDirty(HygieneOptions) (HygieneReport, error)
	RestoreHygiene(HygieneOptions) (HygieneReport, error)
}

type ToolBackend interface {
	Reader
	WriteBackend
	Hygiene
	Recent(int) []Entry
	Source() SourceInfo
	BuildSearchTrace(string, string, string, SearchOptions, []SearchResult, time.Duration) SearchTrace
}

type Maintenance interface {
	RecordTurn(string) MaintenanceEvent
	ForgetSession(string)
	Decay(float64) int
	Expire() int
	Promote(string) error
	CompactShortTerm(context.Context, int, int) (int, error)
}

// RuntimeStore excludes legacy Save*, raw search and reranker configuration.
// Consumers receive narrower facets; the application owns Close.
type RuntimeStore interface {
	Inspector
	ToolBackend
	ContextReader
	Maintenance
	TidalStats() (diagnostic.TidalStoreStats, bool, error)
	Close() error
}

type Runtime struct {
	Store     RuntimeStore
	ShortTerm SessionShortTermStore
	Summaries MidTermStore
}

type SummaryRequest struct {
	SessionID, UserID, Title, Content string
	Turns                             []ConversationTurn
}

// Store is the legacy adapter/fixture surface. Application consumers use
// Inspector, ToolBackend or RuntimeStore instead.
type Store interface {
	Reader
	Writer
	ContentAnalyzer
	Activate(query string, opts ActivationOptions) []ActivationScore
	ActivationReranker() ActivationReranker
	ActiveStateIDs(stateKey string) []string
	AuditHygiene(opts HygieneOptions) HygieneReport
	BacklinksFor(id string) []BacklinkContext
	BuildSearchTrace(query string, mode string, source string, opts SearchOptions, results []SearchResult, duration time.Duration) SearchTrace
	ByCategory(category string) []Entry
	ByTier(tier Tier) []Entry
	ByTierWithOptions(tier Tier, opts RouteOptions) []Entry
	Close() error
	Count() int
	Decay(threshold float64) int
	Dedup() int
	Delete(id string) error
	DeleteDirty(opts HygieneOptions) (HygieneReport, error)
	Dir() string
	Expire() int
	ForgetSession(sessionID string)
	Get(id string) (*Entry, error)
	GraphTopology(opts GraphTopologyOptions) GraphTopology
	MigrateGraphMemory(opts GraphMigrationOptions) (GraphMigrationReport, error)
	Promote(id string) error
	PurgeCategory(category string) int
	QuarantineDirty(opts HygieneOptions) (HygieneReport, error)
	Recent(n int) []Entry
	RecordActivationFeedback(query string, entries []Entry, signal string, value float64, now time.Time)
	RecordTurn(sessionID string) MaintenanceEvent
	RenameNotes(opts NoteRenameOptions) (NoteRenameReport, error)
	ResolveTemporal(query string, activeEntries []Entry) TemporalResolution
	RestoreHygiene(opts HygieneOptions) (HygieneReport, error)
	Route(query string) RouteAnalysis
	RouteWithOptions(query string, opts RouteOptions) RouteAnalysis
	Save(content string, category string) error
	SaveLongTerm(content string, category string) error
	SaveShortTerm(content string, category string) error
	SaveShortTermTTL(content string, category string, ttl time.Duration) error
	SaveWithMetadata(content string, category string, tier Tier, importance float64, tags []string, links []string, aliases []string) error
	SaveWithOptions(content string, category string, tier Tier, importance float64, opts SaveOptions) error
	SaveWithOptionsResult(content string, category string, tier Tier, importance float64, opts SaveOptions) (SaveResult, error)
	SaveWithTier(content string, category string, tier Tier, importance float64) error
	SaveWithTierAndTags(content string, category string, tier Tier, importance float64, tags []string) error
	Search(query string) []Entry
	SearchParallel(query string, limit int) []Entry
	SearchWithOptions(query string, opts SearchOptions) []SearchResult
	SetActivationReranker(reranker ActivationReranker)
	Stats() map[Tier]int
	Summarize(ids []string, summary string, category string) error
	SupersedeEntries(ids []string) ([]string, error)
	UnlinkedMentions() []UnlinkedMention
}

type ShortTermBuffer interface {
	Add(role string, content string)
	Clear()
	Compress()
	GetContext() []Message
	MaxTurns() int
	MessageCount() int
	String() string
	Summary() string
}

type SessionShortTermStore interface {
	Add(sessionID string, role string, content string)
	Buffer(sessionID string) ShortTermBuffer
	Clear(sessionID string)
	Get(sessionID string) (ShortTermBuffer, bool)
	GetContext(sessionID string) []Message
	SessionCount() int
	Summary(sessionID string) string
}

type MidTermStore interface {
	SaveSummary(context.Context, SummaryRequest) error
	Count() int
	Delete(sessionID string) error
	ExpireOldSummaries(olderThan time.Duration) int
	Get(sessionID string) (*SessionSummary, bool)
	ListAll() []SessionSummary
	SaveSessionSummary(summary *SessionSummary) error
	SearchSummaries(query string, topK int) []SessionSummary
}
