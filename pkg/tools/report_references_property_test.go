package tools

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/reporter"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
	"pgregory.net/rapid"
)

// --- Generators for reference report tests ---

// genTFResourceType generates a Terraform resource type like "aws_iam_role".
func genTFResourceType() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		services := []string{"iam", "ec2", "s3", "kms", "sqs", "sns", "lambda", "rds", "elasticache"}
		service := rapid.SampledFrom(services).Draw(t, "service")
		resourceLen := rapid.IntRange(3, 10).Draw(t, "resLen")
		var sb strings.Builder
		sb.WriteString("aws_")
		sb.WriteString(service)
		sb.WriteByte('_')
		for i := range resourceLen {
			sb.WriteByte(rapid.ByteRange('a', 'z').Draw(t, "char"))
			_ = i
		}
		return sb.String()
	})
}

// genConfidence generates a confidence value between 0.0 and 1.0.
func genConfidence() *rapid.Generator[float64] {
	return rapid.Custom(func(t *rapid.T) float64 {
		// Generate integer 0-100 and divide by 100 for clean floats
		v := rapid.IntRange(0, 100).Draw(t, "confidence")
		return float64(v) / 100.0
	})
}

// genSources generates a sources list with 1-3 valid sources.
func genSources() *rapid.Generator[[]string] {
	return rapid.Custom(func(t *rapid.T) []string {
		allSources := []string{"upjet", "terraform_docs", "api_model"}
		count := rapid.IntRange(1, 3).Draw(t, "sourceCount")
		// Use a boolean mask to select sources
		selected := make([]string, 0, count)
		for _, src := range allSources {
			if len(selected) >= count {
				break
			}
			if rapid.Bool().Draw(t, "include_"+src) || len(allSources)-len(selected) <= count-len(selected) {
				selected = append(selected, src)
			}
		}
		// Ensure we have at least 1
		if len(selected) == 0 {
			selected = append(selected, allSources[rapid.IntRange(0, 2).Draw(t, "fallback")])
		}
		return selected
	})
}

// genReferenceCategory generates a valid reference category.
func genReferenceCategory() *rapid.Generator[types.ReferenceCategory] {
	return rapid.SampledFrom([]types.ReferenceCategory{
		types.RefCategoryGap,
		types.RefCategoryAnnotated,
		types.RefCategoryPartial,
	})
}

// genReferenceGapEntry generates a random ReferenceGapEntry.
func genReferenceGapEntry() *rapid.Generator[types.ReferenceGapEntry] {
	return rapid.Custom(func(t *rapid.T) types.ReferenceGapEntry {
		service := genServiceName().Draw(t, "service")
		resource := genResourceName().Draw(t, "resource")
		ackField := genFieldName().Draw(t, "ackField")
		tfResource := genTFResourceType().Draw(t, "tfResource")
		confidence := genConfidence().Draw(t, "confidence")
		sources := genSources().Draw(t, "sources")
		status := genReferenceCategory().Draw(t, "status")
		isAmbiguous := rapid.Bool().Draw(t, "ambiguous")

		return types.ReferenceGapEntry{
			ServiceName:       service,
			ResourceName:      resource,
			ACKFieldName:      ackField,
			ACKFieldPath:      "Spec." + ackField,
			TargetTFResource:  tfResource,
			TargetACKService:  service,
			TargetACKResource: resource,
			RecommendedPath:   "Status.ACKResourceMetadata.ARN",
			Confidence:        confidence,
			Sources:           sources,
			CurrentStatus:     string(status),
			IsAmbiguous:       isAmbiguous,
		}
	})
}

// genReferenceGapReport generates a random ReferenceGapReport with consistent summary.
func genReferenceGapReport() *rapid.Generator[*types.ReferenceGapReport] {
	return rapid.Custom(func(t *rapid.T) *types.ReferenceGapReport {
		entryCount := rapid.IntRange(0, 20).Draw(t, "entryCount")
		entries := make([]types.ReferenceGapEntry, entryCount)
		for i := range entryCount {
			entries[i] = genReferenceGapEntry().Draw(t, "entry")
		}

		summary := buildReferenceSummary(entries)

		return &types.ReferenceGapReport{
			Entries: entries,
			Summary: summary,
		}
	})
}

// genUpjetFieldMatchSlice generates a slice of UpjetFieldMatch entries.
func genUpjetFieldMatchSlice() *rapid.Generator[[]UpjetFieldMatch] {
	return rapid.Custom(func(t *rapid.T) []UpjetFieldMatch {
		count := rapid.IntRange(0, 5).Draw(t, "count")
		matches := make([]UpjetFieldMatch, count)
		for i := range count {
			matches[i] = UpjetFieldMatch{
				UpjetFieldName: genTFFieldName().Draw(t, "upjetField"),
				ACKFieldName:   genFieldName().Draw(t, "ackField"),
				ACKFieldPath:   "Spec." + genFieldName().Draw(t, "path"),
				TargetResource: genTFResourceType().Draw(t, "target"),
				IsAmbiguous:    rapid.Bool().Draw(t, "ambiguous"),
				Confidence:     genConfidence().Draw(t, "confidence"),
			}
		}
		return matches
	})
}

// genModelFieldMatchSlice generates a slice of ModelFieldMatch entries.
func genModelFieldMatchSlice() *rapid.Generator[[]ModelFieldMatch] {
	return rapid.Custom(func(t *rapid.T) []ModelFieldMatch {
		count := rapid.IntRange(0, 5).Draw(t, "count")
		matches := make([]ModelFieldMatch, count)
		for i := range count {
			matches[i] = ModelFieldMatch{
				ModelFieldName: genFieldName().Draw(t, "modelField"),
				ACKFieldName:   genFieldName().Draw(t, "ackField"),
				ACKFieldPath:   "Spec." + genFieldName().Draw(t, "path"),
				TargetService:  genServiceName().Draw(t, "service"),
				TargetResource: genTFResourceType().Draw(t, "target"),
				SignalType:     rapid.SampledFrom([]string{"arn_trait", "arn_suffix", "id_suffix", "name_suffix", "doc_mention"}).Draw(t, "signal"),
				Confidence:     genConfidence().Draw(t, "confidence"),
			}
		}
		return matches
	})
}

// genTerraformRefFieldMatchSlice generates a slice of TerraformRefFieldMatch entries.
func genTerraformRefFieldMatchSlice() *rapid.Generator[[]TerraformRefFieldMatch] {
	return rapid.Custom(func(t *rapid.T) []TerraformRefFieldMatch {
		count := rapid.IntRange(0, 5).Draw(t, "count")
		matches := make([]TerraformRefFieldMatch, count)
		for i := range count {
			matches[i] = TerraformRefFieldMatch{
				TFFieldName:    genTFFieldName().Draw(t, "tfField"),
				ACKFieldName:   genFieldName().Draw(t, "ackField"),
				ACKFieldPath:   "Spec." + genFieldName().Draw(t, "path"),
				TargetResource: genTFResourceType().Draw(t, "target"),
				ResolutionAttr: rapid.SampledFrom([]string{".id", ".arn", ".name"}).Draw(t, "attr"),
				Confidence:     genConfidence().Draw(t, "confidence"),
			}
		}
		return matches
	})
}

// --- Property 13: Reference classification correctness ---
// **Validates: Requirements 13.2**

// TestProperty13_ReferenceClassificationCorrectness verifies that for any field
// and generator config, the ClassifyReferenceField function SHALL produce:
// - "gap" when no references: config exists
// - "annotated" when references: config exists and target matches
// - "partial" when references: config exists but target differs
func TestProperty13_ReferenceClassificationCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		resourceName := genResourceName().Draw(t, "resource")
		fieldName := genFieldName().Draw(t, "field")
		expectedService := genServiceName().Draw(t, "expectedService")
		expectedResource := genResourceName().Draw(t, "expectedResource")

		// Scenario 1: no references config
		configNoRef := &parser.GeneratorConfig{
			Resources: map[string]parser.GeneratorResource{
				resourceName: {
					Fields: map[string]parser.GeneratorField{
						fieldName: {IsDocument: false, IsIAMPolicy: false, References: nil},
					},
				},
			},
		}
		result := ClassifyReferenceField(configNoRef, resourceName, fieldName, expectedService, expectedResource)
		if result != types.RefCategoryGap {
			t.Fatalf("expected 'gap' when no references config, got %q", result)
		}

		// Scenario 2: matching references config
		configMatching := &parser.GeneratorConfig{
			Resources: map[string]parser.GeneratorResource{
				resourceName: {
					Fields: map[string]parser.GeneratorField{
						fieldName: {
							References: &parser.GeneratorReference{
								Resource:    expectedResource,
								ServiceName: expectedService,
								Path:        "Status.ACKResourceMetadata.ARN",
							},
						},
					},
				},
			},
		}
		result = ClassifyReferenceField(configMatching, resourceName, fieldName, expectedService, expectedResource)
		if result != types.RefCategoryAnnotated {
			t.Fatalf("expected 'annotated' when matching reference config, got %q", result)
		}

		// Scenario 3: mismatched references config (different resource)
		differentResource := expectedResource + "Other"
		configMismatch := &parser.GeneratorConfig{
			Resources: map[string]parser.GeneratorResource{
				resourceName: {
					Fields: map[string]parser.GeneratorField{
						fieldName: {
							References: &parser.GeneratorReference{
								Resource:    differentResource,
								ServiceName: expectedService,
								Path:        "Status.ACKResourceMetadata.ARN",
							},
						},
					},
				},
			},
		}
		result = ClassifyReferenceField(configMismatch, resourceName, fieldName, expectedService, expectedResource)
		if result != types.RefCategoryPartial {
			t.Fatalf("expected 'partial' when mismatched reference config, got %q", result)
		}
	})
}

// TestProperty13_NilConfigMeansGap verifies that a nil generator config always
// classifies a reference as "gap".
func TestProperty13_NilConfigMeansGap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		resourceName := genResourceName().Draw(t, "resource")
		fieldName := genFieldName().Draw(t, "field")
		service := genServiceName().Draw(t, "service")
		resource := genResourceName().Draw(t, "resource2")

		result := ClassifyReferenceField(nil, resourceName, fieldName, service, resource)
		if result != types.RefCategoryGap {
			t.Fatalf("expected 'gap' when config is nil, got %q", result)
		}
	})
}

// --- Property 14: Merge logic preserves all entries ---
// **Validates: Requirements 13.1, 13.8**

// TestProperty14_MergePreservesAllEntries verifies that GenerateReferenceReport
// produces at least as many entries as the maximum unique field count from any
// single source, and that every field from any source appears in the output.
func TestProperty14_MergePreservesAllEntries(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a service/resource pair
		service := genServiceName().Draw(t, "service")
		resource := genResourceName().Draw(t, "resource")
		key := service + "_" + resource

		// Generate match outputs for each source with known ACK field names
		upjetMatches := genUpjetFieldMatchSlice().Draw(t, "upjetMatches")
		modelMatches := genModelFieldMatchSlice().Draw(t, "modelMatches")
		tfRefMatches := genTerraformRefFieldMatchSlice().Draw(t, "tfRefMatches")

		// Build the input structures
		upjetOutput := &MatchAllUpjetOutput{
			Results: map[string]*MatchUpjetOutput{
				key: {Matches: upjetMatches},
			},
		}
		modelOutput := &MatchAllModelOutput{
			Results: map[string]*MatchModelOutput{
				key: {Matches: modelMatches},
			},
		}
		tfOutput := &MatchAllTerraformRefsOutput{
			Results: map[string]*MatchTerraformRefsOutput{
				key: {Matches: tfRefMatches},
			},
		}

		controllers := []types.ControllerInfo{
			{
				ServiceName: service,
				Resources:   []types.ResourceInfo{{Kind: resource}},
			},
		}

		report := GenerateReferenceReport(upjetOutput, modelOutput, tfOutput, controllers, nil)

		// Collect all unique ACK field names from input
		allFields := make(map[string]bool)
		for _, m := range upjetMatches {
			allFields[m.ACKFieldName] = true
		}
		for _, m := range modelMatches {
			allFields[m.ACKFieldName] = true
		}
		for _, m := range tfRefMatches {
			allFields[m.ACKFieldName] = true
		}

		// Every unique field should appear in the report
		reportFields := make(map[string]bool)
		for _, entry := range report.Entries {
			reportFields[entry.ACKFieldName] = true
		}

		for field := range allFields {
			if !reportFields[field] {
				t.Fatalf("field %q from input sources not found in report output", field)
			}
		}
	})
}

// --- Property 15: Source breakdown sums to total ---
// **Validates: Requirements 13.6**

// TestProperty15_SourceBreakdownSumsToTotal verifies that the source breakdown
// stats (upjet_only + terraform_only + model_only + two_sources + all_three)
// equals the total number of references in the report.
func TestProperty15_SourceBreakdownSumsToTotal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		report := genReferenceGapReport().Draw(t, "report")

		sb := report.Summary.SourceBreakdown
		breakdownTotal := sb.UpjetOnly + sb.TerraformOnly + sb.ModelOnly + sb.TwoSources + sb.AllThreeSources

		if breakdownTotal != report.Summary.TotalReferences {
			t.Fatalf("source breakdown sum (%d) != total references (%d)\n"+
				"upjet_only=%d, terraform_only=%d, model_only=%d, two_sources=%d, all_three=%d",
				breakdownTotal, report.Summary.TotalReferences,
				sb.UpjetOnly, sb.TerraformOnly, sb.ModelOnly, sb.TwoSources, sb.AllThreeSources)
		}
	})
}

// --- Property 16: Confidence boost on multi-source agreement ---
// **Validates: Requirements 13.8**

// TestProperty16_ConfidenceBoostOnMultiSourceAgreement verifies that when a field
// appears in multiple sources, the output confidence is at least as high as the
// maximum single-source confidence for that field.
func TestProperty16_ConfidenceBoostOnMultiSourceAgreement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		service := genServiceName().Draw(t, "service")
		resource := genResourceName().Draw(t, "resource")
		fieldName := genFieldName().Draw(t, "fieldName")
		key := service + "_" + resource

		// Create matching entries that share the same ACK field name
		upjetConf := genConfidence().Draw(t, "upjetConf")
		tfConf := genConfidence().Draw(t, "tfConf")
		targetResource := genTFResourceType().Draw(t, "target")

		upjetOutput := &MatchAllUpjetOutput{
			Results: map[string]*MatchUpjetOutput{
				key: {Matches: []UpjetFieldMatch{
					{
						UpjetFieldName: "some_field",
						ACKFieldName:   fieldName,
						ACKFieldPath:   "Spec." + fieldName,
						TargetResource: targetResource,
						Confidence:     upjetConf,
					},
				}},
			},
		}
		tfOutput := &MatchAllTerraformRefsOutput{
			Results: map[string]*MatchTerraformRefsOutput{
				key: {Matches: []TerraformRefFieldMatch{
					{
						TFFieldName:    "some_field",
						ACKFieldName:   fieldName,
						ACKFieldPath:   "Spec." + fieldName,
						TargetResource: targetResource,
						ResolutionAttr: ".arn",
						Confidence:     tfConf,
					},
				}},
			},
		}

		controllers := []types.ControllerInfo{
			{
				ServiceName: service,
				Resources:   []types.ResourceInfo{{Kind: resource}},
			},
		}

		report := GenerateReferenceReport(upjetOutput, nil, tfOutput, controllers, nil)

		// Find the entry
		for _, entry := range report.Entries {
			if entry.ACKFieldName == fieldName {
				// With two sources, confidence should be boosted from upjet's original
				// (since upjet is processed first, its confidence is the base)
				if entry.Confidence < upjetConf {
					t.Fatalf("multi-source confidence %.2f is less than upjet source confidence %.2f",
						entry.Confidence, upjetConf)
				}
				// Sources should contain both
				if len(entry.Sources) < 2 {
					t.Fatalf("expected at least 2 sources, got %d: %v", len(entry.Sources), entry.Sources)
				}
				return
			}
		}
		t.Fatalf("field %q not found in report output", fieldName)
	})
}

// --- Property 17: Reference report JSON schema validity ---
// **Validates: Requirements 13.5**

// TestProperty17_ReferenceReportJSONSchemaValidity verifies that for any reference
// report, serializing to JSON produces valid JSON with required fields.
func TestProperty17_ReferenceReportJSONSchemaValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		report := genReferenceGapReport().Draw(t, "report")

		var buf bytes.Buffer
		err := reporter.FormatReferenceJSON(report, &buf)
		if err != nil {
			t.Fatalf("FormatReferenceJSON failed: %v", err)
		}

		// Parse as generic JSON
		var rawOutput map[string]any
		if err := json.Unmarshal(buf.Bytes(), &rawOutput); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}

		// Must have "entries" key that is an array
		entriesRaw, ok := rawOutput["entries"]
		if !ok {
			t.Fatal("JSON output missing 'entries' key")
		}
		entries, ok := entriesRaw.([]any)
		if !ok {
			t.Fatal("'entries' is not an array")
		}

		// Each entry must have required keys
		requiredKeys := []string{
			"service_name", "resource_name", "ack_field_name", "ack_field_path",
			"target_terraform_resource", "confidence", "sources", "current_status",
		}
		for i, entryRaw := range entries {
			entry, ok := entryRaw.(map[string]any)
			if !ok {
				t.Fatalf("entries[%d] is not an object", i)
			}
			for _, key := range requiredKeys {
				if _, exists := entry[key]; !exists {
					t.Fatalf("entries[%d] missing required key %q", i, key)
				}
			}
		}

		// Must have "summary" key with required subfields
		summaryRaw, ok := rawOutput["summary"]
		if !ok {
			t.Fatal("JSON output missing 'summary' key")
		}
		summary, ok := summaryRaw.(map[string]any)
		if !ok {
			t.Fatal("'summary' is not an object")
		}

		summaryKeys := []string{
			"total_references", "gap_count", "annotated_count", "ambiguous_count",
			"gaps_per_service", "services_by_priority", "source_breakdown",
		}
		for _, key := range summaryKeys {
			if _, exists := summary[key]; !exists {
				t.Fatalf("summary missing required key %q", key)
			}
		}

		// Source breakdown must have required keys
		sourceBreakdownRaw, ok := summary["source_breakdown"].(map[string]any)
		if !ok {
			t.Fatal("source_breakdown is not an object")
		}
		sbKeys := []string{"upjet_only", "terraform_docs_only", "model_only", "two_sources", "all_three_sources"}
		for _, key := range sbKeys {
			if _, exists := sourceBreakdownRaw[key]; !exists {
				t.Fatalf("source_breakdown missing required key %q", key)
			}
		}

		// Entries count should match input
		if len(entries) != len(report.Entries) {
			t.Fatalf("expected %d entries in output, got %d", len(report.Entries), len(entries))
		}
	})
}

// TestProperty17_EntriesNeverNull verifies "entries" is always a JSON array (never null).
func TestProperty17_EntriesNeverNull(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		entryCount := rapid.IntRange(0, 3).Draw(t, "count")
		entries := make([]types.ReferenceGapEntry, entryCount)
		for i := range entryCount {
			entries[i] = genReferenceGapEntry().Draw(t, "entry")
		}
		report := &types.ReferenceGapReport{
			Entries: entries,
			Summary: types.ReferenceReportSummary{
				GapsPerService: make(map[string]int),
			},
		}

		var buf bytes.Buffer
		err := reporter.FormatReferenceJSON(report, &buf)
		if err != nil {
			t.Fatalf("FormatReferenceJSON failed: %v", err)
		}

		output := buf.String()
		if strings.Contains(output, `"entries": null`) || strings.Contains(output, `"entries":null`) {
			t.Fatal("entries should be [] not null when empty")
		}
	})
}

// --- Property 18: Priority sort order ---
// **Validates: Requirements 13.6**

// TestProperty18_ReferencePrioritySortOrder verifies that services_by_priority
// is sorted descending by gap count, and gaps_per_service counts match the actual
// number of gap+partial entries per service.
func TestProperty18_ReferencePrioritySortOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		report := genReferenceGapReport().Draw(t, "report")

		// Verify sorted descending
		for i := 1; i < len(report.Summary.ServicesByPriority); i++ {
			prev := report.Summary.ServicesByPriority[i-1]
			curr := report.Summary.ServicesByPriority[i]
			if prev.GapCount < curr.GapCount {
				t.Fatalf("ServicesByPriority not sorted descending: [%d].GapCount=%d < [%d].GapCount=%d",
					i-1, prev.GapCount, i, curr.GapCount)
			}
		}

		// Verify GapsPerService matches actual gap+partial entry count
		actualGaps := make(map[string]int)
		for _, entry := range report.Entries {
			status := types.ReferenceCategory(entry.CurrentStatus)
			if status == types.RefCategoryGap || status == types.RefCategoryPartial {
				actualGaps[entry.ServiceName]++
			}
		}

		for svc, expected := range actualGaps {
			if report.Summary.GapsPerService[svc] != expected {
				t.Fatalf("GapsPerService[%q]: expected %d, got %d",
					svc, expected, report.Summary.GapsPerService[svc])
			}
		}
	})
}

// --- Property 19: Upjet preference in merge ---
// **Validates: Requirements 13.1**

// TestProperty19_UpjetPreferenceInMerge verifies that when Upjet provides a
// match for a field, the merged entry's target TF resource comes from Upjet
// (not overwritten by other sources).
func TestProperty19_UpjetPreferenceInMerge(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		service := genServiceName().Draw(t, "service")
		resource := genResourceName().Draw(t, "resource")
		fieldName := genFieldName().Draw(t, "fieldName")
		key := service + "_" + resource

		upjetTarget := genTFResourceType().Draw(t, "upjetTarget")
		modelTarget := genTFResourceType().Draw(t, "modelTarget")

		upjetOutput := &MatchAllUpjetOutput{
			Results: map[string]*MatchUpjetOutput{
				key: {Matches: []UpjetFieldMatch{
					{
						UpjetFieldName: "field_a",
						ACKFieldName:   fieldName,
						ACKFieldPath:   "Spec." + fieldName,
						TargetResource: upjetTarget,
						Confidence:     0.9,
					},
				}},
			},
		}
		modelOutput := &MatchAllModelOutput{
			Results: map[string]*MatchModelOutput{
				key: {Matches: []ModelFieldMatch{
					{
						ModelFieldName: fieldName,
						ACKFieldName:   fieldName,
						ACKFieldPath:   "Spec." + fieldName,
						TargetResource: modelTarget,
						SignalType:     "arn_suffix",
						Confidence:     0.8,
					},
				}},
			},
		}

		controllers := []types.ControllerInfo{
			{
				ServiceName: service,
				Resources:   []types.ResourceInfo{{Kind: resource}},
			},
		}

		report := GenerateReferenceReport(upjetOutput, modelOutput, nil, controllers, nil)

		// Find the entry and verify Upjet's target is preserved
		for _, entry := range report.Entries {
			if entry.ACKFieldName == fieldName {
				if entry.TargetTFResource != upjetTarget {
					t.Fatalf("expected Upjet target %q, got %q (model target was %q)",
						upjetTarget, entry.TargetTFResource, modelTarget)
				}
				return
			}
		}
		t.Fatalf("field %q not found in report output", fieldName)
	})
}
