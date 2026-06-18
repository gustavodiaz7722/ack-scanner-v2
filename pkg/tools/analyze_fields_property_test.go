package tools

import (
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
	"pgregory.net/rapid"
)

// TestProperty8_FieldAnalysisOutputSchemaValidity verifies that for any field
// analysis result, each entry SHALL have `field_name`, `field_type` (must be
// one of "json_document" or "iam_policy"), and `confidence` (number between 0 and 1).
//
// **Validates: Requirements 4.3**
func TestProperty8_FieldAnalysisOutputSchemaValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random AnalyzeFieldsOutput with valid schema
		numFields := rapid.IntRange(0, 15).Draw(t, "numFields")
		fields := make([]types.JSONFieldInfo, numFields)

		for i := range fields {
			// Generate a non-empty field_name
			nameLen := rapid.IntRange(1, 30).Draw(t, "nameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "nameByte"))
			}
			fieldName := string(nameBytes)

			// Generate field_type: must be one of the two valid values
			fieldType := rapid.SampledFrom([]string{"json_document", "iam_policy"}).Draw(t, "fieldType")

			// Generate confidence: must be between 0 and 1
			// Use integer from 0 to 100 and divide to get a float
			confidenceInt := rapid.IntRange(0, 100).Draw(t, "confidenceInt")
			confidence := float64(confidenceInt) / 100.0

			// Generate reasoning
			reasoningLen := rapid.IntRange(5, 50).Draw(t, "reasoningLen")
			reasoningBytes := make([]byte, reasoningLen)
			for j := range reasoningBytes {
				reasoningBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "reasoningByte"))
			}
			reasoning := string(reasoningBytes)

			fields[i] = types.JSONFieldInfo{
				FieldName:  fieldName,
				FieldType:  fieldType,
				Confidence: confidence,
				Reasoning:  reasoning,
			}
		}

		// Generate resource_type
		resourceTypeLen := rapid.IntRange(5, 30).Draw(t, "resourceTypeLen")
		resourceTypeBytes := make([]byte, resourceTypeLen)
		for j := range resourceTypeBytes {
			resourceTypeBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "resourceTypeByte"))
		}
		resourceType := "aws_" + string(resourceTypeBytes)

		output := &AnalyzeFieldsOutput{
			ResourceType: resourceType,
			JSONFields:   fields,
		}

		// Property: ValidateAnalyzeFieldsOutput must pass for well-formed output
		if err := ValidateAnalyzeFieldsOutput(output); err != nil {
			t.Fatalf("valid output failed validation: %v", err)
		}

		// Property: every field has non-empty field_name
		for i, field := range output.JSONFields {
			if field.FieldName == "" {
				t.Fatalf("json_fields[%d]: field_name is empty", i)
			}
		}

		// Property: every field_type is one of the two valid values
		for i, field := range output.JSONFields {
			if field.FieldType != "json_document" && field.FieldType != "iam_policy" {
				t.Fatalf("json_fields[%d]: field_type %q is not valid (must be \"json_document\" or \"iam_policy\")", i, field.FieldType)
			}
		}

		// Property: every confidence is between 0 and 1
		for i, field := range output.JSONFields {
			if field.Confidence < 0 || field.Confidence > 1 {
				t.Fatalf("json_fields[%d]: confidence %f is out of range [0, 1]", i, field.Confidence)
			}
		}
	})
}

// TestProperty8_InvalidFieldTypeDetected verifies that ValidateAnalyzeFieldsOutput
// correctly rejects outputs with invalid field_type values.
func TestProperty8_InvalidFieldTypeDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a field with an invalid field_type
		invalidTypes := []string{"json", "policy", "document", "iam", "unknown", "", "JSON_DOCUMENT", "IAM_POLICY"}
		invalidType := rapid.SampledFrom(invalidTypes).Draw(t, "invalidType")

		output := &AnalyzeFieldsOutput{
			ResourceType: "aws_test_resource",
			JSONFields: []types.JSONFieldInfo{
				{
					FieldName:  "test_field",
					FieldType:  invalidType,
					Confidence: 0.9,
					Reasoning:  "test reasoning",
				},
			},
		}

		// Property: validation must fail for invalid field_type
		err := ValidateAnalyzeFieldsOutput(output)
		if err == nil {
			t.Fatalf("expected validation error for invalid field_type %q, got nil", invalidType)
		}
	})
}

// TestProperty8_InvalidConfidenceDetected verifies that ValidateAnalyzeFieldsOutput
// correctly rejects outputs with confidence values outside [0, 1].
func TestProperty8_InvalidConfidenceDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a confidence value outside [0, 1]
		// Either negative or greater than 1
		isNegative := rapid.Bool().Draw(t, "isNegative")
		var confidence float64
		if isNegative {
			// Negative: -0.01 to -10.0
			confidenceInt := rapid.IntRange(1, 1000).Draw(t, "negativeConfidence")
			confidence = -float64(confidenceInt) / 100.0
		} else {
			// Greater than 1: 1.01 to 10.0
			confidenceInt := rapid.IntRange(101, 1000).Draw(t, "overConfidence")
			confidence = float64(confidenceInt) / 100.0
		}

		output := &AnalyzeFieldsOutput{
			ResourceType: "aws_test_resource",
			JSONFields: []types.JSONFieldInfo{
				{
					FieldName:  "test_field",
					FieldType:  "json_document",
					Confidence: confidence,
					Reasoning:  "test reasoning",
				},
			},
		}

		// Property: validation must fail for out-of-range confidence
		err := ValidateAnalyzeFieldsOutput(output)
		if err == nil {
			t.Fatalf("expected validation error for confidence %f, got nil", confidence)
		}
	})
}

// TestProperty8_EmptyFieldNameDetected verifies that ValidateAnalyzeFieldsOutput
// correctly rejects outputs with empty field_name.
func TestProperty8_EmptyFieldNameDetected(t *testing.T) {
	output := &AnalyzeFieldsOutput{
		ResourceType: "aws_test_resource",
		JSONFields: []types.JSONFieldInfo{
			{
				FieldName:  "",
				FieldType:  "iam_policy",
				Confidence: 0.8,
				Reasoning:  "test reasoning",
			},
		},
	}

	err := ValidateAnalyzeFieldsOutput(output)
	if err == nil {
		t.Fatal("expected validation error for empty field_name, got nil")
	}
}

// TestDeriveItemKey verifies item key extraction from doc file paths.
func TestDeriveItemKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"website/docs/r/s3_bucket.html.markdown", "s3_bucket"},
		{"website/docs/r/iam_role.html.markdown", "iam_role"},
		{"website/docs/r/lambda_function.html.markdown", "lambda_function"},
		{"website/docs/r/appautoscaling_target.html.markdown", "appautoscaling_target"},
		{"s3_bucket.html.markdown", "s3_bucket"},
	}

	for _, tt := range tests {
		got := deriveItemKey(tt.input)
		if got != tt.expected {
			t.Errorf("deriveItemKey(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
