package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSend(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workspace := filepath.Join(home, ".luckyagent", "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	src := filepath.Join(workspace, "report.pdf")
	if err := os.WriteFile(src, []byte("pdf-bytes"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	r := NewRegistry()
	r.Register(FileSendTool())
	tool, ok := r.Get("file_send")
	if !ok {
		t.Fatal("file_send not registered")
	}
	if tool.Permission != PermApprove || tool.ParallelSafe || !tool.ShellAware {
		t.Fatalf("flags: perm=%v parallel=%v shell=%v", tool.Permission, tool.ParallelSafe, tool.ShellAware)
	}

	t.Run("happy copy", func(t *testing.T) {
		raw, err := r.Call("file_send", map[string]any{"path": src, "name": "report.pdf"})
		if err != nil {
			t.Fatalf("file_send: %v", err)
		}
		var got fileSendResult
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("json: %v body=%s", err, raw)
		}
		if got.FileName != "report.pdf" || got.MimeType != "application/pdf" || got.FileSize != 9 {
			t.Fatalf("payload: %+v", got)
		}
		if !strings.Contains(got.FilePath, filepath.Join(".luckyagent", "workspace", "deliverables")) {
			t.Fatalf("file_path: %s", got.FilePath)
		}
		if !strings.HasPrefix(got.FileURL, "/api/v1/artifacts?path=workspace%2Fdeliverables%2F") || !strings.HasSuffix(got.FileURL, ".pdf") {
			t.Fatalf("file_url: %s", got.FileURL)
		}
		info, err := os.Stat(got.FilePath)
		if err != nil || info.Size() != 9 || info.IsDir() {
			t.Fatalf("copied file: %v %+v", err, info)
		}
		if _, err := os.Stat(src); err != nil {
			t.Fatalf("source removed: %v", err)
		}
	})

	t.Run("reject missing", func(t *testing.T) {
		if _, err := r.Call("file_send", map[string]any{}); err == nil {
			t.Fatal("expected missing path error")
		}
		missing := filepath.Join(workspace, "nope.pdf")
		if _, err := r.Call("file_send", map[string]any{"path": missing}); err == nil {
			t.Fatal("expected missing file error")
		}
	})

	t.Run("reject dir", func(t *testing.T) {
		dir := filepath.Join(workspace, "folder")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Call("file_send", map[string]any{"path": dir}); err == nil || !strings.Contains(err.Error(), "directory") {
			t.Fatalf("expected directory error, got %v", err)
		}
	})

	t.Run("reject symlink escape", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "secret.pdf")
		if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(workspace, "escape.pdf")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Call("file_send", map[string]any{"path": link}); err == nil {
			t.Fatal("expected symlink escape to be rejected")
		}
	})

	t.Run("reject over 31MiB", func(t *testing.T) {
		big := filepath.Join(workspace, "big.bin")
		f, err := os.Create(big)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(maxFileSendBytes + 1); err != nil {
			t.Fatal(err)
		}
		f.Close()
		if _, err := r.Call("file_send", map[string]any{"path": big}); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("expected size error, got %v", err)
		}
	})

	t.Run("reject dotdot", func(t *testing.T) {
		if _, err := r.Call("file_send", map[string]any{"path": filepath.Join(workspace, "..", "..", "etc", "passwd")}); err == nil {
			t.Fatal("expected traversal rejection")
		}
	})
}
