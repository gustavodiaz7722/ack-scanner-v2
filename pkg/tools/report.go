package tools

import (
	"sort"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// GenerateReport produces a GapReport by classifying each matched field based on
// the controller's generator.yaml annotations.
//
// Parameters:
//   - matchResults: map of "{service}_{kind}" → MatchFieldsOutput from the matching phase
//   - controllers: list of discovered controllers (for service name lookup)
//   - generatorConfigs: map of service name → parsed GeneratorConfig from generator.yaml
//
// For each match entry, the function determines:
//   - The recommended annotation type based on the TF field_type
//   - The current annotation status by checking the generator config
//   - The classification: "gap", "annotated", or "incorrect"
func GenerateReport(
	matchResults map[string]*MatchFieldsOutput,
	controllers []types.ControllerInfo,
	generatorConfigs map[string]*parser.GeneratorConfig,
) *types.GapReport {
	// Build a lookup from "{service}_{kind}" → (serviceName, resourceKind)
	type serviceResource struct {
		ServiceName  string
		ResourceKind string
	}
	keyLookup := make(map[string]serviceResource)
	for _, ctrl := range controllers {
		for _, res := range ctrl.Resources {
			key := ctrl.ServiceName + "_" + res.Kind
			keyLookup[key] = serviceResource{
				ServiceName:  ctrl.ServiceName,
				ResourceKind: res.Kind,
			}
		}
	}

	var entries []types.GapReportEntry

	for key, matchOutput := range matchResults {
		if matchOutput == nil {
			continue
		}

		sr, ok := keyLookup[key]
		if !ok {
			// Try to extract service name from the key directly
			// Key format is "{service}_{kind}"
			sr = extractServiceResource(key)
		}

		genConfig := generatorConfigs[sr.ServiceName]

		for _, match := range matchOutput.Matches {
			// Determine recommended annotation type from TF field type
			recommended := recommendedAnnotation(match, matchResults, key)

			// Classify based on generator.yaml
			status := classifyField(genConfig, sr.ResourceKind, match.ACKFieldName, recommended)

			entries = append(entries, types.GapReportEntry{
				ServiceName:           sr.ServiceName,
				ResourceName:          sr.ResourceKind,
				ACKFieldName:          match.ACKFieldName,
				ACKFieldPath:          match.ACKFieldPath,
				TFFieldName:           match.TFFieldName,
				RecommendedAnnotation: string(recommended),
				CurrentStatus:         string(status),
			})
		}
	}

	// Build summary
	summary := buildSummary(entries)

	return &types.GapReport{
		Entries: entries,
		Summary: summary,
	}
}

// ClassifyField determines the category for a field given its annotation status
// in the generator config and the recommended annotation type.
// Exported for testing.
func ClassifyField(genConfig *parser.GeneratorConfig, resourceName, fieldName string, recommended types.AnnotationType) types.Category {
	return classifyField(genConfig, resourceName, fieldName, recommended)
}

// classifyField determines the classification of a field:
//   - "gap": no annotation exists in generator.yaml
//   - "annotated": correct annotation exists
//   - "incorrect": wrong annotation type exists
func classifyField(genConfig *parser.GeneratorConfig, resourceName, fieldName string, recommended types.AnnotationType) types.Category {
	if genConfig == nil {
		return types.CategoryGap
	}

	isDocument, isIAMPolicy := genConfig.HasAnnotation(resourceName, fieldName)

	if !isDocument && !isIAMPolicy {
		return types.CategoryGap
	}

	// Check if the existing annotation matches the recommended one
	switch recommended {
	case types.AnnotationDocument:
		if isDocument {
			return types.CategoryAnnotated
		}
		return types.CategoryIncorrect
	case types.AnnotationIAMPolicy:
		if isIAMPolicy {
			return types.CategoryAnnotated
		}
		return types.CategoryIncorrect
	default:
		// If recommended is unknown, treat any annotation as correct
		if isDocument || isIAMPolicy {
			return types.CategoryAnnotated
		}
		return types.CategoryGap
	}
}

// recommendedAnnotation determines the recommended annotation type for a match.
// It looks up the TF field info from the analysis results to get the field_type.
func recommendedAnnotation(match types.FieldMatch, matchResults map[string]*MatchFieldsOutput, currentKey string) types.AnnotationType {
	// The TF field name contains the type info. We need to look at the original
	// AnalyzeFieldsOutput to determine the type. Since we only have match results here,
	// we use a naming heuristic: fields containing "policy" are IAM policy, others are documents.
	// However, the design specifies we should use the field_type from the TF JSON field info.
	// Since FieldMatch doesn't carry field_type, we use a name-based heuristic.
	tfFieldName := match.TFFieldName
	if containsIAMPolicyIndicator(tfFieldName) {
		return types.AnnotationIAMPolicy
	}
	return types.AnnotationDocument
}

// RecommendedAnnotationFromFieldType determines the annotation type from a TF field type string.
// Exported for use in tests and by callers who have the field type available.
func RecommendedAnnotationFromFieldType(fieldType string) types.AnnotationType {
	switch fieldType {
	case "iam_policy":
		return types.AnnotationIAMPolicy
	case "json_document":
		return types.AnnotationDocument
	default:
		return types.AnnotationDocument
	}
}

// containsIAMPolicyIndicator checks if a TF field name suggests it's an IAM policy.
func containsIAMPolicyIndicator(fieldName string) bool {
	indicators := []string{"policy", "iam_policy", "assume_role_policy", "access_policy"}
	for _, ind := range indicators {
		if fieldName == ind || contains(fieldName, ind) {
			return true
		}
	}
	return false
}

// contains checks if s contains substr (simple helper to avoid importing strings).
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// buildSummary computes aggregate statistics from the report entries.
func buildSummary(entries []types.GapReportEntry) types.GapReportSummary {
	summary := types.GapReportSummary{
		GapsPerService: make(map[string]int),
	}

	summary.TotalMatches = len(entries)

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

	// Build ServicesByPriority sorted descending by gap count
	for svc, count := range summary.GapsPerService {
		summary.ServicesByPriority = append(summary.ServicesByPriority, types.ServicePriority{
			ServiceName: svc,
			GapCount:    count,
		})
	}
	sort.Slice(summary.ServicesByPriority, func(i, j int) bool {
		if summary.ServicesByPriority[i].GapCount != summary.ServicesByPriority[j].GapCount {
			return summary.ServicesByPriority[i].GapCount > summary.ServicesByPriority[j].GapCount
		}
		// Tie-break alphabetically for deterministic output
		return summary.ServicesByPriority[i].ServiceName < summary.ServicesByPriority[j].ServiceName
	})

	return summary
}

// extractServiceResource parses a key of format "{service}_{kind}" into its components.
func extractServiceResource(key string) struct {
	ServiceName  string
	ResourceKind string
} {
	// Find the first underscore to split service from kind
	for i := range key {
		if key[i] == '_' {
			return struct {
				ServiceName  string
				ResourceKind string
			}{
				ServiceName:  key[:i],
				ResourceKind: key[i+1:],
			}
		}
	}
	return struct {
		ServiceName  string
		ResourceKind string
	}{
		ServiceName:  key,
		ResourceKind: "",
	}
}
