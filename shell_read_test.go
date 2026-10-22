package tool

import (
	"strings"
	"testing"
)

func TestShellFileReadRedirect(t *testing.T) {
	tests := []struct {
		command  string
		toolName string
		path     string
		ok       bool
	}{
		{command: "cat README.md", toolName: "file_read", path: "README.md", ok: true},
		{command: "head -n 40 agent/loop.go", toolName: "file_read", path: "agent/loop.go", ok: true},
		{command: "tail -n 20 notes.txt", toolName: "file_read", path: "notes.txt", ok: true},
		{command: "sed -n '1,80p' docs/guide.md", toolName: "file_read", path: "docs/guide.md", ok: true},
		{command: `python3 -c "print(open('README.md').read())"`, toolName: "file_read", path: "README.md", ok: true},
		{command: "cat report.pdf", toolName: "document_read", path: "report.pdf", ok: true},
		{command: "ls ~/.luckyagent/", ok: false},
		{command: "go test ./...", ok: false},
		{command: "git status --short", ok: false},
		{command: "ps aux | head", ok: false},
		{command: "sed -n '1,20p' a.go | head", ok: false},
		{command: "sed -i 's/a/b/' file.go", ok: false},
		{command: "cat < notes.txt", ok: false},
		{command: "echo hello", ok: false},
		{command: "python3 script.py", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			toolName, path, ok := shellFileReadRedirect(tt.command)
			if ok != tt.ok || toolName != tt.toolName || path != tt.path {
				t.Fatalf("shellFileReadRedirect(%q) = %q %q %v, want %q %q %v", tt.command, toolName, path, ok, tt.toolName, tt.path, tt.ok)
			}
		})
	}
}

func TestTerminalRejectsFilePrintWithoutRunning(t *testing.T) {
	_, err := handleShell(map[string]any{"command": "cat README.md"})
	if err == nil || !strings.Contains(err.Error(), "file_read") || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("expected file_read redirect, got %v", err)
	}
}
