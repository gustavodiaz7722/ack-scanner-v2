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
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
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
	log       *logger.Logger
}

// NewGitHubDiscoverer creates a new GitHubDiscoverer with a GitHub token and repo cache.
// If token is empty, unauthenticated access is used (subject to rate limits).
func NewGitHubDiscoverer(token string, repoCache *cache.RepoCache, log ...*logger.Logger) *GitHubDiscoverer {
	var httpClient *github.Client
	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		tc := oauth2.NewClient(context.Background(), ts)
		httpClient = github.NewClient(tc)
	} else {
		httpClient = github.NewClient(nil)
	}

	l := logger.Nop()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}

	return &GitHubDiscoverer{
		ghClient:  &githubClientWrapper{client: httpClient},
		repoCache: repoCache,
		parser:    parser.NewCRDParser(),
		log:       l,
	}
}

// NewGitHubDiscovererWithClient creates a GitHubDiscoverer with a custom GitHubClient.
// This is useful for testing.
func NewGitHubDiscovererWithClient(client GitHubClient, repoCache *cache.RepoCache) *GitHubDiscoverer {
	return &GitHubDiscoverer{
		ghClient:  client,
		repoCache: repoCache,
		parser:    parser.NewCRDParser(),
		log:       logger.Nop(),
	}
}

// DiscoverControllers lists all ACK controller repos, clones/fetches them,
// parses their CRDs, and returns ControllerInfo for each controller that
// has at least one CRD with string fields. Controllers with no CRDs are excluded.
func (d *GitHubDiscoverer) DiscoverControllers(ctx context.Context) ([]types.ControllerInfo, error) {
	d.log.Info("discover_controllers: listing controller repos from GitHub org %s", ACKOrg)

	repos, err := d.listControllerRepos(ctx)
	if err != nil {
		d.log.Error("discover_controllers: failed to list repos: %v", err)
		return nil, fmt.Errorf("listing controller repos: %w", err)
	}

	d.log.Info("discover_controllers: found %d controller repos, processing CRDs", len(repos))

	var controllers []types.ControllerInfo
	skipped := 0
	for _, repo := range repos {
		info, err := d.processController(repo)
		if err != nil {
			d.log.Debug("discover_controllers: skipping %s: %v", repo.GetName(), err)
			skipped++
			continue
		}
		if info != nil && len(info.Resources) > 0 {
			controllers = append(controllers, *info)
			d.log.Debug("discover_controllers: %s — %d resources, %d string fields",
				info.ServiceName, len(info.Resources), countStringFields(info.Resources))
		}
	}

	d.log.Info("discover_controllers: %d controllers with CRDs, %d skipped", len(controllers), skipped)

	return controllers, nil
}

// countStringFields counts total string fields across resources.
func countStringFields(resources []types.ResourceInfo) int {
	n := 0
	for _, r := range resources {
		n += len(r.StringFields)
	}
	return n
}

// listControllerRepos fetches all repos in the ACK org and filters for controllers.
func (d *GitHubDiscoverer) listControllerRepos(ctx context.Context) ([]*github.Repository, error) {
	var allRepos []*github.Repository
	opts := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	page := 1
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

		d.log.Debug("discover_controllers: fetched page %d, %d repos total so far", page, len(allRepos))

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		page++
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

// GeneratorYAMLPath is the relative path to the generator.yaml file in a controller repo.
const GeneratorYAMLPath = "generator.yaml"

// processController clones/fetches a controller repo and parses its CRDs.
// After CRD parsing, it loads generator.yaml and enriches each FieldInfo with
// annotation status (is_document, is_iam_policy, has_reference, reference_config).
// When generator.yaml is missing or unparseable, annotation fields remain at zero values.
func (d *GitHubDiscoverer) processController(repo *github.Repository) (*types.ControllerInfo, error) {
	repoName := repo.GetName()
	serviceName := strings.TrimSuffix(repoName, ControllerSuffix)

	// Clone or fetch the repo via cache
	d.log.Debug("discover_controllers: ensuring repo cache for %s", repoName)
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

	// Load generator.yaml to enrich fields with annotation status.
	// If the file is missing or unparseable, all annotation fields stay at zero values.
	genPath := filepath.Join(repoDir, GeneratorYAMLPath)
	genConfig, genErr := parser.ParseGeneratorConfig(genPath)
	if genErr != nil {
		d.log.Debug("discover_controllers: %s: generator.yaml not available: %v", repoName, genErr)
	}

	// Enrich fields with annotation status from generator.yaml
	if genConfig != nil {
		for i := range resources {
			EnrichResourceFields(&resources[i], genConfig)
		}
	}

	return &types.ControllerInfo{
		ServiceName: serviceName,
		RepoName:    repoName,
		Resources:   resources,
	}, nil
}

// EnrichResourceFields enriches the string fields of a resource with annotation
// status from the parsed generator.yaml configuration.
func EnrichResourceFields(resource *types.ResourceInfo, genConfig *parser.GeneratorConfig) {
	for i := range resource.StringFields {
		field := &resource.StringFields[i]
		isDoc, isIAM := genConfig.HasAnnotation(resource.Kind, field.Name)
		field.IsDocument = isDoc
		field.IsIAMPolicy = isIAM

		ref := genConfig.HasReference(resource.Kind, field.Name)
		if ref != nil {
			field.HasReference = true
			field.ReferenceConfig = &types.ReferenceInfo{
				Resource:    ref.Resource,
				ServiceName: ref.ServiceName,
				Path:        ref.Path,
			}
		}
	}
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
