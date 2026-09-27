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

// The layout of this module is a claim about its dependencies, and the
// directories can only carry half of it.
//
// They carry which packages are doors — the ones at the top level, that an
// integration in another repository imports — and which are behind one, under
// an `internal/`. What they cannot carry is depth. `command/install` imports
// nothing of ours and `command/doctor` imports ten; they are siblings on disk
// because they are both subcommands, and no arrangement of directories can say
// both things at once. Nesting by depth instead would bury `subscribe`, which
// every display imports, six levels down a path nobody outside could love.
//
// So the depth lives here. Each package's floor is the longest chain of our own
// packages below it: a leaf is 0, and a package is one more than the deepest
// thing it imports. The numbers are a measurement, not a wish — the test
// recomputes them from the source and fails when the table is out of date.
// That is the point. Adding an import that deepens a package is allowed and
// sometimes right; doing it without noticing is what this catches, because a
// floor that moves is the shape of the program changing.
var theFloorEachPackageIsOn = map[string]int{
	// The floor. Nothing of ours below these.
	"session":                 0, // the vocabulary: every other package speaks it
	"internal/paths":          0, // every location this program owns
	"capture":                 0, // read the agent's environment, from inside it
	"hook/internal/branch":    0, // the one package with a single owner, so it nests
	"command/internal/exit":   0,
	"command/internal/onpath": 0,

	// One step up: they know the vocabulary, or where things are, and nothing else.
	"internal/config":       1,
	"internal/process":      1,
	"logs":                  1,
	"tool":                  1,
	"container":             1,
	"command/internal/rows": 1,
	"command/install":       1, // a dispatcher: it execs, so it links almost nothing

	"internal/sessionstore": 2,
	"internal/subcommand":   2,

	"internal/core":       3,
	"internal/containers": 3,

	"internal/sessionwatcher": 4,
	"command/internal/find":   4,
	"command/list":            4,

	// The doors an integration in another repository imports, and the
	// subcommands, which are clients of exactly the same things (R14).
	"hook":                5,
	"subscribe":           5,
	"command/focus":       5,
	"command/annotate":    5,
	"command/watcher":     5,
	"command/doctor":      6,
	"command/replay":      6,
	"command/reportevent": 6,
	"command/tail":        6,

	// main, which imports every command and is imported by nothing.
	".": 7,
}

const modulePath = "github.com/lassoColombo/agent-notify"

func TestEveryPackageIsOnTheFloorTheTableSaysItIs(t *testing.T) {
	graph := whatEachPackageImports(t)

	for path := range graph {
		if _, placed := theFloorEachPackageIsOn[path]; !placed {
			t.Errorf("package %q is in no floor: add it to theFloorEachPackageIsOn, "+
				"which means deciding where it sits before writing it", path)
		}
	}
	for path := range theFloorEachPackageIsOn {
		if _, exists := graph[path]; !exists {
			t.Errorf("the table names %q, which is not a package any more", path)
		}
	}
	if t.Failed() {
		return
	}

	measured := map[string]int{}
	var floorOf func(string) int
	floorOf = func(path string) int {
		if known, done := measured[path]; done {
			return known
		}
		measured[path] = 0 // breaks a cycle the compiler would have refused anyway
		deepest := -1
		for _, imported := range graph[path] {
			if below := floorOf(imported); below > deepest {
				deepest = below
			}
		}
		measured[path] = deepest + 1
		return measured[path]
	}

	for _, path := range sorted(graph) {
		want, got := theFloorEachPackageIsOn[path], floorOf(path)
		if want == got {
			continue
		}
		t.Errorf("%s is on floor %d, and the table says %d.\n"+
			"    It imports: %s\n"+
			"    Either the import that moved it is wrong, or the table is out of date.",
			path, got, want, strings.Join(graph[path], ", "))
	}
}

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
		"container": true, "capture": true, "tool": true, "logs": true,
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
// list`, so that files excluded by a build tag are read too. An import that
// only exists on Linux is still an edge in this graph, and a layout that is
// only true on the machine the test ran on is not worth having.
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

func sorted(graph map[string][]string) []string {
	paths := make([]string, 0, len(graph))
	for path := range graph {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
