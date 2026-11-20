package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// terminalFilesystemPreflight checks explicit cwd, literal cd and redirection
// paths. It deliberately does not interpret scripts, expansions, or arbitrary
// command arguments and is not a complete shell parser or an OS boundary.
func terminalFilesystemPreflight(command, workdir, baseCwd string, policy FilesystemPolicy) (string, error) {
	cwd := workdir
	if cwd == "" {
		cwd, _ = os.Getwd()
	} else {
		resolved, err := resolvePathForOperation(cwd, baseCwd, filesystemRead, policy)
		if err != nil {
			return "", err
		}
		if err := terminalDirectoryExists(resolved); err != nil {
			return "", err
		}
		workdir, cwd = resolved, resolved
	}
	// The host uses PowerShell on Windows; do not apply POSIX quoting to it.
	if runtime.GOOS == "windows" {
		return workdir, nil
	}
	tokens, complete := scanTerminalPaths(command)
	if !complete {
		return workdir, nil // Let the real shell report malformed/unsupported syntax.
	}
	complex := false
	atCommandStart := true
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token.operator && strings.Contains(" | || & ( ) ;; ", " "+token.text+" ") {
			complex = true
		}
		if token.operator {
			if strings.ContainsAny(token.text, "<>") {
				i++ // The redirection operand cannot be a shell keyword.
			} else {
				atCommandStart = true
			}
			continue
		}
		if atCommandStart && !token.quoted {
			switch token.text {
			case "if", "then", "for", "while", "until", "case", "function", "{", "}":
				complex = true
			}
		}
		atCommandStart = false
	}
	knownCwd := true
	var words []terminalPathToken
	finish := func() error {
		defer func() { words = nil }()
		if len(words) == 0 || words[0].text != "cd" {
			return nil
		}
		args := words[1:]
		if len(args) > 0 && args[0].text == "--" {
			args = args[1:]
		}
		if len(args) > 1 || (len(args) == 1 && (!args[0].literal || strings.HasPrefix(args[0].text, "-"))) {
			knownCwd = false
			return nil
		}
		path := sandboxHomeDir()
		if len(args) == 1 {
			if err := rejectTerminalLiteralSpelling(args[0], policy); err != nil {
				return err
			}
			path = args[0].text
		}
		if !knownCwd && !filepath.IsAbs(expandSandboxPath(path)) {
			return nil
		}
		if err := rejectTerminalSymlinkParent(path, cwd, policy); err != nil {
			return err
		}
		resolved, err := resolvePathForOperation(path, cwd, filesystemRead, policy)
		if err != nil {
			return err
		}
		if err := terminalDirectoryExists(resolved); err != nil {
			return err
		}
		// In a pipeline, condition, or subshell, cd does not necessarily change
		// the following parent command. Do not guess a base for relative paths.
		if complex {
			knownCwd = false
		} else {
			// Keep the logical spelling. A later parent step must not silently
			// lose an earlier symlink before checking the original shell path.
			cwd = resolved
		}
		return nil
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if !token.operator {
			words = append(words, token)
			continue
		}
		if strings.ContainsAny(token.text, "<>") {
			if i+1 >= len(tokens) || tokens[i+1].operator {
				continue
			}
			i++
			target := tokens[i]
			if token.text == "<<" || token.text == "<<-" || token.text == "<<<" || !target.literal {
				continue
			}
			if (token.text == ">&" || token.text == "<&") && (target.text == "-" || terminalDigits(target.text)) {
				continue
			}
			if err := rejectTerminalLiteralSpelling(target, policy); err != nil {
				return "", err
			}
			if !knownCwd && !filepath.IsAbs(expandSandboxPath(target.text)) {
				continue
			}
			if err := rejectTerminalSymlinkParent(target.text, cwd, policy); err != nil {
				return "", err
			}
			op := filesystemWrite
			if token.text == "<" || token.text == "<&" {
				op = filesystemRead
			}
			path, err := resolvePathForOperation(target.text, cwd, op, policy)
			if err != nil {
				return "", err
			}
			if op == filesystemRead {
				if _, err := os.Stat(path); err != nil {
					return "", filesystemPathError("stat input", path, err)
				}
			}
			continue
		}
		if err := finish(); err != nil {
			return "", err
		}
	}
	return workdir, finish()
}

// Unlike tool paths, shell words retain boundary spaces and quoted tildes.
// Refuse spellings the shared resolver would reinterpret instead of authorizing
// one path and handing a different original spelling to the shell.
func rejectTerminalLiteralSpelling(token terminalPathToken, policy FilesystemPolicy) error {
	reason := ""
	if token.text != strings.TrimSpace(token.text) {
		reason = "boundary whitespace"
	} else if token.quoted && strings.HasPrefix(token.text, "~") {
		reason = "quoted or escaped leading tilde"
	}
	if reason != "" {
		return fmt.Errorf("access denied: ambiguous literal terminal path (%s): %q; use an unambiguous absolute path instead; %s", reason, token.text, filesystemPolicyDescription(policy))
	}
	return nil
}

// File tools execute their cleaned path, whereas terminal executes the original
// command. A link followed by '..' can therefore make the kernel write somewhere
// other than Clean(path) authorized. Reject that ambiguity before any Join/Clean;
// ordinary parent traversal without a preceding symlink still uses the policy.
func rejectTerminalSymlinkParent(path, cwd string, policy FilesystemPolicy) error {
	raw := strings.TrimSpace(path)
	if raw == "~" {
		raw = sandboxHomeDir()
	} else if strings.HasPrefix(raw, "~/") {
		raw = strings.TrimRight(sandboxHomeDir(), "/") + raw[1:]
	}
	if !filepath.IsAbs(raw) {
		raw = strings.TrimRight(cwd, "/") + "/" + raw
	}
	prefix, sawSymlink := "/", false
	for _, component := range strings.Split(raw, "/") {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			if sawSymlink {
				return fmt.Errorf("access denied: ambiguous terminal path traverses '..' after a symlink: %s; use a resolved literal path instead; %s", raw, filesystemPolicyDescription(policy))
			}
			prefix = filepath.Dir(prefix)
			continue
		}
		prefix = filepath.Join(prefix, component)
		if info, err := os.Lstat(prefix); err == nil && info.Mode()&os.ModeSymlink != 0 {
			sawSymlink = true
		}
	}
	return nil
}

func terminalDirectoryExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return filesystemPathError("stat working directory", path, err)
	}
	if !info.IsDir() {
		return &os.PathError{Op: "chdir", Path: path, Err: os.ErrInvalid}
	}
	return nil
}

type terminalPathToken struct {
	text              string
	operator, literal bool
	quoted            bool
	end               int
}

// Only lexical boundaries are retained: words, operators, and opaque expansion
// words. Heredoc bodies are data and never interpreted as commands or paths.
func scanTerminalPaths(command string) ([]terminalPathToken, bool) {
	var tokens []terminalPathToken
	var word strings.Builder
	started, literal, protected := false, true, false
	var quote byte
	type heredoc struct {
		delimiter string
		tabs      bool
	}
	var heredocs []heredoc
	waitingDoc := ""
	flush := func(end int) {
		if !started {
			return
		}
		text := word.String()
		tokens = append(tokens, terminalPathToken{text: text, literal: literal, quoted: protected, end: end})
		if waitingDoc != "" {
			heredocs = append(heredocs, heredoc{text, waitingDoc == "<<-"})
			waitingDoc = ""
		}
		word.Reset()
		started, literal, protected = false, true, false
	}
	for i := 0; i < len(command); {
		c := command[i]
		if c == '\\' && quote != '\'' {
			if i+1 >= len(command) {
				return nil, false
			}
			started, protected = true, true
			if quote == '"' && !strings.ContainsRune("$\\\"`\n", rune(command[i+1])) {
				word.WriteByte(c)
				i++
				continue
			}
			if command[i+1] != '\n' {
				word.WriteByte(command[i+1])
			}
			i += 2
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
				i++
				continue
			}
			if quote == '"' && (c == '$' || c == '`') {
				literal = false
			}
			word.WriteByte(c)
			i++
			continue
		}
		if c == '\'' || c == '"' {
			started, protected = true, true
			quote = c
			i++
			continue
		}
		if c == '`' || (c == '$' && i+1 < len(command) && command[i+1] == '(') {
			// Command substitution is opaque; parsing its nested grammar belongs
			// to the shell. Avoid interpreting its operators as outer redirects.
			return nil, false
		}
		if c == '#' && !started {
			for i < len(command) && command[i] != '\n' {
				i++
			}
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' {
			flush(i)
			i++
			continue
		}
		op := ""
		for _, candidate := range []string{"<<<", "<<-", "&>>", "&&", "||", ">>", "<<", ">|", ">&", "<&", "<>", "&>", ";;", ">", "<", ";", "|", "&", "(", ")", "\n"} {
			if strings.HasPrefix(command[i:], candidate) {
				op = candidate
				break
			}
		}
		if op == "" {
			started = true
			if strings.ContainsRune("$*?[", rune(c)) {
				literal = false
			}
			word.WriteByte(c)
			i++
			continue
		}
		flush(i)
		if strings.ContainsAny(op, "<>") && len(tokens) > 0 {
			last := tokens[len(tokens)-1]
			if !last.operator && !last.quoted && last.end == i && terminalDigits(last.text) {
				tokens = tokens[:len(tokens)-1]
			}
		}
		tokens = append(tokens, terminalPathToken{text: op, operator: true, end: i + len(op)})
		i += len(op)
		if op == "<<" || op == "<<-" {
			waitingDoc = op
		}
		if op == "\n" && len(heredocs) > 0 {
			for _, doc := range heredocs {
				for i < len(command) {
					end := strings.IndexByte(command[i:], '\n')
					if end < 0 {
						end = len(command) - i
					}
					line := command[i : i+end]
					i += end
					if i < len(command) {
						i++
					}
					if doc.tabs {
						line = strings.TrimLeft(line, "\t")
					}
					if line == doc.delimiter {
						break
					}
				}
			}
			heredocs = nil
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush(len(command))
	return tokens, true
}

func terminalDigits(word string) bool {
	if word == "" {
		return false
	}
	for _, c := range word {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
