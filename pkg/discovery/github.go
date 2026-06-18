// Package discovery provides GitHub-based discovery of ACK controllers.
package discovery

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

const (
	// ACKOrg is the GitHub organization for ACK controllers.
	ACKOrg = "aws-controllers-k8s"
	// ControllerSuffix is the suffix for controller repository names.
	ControllerSuffix = "-controller"
	// CRDBasesPath is the relative path to CRD base YAML files in a controller repo.
	CRDBasesPath = "config/crd/bases"
)

// GitHubClient abstracts the GitHub API for testability.
type GitHubClient interface {
	ListOrgRepos(ctx context.Context, org string, opts *github.RepositoryListByOrgOptions) ([]*github.Repository, *github.Response, error)
}

// githubClientWrapper wraps the real go-github client to implement GitHubClient.
type githubClientWrapper struct {
	client *github.Client
}

func (w *githubClientWrapper) ListOrgRepos(ctx context.Context, org string, opts *github.RepositoryListByOrgOptions) ([]*github.Repository, *github.Response, error) {
	return w.client.Repositories.ListByOrg(ctx, org, opts)
}

// GitHubDiscoverer discovers ACK controllers from GitHub.
type GitHubDiscoverer struct {
	ghClient  GitHubClient
	repoCache *cache.RepoCache
	parser    *parser.CRDParser
}

// NewGitHubDiscoverer creates a new GitHubDiscoverer with a GitHub token and repo cache.
// If token is empty, unauthenticated access is used (subject to rate limits).
func NewGitHubDiscoverer(token string, repoCache *cache.RepoCache) *GitHubDiscoverer {
	var httpClient *github.Client
	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		tc := oauth2.NewClient(context.Background(), ts)
		httpClient = github.NewClient(tc)
	} else {
		httpClient = github.NewClient(nil)
	}

	return &GitHubDiscoverer{
		ghClient:  &githubClientWrapper{client: httpClient},
		repoCache: repoCache,
		parser:    parser.NewCRDParser(),
	}
}

// NewGitHubDiscovererWithClient creates a GitHubDiscoverer with a custom GitHubClient.
// This is useful for testing.
func NewGitHubDiscovererWithClient(client GitHubClient, repoCache *cache.RepoCache) *GitHubDiscoverer {
	return &GitHubDiscoverer{
		ghClient:  client,
		repoCache: repoCache,
		parser:    parser.NewCRDParser(),
	}
}

// DiscoverControllers lists all ACK controller repos, clones/fetches them,
// parses their CRDs, and returns ControllerInfo for each controller that
// has at least one CRD with string fields. Controllers with no CRDs are excluded.
func (d *GitHubDiscoverer) DiscoverControllers(ctx context.Context) ([]types.ControllerInfo, error) {
	repos, err := d.listControllerRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing controller repos: %w", err)
	}

	var controllers []types.ControllerInfo
	for _, repo := range repos {
		info, err := d.processController(repo)
		if err != nil {
			// Skip controllers that fail to process but continue with others
			continue
		}
		if info != nil && len(info.Resources) > 0 {
			controllers = append(controllers, *info)
		}
	}

	return controllers, nil
}

// listControllerRepos fetches all repos in the ACK org and filters for controllers.
func (d *GitHubDiscoverer) listControllerRepos(ctx context.Context) ([]*github.Repository, error) {
	var allRepos []*github.Repository
	opts := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := d.ghClient.ListOrgRepos(ctx, ACKOrg, opts)
		if err != nil {
			return nil, fmt.Errorf("fetching repos from GitHub: %w", err)
		}

		for _, repo := range repos {
			if IsControllerRepo(repo) {
				allRepos = append(allRepos, repo)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

// IsControllerRepo determines if a GitHub repository is a valid ACK controller repo.
// It must: end with "-controller", not be archived, and not be a fork.
func IsControllerRepo(repo *github.Repository) bool {
	name := repo.GetName()
	if !strings.HasSuffix(name, ControllerSuffix) {
		return false
	}
	if repo.GetArchived() {
		return false
	}
	if repo.GetFork() {
		return false
	}
	return true
}

// processController clones/fetches a controller repo and parses its CRDs.
func (d *GitHubDiscoverer) processController(repo *github.Repository) (*types.ControllerInfo, error) {
	repoName := repo.GetName()
	serviceName := strings.TrimSuffix(repoName, ControllerSuffix)

	// Clone or fetch the repo via cache
	repoDir, err := d.repoCache.EnsureRepo(ACKOrg, repoName)
	if err != nil {
		return nil, fmt.Errorf("ensuring repo %s: %w", repoName, err)
	}

	// Parse CRDs
	crdDir := filepath.Join(repoDir, CRDBasesPath)
	resources, err := d.parser.ParseCRDs(crdDir)
	if err != nil {
		// No CRD directory or parse error — exclude this controller
		return nil, nil
	}

	if len(resources) == 0 {
		return nil, nil
	}

	return &types.ControllerInfo{
		ServiceName: serviceName,
		RepoName:    repoName,
		Resources:   resources,
	}, nil
}

// FilterControllerRepoNames filters a list of repository metadata, returning only
// those that pass the controller repo criteria: name ends with "-controller",
// not archived, not a fork.
func FilterControllerRepoNames(repos []RepoMeta) []RepoMeta {
	var result []RepoMeta
	for _, repo := range repos {
		if !strings.HasSuffix(repo.Name, ControllerSuffix) {
			continue
		}
		if repo.Archived {
			continue
		}
		if repo.Fork {
			continue
		}
		result = append(result, repo)
	}
	return result
}

// RepoMeta is a lightweight representation of a GitHub repository for filtering.
type RepoMeta struct {
	Name     string
	Archived bool
	Fork     bool
}
