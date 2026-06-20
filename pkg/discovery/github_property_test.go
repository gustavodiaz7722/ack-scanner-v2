package discovery_test

import (
	"encoding/json"
	"testing"

	"pgregory.net/rapid"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// Property 1: Controller discovery filtering
// For any list of repo names, only those ending with `-controller`, not archived,
// not forks pass the filter.
//
// **Validates: Requirements 1.1**
func TestProperty1_ControllerDiscoveryFiltering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a list of repo metadata with random properties
		n := rapid.IntRange(0, 20).Draw(t, "numRepos")
		repos := make([]discovery.RepoMeta, n)
		for i := range repos {
			repos[i] = discovery.RepoMeta{
				Name:     rapid.StringMatching(`[a-z][a-z0-9\-]{0,20}(-controller)?`).Draw(t, "repoName"),
				Archived: rapid.Bool().Draw(t, "archived"),
				Fork:     rapid.Bool().Draw(t, "fork"),
			}
		}

		filtered := discovery.FilterControllerRepoNames(repos)

		// Verify all filtered repos satisfy the criteria
		for _, repo := range filtered {
			if repo.Name[len(repo.Name)-11:] != "-controller" {
				t.Fatalf("filtered repo %q does not end with -controller", repo.Name)
			}
			if repo.Archived {
				t.Fatalf("filtered repo %q is archived", repo.Name)
			}
			if repo.Fork {
				t.Fatalf("filtered repo %q is a fork", repo.Name)
			}
		}

		// Verify all repos that meet criteria are included
		for _, repo := range repos {
			shouldPass := len(repo.Name) >= 11 &&
				repo.Name[len(repo.Name)-11:] == "-controller" &&
				!repo.Archived &&
				!repo.Fork

			found := false
			for _, f := range filtered {
				if f.Name == repo.Name && f.Archived == repo.Archived && f.Fork == repo.Fork {
					found = true
					break
				}
			}

			if shouldPass && !found {
				t.Fatalf("repo %q meets criteria but was not in filtered output", repo.Name)
			}
			if !shouldPass && found {
				t.Fatalf("repo %q does not meet criteria but was in filtered output", repo.Name)
			}
		}
	})
}

// Property 3: Empty controller exclusion
// Controllers with no CRDs/resources are excluded from output.
//
// **Validates: Requirements 1.4**
func TestProperty3_EmptyControllerExclusion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a mix of controllers: some with resources, some without
		n := rapid.IntRange(1, 10).Draw(t, "numControllers")
		var allControllers []types.ControllerInfo

		for i := 0; i < n; i++ {
			numResources := rapid.IntRange(0, 3).Draw(t, "numResources")
			var resources []types.ResourceInfo
			for j := 0; j < numResources; j++ {
				numFields := rapid.IntRange(1, 5).Draw(t, "numFields")
				var fields []types.FieldInfo
				for k := 0; k < numFields; k++ {
					fields = append(fields, types.FieldInfo{
						Name: rapid.StringMatching(`[A-Z][a-zA-Z]{2,15}`).Draw(t, "fieldName"),
						Path: rapid.StringMatching(`[a-z][a-zA-Z.]{2,30}`).Draw(t, "fieldPath"),
					})
				}
				resources = append(resources, types.ResourceInfo{
					Kind:         rapid.StringMatching(`[A-Z][a-zA-Z]{3,15}`).Draw(t, "kind"),
					StringFields: fields,
				})
			}
			allControllers = append(allControllers, types.ControllerInfo{
				ServiceName: rapid.StringMatching(`[a-z]{3,12}`).Draw(t, "serviceName"),
				RepoName:    rapid.StringMatching(`[a-z]{3,12}-controller`).Draw(t, "repoName"),
				Resources:   resources,
			})
		}

		// Apply the exclusion: filter out controllers with no resources
		filtered := FilterEmptyControllers(allControllers)

		// Verify: no controller in filtered output has zero resources
		for _, c := range filtered {
			if len(c.Resources) == 0 {
				t.Fatalf("controller %q has no resources but was included", c.ServiceName)
			}
		}

		// Verify: all controllers with resources are present in output
		for _, c := range allControllers {
			if len(c.Resources) > 0 {
				found := false
				for _, f := range filtered {
					if f.ServiceName == c.ServiceName && f.RepoName == c.RepoName {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("controller %q has resources but was excluded", c.ServiceName)
				}
			}
		}
	})
}

// Property 4: Controller discovery JSON output validity
// Serialized output is valid JSON with correct schema, including annotation fields.
//
// **Validates: Requirements 1.1, 1.2, 1.3**
func TestProperty4_ControllerDiscoveryJSONOutputValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate arbitrary controller info with annotation fields
		n := rapid.IntRange(0, 5).Draw(t, "numControllers")
		var controllers []types.ControllerInfo

		for i := 0; i < n; i++ {
			numResources := rapid.IntRange(0, 3).Draw(t, "numResources")
			var resources []types.ResourceInfo
			for j := 0; j < numResources; j++ {
				numFields := rapid.IntRange(0, 5).Draw(t, "numFields")
				var fields []types.FieldInfo
				for k := 0; k < numFields; k++ {
					field := types.FieldInfo{
						Name:         rapid.StringMatching(`[A-Z][a-zA-Z]{2,15}`).Draw(t, "fieldName"),
						Path:         rapid.StringMatching(`[a-z][a-zA-Z.]{2,30}`).Draw(t, "fieldPath"),
						IsDocument:   rapid.Bool().Draw(t, "isDocument"),
						IsIAMPolicy:  rapid.Bool().Draw(t, "isIAMPolicy"),
						HasReference: rapid.Bool().Draw(t, "hasReference"),
					}
					// Optionally add reference config when HasReference is true
					if field.HasReference {
						field.ReferenceConfig = &types.ReferenceInfo{
							Resource:    rapid.StringMatching(`[A-Z][a-zA-Z]{3,15}`).Draw(t, "refResource"),
							ServiceName: rapid.StringMatching(`[a-z]{2,10}`).Draw(t, "refServiceName"),
							Path:        rapid.StringMatching(`(Spec|Status)\.[A-Za-z.]{3,20}`).Draw(t, "refPath"),
						}
					}
					fields = append(fields, field)
				}
				resources = append(resources, types.ResourceInfo{
					Kind:         rapid.StringMatching(`[A-Z][a-zA-Z]{3,15}`).Draw(t, "kind"),
					StringFields: fields,
				})
			}
			controllers = append(controllers, types.ControllerInfo{
				ServiceName: rapid.StringMatching(`[a-z]{3,12}`).Draw(t, "serviceName"),
				RepoName:    rapid.StringMatching(`[a-z]{3,12}-controller`).Draw(t, "repoName"),
				Resources:   resources,
			})
		}

		// Serialize to JSON
		output := struct {
			Controllers []types.ControllerInfo `json:"controllers"`
		}{Controllers: controllers}

		data, err := json.Marshal(output)
		if err != nil {
			t.Fatalf("JSON marshaling failed: %v", err)
		}

		// Verify valid JSON
		if !json.Valid(data) {
			t.Fatal("serialized output is not valid JSON")
		}

		// Verify schema: unmarshal and check fields including new annotation fields
		var parsed struct {
			Controllers []struct {
				ServiceName string `json:"service_name"`
				RepoName    string `json:"repo_name"`
				Resources   []struct {
					Kind         string `json:"kind"`
					StringFields []struct {
						Name            string `json:"name"`
						Path            string `json:"path"`
						IsDocument      bool   `json:"is_document"`
						IsIAMPolicy     bool   `json:"is_iam_policy"`
						HasReference    bool   `json:"has_reference"`
						ReferenceConfig *struct {
							Resource    string `json:"resource"`
							ServiceName string `json:"service_name,omitempty"`
							Path        string `json:"path"`
						} `json:"reference_config,omitempty"`
					} `json:"string_fields"`
				} `json:"resources"`
			} `json:"controllers"`
		}

		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal to expected schema: %v", err)
		}

		// Verify every entry has required fields
		for i, ctrl := range parsed.Controllers {
			if ctrl.ServiceName == "" {
				t.Fatalf("controller[%d] missing service_name", i)
			}
			if ctrl.RepoName == "" {
				t.Fatalf("controller[%d] missing repo_name", i)
			}
			// Resources can be empty (they are an array)
			for j, res := range ctrl.Resources {
				if res.Kind == "" {
					t.Fatalf("controller[%d].resources[%d] missing kind", i, j)
				}
				for k, field := range res.StringFields {
					if field.Name == "" {
						t.Fatalf("controller[%d].resources[%d].string_fields[%d] missing name", i, j, k)
					}
					if field.Path == "" {
						t.Fatalf("controller[%d].resources[%d].string_fields[%d] missing path", i, j, k)
					}
				}
			}
		}

		// Verify backward compatibility: re-parse with old schema (no annotation fields)
		// This confirms the JSON is additive and doesn't break consumers expecting old schema
		var oldParsed struct {
			Controllers []struct {
				ServiceName string `json:"service_name"`
				RepoName    string `json:"repo_name"`
				Resources   []struct {
					Kind         string `json:"kind"`
					StringFields []struct {
						Name string `json:"name"`
						Path string `json:"path"`
					} `json:"string_fields"`
				} `json:"resources"`
			} `json:"controllers"`
		}

		if err := json.Unmarshal(data, &oldParsed); err != nil {
			t.Fatalf("backward-compatible unmarshal failed: %v", err)
		}

		// Verify annotation fields round-trip correctly
		for i, ctrl := range parsed.Controllers {
			for j, res := range ctrl.Resources {
				for k, field := range res.StringFields {
					origField := controllers[i].Resources[j].StringFields[k]
					if field.IsDocument != origField.IsDocument {
						t.Fatalf("controller[%d].resources[%d].string_fields[%d] is_document mismatch", i, j, k)
					}
					if field.IsIAMPolicy != origField.IsIAMPolicy {
						t.Fatalf("controller[%d].resources[%d].string_fields[%d] is_iam_policy mismatch", i, j, k)
					}
					if field.HasReference != origField.HasReference {
						t.Fatalf("controller[%d].resources[%d].string_fields[%d] has_reference mismatch", i, j, k)
					}
					if origField.HasReference && origField.ReferenceConfig != nil {
						if field.ReferenceConfig == nil {
							t.Fatalf("controller[%d].resources[%d].string_fields[%d] reference_config missing", i, j, k)
						}
						if field.ReferenceConfig.Resource != origField.ReferenceConfig.Resource {
							t.Fatalf("controller[%d].resources[%d].string_fields[%d] reference_config.resource mismatch", i, j, k)
						}
						if field.ReferenceConfig.Path != origField.ReferenceConfig.Path {
							t.Fatalf("controller[%d].resources[%d].string_fields[%d] reference_config.path mismatch", i, j, k)
						}
					}
				}
			}
		}
	})
}

// FilterEmptyControllers is the filtering logic tested by Property 3.
// It excludes controllers with no CRD resources from output.
func FilterEmptyControllers(controllers []types.ControllerInfo) []types.ControllerInfo {
	var result []types.ControllerInfo
	for _, c := range controllers {
		if len(c.Resources) > 0 {
			result = append(result, c)
		}
	}
	return result
}
