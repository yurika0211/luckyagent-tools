package tool

import (
	"archive/zip"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCurrentTimeTool(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	result, err := r.Call("current_time", map[string]any{})
	if err != nil {
		t.Fatalf("current_time call: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty time result")
	}
}

func TestCurrentTimeToolUsesNetworkTimeForLocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Asia/Shanghai" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"datetime":"2026-05-14T10:00:00+08:00"}`))
	}))
	defer server.Close()

	oldClient := currentTimeHTTPClient
	oldBase := currentTimeAPIBaseURL
	currentTimeHTTPClient = server.Client()
	currentTimeAPIBaseURL = server.URL
	defer func() {
		currentTimeHTTPClient = oldClient
		currentTimeAPIBaseURL = oldBase
	}()

	r := NewRegistry()
	RegisterBuiltinTools(r)

	result, err := r.Call("current_time", map[string]any{
		"location": "北京",
	})
	if err != nil {
		t.Fatalf("current_time call: %v", err)
	}
	if !strings.Contains(result, "Asia/Shanghai") {
		t.Fatalf("expected timezone in result, got %q", result)
	}
	if !strings.Contains(result, "location: 北京") {
		t.Fatalf("expected location label, got %q", result)
	}
}

func TestCurrentTimeToolFallsBackToLocalOnNetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	oldClient := currentTimeHTTPClient
	oldBase := currentTimeAPIBaseURL
	currentTimeHTTPClient = server.Client()
	currentTimeAPIBaseURL = server.URL
	defer func() {
		currentTimeHTTPClient = oldClient
		currentTimeAPIBaseURL = oldBase
	}()

	r := NewRegistry()
	RegisterBuiltinTools(r)

	result, err := r.Call("current_time", map[string]any{
		"location": "北京",
	})
	if err != nil {
		t.Fatalf("current_time call: %v", err)
	}
	if !strings.Contains(result, "source: local") {
		t.Fatalf("expected local fallback, got %q", result)
	}
}

func TestCalculateTool(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	result, err := r.Call("calculate", map[string]any{
		"expression": "sqrt(144)+pow(2,3)",
	})
	if err != nil {
		t.Fatalf("calculate call: %v", err)
	}
	if result != "20" {
		t.Fatalf("expected 20, got %q", result)
	}
}

func TestLogTool(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "app.log")
	content := "one\nerror first\ntwo\nthree\nerror second\nfour\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	tail, err := r.Call("data", map[string]any{"action": "log", "mode": "tail", "path": testFile, "lines": 2})
	if err != nil {
		t.Fatalf("log tail: %v", err)
	}
	if !strings.Contains(tail, "error second") || !strings.Contains(tail, "four") {
		t.Fatalf("unexpected tail output: %q", tail)
	}

	grep, err := r.Call("data", map[string]any{"action": "log", "mode": "grep", "path": testFile, "pattern": "error", "before": 1, "after": 0})
	if err != nil {
		t.Fatalf("log grep: %v", err)
	}
	if !strings.Contains(grep, "> 2| error first") || !strings.Contains(grep, "> 5| error second") {
		t.Fatalf("unexpected grep output: %q", grep)
	}

	noMatch, err := r.Call("data", map[string]any{"action": "log", "mode": "grep", "path": testFile, "pattern": "panic"})
	if err != nil {
		t.Fatalf("log grep no match should not fail: %v", err)
	}
	if !strings.Contains(noMatch, "No matches") {
		t.Fatalf("unexpected no-match output: %q", noMatch)
	}

	ignoreCase, err := r.Call("data", map[string]any{"action": "log", "mode": "grep", "path": testFile, "pattern": "ERROR", "ignore_case": true, "max_matches": 1})
	if err != nil {
		t.Fatalf("log grep ignore_case: %v", err)
	}
	if !strings.Contains(ignoreCase, "> 2| error first") {
		t.Fatalf("unexpected ignore_case output: %q", ignoreCase)
	}

	numberedTail, err := r.Call("data", map[string]any{"action": "log", "mode": "tail", "path": testFile, "lines": 2, "with_line_numbers": true})
	if err != nil {
		t.Fatalf("log tail with line numbers: %v", err)
	}
	if !strings.Contains(numberedTail, "5| error second") || !strings.Contains(numberedTail, "6| four") {
		t.Fatalf("unexpected numbered tail output: %q", numberedTail)
	}

	if _, err := r.Call("data", map[string]any{"action": "log", "path": testFile}); err == nil || !strings.Contains(err.Error(), "invalid data log mode") {
		t.Fatalf("expected invalid data log mode, got %v", err)
	}
}

func TestHTTPRequestToolValidation(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	_, err := r.Call("http_request", map[string]any{
		"url": "http://localhost:9999/test",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "localhost") {
		t.Fatalf("expected localhost validation error, got %v", err)
	}
}

func TestStructuredQueryTools(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()

	jsonPath := filepath.Join(tmpDir, "sample.json")
	if err := os.WriteFile(jsonPath, []byte(`{"user":{"name":"Ada"},"metadata":{"app.kubernetes.io/name":"api"},"items":[{"id":1},{"id":2}]}`), 0644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	jsonResult, err := r.Call("data", map[string]any{"action": "query", "format": "json", "path": jsonPath, "query": "items[1].id"})
	if err != nil {
		t.Fatalf("data_query json: %v", err)
	}
	if strings.TrimSpace(jsonResult) != "2" {
		t.Fatalf("unexpected data_query json output: %q", jsonResult)
	}
	jsonBracketResult, err := r.Call("data", map[string]any{"action": "query", "format": "json", "path": jsonPath, "query": `metadata["app.kubernetes.io/name"]`})
	if err != nil {
		t.Fatalf("data_query json bracket key: %v", err)
	}
	if strings.TrimSpace(jsonBracketResult) != `"api"` {
		t.Fatalf("unexpected data_query json bracket output: %q", jsonBracketResult)
	}
	jsonWildcardResult, err := r.Call("data", map[string]any{"action": "query", "format": "json", "path": jsonPath, "query": "items[*].id"})
	if err != nil {
		t.Fatalf("data_query json wildcard: %v", err)
	}
	if !strings.Contains(jsonWildcardResult, "1") || !strings.Contains(jsonWildcardResult, "2") {
		t.Fatalf("unexpected data_query json wildcard output: %q", jsonWildcardResult)
	}

	yamlPath := filepath.Join(tmpDir, "sample.yaml")
	if err := os.WriteFile(yamlPath, []byte("service:\n  name: api\n---\nservice:\n  name: worker\n"), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	yamlResult, err := r.Call("data", map[string]any{"action": "query", "format": "yaml", "path": yamlPath, "query": "service.name"})
	if err != nil {
		t.Fatalf("data_query yaml: %v", err)
	}
	if strings.TrimSpace(yamlResult) != `"api"` {
		t.Fatalf("unexpected data_query yaml output: %q", yamlResult)
	}
	yamlDocResult, err := r.Call("data", map[string]any{"action": "query", "format": "yml", "path": yamlPath, "query": "service.name", "document": 1})
	if err != nil {
		t.Fatalf("data_query yaml document: %v", err)
	}
	if strings.TrimSpace(yamlDocResult) != `"worker"` {
		t.Fatalf("unexpected data_query yaml document output: %q", yamlDocResult)
	}
	yamlAllResult, err := r.Call("data", map[string]any{"action": "query", "format": "yaml", "path": yamlPath, "query": "service.name", "all_documents": true})
	if err != nil {
		t.Fatalf("data_query yaml all_documents: %v", err)
	}
	if !strings.Contains(yamlAllResult, `"api"`) || !strings.Contains(yamlAllResult, `"worker"`) {
		t.Fatalf("unexpected data_query yaml all_documents output: %q", yamlAllResult)
	}
	if _, err := r.Call("data", map[string]any{"path": jsonPath, "query": "items[1].id"}); err == nil || !strings.Contains(err.Error(), "invalid data action") {
		t.Fatalf("expected invalid data_query format, got %v", err)
	}

	csvPath := filepath.Join(tmpDir, "sample.csv")
	if err := os.WriteFile(csvPath, []byte("name,role\nAda,admin\nBob,user\n"), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	csvResult, err := r.Call("data", map[string]any{"action": "csv", "path": csvPath, "column": "role", "equals": "admin"})
	if err != nil {
		t.Fatalf("csv_query: %v", err)
	}
	if !strings.Contains(csvResult, `"name": "Ada"`) {
		t.Fatalf("unexpected csv_query output: %q", csvResult)
	}
}

func TestSQLiteTools(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "sample.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT); INSERT INTO users(name) VALUES ('Ada'), ('Bob');`); err != nil {
		t.Fatalf("seed sqlite: %v", err)
	}

	schemaResult, err := r.Call("data", map[string]any{"action": "schema", "path": dbPath, "table": "users"})
	if err != nil {
		t.Fatalf("db_schema: %v", err)
	}
	if !strings.Contains(schemaResult, `"name": "id"`) || !strings.Contains(schemaResult, `"name": "name"`) {
		t.Fatalf("unexpected db_schema output: %q", schemaResult)
	}

	queryResult, err := r.Call("data", map[string]any{"action": "sql", "path": dbPath, "query": "SELECT name FROM users ORDER BY id"})
	if err != nil {
		t.Fatalf("sql_query: %v", err)
	}
	if !strings.Contains(queryResult, `"name": "Ada"`) || !strings.Contains(queryResult, `"name": "Bob"`) {
		t.Fatalf("unexpected sql_query output: %q", queryResult)
	}
}

func TestFileReadWriteTool(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	// 创建临时目录
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// 写文件
	writeResult, err := r.Call("file_write", map[string]any{
		"path":    testFile,
		"content": "Hello, LuckyAgent!",
	})
	if err != nil {
		t.Fatalf("file_write: %v", err)
	}
	if writeResult == "" {
		t.Error("expected write result")
	}

	// 读文件
	readResult, err := r.Call("file_read", map[string]any{
		"path": testFile,
	})
	if err != nil {
		t.Fatalf("file_read: %v", err)
	}
	if readResult == "" {
		t.Error("expected read result")
	}
}

func TestFileWriteToolModesDryRunAndHash(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	path := filepath.Join(t.TempDir(), "write.txt")
	dryRun, err := r.Call("file_write", map[string]any{
		"path":    path,
		"content": "planned",
		"mode":    "create",
		"dry_run": true,
	})
	if err != nil {
		t.Fatalf("file_write dry_run: %v", err)
	}
	if !strings.Contains(dryRun, "Dry run") || !strings.Contains(dryRun, `"action": "create"`) {
		t.Fatalf("unexpected dry_run result: %q", dryRun)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry_run should not create file, got %v", err)
	}

	if _, err := r.Call("file_write", map[string]any{
		"path":    path,
		"content": "initial",
		"mode":    "create",
	}); err != nil {
		t.Fatalf("file_write create: %v", err)
	}
	if _, err := r.Call("file_write", map[string]any{
		"path":    path,
		"content": "again",
		"mode":    "create",
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected create mode existing error, got %v", err)
	}

	oldHash := sha256Hex([]byte("initial"))
	if _, err := r.Call("file_write", map[string]any{
		"path":            path,
		"content":         "updated",
		"expected_sha256": oldHash,
	}); err != nil {
		t.Fatalf("file_write expected hash: %v", err)
	}
	if _, err := r.Call("file_write", map[string]any{
		"path":            path,
		"content":         "bad",
		"expected_sha256": oldHash,
	}); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected hash mismatch, got %v", err)
	}

	skip, err := r.Call("file_write", map[string]any{
		"path":    path,
		"content": "updated",
	})
	if err != nil {
		t.Fatalf("file_write same content: %v", err)
	}
	if !strings.Contains(skip, "Skipped write") {
		t.Fatalf("expected same-content skip, got %q", skip)
	}
}

func TestFileWriteToolPreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bit preservation is platform-specific on Windows")
	}
	r := NewRegistry()
	RegisterBuiltinTools(r)

	path := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if _, err := r.Call("file_write", map[string]any{
		"path":    path,
		"content": "new",
	}); err != nil {
		t.Fatalf("file_write overwrite: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat script: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("expected permissions 0755, got %o", got)
	}
}

func TestFileReadToolRangeAndContinuation(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	path := filepath.Join(t.TempDir(), "range.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatalf("write range file: %v", err)
	}

	result, err := r.Call("file_read", map[string]any{
		"path":   path,
		"offset": 2,
		"limit":  2,
	})
	if err != nil {
		t.Fatalf("file_read range: %v", err)
	}
	for _, want := range []string{"File:", "Size:", "2| two", "3| three", "use offset=4"} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected %q in result:\n%s", want, result)
		}
	}
	if strings.Contains(result, "4| four") {
		t.Fatalf("limit should hide line 4:\n%s", result)
	}
}

func TestFileReadToolRejectsBinary(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	path := filepath.Join(t.TempDir(), "blob.bin")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0x02, 0x03}, 0o644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}

	_, err := r.Call("file_read", map[string]any{"path": path})
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("expected binary rejection, got %v", err)
	}
}

func TestDocumentReadToolReadsDocx(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	path := filepath.Join(t.TempDir(), "sample.docx")
	writeZipTestFile(t, path, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Hello DOCX</w:t></w:r></w:p><w:p><w:r><w:t>Second paragraph</w:t></w:r></w:p></w:body></w:document>`,
		"word/header1.xml":  `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>Header text</w:t></w:r></w:p></w:hdr>`,
		"word/comments.xml": `<w:comments xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:comment><w:p><w:r><w:t>Review comment</w:t></w:r></w:p></w:comment></w:comments>`,
	})

	result, err := r.Call("document_read", map[string]any{"path": path})
	if err != nil {
		t.Fatalf("document_read docx: %v", err)
	}
	if !strings.Contains(result, "Format: docx") || !strings.Contains(result, "Hello DOCX") || !strings.Contains(result, "Header text") || !strings.Contains(result, "Review comment") {
		t.Fatalf("unexpected docx extraction:\n%s", result)
	}
}

func TestDocumentReadToolReadsPptxWithLimit(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	path := filepath.Join(t.TempDir(), "slides.pptx")
	writeZipTestFile(t, path, map[string]string{
		"ppt/presentation.xml":            `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"/>`,
		"ppt/slides/slide2.xml":           `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Second slide</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/slide1.xml":           `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>First slide</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/notesSlides/notesSlide1.xml": `<p:notes xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Speaker note</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:notes>`,
	})

	result, err := r.Call("document_read", map[string]any{"path": path, "limit": 4})
	if err != nil {
		t.Fatalf("document_read pptx: %v", err)
	}
	if !strings.Contains(result, "Format: pptx") || !strings.Contains(result, "[Slide 1]") || !strings.Contains(result, "First slide") {
		t.Fatalf("unexpected pptx extraction:\n%s", result)
	}
	if strings.Contains(result, "Second slide") {
		t.Fatalf("limit should hide later slide content:\n%s", result)
	}
	if !strings.Contains(result, "use offset=") {
		t.Fatalf("expected continuation hint for limited output:\n%s", result)
	}
}

func TestDocumentReadToolDetectsOfficeFormatByContent(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	path := filepath.Join(t.TempDir(), "misnamed.bin")
	writeZipTestFile(t, path, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Detected DOCX</w:t></w:r></w:p></w:body></w:document>`,
	})

	result, err := r.Call("document_read", map[string]any{"path": path})
	if err != nil {
		t.Fatalf("document_read detected docx: %v", err)
	}
	if !strings.Contains(result, "Format: docx") || !strings.Contains(result, "Detected DOCX") {
		t.Fatalf("unexpected detected docx extraction:\n%s", result)
	}
}

func TestDocumentReadToolRejectsLegacyOffice(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	path := filepath.Join(t.TempDir(), "legacy.doc")
	if err := os.WriteFile(path, []byte("legacy"), 0o600); err != nil {
		t.Fatalf("write legacy doc: %v", err)
	}
	_, err := r.Call("document_read", map[string]any{"path": path})
	if err == nil || !strings.Contains(err.Error(), "convert to .docx") {
		t.Fatalf("expected legacy doc conversion error, got %v", err)
	}
}

func TestFileMkdirMoveDeleteTools(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "ops")
	nested := filepath.Join(root, "a", "b")

	mkdirResult, err := r.Call("file_mkdir", map[string]any{
		"path": nested,
	})
	if err != nil {
		t.Fatalf("file_mkdir: %v", err)
	}
	if !strings.Contains(mkdirResult, "Created directory") {
		t.Fatalf("unexpected mkdir result: %q", mkdirResult)
	}

	src := filepath.Join(nested, "note.txt")
	if err := os.WriteFile(src, []byte("payload"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	dst := filepath.Join(root, "renamed.txt")

	moveResult, err := r.Call("file_move", map[string]any{
		"src": src,
		"dst": dst,
	})
	if err != nil {
		t.Fatalf("file_move: %v", err)
	}
	if !strings.Contains(moveResult, "Moved file") {
		t.Fatalf("unexpected move result: %q", moveResult)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("expected moved file at destination: %v", err)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected source to be gone, got %v", err)
	}

	deleteResult, err := r.Call("file_delete", map[string]any{
		"path": dst,
	})
	if err != nil {
		t.Fatalf("file_delete file: %v", err)
	}
	if !strings.Contains(deleteResult, "Deleted") {
		t.Fatalf("unexpected delete result: %q", deleteResult)
	}

	_, err = r.Call("file_delete", map[string]any{
		"path": root,
	})
	if err == nil {
		t.Fatal("expected non-recursive directory delete to fail")
	}

	_, err = r.Call("file_delete", map[string]any{
		"path":      root,
		"recursive": true,
	})
	if err != nil {
		t.Fatalf("file_delete recursive: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected root to be deleted, got %v", err)
	}

	missingResult, err := r.Call("file_delete", map[string]any{
		"path":       root,
		"missing_ok": true,
	})
	if err != nil {
		t.Fatalf("file_delete missing_ok: %v", err)
	}
	if !strings.Contains(missingResult, "already absent") {
		t.Fatalf("unexpected missing_ok result: %q", missingResult)
	}
}

func TestFileMoveToolDryRunAndSafetyChecks(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dryRun, err := r.Call("file_move", map[string]any{
		"src":     src,
		"dst":     dst,
		"dry_run": true,
	})
	if err != nil {
		t.Fatalf("file_move dry_run: %v", err)
	}
	if !strings.Contains(dryRun, "Dry run") || !strings.Contains(dryRun, `"kind": "file"`) {
		t.Fatalf("unexpected dry_run result: %q", dryRun)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("dry_run should keep source: %v", err)
	}

	dir := filepath.Join(tmpDir, "dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	_, err = r.Call("file_move", map[string]any{
		"src": dir,
		"dst": filepath.Join(dir, "child"),
	})
	if err == nil || !strings.Contains(err.Error(), "into itself") {
		t.Fatalf("expected self move rejection, got %v", err)
	}

	existingDir := filepath.Join(tmpDir, "existing-dir")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("mkdir existing dir: %v", err)
	}
	_, err = r.Call("file_move", map[string]any{
		"src":       src,
		"dst":       existingDir,
		"overwrite": true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot overwrite") {
		t.Fatalf("expected overwrite type rejection, got %v", err)
	}

	link := filepath.Join(tmpDir, "src-link")
	if err := os.Symlink(src, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err = r.Call("file_move", map[string]any{
		"src": src,
		"dst": link,
	})
	if err == nil || !strings.Contains(err.Error(), "same path") {
		t.Fatalf("expected resolved same path rejection, got %v", err)
	}
}

func TestFileDeleteToolDryRunLimitTrashAndSymlink(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	home := t.TempDir()
	t.Setenv("HOME", home)
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "tree")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write tree file: %v", err)
		}
	}
	dryRun, err := r.Call("file_delete", map[string]any{
		"path":      root,
		"recursive": true,
		"dry_run":   true,
	})
	if err != nil {
		t.Fatalf("file_delete dry_run: %v", err)
	}
	if !strings.Contains(dryRun, "Dry run") || !strings.Contains(dryRun, `"kind": "directory"`) {
		t.Fatalf("unexpected dry_run result: %q", dryRun)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("dry_run should keep tree: %v", err)
	}

	_, err = r.Call("file_delete", map[string]any{
		"path":        root,
		"recursive":   true,
		"max_entries": 1,
	})
	if err == nil || !strings.Contains(err.Error(), "max_entries") {
		t.Fatalf("expected recursive limit error, got %v", err)
	}

	trashResult, err := r.Call("file_delete", map[string]any{
		"path":      root,
		"recursive": true,
		"trash":     true,
	})
	if err != nil {
		t.Fatalf("file_delete trash: %v", err)
	}
	if !strings.Contains(trashResult, "Moved") || !strings.Contains(trashResult, ".luckyagent") {
		t.Fatalf("unexpected trash result: %q", trashResult)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trash should remove original path, got %v", err)
	}

	target := filepath.Join(tmpDir, "target.txt")
	link := filepath.Join(tmpDir, "target-link")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := r.Call("file_delete", map[string]any{"path": link}); err != nil {
		t.Fatalf("file_delete symlink: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("deleting symlink should keep target: %v", err)
	}
}

func TestFileMkdirToolDryRunAndParentErrors(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "a", "b")
	result, err := r.Call("file_mkdir", map[string]any{
		"path":    target,
		"dry_run": true,
	})
	if err != nil {
		t.Fatalf("file_mkdir dry_run: %v", err)
	}
	if !strings.Contains(result, "Dry run") || !strings.Contains(result, `"create_dirs"`) {
		t.Fatalf("unexpected dry_run result: %q", result)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry_run should not create target, got %v", err)
	}

	_, err = r.Call("file_mkdir", map[string]any{
		"path":      target,
		"recursive": false,
	})
	if err == nil || !strings.Contains(err.Error(), "set recursive=true") {
		t.Fatalf("expected clear recursive hint, got %v", err)
	}

	if _, err := r.Call("file_mkdir", map[string]any{"path": tmpDir}); err != nil {
		t.Fatalf("existing directory should be idempotent: %v", err)
	}
}

func TestFilePatchTool(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "patch.txt")
	if err := os.WriteFile(testFile, []byte("alpha\nbeta\nbeta\ngamma\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := r.Call("file_patch", map[string]any{
		"path":       testFile,
		"match":      "beta",
		"replace":    "delta",
		"occurrence": 2,
	})
	if err != nil {
		t.Fatalf("file_patch: %v", err)
	}
	if !strings.Contains(result, "Patched") {
		t.Fatalf("expected patch result, got %q", result)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if got := string(data); got != "alpha\nbeta\ndelta\ngamma\n" {
		t.Fatalf("unexpected patched content: %q", got)
	}
}

func TestFilePatchToolReplaceAll(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "patch-all.txt")
	if err := os.WriteFile(testFile, []byte("foo bar foo\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := r.Call("file_patch", map[string]any{
		"path":        testFile,
		"match":       "foo",
		"replace":     "baz",
		"replace_all": true,
	})
	if err != nil {
		t.Fatalf("file_patch replace_all: %v", err)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if got := string(data); got != "baz bar baz\n" {
		t.Fatalf("unexpected patched content: %q", got)
	}
}

func TestFilePatchToolErrors(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "patch-error.txt")
	if err := os.WriteFile(testFile, []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := r.Call("file_patch", map[string]any{
		"path":    testFile,
		"match":   "missing",
		"replace": "x",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing match error, got %v", err)
	}

	_, err = r.Call("file_patch", map[string]any{
		"path":       testFile,
		"match":      "two",
		"replace":    "x",
		"occurrence": 2,
	})
	if err == nil || !strings.Contains(err.Error(), "occurrence") {
		t.Fatalf("expected occurrence error, got %v", err)
	}
}

func TestFilePatchToolDiffMode(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "patch-diff.txt")
	if err := os.WriteFile(testFile, []byte("alpha\nbeta\ngamma\ndelta\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := r.Call("file_patch", map[string]any{
		"path": testFile,
		"diff": "@@\n alpha\n-beta\n+beta-2\n gamma\n@@\n gamma\n+gamma-half\n delta\n",
	})
	if err != nil {
		t.Fatalf("file_patch diff: %v", err)
	}
	if !strings.Contains(result, "2 hunks") {
		t.Fatalf("expected hunk count in result, got %q", result)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if got := string(data); got != "alpha\nbeta-2\ngamma\ngamma-half\ndelta\n" {
		t.Fatalf("unexpected diff patched content: %q", got)
	}
}

func TestFilePatchToolUnifiedDiffHeaders(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "patch-unified.txt")
	if err := os.WriteFile(testFile, []byte("one\ntwo\nthree\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := r.Call("file_patch", map[string]any{
		"path": testFile,
		"diff": "--- a/patch-unified.txt\n+++ b/patch-unified.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+two-updated\n three\n",
	})
	if err != nil {
		t.Fatalf("file_patch unified diff: %v", err)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if got := string(data); got != "one\ntwo-updated\nthree\n" {
		t.Fatalf("unexpected unified diff content: %q", got)
	}
}

func TestFilePatchToolDiffModeErrors(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "patch-diff-error.txt")
	if err := os.WriteFile(testFile, []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := r.Call("file_patch", map[string]any{
		"path": testFile,
		"diff": "two\n+three\n",
	})
	if err == nil || !strings.Contains(err.Error(), "must start with space") {
		t.Fatalf("expected invalid diff syntax error, got %v", err)
	}

	_, err = r.Call("file_patch", map[string]any{
		"path": testFile,
		"diff": "@@\n missing\n+three\n",
	})
	if err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("expected missing hunk match error, got %v", err)
	}
}

func TestFilePatchToolDryRunHashNoopAndPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bit preservation is platform-specific on Windows")
	}
	r := NewRegistry()
	RegisterBuiltinTools(r)

	path := filepath.Join(t.TempDir(), "patch-plan.sh")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o755); err != nil {
		t.Fatalf("write patch target: %v", err)
	}
	oldHash := sha256Hex([]byte("alpha\nbeta\n"))
	dryRun, err := r.Call("file_patch", map[string]any{
		"path":            path,
		"match":           "beta",
		"replace":         "gamma",
		"expected_sha256": oldHash,
		"dry_run":         true,
	})
	if err != nil {
		t.Fatalf("file_patch dry_run: %v", err)
	}
	if !strings.Contains(dryRun, "Dry run") || !strings.Contains(dryRun, `"kind": "replacement"`) {
		t.Fatalf("unexpected dry_run result: %q", dryRun)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after dry_run: %v", err)
	}
	if string(data) != "alpha\nbeta\n" {
		t.Fatalf("dry_run should not patch file: %q", data)
	}

	if _, err := r.Call("file_patch", map[string]any{
		"path":            path,
		"match":           "beta",
		"replace":         "gamma",
		"expected_sha256": oldHash,
	}); err != nil {
		t.Fatalf("file_patch expected hash: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after patch: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("expected permissions 0755, got %o", got)
	}

	if _, err := r.Call("file_patch", map[string]any{
		"path":    path,
		"match":   "gamma",
		"replace": "gamma",
	}); err == nil || !strings.Contains(err.Error(), "no changes") {
		t.Fatalf("expected no-op error, got %v", err)
	}
	if _, err := r.Call("file_patch", map[string]any{
		"path":            path,
		"match":           "gamma",
		"replace":         "delta",
		"expected_sha256": oldHash,
	}); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
}

func TestFileListTool(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	// 创建临时目录和文件
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644)

	result, err := r.Call("file_list", map[string]any{
		"path": tmpDir,
	})
	if err != nil {
		t.Fatalf("file_list: %v", err)
	}
	if result == "" {
		t.Error("expected list result")
	}
}

func TestFileToolsResolveRelativePathsAgainstShellContext(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	if _, err := r.CallWithShellContext("file_write", map[string]any{
		"path":    "nested/out.txt",
		"content": "ok",
	}, &ShellContext{Cwd: tmpDir}); err != nil {
		t.Fatalf("file_write with cwd: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "nested", "out.txt")); err != nil {
		t.Fatalf("expected file under shell cwd: %v", err)
	}

	listResult, err := r.CallWithShellContext("file_list", map[string]any{
		"path": ".",
	}, &ShellContext{Cwd: filepath.Join(tmpDir, "nested")})
	if err != nil {
		t.Fatalf("file_list with cwd: %v", err)
	}
	if !strings.Contains(listResult, "out.txt") {
		t.Fatalf("expected out.txt in relative list result, got %q", listResult)
	}

	readResult, err := r.CallWithShellContext("file_read", map[string]any{
		"path": "out.txt",
	}, &ShellContext{Cwd: filepath.Join(tmpDir, "nested")})
	if err != nil {
		t.Fatalf("file_read with cwd: %v", err)
	}
	if !strings.Contains(readResult, "ok") {
		t.Fatalf("expected file contents in read result, got %q", readResult)
	}
}

func TestFileListToolTruncatesRecursiveOutput(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	tmpDir := t.TempDir()
	for i := 0; i < 20; i++ {
		if err := os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("f%02d.txt", i)), []byte("x"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	result, err := r.Call("file_list", map[string]any{
		"path":        tmpDir,
		"recursive":   true,
		"max_entries": 5,
	})
	if err != nil {
		t.Fatalf("file_list recursive: %v", err)
	}
	if !strings.Contains(result, "truncated after 5 entries") {
		t.Fatalf("expected truncation marker, got %q", result)
	}
}

func TestShellTool(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinTools(r)

	result, err := r.Call("terminal", map[string]any{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("terminal call: %v", err)
	}
	if result == "" {
		t.Error("expected terminal result")
	}
}

func TestBuildShellCommandWindowsUsesPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific test")
	}

	cmd, err := buildShellCommand("Get-Location")
	if err != nil {
		t.Fatalf("buildShellCommand: %v", err)
	}
	if len(cmd.Args) < 5 {
		t.Fatalf("unexpected command args: %#v", cmd.Args)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(strings.ToLower(filepath.Base(cmd.Args[0])), "powershell") &&
		!strings.Contains(strings.ToLower(filepath.Base(cmd.Args[0])), "pwsh") {
		t.Fatalf("expected PowerShell executable on windows, got %#v", cmd.Args)
	}
	if !strings.Contains(joined, "-NoProfile") || !strings.Contains(joined, "-Command") || !strings.Contains(joined, "Get-Location") {
		t.Fatalf("expected PowerShell command flags on windows, got %#v", cmd.Args)
	}
}

func TestShellEnvPrefixUsesPlatformSyntax(t *testing.T) {
	env := map[string]string{
		"GOOD":    "it's ok",
		"BAD-KEY": "ignored",
	}

	unixPrefix := shellEnvPrefix(env, "linux")
	if !strings.Contains(unixPrefix, "export GOOD='it'\\''s ok'; ") {
		t.Fatalf("unexpected unix env prefix: %q", unixPrefix)
	}
	if strings.Contains(unixPrefix, "BAD-KEY") {
		t.Fatalf("invalid env key should be ignored: %q", unixPrefix)
	}

	windowsPrefix := shellEnvPrefix(env, "windows")
	if !strings.Contains(windowsPrefix, "$env:GOOD = 'it''s ok'; ") {
		t.Fatalf("unexpected windows env prefix: %q", windowsPrefix)
	}
	if strings.Contains(windowsPrefix, "BAD-KEY") {
		t.Fatalf("invalid env key should be ignored: %q", windowsPrefix)
	}
}

func TestPathTraversal(t *testing.T) {
	err := validatePath("../../etc/passwd")
	if err == nil {
		t.Error("expected path traversal error")
	}

	err = validatePath("/tmp/safe/path")
	if err != nil {
		t.Errorf("safe path should pass: %v", err)
	}
}

func TestSandboxPathValidation(t *testing.T) {
	home, _ := os.UserHomeDir()
	lhDir := filepath.Join(home, ".luckyagent")

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"luckyagent dir allowed", lhDir, false},
		{"luckyagent subfile allowed", filepath.Join(lhDir, "memory.json"), false},
		{"tmp allowed", "/tmp/test.txt", false},
		{"nanobot denied", filepath.Join(home, ".nanobot", "config.json"), true},
		{"ssh denied", filepath.Join(home, ".ssh", "id_rsa"), true},
		{"etc shadow denied", "/etc/shadow", true},
		{"root home denied", home + "/.bashrc", true},
		{"path traversal denied", filepath.Join(lhDir, "../../../etc/passwd"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestSandboxPathValidationAllowsProjectLocalHome(t *testing.T) {
	projectHome := filepath.Join(t.TempDir(), ".lh-home")
	if err := os.MkdirAll(filepath.Join(projectHome, ".luckyagent"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("HOME", projectHome)

	if err := validatePath(filepath.Join(projectHome, "skills")); err != nil {
		t.Fatalf("expected project-local lh home to be allowed, got %v", err)
	}
}

func TestSandboxPathValidationUsesWindowsHomeAndTemp(t *testing.T) {
	tmpDir := t.TempDir()
	windowsHome := filepath.Join(tmpDir, "Users", "Alice")
	if err := os.MkdirAll(filepath.Join(windowsHome, ".luckyagent"), 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", windowsHome)
	t.Setenv("USERPROFILE", windowsHome)
	t.Setenv("TEMP", filepath.Join(tmpDir, "Temp"))
	t.Setenv("TMP", filepath.Join(tmpDir, "Temp"))

	if err := validatePath(filepath.Join(windowsHome, ".luckyagent", "memory.md")); err != nil {
		t.Fatalf("expected windows home luckyagent path to pass, got %v", err)
	}
	if err := validatePath(filepath.Join(os.TempDir(), "scratch.txt")); err != nil {
		t.Fatalf("expected temp path to pass, got %v", err)
	}
}

func sandboxExternalTestRoot(t *testing.T) string {
	t.Helper()
	if wd, err := os.Getwd(); err == nil {
		root, err := os.MkdirTemp(wd, "sandbox-external-*")
		if err == nil {
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			return root
		}
	}
	return t.TempDir()
}

func TestFileReadAllowsConfiguredReadRoot(t *testing.T) {
	root := sandboxExternalTestRoot(t)
	home := filepath.Join(root, "home")
	tempRoot := filepath.Join(root, "runtime-temp")
	externalRoot := filepath.Join(root, "external-notes")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	if err := os.MkdirAll(externalRoot, 0o755); err != nil {
		t.Fatalf("mkdir external root: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TEMP", tempRoot)
	t.Setenv("TMP", tempRoot)
	t.Setenv("TMPDIR", tempRoot)

	notePath := filepath.Join(externalRoot, "questions.md")
	if err := os.WriteFile(notePath, []byte("classic interview question"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	if _, err := FileReadTool().Handler(map[string]any{"path": notePath}); err == nil {
		t.Logf("default sandbox already allows %s; skipping default rejection assertion", notePath)
	} else if !strings.Contains(err.Error(), "outside sandbox") {
		t.Fatalf("expected default sandbox rejection, got %v", err)
	}

	readTool := FileReadTool(FilesystemPolicy{AllowedReadRoots: []string{externalRoot}})
	out, err := readTool.Handler(map[string]any{"path": notePath})
	if err != nil {
		t.Fatalf("configured read root should allow file_read: %v", err)
	}
	if !strings.Contains(out, "classic interview question") {
		t.Fatalf("expected file contents in output, got %q", out)
	}

	listTool := FileListTool(FilesystemPolicy{AllowedReadRoots: []string{externalRoot}})
	listOut, err := listTool.Handler(map[string]any{"path": externalRoot})
	if err != nil {
		t.Fatalf("configured read root should allow file_list: %v", err)
	}
	if !strings.Contains(listOut, "questions.md") {
		t.Fatalf("expected listed note, got %q", listOut)
	}
}

func TestConfiguredReadRootDoesNotAllowWrite(t *testing.T) {
	root := sandboxExternalTestRoot(t)
	home := filepath.Join(root, "home")
	tempRoot := filepath.Join(root, "runtime-temp")
	externalRoot := filepath.Join(root, "external-notes")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	if err := os.MkdirAll(externalRoot, 0o755); err != nil {
		t.Fatalf("mkdir external root: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TEMP", tempRoot)
	t.Setenv("TMP", tempRoot)
	t.Setenv("TMPDIR", tempRoot)

	writePath := filepath.Join(externalRoot, "created.md")
	if err := validatePath(writePath); err == nil {
		t.Skipf("test external root is already inside the default sandbox: %s", writePath)
	}
	if _, err := FileWriteTool().Handler(map[string]any{
		"path":    writePath,
		"content": "should not write",
	}); err == nil || !strings.Contains(err.Error(), "outside sandbox") {
		t.Fatalf("expected write outside sandbox to be rejected, got %v", err)
	}
	if _, err := os.Stat(writePath); !os.IsNotExist(err) {
		t.Fatalf("external file should not have been written, stat err %v", err)
	}
}

func TestShellSandboxValidation(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantErr bool
	}{
		{"ls luckyagent ok", "ls ~/.luckyagent/", false},
		{"cat nanobot denied", "cat ~/.nanobot/config.json", true},
		{"grep ssh denied", "grep key ~/.ssh/id_rsa", true},
		{"echo OPENAI_API_KEY denied", "echo $OPENAI_API_KEY", true},
		{"cat FILEBROWSER denied", "echo $FILEBROWSER_TOKEN", true},
		{"normal command ok", "ls -la /tmp/", false},
		{"python script ok", "python3 /tmp/test.py", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShellSandbox(tt.cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateShellSandbox(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
			}
		})
	}
}

func TestResolveExaAPIKey(t *testing.T) {
	t.Setenv("LH_SEARCH_EXA_KEY", "")
	t.Setenv("EXA_API_KEY", "")

	cfg := &WebSearchConfig{Provider: "exa", APIKey: "cfg-key"}
	if got := resolveExaAPIKey(cfg); got != "cfg-key" {
		t.Fatalf("expected cfg key, got %q", got)
	}

	t.Setenv("LH_SEARCH_EXA_KEY", "lh-exa-key")
	if got := resolveExaAPIKey(&WebSearchConfig{}); got != "lh-exa-key" {
		t.Fatalf("expected LH_SEARCH_EXA_KEY, got %q", got)
	}

	t.Setenv("LH_SEARCH_EXA_KEY", "")
	t.Setenv("EXA_API_KEY", "env-exa-key")
	if got := resolveExaAPIKey(&WebSearchConfig{}); got != "env-exa-key" {
		t.Fatalf("expected EXA_API_KEY, got %q", got)
	}
}

func TestQuickSearchOrderPrefersProvider(t *testing.T) {
	order := quickSearchOrder("searxng", &WebSearchConfig{BaseURL: "https://search.shiokou.asia"})
	if len(order) == 0 || order[0] != "searxng" {
		t.Fatalf("expected provider first, got %v", order)
	}
}

func TestDeepSearchOrderPrefersProvider(t *testing.T) {
	t.Setenv("EXA_API_KEY", "env-exa-key")
	order := deepSearchOrder("searxng", &WebSearchConfig{BaseURL: "https://search.shiokou.asia"})
	if len(order) == 0 || order[0] != "searxng" {
		t.Fatalf("expected provider first, got %v", order)
	}
}

func writeZipTestFile(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip test file: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip member %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip member %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip test file: %v", err)
	}
}
