package tool

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTerminalFilesystemRejectsSymlinkBeforeParentTraversal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell and symlink semantics")
	}
	f := newFilesystemPolicyFixture(t)
	outsideDir := filepath.Join(f.outside, "dir")
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.write, "link")
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Fatal(err)
	}
	tool := TerminalTool(f.policy())
	for _, tc := range []struct {
		name, output, filename string
	}{
		// Join would erase link/.. before the actual shell can interpret it.
		{"absolute", link + "/../escaped.txt", "escaped.txt"},
		{"relative", "link/../escaped2.txt", "escaped2.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Handler(map[string]any{
				"command": "printf x > " + shellPathQuote(tc.output),
				"workdir": f.write,
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
				t.Fatalf("ambiguous symlink/.. must fail before execution, got %v", err)
			}
			requirePolicyAbsent(t, filepath.Join(f.outside, tc.filename))
			requirePolicyAbsent(t, filepath.Join(f.write, tc.filename))
		})
	}
	for dir, want := range map[string]string{f.outside: "dir", f.write: "link", f.read: ""} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if len(entries) != 0 {
				t.Fatalf("rejected command created files in %s: %v", dir, entries)
			}
		} else if len(entries) != 1 || entries[0].Name() != want {
			t.Fatalf("rejected command changed fixture directory %s: %v", dir, entries)
		}
	}
}

func TestTerminalFilesystemConservativelyRejectsInternalSymlinkParentTraversal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell and symlink semantics")
	}
	f := newFilesystemPolicyFixture(t)
	insideDir := filepath.Join(f.write, "real", "dir")
	if err := os.MkdirAll(insideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.write, "internal-link")
	if err := os.Symlink(insideDir, link); err != nil {
		t.Fatal(err)
	}
	_, err := TerminalTool(f.policy()).Handler(map[string]any{
		"command": "printf x > " + shellPathQuote(link+"/../blocked.txt"),
		"workdir": f.write,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("symlink/.. ambiguity should be rejected even if both targets are allowed: %v", err)
	}
	requirePolicyAbsent(t, filepath.Join(f.write, "real", "blocked.txt"))
	requirePolicyAbsent(t, filepath.Join(f.write, "blocked.txt"))
}

func TestTerminalFilesystemOrdinaryParentTraversalStillWorks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command syntax")
	}
	f := newFilesystemPolicyFixture(t)
	if err := os.Mkdir(filepath.Join(f.write, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	tool := TerminalTool(f.policy())
	for _, command := range []string{
		"cd child && cd .. && printf ok > normal.txt",
		"printf plain > 'child/../plain.txt'",
	} {
		if _, err := tool.Handler(map[string]any{"command": command, "workdir": f.write}); err != nil {
			t.Fatalf("ordinary directories with parent traversal should remain usable: %s: %v", command, err)
		}
	}
	requirePolicyFile(t, filepath.Join(f.write, "normal.txt"), "ok")
	requirePolicyFile(t, filepath.Join(f.write, "plain.txt"), "plain")
}
