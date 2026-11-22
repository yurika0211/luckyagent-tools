package tool

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yurika0211/luckyagent-tools/sandbox"
)

func terminalPolicyFixture(t *testing.T) filesystemPolicyFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("literal POSIX shell preflight")
	}
	return newFilesystemPolicyFixture(t)
}

func shellPathQuote(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'" }

func TestTerminalFilesystemCwdAndInputUseReadRoots(t *testing.T) {
	f := terminalPolicyFixture(t)
	tool := TerminalTool(f.policy())
	if out, err := tool.Handler(map[string]any{"command": "pwd", "workdir": f.read}); err != nil || !strings.Contains(out, f.read) {
		t.Fatalf("read root cwd: %q, %v", out, err)
	}
	input := filepath.Join(f.read, "input with spaces.txt")
	if err := os.WriteFile(input, []byte("four"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := tool.Handler(map[string]any{"command": "wc -c < " + shellPathQuote(input)}); err != nil || !strings.Contains(out, "4") {
		t.Fatalf("input redirection: %q, %v", out, err)
	}
	missing := filepath.Join(f.read, "missing.txt")
	if _, err := tool.Handler(map[string]any{"command": "wc -c < " + shellPathQuote(missing)}); !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "not_found") {
		t.Fatalf("missing allowed input: %v", err)
	}
	if _, err := tool.Handler(map[string]any{"command": "pwd", "workdir": filepath.Join(f.read, "missing")}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing allowed cwd: %v", err)
	}
	if _, err := tool.Handler(map[string]any{"command": "wc -c < " + shellPathQuote(filepath.Join(f.outside, "missing"))}); err == nil || !strings.Contains(err.Error(), "outside sandbox") {
		t.Fatalf("outside missing input stays denied: %v", err)
	}
}

func TestTerminalFilesystemRedirectionsAndCdUseWriteRoots(t *testing.T) {
	f := terminalPolicyFixture(t)
	tool := TerminalTool(f.policy())
	for _, command := range []string{
		"printf blocked > " + shellPathQuote(filepath.Join(f.read, "blocked")),
		"cd " + shellPathQuote(f.read) + " && printf blocked > blocked",
		"cd " + shellPathQuote(f.read) + " && printf 'if' > blocked",
		"cd " + shellPathQuote(f.read) + " && printf for > blocked",
	} {
		if _, err := tool.Handler(map[string]any{"command": command, "workdir": f.write}); err == nil || !strings.Contains(err.Error(), "outside sandbox") {
			t.Fatalf("read-only write should fail: %q: %v", command, err)
		}
	}
	requirePolicyAbsent(t, filepath.Join(f.read, "blocked"))
	requirePolicyAbsent(t, filepath.Join(f.write, "blocked"))
	command := "cd -- " + shellPathQuote(f.write) + " && printf first > 'new note.txt' && printf second >> 'new note.txt'"
	if out, err := tool.Handler(map[string]any{"command": command}); err != nil {
		t.Fatalf("allowed cd/write: %q, %v", out, err)
	}
	requirePolicyFile(t, filepath.Join(f.write, "new note.txt"), "firstsecond")
}

func TestTerminalFilesystemRestrictedPathGuardIncludesActualRoots(t *testing.T) {
	f := terminalPolicyFixture(t)
	_, err := TerminalTool(f.policy()).Handler(map[string]any{"command": "printf config.json"})
	if err == nil {
		t.Fatal("the existing restricted-path guard must remain active")
	}
	for _, want := range []string{"restricted path", "allowed_read_roots", "allowed_write_roots", f.read, f.write} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("guard error %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "outside sandbox") {
		t.Fatalf("in-root config guard must not claim outside sandbox: %v", err)
	}
}

func TestTerminalFilesystemQuotedOperatorsAndDescriptorCopiesAreNotPaths(t *testing.T) {
	f := terminalPolicyFixture(t)
	tool := TerminalTool(f.policy())
	text := "literal > " + filepath.Join(f.outside, "not-a-target")
	if out, err := tool.Handler(map[string]any{"command": "printf '%s' " + shellPathQuote(text)}); err != nil || !strings.Contains(out, text) {
		t.Fatalf("quoted operator: %q, %v", out, err)
	}
	if out, err := tool.Handler(map[string]any{"command": "printf descriptor > copied.txt 2>&1", "workdir": f.write}); err != nil {
		t.Fatalf("fd duplication: %q, %v", out, err)
	}
	requirePolicyFile(t, filepath.Join(f.write, "copied.txt"), "descriptor")
	requirePolicyAbsent(t, filepath.Join(f.outside, "not-a-target"))
}

func TestTerminalFilesystemHeredocBodiesAreData(t *testing.T) {
	f := terminalPolicyFixture(t)
	outPath := filepath.Join(f.write, "heredoc.txt")
	body := "cd " + f.outside + "\nbody > " + filepath.Join(f.outside, "not-a-target") + "\n"
	command := "cat <<'EOF' > " + shellPathQuote(outPath) + "\n" + body + "EOF\n"
	if out, err := TerminalTool(f.policy()).Handler(map[string]any{"command": command}); err != nil {
		t.Fatalf("heredoc data: %q, %v", out, err)
	}
	requirePolicyFile(t, outPath, body)
	requirePolicyAbsent(t, filepath.Join(f.outside, "not-a-target"))
}

func TestTerminalFilesystemComplexCdDoesNotAssumeParentDirectoryChanged(t *testing.T) {
	f := terminalPolicyFixture(t)
	command := "(cd " + shellPathQuote(f.read) + "); printf parent > parent.txt"
	if out, err := TerminalTool(f.policy()).Handler(map[string]any{"command": command, "workdir": f.write}); err != nil {
		t.Fatalf("subshell cd: %q, %v", out, err)
	}
	requirePolicyFile(t, filepath.Join(f.write, "parent.txt"), "parent")
	requirePolicyAbsent(t, filepath.Join(f.read, "parent.txt"))
}

func TestTerminalFilesystemPreflightRunsBeforeBackgroundStart(t *testing.T) {
	f := terminalPolicyFixture(t)
	t.Cleanup(resetShellBackgroundTasks)
	tool := TerminalTool(f.policy())
	badPath := filepath.Join(f.read, "background-denied.txt")
	if out, err := tool.Handler(map[string]any{"command": "printf blocked > " + shellPathQuote(badPath), "background": true}); err == nil || strings.Contains(out, "task_id=") {
		t.Fatalf("denied background should not start: %q, %v", out, err)
	}
	requirePolicyAbsent(t, badPath)
	goodPath := filepath.Join(f.write, "background.txt")
	out, err := tool.Handler(map[string]any{"command": "printf background > " + shellPathQuote(goodPath), "background": true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handleShellOutput(map[string]any{"task_id": taskIDFromOutput(out), "timeout": 1}); err != nil {
		t.Fatal(err)
	}
	requirePolicyFile(t, goodPath, "background")
}

func TestTerminalFilesystemDevRetainsExistingUnrestrictedPaths(t *testing.T) {
	f := terminalPolicyFixture(t)
	path := filepath.Join(f.outside, "dev.txt")
	tool := TerminalTool(f.policy())
	result, err := tool.ContextDetailedHandler(ExecutionContext{Sandbox: &sandbox.Snapshot{Mode: sandbox.ModeDev}}, map[string]any{
		"command": "printf dev > dev.txt", "workdir": f.outside,
	})
	if err != nil {
		t.Fatalf("dev paths: %q, %v", result.Output, err)
	}
	requirePolicyFile(t, path, "dev")
	if _, err := tool.Handler(map[string]any{"command": "cat " + shellPathQuote(path)}); err == nil || !strings.Contains(err.Error(), "file_read") {
		t.Fatalf("pure reads must still point to file_read: %v", err)
	}
}

func TestTerminalFilesystemDanglingOutputSymlinkCannotEscape(t *testing.T) {
	f := terminalPolicyFixture(t)
	target := filepath.Join(f.outside, "not-created.txt")
	link := filepath.Join(f.write, "dangling-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := TerminalTool(f.policy()).Handler(map[string]any{"command": "printf blocked > " + shellPathQuote(link)}); err == nil {
		t.Fatal("dangling output symlink should be denied")
	}
	requirePolicyAbsent(t, target)
}
