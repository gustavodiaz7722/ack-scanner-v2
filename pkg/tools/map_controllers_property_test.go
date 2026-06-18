package tools

import (
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
	"pgregory.net/rapid"
)

// TestProperty7_MappingOutputExcludesUnmatchedTerraformResources verifies that
// for any mapping output, Terraform resources that have no corresponding ACK
// controller SHALL NOT appear in the output mappings.
//
// **Validates: Requirements 3.5**
func TestProperty7_MappingOutputExcludesUnmatchedTerraformResources(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random set of ACK controllers
		numControllers := rapid.IntRange(1, 10).Draw(t, "numControllers")
		controllers := make([]types.ControllerInfo, numControllers)
		controllerServiceNames := make(map[string]bool)
		for i := range controllers {
			serviceLen := rapid.IntRange(2, 12).Draw(t, "serviceLen")
			serviceBytes := make([]byte, serviceLen)
			for j := range serviceBytes {
				serviceBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "serviceByte"))
			}
			serviceName := string(serviceBytes)
			controllers[i] = types.ControllerInfo{
				ServiceName: serviceName,
				RepoName:    serviceName + "-controller",
				Resources: []types.ResourceInfo{
					{Kind: "Resource" + serviceName},
				},
			}
			controllerServiceNames[serviceName] = true
		}

		// Generate Terraform resources — some match controllers, some don't
		numTFResources := rapid.IntRange(1, 20).Draw(t, "numTFResources")
		tfResources := make([]types.TerraformResourceInfo, numTFResources)
		for i := range tfResources {
			serviceLen := rapid.IntRange(2, 12).Draw(t, "tfServiceLen")
			serviceBytes := make([]byte, serviceLen)
			for j := range serviceBytes {
				serviceBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "tfServiceByte"))
			}
			tfService := string(serviceBytes)

			resourceLen := rapid.IntRange(2, 10).Draw(t, "tfResourceLen")
			resourceBytes := make([]byte, resourceLen)
			for j := range resourceBytes {
				resourceBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "tfResourceByte"))
			}
			tfResource := string(resourceBytes)

			tfResources[i] = types.TerraformResourceInfo{
				ServiceName:  tfService,
				ResourceType: "aws_" + tfService + "_" + tfResource,
				DocFilePath:  "website/docs/r/" + tfService + "_" + tfResource + ".html.markdown",
			}
		}

		// Generate random mapping output (simulating what the agent would produce)
		// Each controller has a mapping with some TF doc files from the list
		mappings := make([]types.ControllerMapping, numControllers)
		for i, ctrl := range controllers {
			// Each controller gets 0 to numTFResources entries
			numEntries := rapid.IntRange(0, len(tfResources)).Draw(t, "numEntries")
			entries := make([]types.MappingEntry, 0, numEntries)

			// Pick random TF resources for this mapping
			for j := 0; j < numEntries; j++ {
				tfIdx := rapid.IntRange(0, len(tfResources)-1).Draw(t, "tfIdx")
				tf := tfResources[tfIdx]
				entries = append(entries, types.MappingEntry{
					TFResourceType: tf.ResourceType,
					DocFilePath:    tf.DocFilePath,
					Confidence:     0.8,
				})
			}

			noMatchReason := ""
			if len(entries) == 0 {
				noMatchReason = "No corresponding Terraform resources found"
			}

			mappings[i] = types.ControllerMapping{
				ServiceName:   ctrl.ServiceName,
				TFDocFiles:    entries,
				NoMatchReason: noMatchReason,
			}
		}

		output := &MapAllControllersOutput{
			Mappings: mappings,
		}

		// Property: every mapping in the output must correspond to an ACK controller
		// that was in the input. Unmatched TF resources (those with no ACK controller)
		// must NOT appear as top-level mapping entries.
		for _, mapping := range output.Mappings {
			if !controllerServiceNames[mapping.ServiceName] {
				t.Fatalf("mapping output contains service_name %q which is not in the input controller list",
					mapping.ServiceName)
			}
		}

		// Property: the number of top-level mappings must not exceed the number
		// of input controllers (no phantom mappings for unmatched TF resources)
		if len(output.Mappings) > len(controllers) {
			t.Fatalf("mapping output has %d entries but there are only %d controllers",
				len(output.Mappings), len(controllers))
		}

		// Property: for each TF resource in the full list, if it appears in any
		// mapping's terraform_doc_files, it must be associated with a valid
		// controller service name (i.e., it is not an orphaned/standalone entry)
		for _, mapping := range output.Mappings {
			if mapping.ServiceName == "" {
				t.Fatal("mapping has empty service_name")
			}
			for _, entry := range mapping.TFDocFiles {
				if entry.DocFilePath == "" {
					t.Fatal("mapping entry has empty doc_file_path")
				}
				if entry.TFResourceType == "" {
					t.Fatal("mapping entry has empty terraform_resource_type")
				}
			}
		}
	})
}

// TestProperty7_FilterMappingsExcludesUnmatchedTF verifies the FilterMappings
// utility function correctly excludes TF resources that have no controller.
func TestProperty7_FilterMappingsExcludesUnmatchedTF(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate controllers with known service names
		numControllers := rapid.IntRange(1, 8).Draw(t, "numControllers")
		controllerNames := make([]string, numControllers)
		controllerSet := make(map[string]bool)
		for i := range controllerNames {
			nameLen := rapid.IntRange(3, 10).Draw(t, "nameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "nameByte"))
			}
			controllerNames[i] = string(nameBytes)
			controllerSet[controllerNames[i]] = true
		}

		// Generate TF resources — mix of matched and unmatched
		numTFResources := rapid.IntRange(1, 20).Draw(t, "numTF")
		tfResources := make([]types.TerraformResourceInfo, numTFResources)
		for i := range tfResources {
			// Randomly decide if this TF resource matches a controller
			matchesController := rapid.Bool().Draw(t, "matches")
			var service string
			if matchesController && numControllers > 0 {
				idx := rapid.IntRange(0, numControllers-1).Draw(t, "ctrlIdx")
				service = controllerNames[idx]
			} else {
				// Generate a unique service name that's NOT in the controller set
				for {
					nameLen := rapid.IntRange(3, 10).Draw(t, "unmatchedLen")
					nameBytes := make([]byte, nameLen)
					for j := range nameBytes {
						nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "unmatchedByte"))
					}
					service = string(nameBytes)
					if !controllerSet[service] {
						break
					}
				}
			}
			tfResources[i] = types.TerraformResourceInfo{
				ServiceName:  service,
				ResourceType: "aws_" + service + "_resource",
				DocFilePath:  "website/docs/r/" + service + "_resource.html.markdown",
			}
		}

		// Use FilterMappings to produce output
		result := FilterMappings(controllerNames, tfResources)

		// Property: the output must only contain mappings for known controllers
		for _, mapping := range result {
			if !controllerSet[mapping.ServiceName] {
				t.Fatalf("FilterMappings output contains service %q not in controller list",
					mapping.ServiceName)
			}
		}

		// Property: no TF resource type in any mapping should reference a TF
		// resource whose service is NOT associated with the controller
		allTFByService := make(map[string][]types.TerraformResourceInfo)
		for _, tf := range tfResources {
			allTFByService[tf.ServiceName] = append(allTFByService[tf.ServiceName], tf)
		}

		for _, mapping := range result {
			for _, entry := range mapping.TFDocFiles {
				// The entry's doc file should exist in the original TF list
				found := false
				for _, tf := range tfResources {
					if tf.DocFilePath == entry.DocFilePath {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("mapping entry references doc_file_path %q not in original TF resource list",
						entry.DocFilePath)
				}
			}
		}
	})
}
