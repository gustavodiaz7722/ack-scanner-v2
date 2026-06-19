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

// --- Generators ---

// genServiceName generates a lowercase service name (2-10 lowercase letters).
func genServiceName() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		length := rapid.IntRange(2, 10).Draw(t, "length")
		var sb strings.Builder
		for i := range length {
			ch := rapid.ByteRange('a', 'z').Draw(t, "char")
			sb.WriteByte(ch)
			_ = i
		}
		return sb.String()
	})
}

// genResourceName generates a CamelCase resource name (e.g., "Role", "Bucket").
func genResourceName() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		upper := rapid.ByteRange('A', 'Z').Draw(t, "upper")
		length := rapid.IntRange(2, 8).Draw(t, "length")
		var sb strings.Builder
		sb.WriteByte(upper)
		for i := range length {
			lower := rapid.ByteRange('a', 'z').Draw(t, "lower")
			sb.WriteByte(lower)
			_ = i
		}
		return sb.String()
	})
}

// genFieldName generates a CamelCase field name.
func genFieldName() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		wordCount := rapid.IntRange(1, 3).Draw(t, "wordCount")
		var sb strings.Builder
		for i := range wordCount {
			upper := rapid.ByteRange('A', 'Z').Draw(t, "upper")
			sb.WriteByte(upper)
			lowerCount := rapid.IntRange(2, 6).Draw(t, "lowerCount")
			for j := range lowerCount {
				lower := rapid.ByteRange('a', 'z').Draw(t, "lower")
				sb.WriteByte(lower)
				_ = j
			}
			_ = i
		}
		return sb.String()
	})
}

// genTFFieldName generates a snake_case TF field name.
func genTFFieldName() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		wordCount := rapid.IntRange(1, 3).Draw(t, "wordCount")
		var sb strings.Builder
		for i := range wordCount {
			if i > 0 {
				sb.WriteByte('_')
			}
			wordLen := rapid.IntRange(3, 8).Draw(t, "wordLen")
			for j := range wordLen {
				ch := rapid.ByteRange('a', 'z').Draw(t, "char")
				sb.WriteByte(ch)
				_ = j
			}
		}
		return sb.String()
	})
}

// genAnnotationType generates a valid AnnotationType.
func genAnnotationType() *rapid.Generator[types.AnnotationType] {
	return rapid.SampledFrom([]types.AnnotationType{
		types.AnnotationDocument,
		types.AnnotationIAMPolicy,
	})
}

// genGapReportEntry generates a random GapReportEntry.
func genGapReportEntry() *rapid.Generator[types.GapReportEntry] {
	return rapid.Custom(func(t *rapid.T) types.GapReportEntry {
		service := genServiceName().Draw(t, "service")
		resource := genResourceName().Draw(t, "resource")
		ackField := genFieldName().Draw(t, "ackField")
		tfField := genTFFieldName().Draw(t, "tfField")
		annotation := genAnnotationType().Draw(t, "annotation")
		status := rapid.SampledFrom([]types.Category{
			types.CategoryGap,
			types.CategoryAnnotated,
			types.CategoryIncorrect,
		}).Draw(t, "status")

		return types.GapReportEntry{
			ServiceName:           service,
			ResourceName:          resource,
			ACKFieldName:          ackField,
			ACKFieldPath:          "Spec." + ackField,
			TFFieldName:           tfField,
			RecommendedAnnotation: string(annotation),
			CurrentStatus:         string(status),
		}
	})
}

// genGapReport generates a random GapReport with consistent summary.
func genGapReport() *rapid.Generator[*types.GapReport] {
	return rapid.Custom(func(t *rapid.T) *types.GapReport {
		entryCount := rapid.IntRange(0, 30).Draw(t, "entryCount")
		entries := make([]types.GapReportEntry, entryCount)
		for i := range entryCount {
			entries[i] = genGapReportEntry().Draw(t, "entry")
		}

		// Build a consistent summary from the entries
		summary := types.GapReportSummary{
			TotalMatches:   len(entries),
			GapsPerService: make(map[string]int),
		}

		for _, entry := range entries {
			switch types.Category(entry.CurrentStatus) {
			case types.CategoryGap:
				summary.GapCount++
				summary.GapsPerService[entry.ServiceName]++
			case types.CategoryAnnotated:
				summary.AnnotatedCount++
			case types.CategoryIncorrect:
				summary.IncorrectCount++
			}
		}

		// Build sorted ServicesByPriority
		for svc, count := range summary.GapsPerService {
			summary.ServicesByPriority = append(summary.ServicesByPriority, types.ServicePriority{
				ServiceName: svc,
				GapCount:    count,
			})
		}
		// Sort descending by gap count
		for i := range summary.ServicesByPriority {
			for j := i + 1; j < len(summary.ServicesByPriority); j++ {
				if summary.ServicesByPriority[i].GapCount < summary.ServicesByPriority[j].GapCount {
					summary.ServicesByPriority[i], summary.ServicesByPriority[j] =
						summary.ServicesByPriority[j], summary.ServicesByPriority[i]
				}
			}
		}

		return &types.GapReport{
			Entries: entries,
			Summary: summary,
		}
	})
}

// --- Property 10: Field classification correctness ---
// **Validates: Requirements 6.2**

// TestProperty10_FieldClassificationCorrectness verifies that for any matched field
// with a known recommended annotation type and a known current annotation status,
// the classification function SHALL produce:
// - "gap" when no annotation exists
// - "annotated" when the correct annotation exists
// - "incorrect" when a different annotation type exists
func TestProperty10_FieldClassificationCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		resourceName := genResourceName().Draw(t, "resource")
		fieldName := genFieldName().Draw(t, "field")
		recommended := genAnnotationType().Draw(t, "recommended")

		// Scenario: no annotation
		configNoAnnotation := &parser.GeneratorConfig{
			Resources: map[string]parser.GeneratorResource{
				resourceName: {
					Fields: map[string]parser.GeneratorField{
						fieldName: {IsDocument: false, IsIAMPolicy: false},
					},
				},
			},
		}
		result := ClassifyField(configNoAnnotation, resourceName, fieldName, recommended)
		if result != types.CategoryGap {
			t.Fatalf("expected 'gap' when no annotation exists, got %q", result)
		}

		// Scenario: correct annotation exists
		configCorrect := &parser.GeneratorConfig{
			Resources: map[string]parser.GeneratorResource{
				resourceName: {
					Fields: map[string]parser.GeneratorField{
						fieldName: {
							IsDocument:  recommended == types.AnnotationDocument,
							IsIAMPolicy: recommended == types.AnnotationIAMPolicy,
						},
					},
				},
			},
		}
		result = ClassifyField(configCorrect, resourceName, fieldName, recommended)
		if result != types.CategoryAnnotated {
			t.Fatalf("expected 'annotated' when correct annotation exists, got %q (recommended=%s)", result, recommended)
		}

		// Scenario: wrong annotation type
		configIncorrect := &parser.GeneratorConfig{
			Resources: map[string]parser.GeneratorResource{
				resourceName: {
					Fields: map[string]parser.GeneratorField{
						fieldName: {
							// Set the opposite annotation
							IsDocument:  recommended == types.AnnotationIAMPolicy,
							IsIAMPolicy: recommended == types.AnnotationDocument,
						},
					},
				},
			},
		}
		result = ClassifyField(configIncorrect, resourceName, fieldName, recommended)
		if result != types.CategoryIncorrect {
			t.Fatalf("expected 'incorrect' when wrong annotation exists, got %q (recommended=%s)", result, recommended)
		}
	})
}

// TestProperty10_NilConfigMeansGap verifies that a nil generator config always classifies as gap.
func TestProperty10_NilConfigMeansGap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		resourceName := genResourceName().Draw(t, "resource")
		fieldName := genFieldName().Draw(t, "field")
		recommended := genAnnotationType().Draw(t, "recommended")

		result := ClassifyField(nil, resourceName, fieldName, recommended)
		if result != types.CategoryGap {
			t.Fatalf("expected 'gap' when config is nil, got %q", result)
		}
	})
}

// TestProperty10_MissingResourceMeansGap verifies that a missing resource in the config classifies as gap.
func TestProperty10_MissingResourceMeansGap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		resourceName := genResourceName().Draw(t, "resource")
		fieldName := genFieldName().Draw(t, "field")
		recommended := genAnnotationType().Draw(t, "recommended")

		// Config with a different resource
		config := &parser.GeneratorConfig{
			Resources: map[string]parser.GeneratorResource{
				"OtherResource": {
					Fields: map[string]parser.GeneratorField{
						fieldName: {IsDocument: true},
					},
				},
			},
		}

		result := ClassifyField(config, resourceName, fieldName, recommended)
		if result != types.CategoryGap {
			t.Fatalf("expected 'gap' when resource not in config, got %q", result)
		}
	})
}

// --- Property 11: Gap report JSON schema validity ---
// **Validates: Requirements 6.3, 6.5**

// TestProperty11_GapReportJSONSchemaValidity verifies that for any gap report data,
// serializing to JSON SHALL produce valid JSON containing an `entries` array
// (each with service_name, resource_name, ack_field_name, terraform_field_name,
// recommended_annotation, current_status) and a `summary` object (with total_matches,
// gap_count, gaps_per_service, services_by_priority).
func TestProperty11_GapReportJSONSchemaValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		report := genGapReport().Draw(t, "report")

		var buf bytes.Buffer
		err := reporter.FormatJSON(report, &buf)
		if err != nil {
			t.Fatalf("FormatJSON failed: %v", err)
		}

		// Parse as generic JSON to verify schema structure
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
			"service_name", "resource_name", "ack_field_name",
			"terraform_field_name", "recommended_annotation", "current_status",
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

		// Must have "summary" key that is an object
		summaryRaw, ok := rawOutput["summary"]
		if !ok {
			t.Fatal("JSON output missing 'summary' key")
		}
		summary, ok := summaryRaw.(map[string]any)
		if !ok {
			t.Fatal("'summary' is not an object")
		}

		// Summary must have required keys
		summaryKeys := []string{
			"total_matches", "gap_count", "gaps_per_service", "services_by_priority",
		}
		for _, key := range summaryKeys {
			if _, exists := summary[key]; !exists {
				t.Fatalf("summary missing required key %q", key)
			}
		}

		// Entries length should match the input
		if len(entries) != len(report.Entries) {
			t.Fatalf("expected %d entries in output, got %d", len(report.Entries), len(entries))
		}
	})
}

// TestProperty11_EntriesIsAlwaysArray verifies that "entries" is always a JSON array
// (never null) even when there are zero entries.
func TestProperty11_EntriesIsAlwaysArray(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		entryCount := rapid.IntRange(0, 5).Draw(t, "entryCount")
		entries := make([]types.GapReportEntry, entryCount)
		for i := range entryCount {
			entries[i] = genGapReportEntry().Draw(t, "entry")
		}

		report := &types.GapReport{
			Entries: entries,
			Summary: types.GapReportSummary{
				GapsPerService: make(map[string]int),
			},
		}

		var buf bytes.Buffer
		err := reporter.FormatJSON(report, &buf)
		if err != nil {
			t.Fatalf("FormatJSON failed: %v", err)
		}

		output := buf.String()
		if strings.Contains(output, `"entries": null`) || strings.Contains(output, `"entries":null`) {
			t.Fatal("entries should be [] not null when empty")
		}

		var rawOutput map[string]any
		if err := json.Unmarshal(buf.Bytes(), &rawOutput); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		entriesRaw := rawOutput["entries"]
		if _, ok := entriesRaw.([]any); !ok {
			t.Fatal("'entries' is not an array")
		}
	})
}

// --- Property 12: Report priority sort order ---
// **Validates: Requirements 6.6**

// TestProperty12_ReportPrioritySortOrder verifies that services_by_priority SHALL be
// sorted in descending order by gap count, and gaps_per_service counts SHALL equal
// the actual number of gap entries per service in the report.
func TestProperty12_ReportPrioritySortOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		entryCount := rapid.IntRange(0, 30).Draw(t, "entryCount")
		entries := make([]types.GapReportEntry, entryCount)
		for i := range entryCount {
			entries[i] = genGapReportEntry().Draw(t, "entry")
		}

		// Use GenerateReport to build the report from match results
		// Build matchResults and controllers from entries
		matchResults := make(map[string]*MatchFieldsOutput)
		controllers := []types.ControllerInfo{}
		genConfigs := make(map[string]*parser.GeneratorConfig)

		// Group entries into match results per service_resource key
		serviceResources := make(map[string]map[string][]types.FieldMatch) // service -> kind -> matches
		for _, entry := range entries {
			if _, ok := serviceResources[entry.ServiceName]; !ok {
				serviceResources[entry.ServiceName] = make(map[string][]types.FieldMatch)
			}
			serviceResources[entry.ServiceName][entry.ResourceName] = append(
				serviceResources[entry.ServiceName][entry.ResourceName],
				types.FieldMatch{
					TFFieldName:  entry.TFFieldName,
					ACKFieldName: entry.ACKFieldName,
					ACKFieldPath: entry.ACKFieldPath,
					Confidence:   0.9,
				},
			)
		}

		for service, resources := range serviceResources {
			ctrl := types.ControllerInfo{ServiceName: service}
			for kind, matches := range resources {
				ctrl.Resources = append(ctrl.Resources, types.ResourceInfo{Kind: kind})
				key := service + "_" + kind
				matchResults[key] = &MatchFieldsOutput{Matches: matches}
			}
			controllers = append(controllers, ctrl)
			// No generator config → all fields will be "gap"
			genConfigs[service] = nil
		}

		report := GenerateReport(matchResults, controllers, genConfigs, nil)

		// Verify: services_by_priority is sorted descending by gap count
		for i := 1; i < len(report.Summary.ServicesByPriority); i++ {
			prev := report.Summary.ServicesByPriority[i-1]
			curr := report.Summary.ServicesByPriority[i]
			if prev.GapCount < curr.GapCount {
				t.Fatalf("ServicesByPriority not sorted descending: [%d].GapCount=%d < [%d].GapCount=%d",
					i-1, prev.GapCount, i, curr.GapCount)
			}
		}

		// Verify: gaps_per_service counts match actual gap entries per service
		actualGapsPerService := make(map[string]int)
		for _, entry := range report.Entries {
			if entry.CurrentStatus == string(types.CategoryGap) {
				actualGapsPerService[entry.ServiceName]++
			}
		}

		for svc, expectedCount := range actualGapsPerService {
			if report.Summary.GapsPerService[svc] != expectedCount {
				t.Fatalf("GapsPerService[%q]: expected %d, got %d",
					svc, expectedCount, report.Summary.GapsPerService[svc])
			}
		}

		// Also check no extra services in GapsPerService
		for svc, count := range report.Summary.GapsPerService {
			if actualGapsPerService[svc] != count {
				t.Fatalf("GapsPerService[%q]: unexpected count %d (expected %d)",
					svc, count, actualGapsPerService[svc])
			}
		}
	})
}

// TestProperty12_SummaryCountsConsistency verifies that category counts sum to total.
func TestProperty12_SummaryCountsConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		report := genGapReport().Draw(t, "report")

		// Verify that summary counts are consistent
		categorySum := report.Summary.GapCount + report.Summary.AnnotatedCount + report.Summary.IncorrectCount
		if categorySum != report.Summary.TotalMatches {
			t.Fatalf("category counts (%d+%d+%d=%d) != TotalMatches (%d)",
				report.Summary.GapCount, report.Summary.AnnotatedCount,
				report.Summary.IncorrectCount, categorySum, report.Summary.TotalMatches)
		}

		// Verify services_by_priority is sorted descending
		for i := 1; i < len(report.Summary.ServicesByPriority); i++ {
			prev := report.Summary.ServicesByPriority[i-1]
			curr := report.Summary.ServicesByPriority[i]
			if prev.GapCount < curr.GapCount {
				t.Fatalf("ServicesByPriority not sorted descending: [%d].GapCount=%d < [%d].GapCount=%d",
					i-1, prev.GapCount, i, curr.GapCount)
			}
		}
	})
}
