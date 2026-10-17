// Package memory defines LuckyAgent-owned contracts and evidence values.
// It contains no persistence, ranking, provider or SDK implementation.
package memory

import "time"

const (
	TierShort Tier = iota
	TierMedium
	TierLong
)
const MaxDurableMemoryContentRunes = 4000

type ActivationComponents struct {
	Lexical    float64
	Category   float64
	Tags       float64
	Aliases    float64
	Links      float64
	Importance float64
	Tier       float64
	Recency    float64
	Access     float64
	GraphBoost float64
	TidalBoost float64
}

type ActivationEventRecorder interface {
	RecordMemoryActivation(query string, scores []ActivationScore, now time.Time)
}

type ActivationExcerpt struct {
	Heading string
	Block   string
	Context string
	Missing bool
}

type ActivationFeedback struct {
	Query   string
	QueryID string
	Entry   Entry
	Signal  string
	Value   float64
	At      time.Time
	Keys    []string
}

type ActivationFeedbackObserver interface {
	ObserveActivationFeedback(feedback ActivationFeedback)
}

type ActivationOptions struct {
	EntryFilter  func(Entry) bool
	Limit        int
	IncludeGraph bool
	// MaxGraphDepth bounds how many hops the graph spread walks outward from the
	// seed set (breadth-first). Depth 1 matches the original single-hop spread;
	// deeper hops apply an additional decay so distant notes contribute less.
	MaxGraphDepth     int
	MaxGraphBoost     float64
	MaxGraphSeeds     int
	UpdateAccessStats bool
	Explain           bool
}

type ActivationPath struct {
	FromID  string
	ToID    string
	Via     string
	Kind    string
	Heading string
	Block   string
	Weight  float64
	Depth   int
	Boost   float64
}

type ActivationReranker interface {
	RerankMemoryActivations(query string, scores []ActivationScore, now time.Time) []ActivationScore
}

type ActivationScore struct {
	EntryID     string
	Entry       Entry
	Score       float64
	Components  ActivationComponents
	Paths       []ActivationPath
	Excerpts    []ActivationExcerpt
	DirectScore float64
}

type AppliedRoutePolicy struct {
	ID          string `json:"id"`
	EntryID     string `json:"entry_id"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

type BacklinkContext struct {
	SourceID string `json:"source_id"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	Heading  string `json:"heading,omitempty"`
	Block    string `json:"block,omitempty"`
	Display  string `json:"display,omitempty"`
	Embed    bool   `json:"embed,omitempty"`
	Line     int    `json:"line"`
	Excerpt  string `json:"excerpt"`
}

type ConversationTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Entry struct {
	ID            string        `json:"id"`
	Content       string        `json:"content"`
	Category      string        `json:"category"`
	Tier          Tier          `json:"tier"`
	Importance    float64       `json:"importance"`   // 0.0 ~ 1.0，越高越重要
	AccessCount   int           `json:"access_count"` // 被检索次数
	CreatedAt     time.Time     `json:"created_at"`
	AccessedAt    time.Time     `json:"accessed_at"` // 最后被检索时间
	Tags          []string      `json:"tags,omitempty"`
	SummaryOf     []string      `json:"summary_of,omitempty"` // 如果是摘要，记录原始条目 ID
	ExpiresAt     *time.Time    `json:"expires_at,omitempty"` // 过期时间，nil 表示永不过期
	Status        string        `json:"status,omitempty"`     // active/superseded/archived/conflict
	ValidFrom     time.Time     `json:"valid_from,omitempty"`
	ValidUntil    *time.Time    `json:"valid_until,omitempty"`
	Links         []string      `json:"links,omitempty"`     // Obsidian wikilinks referenced by this note
	Aliases       []string      `json:"aliases,omitempty"`   // Obsidian note aliases / concept aliases
	StateKey      string        `json:"state_key,omitempty"` // Stable key for temporal state resolution
	StateValue    string        `json:"state_value,omitempty"`
	Confidence    float64       `json:"confidence,omitempty"`
	Supersedes    []string      `json:"supersedes,omitempty"`
	RoutePolicies []RoutePolicy `json:"route_policies,omitempty"`
	BlockID       string        `json:"block_id,omitempty"` // Stable Obsidian block id for exact references
	Path          string        `json:"path,omitempty"`     // Path relative to the memory vault
}

type GraphLinkRef struct {
	Heading string `json:"heading,omitempty"`
	Block   string `json:"block,omitempty"`
	Display string `json:"display,omitempty"`
	Embed   bool   `json:"embed,omitempty"`
	// Ambiguous means the written name matches more than one note. The edge
	// stays visible, but it is not a resolved relation and does not add degree.
	Ambiguous  bool     `json:"ambiguous,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

type GraphMigrationEntryPlan struct {
	ID          string   `json:"id"`
	Path        string   `json:"path,omitempty"`
	Category    string   `json:"category"`
	Tier        string   `json:"tier"`
	AddLinks    []string `json:"add_links,omitempty"`
	AddAliases  []string `json:"add_aliases,omitempty"`
	AddTags     []string `json:"add_tags,omitempty"`
	Archive     bool     `json:"archive,omitempty"`
	ArchivePath string   `json:"archive_path,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	Preview     string   `json:"preview,omitempty"`
}

type GraphMigrationOptions struct {
	Apply            bool
	ArchiveDirty     bool
	MaxDirtyFindings int
	Now              time.Time
}

type GraphMigrationReport struct {
	Scanned          int                       `json:"scanned"`
	WouldUpdateLinks int                       `json:"would_update_links"`
	UpdatedLinks     int                       `json:"updated_links,omitempty"`
	WouldArchive     int                       `json:"would_archive"`
	Archived         int                       `json:"archived,omitempty"`
	DirtyFindings    []HygieneIssue            `json:"dirty_findings,omitempty"`
	Entries          []GraphMigrationEntryPlan `json:"entries,omitempty"`
	Apply            bool                      `json:"apply"`
}

type GraphTopology struct {
	Nodes         []GraphTopologyNode `json:"nodes"`
	Edges         []GraphTopologyEdge `json:"edges"`
	TotalNotes    int                 `json:"total_notes"`
	TotalEdges    int                 `json:"total_edges"`
	IsolatedCount int                 `json:"isolated_count"`
	Unresolved    int                 `json:"unresolved"`
	Truncated     bool                `json:"truncated"`
	Categories    []string            `json:"categories,omitempty"`
}

type GraphTopologyEdge struct {
	Source string         `json:"source"`
	Target string         `json:"target"`
	Weight int            `json:"weight"`
	Refs   []GraphLinkRef `json:"refs,omitempty"`
}

type GraphTopologyNode struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Category   string   `json:"category,omitempty"`
	Tier       string   `json:"tier,omitempty"`
	Path       string   `json:"path,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Importance float64  `json:"importance"`
	Degree     int      `json:"degree"`
	// Resolved is false for a link target with no note behind it — Obsidian's
	// "unresolved link". Keeping them visible is the point: they show where the
	// vault refers to something it has not written down.
	Resolved bool `json:"resolved"`
	// Ambiguous is true when a link name matches more than one note. The graph
	// lists the candidates and does not pick the first registered note.
	Ambiguous  bool     `json:"ambiguous,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

type GraphTopologyOptions struct {
	// Limit caps the node count, keeping the most connected nodes. Zero means
	// no cap.
	Limit int
	// IncludeIsolated keeps notes that neither link out nor are linked to.
	// They dominate the node count in a typical vault while carrying no
	// topology, so they are excluded by default.
	IncludeIsolated bool
}

type HygieneIssue struct {
	ID              string  `json:"id"`
	Path            string  `json:"path,omitempty"`
	Category        string  `json:"category"`
	Tier            string  `json:"tier"`
	Severity        string  `json:"severity"`
	Reason          string  `json:"reason"`
	SuggestedAction string  `json:"suggested_action"`
	Score           float64 `json:"score"`
	Preview         string  `json:"preview"`
}

type HygieneOptions struct {
	IncludeInactive bool
	MinSeverity     string
	MaxFindings     int
	Now             time.Time
	IDs             []string
}

type HygieneReport struct {
	Scanned     int            `json:"scanned"`
	Findings    []HygieneIssue `json:"findings"`
	Quarantined int            `json:"quarantined,omitempty"`
	Deleted     int            `json:"deleted,omitempty"`
	Restored    int            `json:"restored,omitempty"`
	DryRun      bool           `json:"dry_run,omitempty"`
	Action      string         `json:"action"`
}

type MaintenanceConfig struct {
	DecayEvery     uint64
	SummarizeEvery uint64
	ExpireEvery    uint64
}

type MaintenanceEvent struct {
	SessionID    string
	RuntimeCount uint64
	SessionCount uint64
	RunDecay     bool
	RunSummarize bool
	RunExpire    bool
}

type Message struct {
	Role    string
	Content string
}

type NoteDuplicatePlan struct {
	ID     string `json:"id"`
	Keep   string `json:"keep"`
	Remove string `json:"remove"`
	Backup string `json:"backup,omitempty"`
}

type NoteRenameOptions struct {
	Apply bool
	Limit int
}

type NoteRenamePlan struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Category string `json:"category,omitempty"`
	Tier     string `json:"tier,omitempty"`
	Preview  string `json:"preview,omitempty"`
}

type NoteRenameReport struct {
	Scanned               int                 `json:"scanned"`
	WouldRename           int                 `json:"would_rename"`
	Renamed               int                 `json:"renamed,omitempty"`
	WouldPruneDuplicates  int                 `json:"would_prune_duplicates"`
	PrunedDuplicates      int                 `json:"pruned_duplicates,omitempty"`
	Entries               []NoteRenamePlan    `json:"entries,omitempty"`
	DuplicatePruneEntries []NoteDuplicatePlan `json:"duplicate_prune_entries,omitempty"`
	Apply                 bool                `json:"apply"`
}

type RouteAnalysis struct {
	Query             string                         `json:"query"`
	Entries           []Entry                        `json:"entries"`
	Excerpts          map[string][]ActivationExcerpt `json:"excerpts,omitempty"`
	ToolRequirements  []RouteToolRequirement         `json:"tool_requirements,omitempty"`
	Risks             []RouteRisk                    `json:"risks,omitempty"`
	AppliedPolicies   []AppliedRoutePolicy           `json:"applied_policies,omitempty"`
	RequiredTools     []string                       `json:"required_tools,omitempty"`
	SuggestedSearches []string                       `json:"suggested_searches,omitempty"`
	RiskFlags         []string                       `json:"risk_flags,omitempty"`
	Constraints       []string                       `json:"constraints,omitempty"`
	Clarifications    []string                       `json:"clarifications,omitempty"`
	TemporalNotes     []string                       `json:"temporal_notes,omitempty"`
	EvidenceRefs      []string                       `json:"evidence_refs,omitempty"`
	SupersededRefs    []string                       `json:"superseded_refs,omitempty"`
	ConflictRefs      []string                       `json:"conflict_refs,omitempty"`
	ExpiredRefs       []string                       `json:"expired_refs,omitempty"`
	FutureRefs        []string                       `json:"future_refs,omitempty"`
}

type RouteOptions struct {
	EntryFilter func(Entry) bool
}

type RoutePolicy struct {
	ID             string                 `json:"id" yaml:"id"`
	Match          RoutePolicyMatch       `json:"match,omitempty" yaml:"match,omitempty"`
	Risks          []RouteRisk            `json:"risks,omitempty" yaml:"risks,omitempty"`
	RequiredTools  []RouteToolRequirement `json:"required_tools,omitempty" yaml:"required_tools,omitempty"`
	Constraints    []string               `json:"constraints,omitempty" yaml:"constraints,omitempty"`
	Clarifications []string               `json:"clarifications,omitempty" yaml:"clarifications,omitempty"`
}

type RoutePolicyMatch struct {
	QueryAll  []RouteTermGroup  `json:"query_all,omitempty" yaml:"query_all,omitempty"`
	QueryAny  []string          `json:"query_any,omitempty" yaml:"query_any,omitempty"`
	QueryNone []string          `json:"query_none,omitempty" yaml:"query_none,omitempty"`
	States    []RouteStateMatch `json:"states,omitempty" yaml:"states,omitempty"`
}

type RouteRisk struct {
	Name     string `json:"name" yaml:"name"`
	Priority int    `json:"priority,omitempty" yaml:"priority,omitempty"`
}

type RouteStateMatch struct {
	Key       string   `json:"key" yaml:"key"`
	Values    []string `json:"values,omitempty" yaml:"values,omitempty"`
	NotValues []string `json:"not_values,omitempty" yaml:"not_values,omitempty"`
}

type RouteTermGroup struct {
	Any []string `json:"any" yaml:"any"`
}

type RouteToolCall struct {
	Arguments map[string]any `json:"arguments,omitempty" yaml:"arguments,omitempty"`
}

type RouteToolRequirement struct {
	Name  string          `json:"name" yaml:"name"`
	Calls []RouteToolCall `json:"calls,omitempty" yaml:"calls,omitempty"`
}

type SaveOptions struct {
	Tags          []string
	Links         []string
	Aliases       []string
	Status        string
	ValidFrom     time.Time
	ValidUntil    *time.Time
	ExpiresAt     *time.Time
	StateKey      string
	StateValue    string
	Confidence    float64
	Supersedes    []string
	RoutePolicies []RoutePolicy
}

type SaveResult struct {
	ID              string `json:"id,omitempty"`
	Path            string `json:"path,omitempty"`
	Created         bool   `json:"created"`
	UpdatedExisting bool   `json:"updated_existing"`
	DuplicateOf     string `json:"duplicate_of,omitempty"`
}

type SearchOptions struct {
	EntryFilter     func(Entry) bool
	Limit           int
	Category        string
	Tier            *Tier
	IncludeInactive bool
	IncludeExpired  bool
	AsOf            time.Time
	IncludeGraph    bool
	GraphDepth      int
	Explain         bool
	SkipAccessStats bool
}

type SearchResult struct {
	Entry       Entry               `json:"entry"`
	Score       float64             `json:"score"`
	DirectScore float64             `json:"direct_score,omitempty"`
	GraphScore  float64             `json:"graph_score,omitempty"`
	Paths       []ActivationPath    `json:"paths,omitempty"`
	Excerpts    []ActivationExcerpt `json:"excerpts,omitempty"`
}

type SearchTrace struct {
	Query      string              `json:"query"`
	Mode       string              `json:"mode,omitempty"`
	Source     string              `json:"source,omitempty"`
	Limit      int                 `json:"limit,omitempty"`
	GraphDepth int                 `json:"graph_depth"`
	Filters    SearchTraceFilters  `json:"filters,omitempty"`
	Seeds      []SearchTraceNode   `json:"seeds,omitempty"`
	Hops       []SearchTraceHop    `json:"hops,omitempty"`
	Results    []SearchTraceResult `json:"results,omitempty"`
	Temporal   []string            `json:"temporal_notes,omitempty"`
	Warnings   []string            `json:"warnings,omitempty"`
	DurationMS int64               `json:"duration_ms,omitempty"`
}

type SearchTraceFilters struct {
	Category        string `json:"category,omitempty"`
	Tier            string `json:"tier,omitempty"`
	IncludeInactive bool   `json:"include_inactive,omitempty"`
	IncludeExpired  bool   `json:"include_expired,omitempty"`
	AsOf            string `json:"as_of,omitempty"`
}

type SearchTraceHop struct {
	Depth       int     `json:"depth"`
	FromID      string  `json:"from_id"`
	FromRef     string  `json:"from_ref,omitempty"`
	ToID        string  `json:"to_id"`
	ToRef       string  `json:"to_ref,omitempty"`
	Via         string  `json:"via,omitempty"`
	Kind        string  `json:"kind,omitempty"`
	Weight      float64 `json:"weight,omitempty"`
	Boost       float64 `json:"boost,omitempty"`
	SourceScore float64 `json:"source_score,omitempty"`
	TargetScore float64 `json:"target_score,omitempty"`
}

type SearchTraceNode struct {
	ID             string  `json:"id"`
	Ref            string  `json:"ref,omitempty"`
	Category       string  `json:"category,omitempty"`
	Tier           string  `json:"tier,omitempty"`
	Score          float64 `json:"score,omitempty"`
	DirectScore    float64 `json:"direct_score,omitempty"`
	GraphScore     float64 `json:"graph_score,omitempty"`
	ContentPreview string  `json:"content_preview,omitempty"`
}

type SearchTraceResult struct {
	SearchTraceNode
	Rank int `json:"rank"`
}

type SessionSummary struct {
	SessionID     string    `json:"session_id"`
	UserID        string    `json:"user_id"`
	CreatedAt     time.Time `json:"created_at"`
	Topics        []string  `json:"topics"`
	KeyDecisions  []string  `json:"key_decisions"`
	OpenQuestions []string  `json:"open_questions"`
	CodeContext   string    `json:"code_context"`
	RawSummary    string    `json:"raw_summary"`
}

type TemporalResolution struct {
	Entries        []Entry
	Notes          []string
	SupersededRefs []string
	ConflictRefs   []string
	ExpiredRefs    []string
	FutureRefs     []string
}

type TidalFeedback struct {
	Query   string
	QueryID string
	Entry   Entry
	Signal  string
	Value   float64
	At      time.Time
	Keys    []string
}

type Tier int

type UnlinkedMention struct {
	SourceID string `json:"source_id"`
	Source   string `json:"source"`
	Name     string `json:"name"`
	TargetID string `json:"target_id"`
	Line     int    `json:"line"`
	Excerpt  string `json:"excerpt"`
}

type WikiLink struct {
	Target  string `json:"target"`
	Heading string `json:"heading,omitempty"`
	Block   string `json:"block,omitempty"`
	Display string `json:"display,omitempty"`
	Embed   bool   `json:"embed,omitempty"`
	Raw     string `json:"raw,omitempty"`
}

func (t Tier) String() string {
	switch t {
	case TierShort:
		return "short"
	case TierMedium:
		return "medium"
	case TierLong:
		return "long"
	default:
		return "unknown"
	}
}
