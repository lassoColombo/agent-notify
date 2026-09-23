package branch_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lassoColombo/agent-notify/internal/branch"
)

// checkout writes the two files a repository needs for this package to answer,
// and nothing else. That it can be built out of plain files is the point: this
// reads git's on-disk shape and never runs git, so a test needs no git either.
func checkout(t *testing.T, head string) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, ".git", "HEAD"), head)
	return root
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestTheBranchAHeadNames(t *testing.T) {
	for _, one := range []struct {
		why  string
		head string
		want string
	}{
		{"the ordinary case", "ref: refs/heads/main\n", "main"},
		{"a name with slashes in it is one name", "ref: refs/heads/release/2.1\n", "release/2.1"},
		{"no trailing newline", "ref: refs/heads/main", "main"},
		{"a detached head is a commit and has no branch", "34214d9f1a0c9e1b5b0f1e2d3c4b5a6978889900\n", ""},
		{"something that is not a HEAD at all", "who knows\n", ""},
		{"an empty HEAD", "", ""},
	} {
		if got := branch.At(checkout(t, one.head)); got != one.want {
			t.Errorf("%s: At(...) = %q, want %q", one.why, got, one.want)
		}
	}
}

// TestASubdirectoryFindsTheCheckoutAboveIt. A session's cwd is almost never the
// root of the repository it is in.
func TestASubdirectoryFindsTheCheckoutAboveIt(t *testing.T) {
	root := checkout(t, "ref: refs/heads/main\n")
	deep := filepath.Join(root, "internal", "watcher", "testdata")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if got := branch.At(deep); got != "main" {
		t.Errorf("At(three levels down) = %q, want main", got)
	}
}

// TestAWorktreeIsFollowedToItsOwnHead is the case that has to work rather than
// the case to tolerate: a worktree is how two agents run on two branches of one
// repository at once, which is most of the reason to want this on a bar.
func TestAWorktreeIsFollowedToItsOwnHead(t *testing.T) {
	original := checkout(t, "ref: refs/heads/main\n")
	write(t, filepath.Join(original, ".git", "worktrees", "spike", "HEAD"), "ref: refs/heads/spike\n")

	worktree := t.TempDir()
	write(t, filepath.Join(worktree, ".git"),
		"gitdir: "+filepath.Join(original, ".git", "worktrees", "spike")+"\n")

	if got := branch.At(worktree); got != "spike" {
		t.Errorf("At(a worktree) = %q, want spike — not the branch of the checkout it points into", got)
	}
}

// TestADirectoryInNoRepositoryIsAnOrdinaryAnswer, and it is not a rare one: a
// workspace that merely contains checkouts is exactly where somebody runs an
// agent across three of them.
func TestADirectoryInNoRepositoryIsAnOrdinaryAnswer(t *testing.T) {
	for _, directory := range []string{t.TempDir(), "", "   ", filepath.Join(t.TempDir(), "never-created")} {
		if got := branch.At(directory); got != "" {
			t.Errorf("At(%q) = %q, want nothing", directory, got)
		}
	}
}

// TestAGitFilePointingNowhereSaysNothing. A branch nobody can be sure of is
// worse than no branch: it is the same hazard as a stale pane id (§A11.3).
func TestAGitFilePointingNowhereSaysNothing(t *testing.T) {
	worktree := t.TempDir()
	write(t, filepath.Join(worktree, ".git"), "gitdir: /nowhere/at/all\n")

	if got := branch.At(worktree); got != "" {
		t.Errorf("At(a worktree pointing nowhere) = %q, want nothing", got)
	}
}
