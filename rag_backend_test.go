package tool

import (
	"context"
	"testing"

	"github.com/yurika0211/luckyagent-tools/rag"
)

type nilRAGBackend struct{}

func (*nilRAGBackend) SearchWithOptions(context.Context, string, rag.SearchOptions) ([]rag.RetrievalResult, error) {
	panic("nil backend called")
}
func (*nilRAGBackend) IndexFile(string) (*rag.Document, error) { panic("nil backend called") }

func TestRAGBackendTypedNilRemainsUnavailable(t *testing.T) {
	var backend *nilRAGBackend
	service := NewRAGToolService(backend)
	if _, err := service.HandleSearch(map[string]any{"query": "test"}); err == nil {
		t.Fatal("typed nil backend accepted")
	}
	if _, err := service.HandleIndex(map[string]any{"path": t.TempDir()}); err == nil {
		t.Fatal("typed nil backend accepted")
	}
}
