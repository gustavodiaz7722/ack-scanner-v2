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
	Name    string `json:"name"`
	Path    string `json:"path"`
	JSONTag string `json:"json_tag"`
}

// TerraformResourceInfo describes a Terraform resource and its doc file.
type TerraformResourceInfo struct {
	ServiceName  string `json:"service_name"`
	ResourceType string `json:"resource_type"`
	DocFilePath  string `json:"doc_file_path"`
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
