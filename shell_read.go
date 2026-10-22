package tool

import (
	"fmt"
	"path/filepath"
	"strings"
)

// shellFileReadRedirect reports a command that only prints a file and should
// use file_read, file_list, or document_read instead of terminal. Ambiguous
// commands, pipelines, and real executions return ok=false so the shell still
// runs.
func shellFileReadRedirect(command string) (toolName, path string, ok bool) {
	command = strings.TrimSpace(command)
	if command == "" || shellCommandIsAmbiguous(command) {
		return "", "", false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", "", false
	}
	name := strings.ToLower(filepath.Base(fields[0]))
	switch name {
	case "cat", "head", "tail", "sed", "nl", "od", "hexdump", "strings", "less", "more", "bat":
		path = shellReadPath(name, fields[1:])
		if path == "" || shellPathLooksLikeOption(path) {
			return "", "", false
		}
		return fileReadToolForPath(path), path, true
	case "python", "python3":
		path = pythonPrintPath(fields[1:])
		if path == "" {
			return "", "", false
		}
		return fileReadToolForPath(path), path, true
	default:
		return "", "", false
	}
}

func shellCommandIsAmbiguous(command string) bool {
	var quote rune
	for _, r := range command {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
		case '|', ';', '&', '>', '<', '$', '(', ')':
			return true
		}
	}
	return quote != 0
}

func shellReadPath(command string, args []string) string {
	switch command {
	case "sed":
		return sedPrintPath(args)
	case "head", "tail":
		return lastNonOption(args, true)
	case "od", "hexdump":
		return lastNonOption(args, false)
	default:
		return lastNonOption(args, false)
	}
}

func sedPrintPath(args []string) string {
	var files []string
	hasScriptFlag := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			files = append(files, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			if arg == "-i" || strings.HasPrefix(arg, "-i") {
				return ""
			}
			if arg == "-e" || arg == "-f" || strings.HasPrefix(arg, "-e") && arg != "-e" {
				hasScriptFlag = true
				if arg == "-e" || arg == "-f" {
					i++
				}
			}
			continue
		}
		files = append(files, stripShellQuotes(arg))
	}
	if hasScriptFlag {
		if len(files) == 1 {
			return files[0]
		}
		return ""
	}
	switch len(files) {
	case 1:
		if looksLikeSedScript(files[0]) {
			return ""
		}
		return files[0]
	case 2:
		if !looksLikeSedScript(files[0]) {
			return ""
		}
		return files[1]
	default:
		return ""
	}
}

func looksLikeSedScript(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if strings.Contains(text, "/") {
		return true
	}
	for _, r := range text {
		if r == 'p' || r == 'd' || r == 's' || r == 'y' {
			return true
		}
	}
	return false
}

func stripShellQuotes(text string) string {
	if len(text) >= 2 {
		if (text[0] == '\'' && text[len(text)-1] == '\'') || (text[0] == '"' && text[len(text)-1] == '"') {
			return text[1 : len(text)-1]
		}
	}
	return text
}

func lastNonOption(args []string, allowNumericOption bool) string {
	var path string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 == len(args)-1 || (path == "" && i+1 < len(args)) {
				return args[len(args)-1]
			}
			return ""
		}
		if strings.HasPrefix(arg, "-") {
			if allowNumericOption && isNumericOption(arg) {
				continue
			}
			if (arg == "-n" || arg == "-c" || arg == "-b") && i+1 < len(args) {
				i++
			}
			continue
		}
		if path != "" {
			return ""
		}
		path = arg
	}
	return path
}

func isNumericOption(arg string) bool {
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}
	for _, r := range arg[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func pythonPrintPath(args []string) string {
	if len(args) < 2 || args[0] != "-c" {
		return ""
	}
	code := strings.Join(args[1:], " ")
	code = strings.TrimSpace(code)
	if strings.ContainsAny(code, ";\n") && strings.Count(code, "print") != 1 {
		return ""
	}
	for _, marker := range []string{"open(", "Path("} {
		start := strings.Index(code, marker)
		if start < 0 {
			continue
		}
		rest := code[start+len(marker):]
		if !strings.Contains(rest, "read") && marker == "open(" && !strings.Contains(code, "print") {
			return ""
		}
		path := quotedPath(rest)
		if path == "" || strings.Contains(code, "write") || strings.Contains(code, "system(") {
			return ""
		}
		if strings.Contains(code, "print") || strings.Contains(code, ".read(") {
			return path
		}
	}
	return ""
}

func quotedPath(text string) string {
	if text == "" {
		return ""
	}
	quote := text[0]
	if quote != '\'' && quote != '"' {
		return ""
	}
	end := strings.IndexByte(text[1:], quote)
	if end < 0 {
		return ""
	}
	return text[1 : 1+end]
}

func shellPathLooksLikeOption(path string) bool {
	return path == "" || strings.HasPrefix(path, "-")
}

func fileReadToolForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf", ".docx", ".pptx", ".xlsx", ".doc", ".ppt", ".xls":
		return "document_read"
	default:
		return "file_read"
	}
}

func shellFileReadRedirectError(toolName, path string) error {
	return fmt.Errorf("use %s for %s; terminal was not run because this command only prints a file", toolName, path)
}
