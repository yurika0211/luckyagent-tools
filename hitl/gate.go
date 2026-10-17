// Package hitl coordinates in-process human approval and input requests.
// A Gate belongs to one runtime identity; presentation and authorization belong
// to its caller. External agent bridges keep their own request lifecycles.
package hitl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	KindApproval   = "approval"
	KindInput      = "input"
	KindCredential = "credential"
	DefaultTimeout = 10 * time.Minute
)

type Resolution struct {
	Decision string
	Input    string
}

type Request struct {
	ID, Kind, SessionID, Tool, Action, Reason, Prompt, Summary string
	Args                                                       map[string]any
}

type Pending struct {
	Request
	CreatedAt time.Time
}

type pending struct {
	view     Pending
	response chan Resolution
}

type Options struct {
	Timeout time.Duration
}

type Gate struct {
	mu      sync.Mutex
	pending map[string]*pending
	timeout time.Duration
}

func New(opts Options) *Gate {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	return &Gate{pending: make(map[string]*pending), timeout: opts.Timeout}
}

func (g *Gate) List() []Pending {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Pending, 0, len(g.pending))
	for _, p := range g.pending {
		out = append(out, p.view)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// RequestAndWait registers before notifying and always removes the request on
// return. No response body is included in the notification or pending list.
func (g *Gate) RequestAndWait(ctx context.Context, req Request, notify func(Pending)) (Resolution, error) {
	if g == nil {
		return Resolution{}, fmt.Errorf("hitl gate is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Resolution{}, err
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		req.ID = newRequestID()
	}
	if req.Kind == "" {
		req.Kind = KindApproval
	}
	p := &pending{view: Pending{Request: req, CreatedAt: time.Now().UTC()}, response: make(chan Resolution, 1)}
	g.mu.Lock()
	if g.pending == nil {
		g.pending = make(map[string]*pending)
	}
	if _, exists := g.pending[req.ID]; exists {
		g.mu.Unlock()
		return Resolution{}, fmt.Errorf("hitl request %q is already pending", req.ID)
	}
	g.pending[req.ID] = p
	g.mu.Unlock()
	defer func() { g.mu.Lock(); delete(g.pending, req.ID); g.mu.Unlock() }()
	if notify != nil {
		notify(p.view)
	}
	waitCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		timeout := g.timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	select {
	case response := <-p.response:
		return response, nil
	case <-waitCtx.Done():
		return Resolution{}, fmt.Errorf("hitl request %s timed out or canceled: %w", req.ID, waitCtx.Err())
	}
}

// ResolveText restricts matching to the supplied session. An empty session
// retains the administrative behavior of matching all pending requests.
func (g *Gate) ResolveText(sessionID, text string) (Pending, bool, error) {
	if g == nil {
		return Pending{}, false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Pending{}, false, nil
	}
	var matches []Pending
	for _, p := range g.List() {
		if sessionID == "" || p.SessionID == sessionID {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return Pending{}, false, nil
	}
	var target *Pending
	for i := range matches {
		if strings.Contains(text, matches[i].ID) {
			target = &matches[i]
			break
		}
	}
	if target == nil && len(matches) == 1 {
		target = &matches[0]
	}
	if target == nil {
		return Pending{}, false, fmt.Errorf("multiple pending approvals; include the request id")
	}
	decision, input, ok := InterpretReply(target.Kind, text)
	if !ok {
		return Pending{}, false, nil
	}
	resolved, err := g.Resolve(target.ID, decision, input)
	return resolved, true, err
}

func (g *Gate) Resolve(id, decision, input string) (Pending, error) {
	if g == nil {
		return Pending{}, fmt.Errorf("hitl gate is unavailable")
	}
	id = strings.TrimSpace(id)
	decision = strings.ToLower(strings.TrimSpace(decision))
	if id == "" || decision == "" {
		return Pending{}, fmt.Errorf("approval_id and decision are required")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.pending[id]
	if !ok {
		return Pending{}, fmt.Errorf("approval %q is not pending", id)
	}
	select {
	case p.response <- Resolution{Decision: decision, Input: input}:
	default:
	}
	return p.view, nil
}

// InterpretReply never accepts a credential value through ordinary chat text.
func InterpretReply(kind, text string) (decision, input string, ok bool) {
	trimmed := strings.TrimSpace(text)
	head := strings.ToLower(trimmed)
	if fields := strings.Fields(head); len(fields) > 0 {
		head = fields[0]
	}
	switch head {
	case "允许", "同意", "批准", "yes", "y", "allow", "approve", "ok":
		return "allow", "", true
	case "拒绝", "取消", "no", "n", "deny", "reject", "cancel":
		return "deny", "", true
	}
	if kind == KindInput {
		return "submit", trimmed, true
	}
	return "", "", false
}

func IsAllowDecision(decision string) bool {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "allow", "allow_once", "allow_always", "accept", "approve", "yes", "y", "submit":
		return true
	default:
		return false
	}
}

func IsDenyDecision(decision string) bool {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "deny", "reject", "cancel", "cancelled", "no", "n":
		return true
	default:
		return false
	}
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("hitl-%d", time.Now().UnixNano())
	}
	return "hitl-" + hex.EncodeToString(b[:])
}
