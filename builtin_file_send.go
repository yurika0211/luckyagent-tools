package tool

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// FileSendTool copies an existing sandbox file into workspace/deliverables and
// returns the attachment JSON the websocket already parses.
func FileSendTool() *Tool {
	return &Tool{
		Name:        "file_send",
		Description: "给用户看的文件必须调用 file_send。不要在回复里写 MEDIA: 行，不要只把路径贴在正文里。先 file_write / 生成文件，再 file_send。",
		Category:    CatBuiltin,
		Source:      "builtin",
		Permission:  PermApprove,
		ShellAware:  true,
		ParallelSafe: false,
		Parameters: map[string]Param{
			"path": {Type: "string", Description: "Existing regular file to deliver. Must stay inside the sandbox.", Required: true},
			"name": {Type: "string", Description: "Optional display name shown to the user. The stored file uses a unique name.", Required: false},
		},
		Handler: handleFileSend,
	}
}

type fileSendResult struct {
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
	FilePath string `json:"file_path"`
	FileURL  string `json:"file_url"`
}

func handleFileSend(args map[string]any) (string, error) {
	src, err := resolvePathArg(args, "path")
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink is not allowed: %s", src)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", src)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path is not a regular file: %s", src)
	}
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return "", fmt.Errorf("resolve file: %w", err)
	}
	if err := validatePath(resolved); err != nil {
		return "", err
	}
	workspace := filepath.Clean(sandboxWorkspaceDir())
	if !pathMatchesPrefix(normalizeSandboxPath(resolved), normalizeSandboxPath(workspace)) {
		return "", fmt.Errorf("access denied: path is outside workspace (allowed: ~/.luckyagent/workspace/). Requested: %s", src)
	}
	if info.Size() > maxFileSendBytes {
		return "", fmt.Errorf("file is too large (%d bytes, max %d)", info.Size(), maxFileSendBytes)
	}

	displayName := strings.TrimSpace(stringArg(args["name"]))
	if displayName == "" {
		displayName = filepath.Base(resolved)
	}
	displayName = filepath.Base(displayName)
	if displayName == "" || displayName == "." || displayName == string(filepath.Separator) {
		return "", fmt.Errorf("name is invalid")
	}

	destDir := filepath.Join(workspace, "deliverables")
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", fmt.Errorf("create deliverables: %w", err)
	}
	ext := filepath.Ext(displayName)
	storedName, err := uniqueDeliverableName(destDir, ext)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, storedName)
	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("destination already exists: %s", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat destination: %w", err)
	}
	written, err := copyFileExclusive(resolved, dest, 0o600)
	if err != nil {
		return "", err
	}
	if written > maxFileSendBytes {
		_ = os.Remove(dest)
		return "", fmt.Errorf("file is too large (%d bytes, max %d)", written, maxFileSendBytes)
	}

	rel := "workspace/deliverables/" + storedName
	payload := fileSendResult{
		FileName: displayName,
		MimeType: mime.TypeByExtension(ext),
		FileSize: written,
		FilePath: dest,
		FileURL:  "/api/v1/artifacts?path=" + url.QueryEscape(rel),
	}
	if payload.MimeType == "" {
		payload.MimeType = "application/octet-stream"
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode file_send result: %w", err)
	}
	return string(out), nil
}

func uniqueDeliverableName(dir, ext string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("name deliverable: %w", err)
	}
	name := hex.EncodeToString(buf[:]) + ext
	if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
		return "", fmt.Errorf("destination already exists")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat destination: %w", err)
	}
	return name, nil
}

func copyFileExclusive(src, dest string, mode os.FileMode) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("open file: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return 0, fmt.Errorf("create deliverable: %w", err)
	}
	written, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dest)
		return 0, fmt.Errorf("copy file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return 0, fmt.Errorf("close deliverable: %w", closeErr)
	}
	return written, nil
}
