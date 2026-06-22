package tools

import (
	"sort"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// mergedEntry is an intermediate structure used during the three-source merge.
// It aggregates match data for a single (service, resource, ACK field) tuple.
type mergedEntry struct {
	ServiceName  string
	ResourceName string
	ACKFieldName string
	ACKFieldPath string

	// From Upjet (highest priority)
	TargetTFResource string
	IsAmbiguous      bool

	// From Terraform docs
	ResolutionAttr string

	// Aggregated
	Confidence float64
	Sources    []string
}

// GenerateReferenceReport produces a ReferenceGapReport by merging match results
// from all three sources (Upjet, Terraform docs, API models), translating TF
// resource types to ACK service/resource pairs, and classifying each entry against
// existing generator.yaml references.
//
// Merge priority:
//  1. Upjet matches (highest confidence, human-verified)
//  2. Terraform doc matches (supplement + confirmation)
//  3. API model matches (broadest coverage, lower confidence)
//
// When multiple sources agree on a field, confidence is boosted and all sources
// are listed.
//
// Fields that already have references: configuration in generator.yaml are also
// included in the report as "annotated" entries, providing a complete picture.
func GenerateReferenceReport(
	upjetMatches *MatchAllUpjetOutput,
	modelMatches *MatchAllModelOutput,
	tfRefMatches *MatchAllTerraformRefsOutput,
	controllers []types.ControllerInfo,
	generatorConfigs map[string]*parser.GeneratorConfig,
	log ...*logger.Logger,
) *types.ReferenceGapReport {
	l := resolveLogger(log)

	l.Info("report_references: generating reference gap report")

	// Build controller lookup: "{service}_{kind}" → (serviceName, kind)
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

	// Build TF resource → ACK service/resource translation table from controllers
	tfToACK := BuildTFToACKLookup(controllers)

	// Merge all three sources into a consolidated map keyed by "service|resource|field"
	merged := make(map[string]*mergedEntry)

	// Step 1: Add Upjet matches (highest confidence)
	if upjetMatches != nil {
		for key, result := range upjetMatches.Results {
			if result == nil {
				continue
			}
			sr, ok := keyLookup[key]
			if !ok {
				sr = extractServiceResourceFromKey(key)
			}
			for _, match := range result.Matches {
				entryKey := sr.ServiceName + "|" + sr.ResourceKind + "|" + match.ACKFieldName
				merged[entryKey] = &mergedEntry{
					ServiceName:      sr.ServiceName,
					ResourceName:     sr.ResourceKind,
					ACKFieldName:     match.ACKFieldName,
					ACKFieldPath:     match.ACKFieldPath,
					TargetTFResource: match.TargetResource,
					IsAmbiguous:      match.IsAmbiguous,
					Confidence:       match.Confidence,
					Sources:          []string{"upjet"},
				}
			}
		}
	}

	// Step 2: Add/merge Terraform doc matches
	if tfRefMatches != nil {
		for key, result := range tfRefMatches.Results {
			if result == nil {
				continue
			}
			sr, ok := keyLookup[key]
			if !ok {
				sr = extractServiceResourceFromKey(key)
			}
			for _, match := range result.Matches {
				entryKey := sr.ServiceName + "|" + sr.ResourceKind + "|" + match.ACKFieldName
				if existing, found := merged[entryKey]; found {
					// Upjet already has this field — boost confidence, add source
					existing.Sources = addSource(existing.Sources, "terraform_docs")
					existing.Confidence = boostConfidence(existing.Confidence)
					// If we didn't have a resolution attr before, take it from TF
					if existing.ResolutionAttr == "" {
						existing.ResolutionAttr = match.ResolutionAttr
					}
				} else {
					// New entry from Terraform docs only
					merged[entryKey] = &mergedEntry{
						ServiceName:      sr.ServiceName,
						ResourceName:     sr.ResourceKind,
						ACKFieldName:     match.ACKFieldName,
						ACKFieldPath:     match.ACKFieldPath,
						TargetTFResource: match.TargetResource,
						ResolutionAttr:   match.ResolutionAttr,
						Confidence:       match.Confidence,
						Sources:          []string{"terraform_docs"},
					}
				}
			}
		}
	}

	// Step 3: Add/merge API model matches
	if modelMatches != nil {
		for key, result := range modelMatches.Results {
			if result == nil {
				continue
			}
			sr, ok := keyLookup[key]
			if !ok {
				sr = extractServiceResourceFromKey(key)
			}
			for _, match := range result.Matches {
				entryKey := sr.ServiceName + "|" + sr.ResourceKind + "|" + match.ACKFieldName
				if existing, found := merged[entryKey]; found {
					// Already covered by Upjet and/or TF docs — boost and add source
					existing.Sources = addSource(existing.Sources, "api_model")
					existing.Confidence = boostConfidence(existing.Confidence)
					// If target resource is empty and model provides one, fill it in
					if existing.TargetTFResource == "" && match.TargetResource != "" {
						existing.TargetTFResource = match.TargetResource
					}
				} else {
					// New entry from API model only
					targetTF := match.TargetResource
					merged[entryKey] = &mergedEntry{
						ServiceName:      sr.ServiceName,
						ResourceName:     sr.ResourceKind,
						ACKFieldName:     match.ACKFieldName,
						ACKFieldPath:     match.ACKFieldPath,
						TargetTFResource: targetTF,
						Confidence:       match.Confidence,
						Sources:          []string{"api_model"},
					}
				}
			}
		}
	}

	l.Info("report_references: merged %d unique field entries from all sources", len(merged))

	// Step 4a: Include fields that already have references: configuration.
	// These were filtered out of the matching phase but should appear in the
	// report as "annotated" entries for completeness.
	for _, ctrl := range controllers {
		for _, res := range ctrl.Resources {
			for _, field := range res.StringFields {
				if !field.HasReference || field.ReferenceConfig == nil {
					continue
				}
				entryKey := ctrl.ServiceName + "|" + res.Kind + "|" + field.Name
				if _, exists := merged[entryKey]; exists {
					// Already in merged from a matching source — skip
					continue
				}
				// Build a target TF resource from the reference config
				targetTF := ""
				if field.ReferenceConfig.ServiceName != "" && field.ReferenceConfig.Resource != "" {
					targetTF = "aws_" + field.ReferenceConfig.ServiceName + "_" + camelToSnake(field.ReferenceConfig.Resource)
				} else if field.ReferenceConfig.Resource != "" {
					// Same-service reference
					targetTF = "aws_" + ctrl.ServiceName + "_" + camelToSnake(field.ReferenceConfig.Resource)
				}
				merged[entryKey] = &mergedEntry{
					ServiceName:      ctrl.ServiceName,
					ResourceName:     res.Kind,
					ACKFieldName:     field.Name,
					ACKFieldPath:     field.Path,
					TargetTFResource: targetTF,
					Confidence:       1.0,
					Sources:          []string{"generator_yaml"},
				}
			}
		}
	}

	l.Info("report_references: %d total entries after including annotated fields", len(merged))

	// Step 5: Translate TF resources and classify, build report entries
	var entries []types.ReferenceGapEntry
	for _, m := range merged {
		// Translate TF resource type to ACK service/resource pair
		targetACKService, targetACKResource := translateTFResource(m.TargetTFResource, tfToACK)

		// Determine recommended resolution path
		recommendedPath := RecommendedResolutionPath(m.TargetTFResource, m.ResolutionAttr, targetACKResource)

		// Classify against existing generator.yaml references
		genConfig := generatorConfigs[m.ServiceName]
		status := ClassifyReferenceFieldByPath(genConfig, m.ResourceName, m.ACKFieldName, m.ACKFieldPath, targetACKService, targetACKResource)

		entries = append(entries, types.ReferenceGapEntry{
			ServiceName:       m.ServiceName,
			ResourceName:      m.ResourceName,
			ACKFieldName:      m.ACKFieldName,
			ACKFieldPath:      m.ACKFieldPath,
			TargetTFResource:  m.TargetTFResource,
			TargetACKService:  targetACKService,
			TargetACKResource: targetACKResource,
			RecommendedPath:   recommendedPath,
			Confidence:        m.Confidence,
			Sources:           m.Sources,
			CurrentStatus:     string(status),
			IsAmbiguous:       m.IsAmbiguous,
		})
	}

	// Sort entries for deterministic output: by service, resource, field
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ServiceName != entries[j].ServiceName {
			return entries[i].ServiceName < entries[j].ServiceName
		}
		if entries[i].ResourceName != entries[j].ResourceName {
			return entries[i].ResourceName < entries[j].ResourceName
		}
		return entries[i].ACKFieldName < entries[j].ACKFieldName
	})

	// Build summary
	summary := buildReferenceSummary(entries)

	l.Info("report_references: %d total entries — %d gaps, %d annotated, %d ambiguous",
		summary.TotalReferences, summary.GapCount, summary.AnnotatedCount, summary.AmbiguousCount)

	return &types.ReferenceGapReport{
		Entries: entries,
		Summary: summary,
	}
}

// TFToACKEntry maps a Terraform resource type to an ACK service/resource pair.
type TFToACKEntry struct {
	ACKService  string
	ACKResource string
}

// BuildTFToACKLookup builds a translation table from Terraform resource types
// (e.g., "aws_iam_role") to ACK service/resource pairs using the controller list.
// The convention is: aws_{service}_{resource} → (service, Resource).
func BuildTFToACKLookup(controllers []types.ControllerInfo) map[string]TFToACKEntry {
	lookup := make(map[string]TFToACKEntry)

	for _, ctrl := range controllers {
		for _, res := range ctrl.Resources {
			// Build TF resource type from service name + resource kind
			// e.g., service="iam", Kind="Role" → "aws_iam_role"
			tfResType := "aws_" + ctrl.ServiceName + "_" + camelToSnake(res.Kind)
			lookup[tfResType] = TFToACKEntry{
				ACKService:  ctrl.ServiceName,
				ACKResource: res.Kind,
			}
		}
	}

	return lookup
}

// translateTFResource looks up a Terraform resource type in the translation table.
// Returns (ack_service, ack_resource). Returns empty strings if not found.
func translateTFResource(tfResource string, lookup map[string]TFToACKEntry) (string, string) {
	if tfResource == "" {
		return "", ""
	}

	// Direct lookup
	if entry, found := lookup[tfResource]; found {
		return entry.ACKService, entry.ACKResource
	}

	// Fallback: extract service from the TF resource type pattern "aws_{service}_{resource}"
	parts := strings.SplitN(tfResource, "_", 3)
	if len(parts) >= 3 && parts[0] == "aws" {
		return parts[1], snakeToCamel(parts[2])
	}

	return "", ""
}

// RecommendedResolutionPath determines the resolution path for a reference based
// on the target resource's naming convention.
//
// Heuristics:
//   - If resolution attr is ".arn" or TF resource name suggests ARN → Status.ACKResourceMetadata.ARN
//   - If resolution attr is ".id" → Status.<Resource>ID
//   - If resolution attr is ".name" → Spec.Name
//   - Otherwise, infer from field patterns
func RecommendedResolutionPath(targetTFResource, resolutionAttr, targetACKResource string) string {
	switch resolutionAttr {
	case ".arn":
		return "Status.ACKResourceMetadata.ARN"
	case ".id":
		if targetACKResource != "" {
			return "Status." + targetACKResource + "ID"
		}
		return "Status.ID"
	case ".name":
		return "Spec.Name"
	default:
		// Infer from TF resource naming: if the target is typically ARN-based
		if strings.Contains(targetTFResource, "iam") ||
			strings.Contains(targetTFResource, "lambda") ||
			strings.Contains(targetTFResource, "sqs") ||
			strings.Contains(targetTFResource, "sns") {
			return "Status.ACKResourceMetadata.ARN"
		}
		if targetACKResource != "" {
			return "Status." + targetACKResource + "ID"
		}
		return ""
	}
}

// ClassifyReferenceField determines the classification of a reference field
// against existing generator.yaml references:
//   - "gap": no references config exists
//   - "annotated": references config exists and target matches
//   - "partial": references config exists but target differs
func ClassifyReferenceField(
	genConfig *parser.GeneratorConfig,
	resourceName, fieldName string,
	expectedService, expectedResource string,
) types.ReferenceCategory {
	return ClassifyReferenceFieldByPath(genConfig, resourceName, fieldName, "", expectedService, expectedResource)
}

// ClassifyReferenceFieldByPath determines the classification of a reference field
// using both leaf name and full dot-path for lookup in generator.yaml.
func ClassifyReferenceFieldByPath(
	genConfig *parser.GeneratorConfig,
	resourceName, fieldName, fieldPath string,
	expectedService, expectedResource string,
) types.ReferenceCategory {
	if genConfig == nil {
		return types.RefCategoryGap
	}

	ref := genConfig.HasReference(resourceName, fieldName)
	if ref == nil && fieldPath != "" && strings.Contains(fieldPath, ".") {
		ref = genConfig.HasReferenceByPath(resourceName, fieldPath)
	}
	if ref == nil {
		return types.RefCategoryGap
	}

	// Has a reference config — check if it matches
	if matchesExpected(ref, expectedService, expectedResource) {
		return types.RefCategoryAnnotated
	}

	return types.RefCategoryPartial
}

// matchesExpected checks if an existing reference config matches the expected target.
// We consider it matching if either:
// - The resource name matches (case-insensitive)
// - Both service_name and resource match
func matchesExpected(ref *parser.GeneratorReference, expectedService, expectedResource string) bool {
	if ref == nil {
		return false
	}

	// If we don't know the expected target, we can't confirm a mismatch
	if expectedService == "" && expectedResource == "" {
		return true
	}

	// Check resource name match (case-insensitive)
	if expectedResource != "" && !strings.EqualFold(ref.Resource, expectedResource) {
		return false
	}

	// If service_name is specified in the reference, check it too
	if expectedService != "" && ref.ServiceName != "" &&
		!strings.EqualFold(ref.ServiceName, expectedService) {
		return false
	}

	return true
}

// buildReferenceSummary computes aggregate statistics from the reference report entries.
func buildReferenceSummary(entries []types.ReferenceGapEntry) types.ReferenceReportSummary {
	summary := types.ReferenceReportSummary{
		GapsPerService: make(map[string]int),
	}

	summary.TotalReferences = len(entries)

	for _, entry := range entries {
		switch types.ReferenceCategory(entry.CurrentStatus) {
		case types.RefCategoryGap:
			summary.GapCount++
			summary.GapsPerService[entry.ServiceName]++
		case types.RefCategoryAnnotated:
			summary.AnnotatedCount++
		case types.RefCategoryPartial:
			// Partial is still a gap that needs attention
			summary.GapCount++
			summary.GapsPerService[entry.ServiceName]++
		}

		if entry.IsAmbiguous {
			summary.AmbiguousCount++
		}

		// Source breakdown
		summary.SourceBreakdown = classifySource(summary.SourceBreakdown, entry.Sources)
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
		return summary.ServicesByPriority[i].ServiceName < summary.ServicesByPriority[j].ServiceName
	})

	return summary
}

// classifySource increments the appropriate source breakdown counter based on
// which sources identified the entry.
func classifySource(stats types.SourceStats, sources []string) types.SourceStats {
	sourceSet := make(map[string]bool)
	for _, s := range sources {
		sourceSet[s] = true
	}

	// generator_yaml is a special source indicating pre-existing annotations
	if sourceSet["generator_yaml"] {
		stats.GeneratorYAML++
		return stats
	}

	count := len(sourceSet)
	switch count {
	case 1:
		if sourceSet["upjet"] {
			stats.UpjetOnly++
		} else if sourceSet["terraform_docs"] {
			stats.TerraformOnly++
		} else if sourceSet["api_model"] {
			stats.ModelOnly++
		}
	case 2:
		stats.TwoSources++
	case 3:
		stats.AllThreeSources++
	}

	return stats
}

// boostConfidence increases confidence by 10% (capped at 1.0) when multiple
// sources agree on a reference field.
func boostConfidence(current float64) float64 {
	boosted := current + 0.1
	if boosted > 1.0 {
		return 1.0
	}
	return boosted
}

// addSource appends a source to the list if not already present.
func addSource(sources []string, newSource string) []string {
	for _, s := range sources {
		if s == newSource {
			return sources
		}
	}
	return append(sources, newSource)
}

// extractServiceResourceFromKey parses a key of format "{service}_{kind}" into its components.
func extractServiceResourceFromKey(key string) struct {
	ServiceName  string
	ResourceKind string
} {
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

// camelToSnake converts a CamelCase string to snake_case.
func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			result.WriteRune(r + 32) // lowercase
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// snakeToCamel converts a snake_case string to CamelCase.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	var result strings.Builder
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		result.WriteByte(part[0] - 32) // uppercase first letter
		if len(part) > 1 {
			result.WriteString(part[1:])
		}
	}
	return result.String()
}
