package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The layout of this module is a claim about its dependencies: the packages
// at the top level are the doors an integration in another module imports,
// and everything else is behind an internal/. The compiler refuses a cycle;
// these tests refuse the rest.

const modulePath = "github.com/lassoColombo/agent-notify"

// TestNoCommandImportsAnotherCommand. The nine subcommands are siblings, and
// what two of them share lives under command/internal — where the sharing is
// visible in an import rather than implicit in having been one package.
func TestNoCommandImportsAnotherCommand(t *testing.T) {
	for path, imports := range whatEachPackageImports(t) {
		if !isASubcommand(path) {
			continue
		}
		for _, imported := range imports {
			if isASubcommand(imported) {
				t.Errorf("%s imports %s; a command that needs another command's code "+
					"wants it under command/internal instead", path, imported)
			}
		}
	}
}

func isASubcommand(path string) bool {
	return strings.HasPrefix(path, "command/") &&
		!strings.HasPrefix(path, "command/internal/")
}

// TestNothingBelowTheDoorsIsPublic. Every exported symbol is a promise to a
// repository we do not control (§A10.4), so the packages an integration may
// import are exactly the ones at the top level and everything else is behind an
// internal/.
func TestOnlyTheTopLevelIsImportable(t *testing.T) {
	doors := map[string]bool{
		"session": true, "hook": true, "subscribe": true,
		"container": true, "tool": true, "logs": true,
	}
	for path := range whatEachPackageImports(t) {
		if path == "." || strings.Contains(path, "internal/") || strings.HasPrefix(path, "command/") {
			continue
		}
		if !doors[path] {
			t.Errorf("%s is importable from outside this module and is not one of the "+
				"doors; either it is a door and §A10.4 needs amending, or it belongs "+
				"under an internal/", path)
		}
	}
}

// whatEachPackageImports reads the source rather than shelling out to `go
// list`, so that files excluded by a build tag are read too.
func whatEachPackageImports(t *testing.T) map[string][]string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot find the module root: %v", err)
	}

	graph := map[string][]string{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		here, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		here = filepath.ToSlash(here)
		if _, seen := graph[here]; !seen {
			graph[here] = nil
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			unquoted, err := strconv.Unquote(imported.Path.Value)
			if err != nil || !strings.HasPrefix(unquoted, modulePath) {
				continue
			}
			ours := strings.TrimPrefix(strings.TrimPrefix(unquoted, modulePath), "/")
			if ours == "" {
				ours = "."
			}
			if !contains(graph[here], ours) {
				graph[here] = append(graph[here], ours)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cannot read the module: %v", err)
	}
	for path := range graph {
		sort.Strings(graph[path])
	}
	return graph
}

func contains(haystack []string, needle string) bool {
	for _, straw := range haystack {
		if straw == needle {
			return true
		}
	}
	return false
}
