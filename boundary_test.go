package tool

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNoDependencyOnLuckyAgentRuntime(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			value, _ := strconv.Unquote(imp.Path.Value)
			if value == "github.com/yurika0211/luckyagent" || strings.HasPrefix(value, "github.com/yurika0211/luckyagent/") {
				t.Errorf("%s imports host runtime %s", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExecutionPackagesDoNotDependOnTransportOrToolRoot(t *testing.T) {
	for _, dir := range []string{"approval", "hitl", "skillroute", "execpolicy", "compact", "workflow"} {
		t.Run(dir, func(t *testing.T) {
			err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, imp := range file.Imports {
					name, _ := strconv.Unquote(imp.Path.Value)
					if name == "github.com/yurika0211/luckyagent-tools" ||
						strings.HasPrefix(name, "github.com/yurika0211/luckyagent-api") ||
						strings.HasPrefix(name, "github.com/yurika0211/luckyagent-gateway") {
						t.Errorf("%s imports application/transport dependency %s", path, name)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
