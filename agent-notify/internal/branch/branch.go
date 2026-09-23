// Package branch answers which branch is checked out where a session is
// working.
//
// It is the one place core contains the name of a tool, and R10 is amended
// rather than quietly broken (§A7.4.4, D-63). That rule exists so per-tool
// knowledge lives in the integration that owns the tool, and there is no such
// integration here: every agent runs in somebody's checkout, both of the ones
// we have already believe they know which one, neither can be trusted about it,
// and an adapter-side answer would be these forty lines written once per agent
// forever (R24).
//
// It reads files and never runs git. A subprocess on the path the agent is
// waiting on would be the expensive mistake, and nothing here needs one: the
// two files below say everything.
package branch

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// asFarUpAsItIsWorthLooking bounds the walk towards the root. A session's
	// directory is a handful of levels inside its checkout, and a path that is
	// not in a repository is not going to become one sixty levels up.
	asFarUpAsItIsWorthLooking = 64
	// oneReadWorthOfHead is generous for a file that holds one line, and is
	// here so that pointing this at something enormous called HEAD costs a page
	// rather than the file.
	oneReadWorthOfHead = 4 << 10
)

// At is the branch checked out at this directory, or "" when there is no
// answer.
//
// "" is an ordinary answer and there are three ways to reach it, none of them an
// error: a directory in no repository at all, which is what a session started in
// a workspace that merely contains checkouts looks like; a detached head, which
// is a commit and has no branch to name; and anything unreadable, because a
// branch nobody can be sure of is worse than no branch (§A11.3).
func At(directory string) string {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return ""
	}
	gitDirectory := findTheGitDirectoryAbove(directory)
	if gitDirectory == "" {
		return ""
	}
	return branchNamedIn(filepath.Join(gitDirectory, "HEAD"))
}

// findTheGitDirectoryAbove walks towards the root looking for `.git`, and
// resolves the two shapes it comes in.
//
// In an ordinary checkout `.git` is the directory itself. In a worktree it is a
// file holding `gitdir: <path>`, pointing at a per-worktree directory inside the
// original — and that indirection is not an edge case to tolerate but the case
// that has to work, because a worktree is how you run two agents on two branches
// of one repository at once, which is most of the reason to want this field on a
// bar at all.
func findTheGitDirectoryAbove(directory string) string {
	for range asFarUpAsItIsWorthLooking {
		candidate := filepath.Join(directory, ".git")
		switch facts, err := os.Stat(candidate); {
		case err == nil && facts.IsDir():
			return candidate
		case err == nil:
			if resolved := whereTheGitFilePoints(candidate); resolved != "" {
				return resolved
			}
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
	return ""
}

// whereTheGitFilePoints reads a worktree's `.git` file, which holds one line:
//
//	gitdir: /Users/you/project/.git/worktrees/the-worktree
func whereTheGitFilePoints(path string) string {
	content, err := readAtMost(path)
	if err != nil {
		return ""
	}
	pointer, found := strings.CutPrefix(strings.TrimSpace(string(content)), "gitdir:")
	if !found {
		return ""
	}
	return strings.TrimSpace(pointer)
}

// branchNamedIn reads HEAD, which is either a symbolic ref or a commit:
//
//	ref: refs/heads/model-quota-branch
//	34214d9f1a0c9e1b5b0f1e2d3c4b5a6978889900
//
// The second is a detached head. It has no branch in it, so the answer is ""
// rather than forty characters nobody asked for.
func branchNamedIn(path string) string {
	content, err := readAtMost(path)
	if err != nil {
		return ""
	}
	reference, found := strings.CutPrefix(strings.TrimSpace(string(content)), "ref:")
	if !found {
		return ""
	}

	// A branch name may hold slashes — `release/2.1` is one name — so only the
	// prefix that says what kind of ref this is comes off.
	name := strings.TrimSpace(reference)
	for _, prefix := range []string{"refs/heads/", "refs/"} {
		if trimmed, found := strings.CutPrefix(name, prefix); found {
			return trimmed
		}
	}
	return name
}

// readAtMost reads a file that is supposed to hold one line, and refuses to be
// surprised by one that does not.
func readAtMost(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var content bytes.Buffer
	if _, err := content.ReadFrom(io.LimitReader(file, oneReadWorthOfHead)); err != nil {
		return nil, err
	}
	return content.Bytes(), nil
}
