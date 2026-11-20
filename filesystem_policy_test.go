package tool

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type filesystemPolicyFixture struct {
	home, builtin, temp, read, write, outside string
}

func newFilesystemPolicyFixture(t *testing.T) filesystemPolicyFixture {
	t.Helper()
	root := sandboxExternalTestRoot(t)
	f := filesystemPolicyFixture{
		home: filepath.Join(root, "home"), temp: filepath.Join(root, "runtime-temp"),
		read: filepath.Join(root, "read-only"), write: filepath.Join(root, "writeable"),
		outside: filepath.Join(root, "outside"),
	}
	f.builtin = filepath.Join(f.home, ".luckyagent")
	for _, dir := range []string{f.builtin, f.temp, f.read, f.write, f.outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
	}
	t.Setenv("HOME", f.home)
	t.Setenv("USERPROFILE", f.home)
	t.Setenv("TEMP", f.temp)
	t.Setenv("TMP", f.temp)
	t.Setenv("TMPDIR", f.temp)
	return f
}

func (f filesystemPolicyFixture) policy() FilesystemPolicy {
	return FilesystemPolicy{AllowedReadRoots: []string{f.read}, AllowedWriteRoots: []string{f.write}}
}

func requirePolicyFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("file %s = %q, %v; want %q", path, got, err, want)
	}
}

func requirePolicyAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no filesystem effect at %s, got %v", path, err)
	}
}

func TestFilesystemPolicyEmptyWriteRootsPreservesBuiltinAccess(t *testing.T) {
	f := newFilesystemPolicyFixture(t)
	for _, path := range []string{filepath.Join(f.builtin, "note.md"), filepath.Join(f.temp, "note.md")} {
		if _, err := FileWriteTool().Handler(map[string]any{"path": path, "content": "builtin"}); err != nil {
			t.Fatalf("zero-argument constructor should preserve builtin writes: %v", err)
		}
		requirePolicyFile(t, path, "builtin")
	}
	path := filepath.Join(f.write, "not-authorized.md")
	if _, err := FileWriteTool(FilesystemPolicy{}).Handler(map[string]any{"path": path, "content": "outside"}); err == nil {
		t.Fatal("an empty write-root configuration must not authorize another directory")
	}
	requirePolicyAbsent(t, path)
}

func TestFilesystemPolicyConfiguredWriteRootSupportsFileLifecycle(t *testing.T) {
	f := newFilesystemPolicyFixture(t)
	policy := FilesystemPolicy{AllowedWriteRoots: []string{f.write}}
	path := filepath.Join(f.write, "note.md")
	if _, err := FileWriteTool(policy).Handler(map[string]any{"path": path, "content": "draft"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if out, err := FileReadTool(policy).Handler(map[string]any{"path": path}); err != nil || !strings.Contains(out, "draft") {
		t.Fatalf("a write root must also allow reading its files: %q, %v", out, err)
	}
	if out, err := FileListTool(policy).Handler(map[string]any{"path": f.write}); err != nil || !strings.Contains(out, "note.md") {
		t.Fatalf("list a write root: %q, %v", out, err)
	}
	if _, err := FilePatchTool(policy).Handler(map[string]any{"path": path, "match": "draft", "replace": "final"}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	requirePolicyFile(t, path, "final")
	dir := filepath.Join(f.write, "new", "nested")
	if _, err := FileMkdirTool(policy).Handler(map[string]any{"path": dir}); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dst := filepath.Join(dir, "renamed.md")
	if _, err := FileMoveTool(policy).Handler(map[string]any{"src": path, "dst": dst}); err != nil {
		t.Fatalf("move: %v", err)
	}
	requirePolicyAbsent(t, path)
	requirePolicyFile(t, dst, "final")
	if _, err := FileDeleteTool(policy).Handler(map[string]any{"path": dst}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	requirePolicyAbsent(t, dst)
}

func TestFilesystemPolicyReadRootsNeverAuthorizeMutations(t *testing.T) {
	f := newFilesystemPolicyFixture(t)
	policy := f.policy()
	readFile := filepath.Join(f.read, "existing.md")
	writeFile := filepath.Join(f.write, "source.md")
	for _, path := range []string{readFile, writeFile} {
		if err := os.WriteFile(path, []byte("unchanged"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		tool *Tool
		args map[string]any
	}{
		{"write", FileWriteTool(policy), map[string]any{"path": readFile, "content": "replaced"}},
		{"patch", FilePatchTool(policy), map[string]any{"path": readFile, "match": "unchanged", "replace": "replaced"}},
		{"mkdir", FileMkdirTool(policy), map[string]any{"path": filepath.Join(f.read, "new-directory")}},
		{"delete", FileDeleteTool(policy), map[string]any{"path": readFile}},
		{"move-source", FileMoveTool(policy), map[string]any{"src": readFile, "dst": filepath.Join(f.write, "moved.md")}},
		{"move-destination", FileMoveTool(policy), map[string]any{"src": writeFile, "dst": filepath.Join(f.read, "moved.md")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.tool.Handler(tc.args); err == nil || !strings.Contains(err.Error(), "sandbox") {
				t.Fatalf("read-only root mutation should be sandbox-denied, got %v", err)
			}
			requirePolicyFile(t, readFile, "unchanged")
			requirePolicyFile(t, writeFile, "unchanged")
		})
	}
	requirePolicyAbsent(t, filepath.Join(f.read, "new-directory"))
	requirePolicyAbsent(t, filepath.Join(f.read, "moved.md"))
	requirePolicyAbsent(t, filepath.Join(f.write, "moved.md"))
}

func TestFilesystemPolicyDenialNamesActualRootsIncludingRestrictedTargets(t *testing.T) {
	f := newFilesystemPolicyFixture(t)
	for _, denied := range []string{filepath.Join(f.outside, "missing.md"), filepath.Join(f.home, ".ssh", "missing-key")} {
		_, err := FileReadTool(f.policy()).Handler(map[string]any{"path": denied})
		if err == nil || errors.Is(err, os.ErrNotExist) {
			t.Fatalf("an unauthorized path must be denied without becoming not_found: %v", err)
		}
		msg := filepath.ToSlash(err.Error())
		for _, want := range []string{f.builtin, f.temp, f.read, f.write, "allowed_read_roots", "allowed_write_roots"} {
			needle := filepath.ToSlash(want)
			if runtime.GOOS == "windows" {
				msg, needle = strings.ToLower(msg), strings.ToLower(needle)
			}
			if !strings.Contains(msg, needle) {
				t.Fatalf("denial %q does not name actual policy root/key %q", msg, needle)
			}
		}
	}
}

func TestFilesystemPolicyAllowedMissingPathsKeepNotFound(t *testing.T) {
	f := newFilesystemPolicyFixture(t)
	policy := f.policy()
	for _, tc := range []struct {
		name string
		tool *Tool
		args map[string]any
	}{
		{"read", FileReadTool(policy), map[string]any{"path": filepath.Join(f.read, "missing.md")}},
		{"list", FileListTool(policy), map[string]any{"path": filepath.Join(f.read, "missing-directory")}},
		{"patch", FilePatchTool(policy), map[string]any{"path": filepath.Join(f.write, "missing.md"), "match": "a", "replace": "b"}},
		{"overwrite", FileWriteTool(policy), map[string]any{"path": filepath.Join(f.write, "missing.md"), "content": "replacement", "mode": "overwrite"}},
		{"mkdir-parent", FileMkdirTool(policy), map[string]any{"path": filepath.Join(f.write, "missing-parent", "child"), "recursive": false}},
		{"move", FileMoveTool(policy), map[string]any{"src": filepath.Join(f.write, "missing.md"), "dst": filepath.Join(f.write, "destination.md")}},
		{"delete", FileDeleteTool(policy), map[string]any{"path": filepath.Join(f.write, "missing.md")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.tool.Handler(tc.args)
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("authorized missing path must retain os.ErrNotExist, got %v", err)
			}
			if !strings.Contains(err.Error(), "not_found") {
				t.Fatalf("authorized missing path must include a not_found marker, got %v", err)
			}
			if strings.Contains(err.Error(), "outside sandbox") {
				t.Fatalf("authorized missing path was incorrectly called outside sandbox: %v", err)
			}
		})
	}
}

func TestFilesystemPolicyRelativeWriteNeverFallsBackFromReadOnlyCWD(t *testing.T) {
	f := newFilesystemPolicyFixture(t)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(f.builtin); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore test process cwd: %v", err)
		}
	})
	_, err = FileWriteTool(f.policy()).Handler(map[string]any{
		"path": "relative.md", "_cwd": f.read, "content": "must not be written",
	})
	if err == nil {
		t.Fatal("relative write in read-only cwd must fail, not change to process cwd")
	}
	requirePolicyAbsent(t, filepath.Join(f.read, "relative.md"))
	requirePolicyAbsent(t, filepath.Join(f.builtin, "relative.md"))
}

func TestFilesystemPolicySymlinkTargetsCannotEscapeWriteRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges on Windows")
	}
	f := newFilesystemPolicyFixture(t)
	policy := f.policy()
	outsideFile := filepath.Join(f.outside, "existing.md")
	if err := os.WriteFile(outsideFile, []byte("private fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		filepath.Join(f.write, "external-file"):      outsideFile,
		filepath.Join(f.write, "external-directory"): f.outside,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(f.write, "external-file"),
		filepath.Join(f.write, "external-directory", "not-yet-created", "deeper", "note.md"),
	} {
		if _, err := FileWriteTool(policy).Handler(map[string]any{"path": path, "content": "escape"}); err == nil {
			t.Fatalf("writing through an external symlink should be rejected: %s", path)
		}
	}
	requirePolicyFile(t, outsideFile, "private fixture")
	requirePolicyAbsent(t, filepath.Join(f.outside, "not-yet-created"))

	inside := filepath.Join(f.write, "real-directory")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.write, "internal-directory")
	if err := os.Symlink(inside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := FileWriteTool(policy).Handler(map[string]any{
		"path": filepath.Join(link, "new", "note.md"), "content": "inside",
	}); err != nil {
		t.Fatalf("a resolved target still inside the write root should work: %v", err)
	}
	requirePolicyFile(t, filepath.Join(inside, "new", "note.md"), "inside")
}
