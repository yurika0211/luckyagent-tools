package codex

import (
	"path/filepath"
	"testing"
)

func TestCWDInsideRejectsSiblingPrefix(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "proj")
	sibling := filepath.Join(root, "proj-evil")
	child := filepath.Join(allowed, "src")
	if !cwdInside(allowed, allowed) {
		t.Fatal("root itself should be inside")
	}
	if !cwdInside(allowed, child) {
		t.Fatal("child should be inside")
	}
	if cwdInside(allowed, sibling) {
		t.Fatal("sibling prefix must be outside")
	}
	if cwdInside(allowed, root) {
		t.Fatal("parent must be outside")
	}
}

func TestValidateCWDAllowlist(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "proj")
	m := &Manager{cfg: Config{CWDAllowlist: []string{allowed}}}
	if _, err := m.validateCWD(filepath.Join(allowed, "a")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.validateCWD(filepath.Join(root, "proj-evil")); err == nil {
		t.Fatal("sibling cwd accepted")
	}
}
