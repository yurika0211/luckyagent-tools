package tool

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTerminalFilesystemRejectsQuotedTargetWithSignificantTrailingWhitespace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell literal filename semantics")
	}
	f := newFilesystemPolicyFixture(t)
	// This is a sibling file outside the write root, not the existing root directory.
	outsideFile := f.write + " "
	_, err := TerminalTool(f.policy()).Handler(map[string]any{
		"command": "printf x > " + shellPathQuote(outsideFile),
		"workdir": f.write,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "whitespace") {
		t.Fatalf("significant literal filename whitespace must be rejected before normalization: %v", err)
	}
	requirePolicyAbsent(t, outsideFile)
	if info, err := os.Stat(f.write); err != nil || !info.IsDir() {
		t.Fatalf("the write root must remain an unchanged directory, got %v, %v", info, err)
	}
}

func TestTerminalFilesystemRejectsQuotedTildeInsteadOfAuthorizingTheExpandedHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell quoted tilde semantics")
	}
	f := newFilesystemPolicyFixture(t)
	literalDirectory := filepath.Join(f.read, "~", ".luckyagent")
	if err := os.MkdirAll(literalDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := TerminalTool(f.policy()).Handler(map[string]any{
		"command": "printf x > " + shellPathQuote("~/.luckyagent/out"),
		"workdir": f.read,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "tilde") {
		t.Fatalf("quoted tilde must not be checked as shell-expanded HOME: %v", err)
	}
	requirePolicyAbsent(t, filepath.Join(literalDirectory, "out"))
	requirePolicyAbsent(t, filepath.Join(f.builtin, "out"))
}

func TestTerminalFilesystemUnquotedHomeExpansionStillWorks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell unquoted tilde expansion")
	}
	f := newFilesystemPolicyFixture(t)
	if _, err := TerminalTool(f.policy()).Handler(map[string]any{
		"command": "printf home > ~/.luckyagent/home.txt",
		"workdir": f.read,
	}); err != nil {
		t.Fatalf("ordinary shell HOME expansion should remain usable: %v", err)
	}
	requirePolicyFile(t, filepath.Join(f.builtin, "home.txt"), "home")
}
