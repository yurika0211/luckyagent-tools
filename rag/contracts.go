package rag

import (
	"context"
	"time"
)

// SearchOptions configures a single RAG search without mutating shared
// retriever state.
type SearchOptions struct {
	TopK         int
	MinScore     float64
	UseMMR       bool
	MMRLambda    float64
	FilterSource string
}

// RetrievalResult is a result from the RAG retriever.
type RetrievalResult struct {
	ChunkID   string
	Content   string
	Score     float64
	Metadata  map[string]string
	DocTitle  string
	DocSource string
}

// Document is an indexed document with its chunks.
type Document struct {
	ID        string
	Path      string
	Title     string
	Chunks    []string // chunk IDs
	IndexedAt time.Time
	Metadata  map[string]string
}

// Backend supplies retrieval and indexing without exposing host managers.
type Backend interface {
	SearchWithOptions(context.Context, string, SearchOptions) ([]RetrievalResult, error)
	IndexFile(string) (*Document, error)
}
