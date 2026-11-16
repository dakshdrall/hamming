package harness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git -C %s %s: %v: %s", dir, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// EnsureWorktree makes dir a detached git worktree of repo at commit (a full SHA). An existing worktree is
// reused only if it is at that commit with no tracked file modified; anything else is an error rather than
// something to silently overwrite.
func EnsureWorktree(ctx context.Context, repo, dir, commit string) error {
	if !fullSHA.MatchString(commit) {
		return fmt.Errorf("commit %q is not a full 40-character SHA", commit)
	}
	got, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil || got != commit {
		return fmt.Errorf("commit %s is not in %s; run make inputs", commit, repo)
	}
	if _, err := os.Stat(dir); err == nil {
		head, err := git(ctx, dir, "rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("%s exists but is not a git worktree: %w", dir, err)
		}
		if head != commit {
			return fmt.Errorf("%s is at %s, want %s; remove it and re-run", dir, head, commit)
		}
		dirty, err := git(ctx, dir, "status", "--porcelain", "--untracked-files=no")
		if err != nil {
			return err
		}
		if dirty != "" {
			return fmt.Errorf("%s has modified tracked files:\n%s", dir, dirty)
		}
		return nil
	}
	if _, err := git(ctx, repo, "worktree", "prune"); err != nil {
		return err
	}
	_, err = git(ctx, repo, "worktree", "add", "--detach", dir, commit)
	return err
}
