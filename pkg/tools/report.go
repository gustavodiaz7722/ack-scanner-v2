package tools

import (
	"sort"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/ignore"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
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
//   - ignoreList: optional ignore list to exclude known false positives (may be nil)
//
// For each match entry, the function determines:
//   - The recommended annotation type based on the TF field_type
//   - The current annotation status by checking the generator config
//   - The classification: "gap", "annotated", or "incorrect"
//
// Entries classified as "incorrect" are excluded from the report since they
// represent cases where the scanner's recommendation disagrees with the
// controller's existing annotation (which is typically correct).
// Entries matching the ignore list are also excluded.
func GenerateReport(
	matchResults map[string]*MatchFieldsOutput,
	controllers []types.ControllerInfo,
	generatorConfigs map[string]*parser.GeneratorConfig,
	ignoreList *ignore.List,
	log ...*logger.Logger,
) *types.GapReport {
	l := resolveLogger(log)

	l.Info("report: generating gap report from %d match results, %d controllers, %d generator configs",
		len(matchResults), len(controllers), len(generatorConfigs))
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
	var ignoredCount int

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
			// Check ignore list before processing
			if ignoreList != nil && ignoreList.IsIgnored(sr.ServiceName, sr.ResourceKind, match.ACKFieldName) {
				ignoredCount++
				continue
			}

			// Determine recommended annotation type from TF field type
			recommended := recommendedAnnotation(match, matchResults, key)

			// Classify based on generator.yaml
			status := classifyField(genConfig, sr.ResourceKind, match.ACKFieldName, recommended)

			// Skip "incorrect" entries — these are cases where the scanner's
			// recommendation disagrees with the controller's existing annotation,
			// which is typically the correct one.
			if status == types.CategoryIncorrect {
				continue
			}

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

	l.Info("report: %d total entries — %d gaps, %d annotated, %d incorrect",
		summary.TotalMatches, summary.GapCount, summary.AnnotatedCount, summary.IncorrectCount)

	if ignoredCount > 0 {
		l.Info("report: %d entries excluded by ignore list", ignoredCount)
	}

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
// Only matches fields that are specifically IAM policy documents, not fields that
// merely contain "policy" in their name (e.g., redrivePolicy is a JSON document, not IAM).
func containsIAMPolicyIndicator(fieldName string) bool {
	// Exact matches for known IAM policy fields
	iamPolicyFields := []string{
		"policy",
		"assume_role_policy",
		"access_policy",
		"key_policy",
		"resource_policy",
		"trust_policy",
		"bucket_policy",
		"queue_policy",
		"topic_policy",
		"repository_policy",
	}
	for _, field := range iamPolicyFields {
		if fieldName == field {
			return true
		}
	}
	// Suffix pattern: fields ending in _policy are typically IAM resource policies
	// (e.g., repository_policy, function_policy) but exclude known non-IAM patterns
	if strings.HasSuffix(fieldName, "_policy") {
		// Exclude known non-IAM policy fields (these are JSON documents, not IAM)
		nonIAMPolicies := []string{
			"redrive_policy",
			"redrive_allow_policy",
			"lifecycle_policy",
			"scaling_policy",
			"routing_policy",
		}
		for _, excluded := range nonIAMPolicies {
			if fieldName == excluded {
				return false
			}
		}
		return true
	}
	// Prefix pattern: iam_* fields containing policy
	if strings.HasPrefix(fieldName, "iam_") && strings.Contains(fieldName, "policy") {
		return true
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
