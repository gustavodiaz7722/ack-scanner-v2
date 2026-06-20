// Package types defines all shared data types for ack-scanner-v2.
package types

// ControllerInfo describes a discovered ACK controller with its resources.
type ControllerInfo struct {
	ServiceName string         `json:"service_name"`
	RepoName    string         `json:"repo_name"`
	Resources   []ResourceInfo `json:"resources"`
}

// ResourceInfo describes a CRD resource with its string fields.
type ResourceInfo struct {
	Kind         string      `json:"kind"`
	StringFields []FieldInfo `json:"string_fields"`
}

// FieldInfo describes a string field within a CRD spec.
type FieldInfo struct {
	Name            string         `json:"name"`
	Path            string         `json:"path"`
	JSONTag         string         `json:"json_tag"`
	IsDocument      bool           `json:"is_document"`
	IsIAMPolicy     bool           `json:"is_iam_policy"`
	HasReference    bool           `json:"has_reference"`
	ReferenceConfig *ReferenceInfo `json:"reference_config,omitempty"`
}

// ReferenceInfo describes an existing reference configuration in generator.yaml.
type ReferenceInfo struct {
	Resource    string `json:"resource"`
	ServiceName string `json:"service_name,omitempty"`
	Path        string `json:"path"`
}

// ControllerMapping maps an ACK controller to Terraform doc files.
type ControllerMapping struct {
	ServiceName   string         `json:"service_name"`
	TFDocFiles    []MappingEntry `json:"terraform_doc_files"`
	NoMatchReason string         `json:"no_match_reason,omitempty"`
}

// MappingEntry is a single controller-to-TF-doc association.
type MappingEntry struct {
	TFResourceType string  `json:"terraform_resource_type"`
	DocFilePath    string  `json:"doc_file_path"`
	Confidence     float64 `json:"confidence"`
}

// JSONFieldInfo describes a field identified as accepting JSON content.
type JSONFieldInfo struct {
	FieldName  string  `json:"field_name"`
	FieldType  string  `json:"field_type"` // "json_document" or "iam_policy"
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// FieldMatch maps a Terraform JSON field to an ACK CRD field.
type FieldMatch struct {
	TFFieldName  string   `json:"terraform_field_name"`
	ACKFieldName string   `json:"ack_field_name"`
	ACKFieldPath string   `json:"ack_field_path"`
	Confidence   float64  `json:"confidence"`
	Alternatives []string `json:"alternatives,omitempty"`
}

// GapReportEntry is a single entry in the gap report.
type GapReportEntry struct {
	ServiceName           string `json:"service_name"`
	ResourceName          string `json:"resource_name"`
	ACKFieldName          string `json:"ack_field_name"`
	ACKFieldPath          string `json:"ack_field_path"`
	TFFieldName           string `json:"terraform_field_name"`
	RecommendedAnnotation string `json:"recommended_annotation"` // "is_document" or "is_iam_policy"
	CurrentStatus         string `json:"current_status"`         // "gap", "annotated", "incorrect"
}

// GapReport is the complete report output.
type GapReport struct {
	Entries []GapReportEntry `json:"entries"`
	Summary GapReportSummary `json:"summary"`
}

// GapReportSummary contains aggregate statistics.
type GapReportSummary struct {
	TotalMatches       int               `json:"total_matches"`
	GapCount           int               `json:"gap_count"`
	AnnotatedCount     int               `json:"annotated_count"`
	IncorrectCount     int               `json:"incorrect_count"`
	GapsPerService     map[string]int    `json:"gaps_per_service"`
	ServicesByPriority []ServicePriority `json:"services_by_priority"`
}

// ServicePriority represents a service ranked by gap count.
type ServicePriority struct {
	ServiceName string `json:"service_name"`
	GapCount    int    `json:"gap_count"`
}

// Category classifies a field in the gap report.
type Category string

const (
	CategoryGap       Category = "gap"       // Needs annotation, doesn't have one
	CategoryAnnotated Category = "annotated" // Already correctly annotated
	CategoryIncorrect Category = "incorrect" // Has wrong annotation type
)

// AnnotationType represents the recommended annotation.
type AnnotationType string

const (
	AnnotationDocument  AnnotationType = "is_document"
	AnnotationIAMPolicy AnnotationType = "is_iam_policy"
)

// Confidence represents the agent's certainty in a result.
type Confidence float64

const (
	ConfidenceHigh   Confidence = 0.9
	ConfidenceMedium Confidence = 0.7
	ConfidenceLow    Confidence = 0.5
)

// --- Reference Gap Report Types ---

// ReferenceGapEntry is a single entry in the reference gap report,
// representing a field that should have references: configuration.
type ReferenceGapEntry struct {
	ServiceName       string   `json:"service_name"`
	ResourceName      string   `json:"resource_name"`
	ACKFieldName      string   `json:"ack_field_name"`
	ACKFieldPath      string   `json:"ack_field_path"`
	TargetTFResource  string   `json:"target_terraform_resource"`
	TargetACKService  string   `json:"target_ack_service,omitempty"`
	TargetACKResource string   `json:"target_ack_resource,omitempty"`
	RecommendedPath   string   `json:"recommended_path,omitempty"`
	Confidence        float64  `json:"confidence"`
	Sources           []string `json:"sources"`
	CurrentStatus     string   `json:"current_status"` // "gap", "annotated", "partial"
	IsAmbiguous       bool     `json:"is_ambiguous"`
}

// ReferenceGapReport is the complete reference gap report output.
type ReferenceGapReport struct {
	Entries []ReferenceGapEntry    `json:"entries"`
	Summary ReferenceReportSummary `json:"summary"`
}

// ReferenceReportSummary contains aggregate statistics for the reference gap report.
type ReferenceReportSummary struct {
	TotalReferences    int               `json:"total_references"`
	GapCount           int               `json:"gap_count"`
	AnnotatedCount     int               `json:"annotated_count"`
	AmbiguousCount     int               `json:"ambiguous_count"`
	GapsPerService     map[string]int    `json:"gaps_per_service"`
	ServicesByPriority []ServicePriority `json:"services_by_priority"`
	SourceBreakdown    SourceStats       `json:"source_breakdown"`
}

// SourceStats tracks how many reference entries come from each combination of sources.
type SourceStats struct {
	UpjetOnly       int `json:"upjet_only"`
	TerraformOnly   int `json:"terraform_docs_only"`
	ModelOnly       int `json:"model_only"`
	TwoSources      int `json:"two_sources"`
	AllThreeSources int `json:"all_three_sources"`
}

// ReferenceCategory classifies a reference field in the gap report.
type ReferenceCategory string

const (
	RefCategoryGap       ReferenceCategory = "gap"       // Needs references config, doesn't have one
	RefCategoryAnnotated ReferenceCategory = "annotated" // Already correctly configured
	RefCategoryPartial   ReferenceCategory = "partial"   // Has references but target differs
)
