package sandbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceWriteKeepsOriginalRoot(t *testing.T) {
	root := t.TempDir()
	snapshot, err := Prepare(ModeWorkspaceWrite, root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Mode != ModeWorkspaceWrite || snapshot.Root != root || snapshot.OriginalRoot != root {
		t.Fatalf("workspace-write copied or changed root: %+v", snapshot)
	}
	snapshot.Close()
}

func TestWorkspaceWriteStillEnforcesBoundary(t *testing.T) {
	root := t.TempDir()
	snapshot, err := Prepare(ModeWorkspaceWrite, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.ValidateToolArgs("file_write", map[string]any{"path": filepath.Join(root, "ok.txt")}); err != nil {
		t.Fatalf("workspace path rejected: %v", err)
	}
	if err := snapshot.ValidateToolArgs("file_write", map[string]any{"path": filepath.Join(t.TempDir(), "outside.txt")}); err == nil {
		t.Fatal("outside write was accepted")
	}
	if err := snapshot.ValidateToolArgs("web_fetch", map[string]any{}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("network tool was not blocked: %v", err)
	}
}
