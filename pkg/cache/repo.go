package cache

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RepoCache manages git clone caching for repositories.
// Repositories are cloned once and reused from disk on subsequent calls.
// Use Invalidate or InvalidateAll to force a fresh clone.
type RepoCache struct {
	baseDir string
}

// NewRepoCache creates a new RepoCache rooted at the given base directory.
// The directory is created if it does not exist.
func NewRepoCache(baseDir string) (*RepoCache, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating repo cache directory: %w", err)
	}
	return &RepoCache{baseDir: baseDir}, nil
}

// repoDir returns the local path for a cached repository clone.
func (r *RepoCache) repoDir(org, repo string) string {
	return filepath.Join(r.baseDir, org, repo)
}

// EnsureRepo ensures a repository is cloned locally. If it already exists,
// returns the cached path immediately without fetching. Use Invalidate to
// force a fresh clone.
func (r *RepoCache) EnsureRepo(org, repo string) (string, error) {
	dir := r.repoDir(org, repo)

	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		// Repo already exists — return cached path
		return dir, nil
	}

	// Clone the repository
	url := fmt.Sprintf("https://github.com/%s/%s.git", org, repo)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}
	cmd := exec.Command("git", "clone", url, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone %s: %s: %w", url, string(out), err)
	}
	return dir, nil
}

// EnsureRepoSparse ensures a repository is cloned locally with sparse checkout,
// only checking out the specified paths. If it already exists, returns the
// cached path immediately without fetching. Use Invalidate to force a fresh clone.
func (r *RepoCache) EnsureRepoSparse(org, repo string, paths []string) (string, error) {
	dir := r.repoDir(org, repo)

	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		// Repo already exists — return cached path
		return dir, nil
	}

	// Clone with sparse checkout
	url := fmt.Sprintf("https://github.com/%s/%s.git", org, repo)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}

	// Initialize sparse clone
	cmd := exec.Command("git", "clone", "--filter=blob:none", "--no-checkout", url, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone (sparse) %s: %s: %w", url, string(out), err)
	}

	// Enable sparse checkout
	cmd = exec.Command("git", "sparse-checkout", "init", "--cone")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git sparse-checkout init: %s: %w", string(out), err)
	}

	// Set sparse checkout paths
	args := append([]string{"sparse-checkout", "set"}, paths...)
	cmd = exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git sparse-checkout set: %s: %w", string(out), err)
	}

	// Checkout
	cmd = exec.Command("git", "checkout")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git checkout: %s: %w", string(out), err)
	}

	return dir, nil
}

// Invalidate removes a cached repository.
func (r *RepoCache) Invalidate(org, repo string) error {
	dir := r.repoDir(org, repo)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing cached repo %s/%s: %w", org, repo, err)
	}
	return nil
}

// InvalidateAll removes all cached repositories.
func (r *RepoCache) InvalidateAll() error {
	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading repo cache directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(r.baseDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}
	return nil
}
