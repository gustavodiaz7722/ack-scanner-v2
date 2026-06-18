package tools

import (
	"fmt"
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
	"pgregory.net/rapid"
)

// TestProperty8_MatchFieldsOutputSchemaValidity verifies that for any match result,
// each entry SHALL have `terraform_field_name`, `ack_field_name`, `ack_field_path`,
// and `confidence` (between 0 and 1). The `ack_field_name` should refer to a valid
// ACK field from the resource's string fields.
//
// **Validates: Requirements 5.3**
func TestProperty8_MatchFieldsOutputSchemaValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a set of valid ACK field names
		numACKFields := rapid.IntRange(1, 15).Draw(t, "numACKFields")
		validACKFields := make(map[string]bool)
		ackFieldNames := make([]string, numACKFields)
		for i := range ackFieldNames {
			nameLen := rapid.IntRange(3, 20).Draw(t, "ackFieldNameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('A', 'Z').Draw(t, "ackFieldByte"))
			}
			ackFieldNames[i] = string(nameBytes)
			validACKFields[ackFieldNames[i]] = true
		}

		// Generate match entries that reference valid ACK fields
		numMatches := rapid.IntRange(0, 10).Draw(t, "numMatches")
		matches := make([]types.FieldMatch, numMatches)
		for i := range matches {
			// Generate terraform field name
			tfNameLen := rapid.IntRange(3, 25).Draw(t, "tfNameLen")
			tfNameBytes := make([]byte, tfNameLen)
			for j := range tfNameBytes {
				tfNameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "tfNameByte"))
			}
			tfFieldName := string(tfNameBytes)

			// Pick a valid ACK field
			ackIdx := rapid.IntRange(0, numACKFields-1).Draw(t, "ackIdx")
			ackFieldName := ackFieldNames[ackIdx]

			// Generate ACK field path
			pathLen := rapid.IntRange(5, 30).Draw(t, "pathLen")
			pathBytes := make([]byte, pathLen)
			for j := range pathBytes {
				pathBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "pathByte"))
			}
			ackFieldPath := "spec." + string(pathBytes)

			// Generate confidence between 0 and 1
			confidenceInt := rapid.IntRange(0, 100).Draw(t, "confidenceInt")
			confidence := float64(confidenceInt) / 100.0

			matches[i] = types.FieldMatch{
				TFFieldName:  tfFieldName,
				ACKFieldName: ackFieldName,
				ACKFieldPath: ackFieldPath,
				Confidence:   confidence,
			}
		}

		// Generate unmatched fields
		numUnmatched := rapid.IntRange(0, 5).Draw(t, "numUnmatched")
		unmatched := make([]string, numUnmatched)
		for i := range unmatched {
			nameLen := rapid.IntRange(3, 20).Draw(t, "unmatchedNameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "unmatchedNameByte"))
			}
			unmatched[i] = string(nameBytes)
		}

		output := &MatchFieldsOutput{
			Matches:   matches,
			Unmatched: unmatched,
		}

		// Property: ValidateMatchFieldsOutput must pass for well-formed output
		err := ValidateMatchFieldsOutput(output, validACKFields)
		if err != nil {
			t.Fatalf("valid output failed validation: %v", err)
		}

		// Property: every match has non-empty terraform_field_name
		for i, match := range output.Matches {
			if match.TFFieldName == "" {
				t.Fatalf("matches[%d]: terraform_field_name is empty", i)
			}
		}

		// Property: every match has non-empty ack_field_name
		for i, match := range output.Matches {
			if match.ACKFieldName == "" {
				t.Fatalf("matches[%d]: ack_field_name is empty", i)
			}
		}

		// Property: every match has non-empty ack_field_path
		for i, match := range output.Matches {
			if match.ACKFieldPath == "" {
				t.Fatalf("matches[%d]: ack_field_path is empty", i)
			}
		}

		// Property: every confidence is between 0 and 1
		for i, match := range output.Matches {
			if match.Confidence < 0 || match.Confidence > 1 {
				t.Fatalf("matches[%d]: confidence %f is out of range [0, 1]", i, match.Confidence)
			}
		}

		// Property: every ack_field_name refers to a valid ACK field
		for i, match := range output.Matches {
			if !validACKFields[match.ACKFieldName] {
				t.Fatalf("matches[%d]: ack_field_name %q is not a valid ACK field", i, match.ACKFieldName)
			}
		}
	})
}

// TestProperty8_InvalidACKFieldNameDetected verifies that ValidateMatchFieldsOutput
// correctly rejects outputs where ack_field_name is not in the valid set.
func TestProperty8_InvalidACKFieldNameDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validACKFields := map[string]bool{
			"PolicyDocument": true,
			"RoleName":       true,
			"Description":    true,
		}

		// Generate an invalid ACK field name that is NOT in the valid set
		invalidLen := rapid.IntRange(5, 15).Draw(t, "invalidLen")
		invalidBytes := make([]byte, invalidLen)
		for j := range invalidBytes {
			invalidBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "invalidByte"))
		}
		invalidName := "Invalid" + string(invalidBytes)

		output := &MatchFieldsOutput{
			Matches: []types.FieldMatch{
				{
					TFFieldName:  "assume_role_policy",
					ACKFieldName: invalidName,
					ACKFieldPath: "spec.assumeRolePolicy",
					Confidence:   0.9,
				},
			},
			Unmatched: []string{},
		}

		// Property: validation must fail for invalid ACK field name
		err := ValidateMatchFieldsOutput(output, validACKFields)
		if err == nil {
			t.Fatalf("expected validation error for invalid ack_field_name %q, got nil", invalidName)
		}
	})
}

// TestProperty8_InvalidConfidenceInMatchDetected verifies that
// ValidateMatchFieldsOutput correctly rejects outputs with confidence
// values outside [0, 1].
func TestProperty8_InvalidConfidenceInMatchDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validACKFields := map[string]bool{"PolicyDocument": true}

		// Generate a confidence value outside [0, 1]
		isNegative := rapid.Bool().Draw(t, "isNegative")
		var confidence float64
		if isNegative {
			confidenceInt := rapid.IntRange(1, 1000).Draw(t, "negativeConfidence")
			confidence = -float64(confidenceInt) / 100.0
		} else {
			confidenceInt := rapid.IntRange(101, 1000).Draw(t, "overConfidence")
			confidence = float64(confidenceInt) / 100.0
		}

		output := &MatchFieldsOutput{
			Matches: []types.FieldMatch{
				{
					TFFieldName:  "policy",
					ACKFieldName: "PolicyDocument",
					ACKFieldPath: "spec.policyDocument",
					Confidence:   confidence,
				},
			},
			Unmatched: []string{},
		}

		// Property: validation must fail for out-of-range confidence
		err := ValidateMatchFieldsOutput(output, validACKFields)
		if err == nil {
			t.Fatalf("expected validation error for confidence %f, got nil", confidence)
		}
	})
}

// TestProperty8_EmptyTFFieldNameDetected verifies that ValidateMatchFieldsOutput
// correctly rejects outputs with empty terraform_field_name.
func TestProperty8_EmptyTFFieldNameDetected(t *testing.T) {
	validACKFields := map[string]bool{"PolicyDocument": true}

	output := &MatchFieldsOutput{
		Matches: []types.FieldMatch{
			{
				TFFieldName:  "",
				ACKFieldName: "PolicyDocument",
				ACKFieldPath: "spec.policyDocument",
				Confidence:   0.9,
			},
		},
		Unmatched: []string{},
	}

	err := ValidateMatchFieldsOutput(output, validACKFields)
	if err == nil {
		t.Fatal("expected validation error for empty terraform_field_name, got nil")
	}
}

// TestProperty9_MatchOutputCompleteness verifies that for any set of TF JSON
// fields provided to matching, every TF field appears either in the matches
// list or in the unmatched list — none are silently dropped.
//
// **Validates: Requirements 5.4, 5.5**
func TestProperty9_MatchOutputCompleteness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a set of TF JSON fields
		numTFFields := rapid.IntRange(1, 20).Draw(t, "numTFFields")
		tfJSONFields := make([]types.JSONFieldInfo, numTFFields)
		tfFieldNames := make([]string, numTFFields)
		for i := range tfJSONFields {
			nameLen := rapid.IntRange(3, 25).Draw(t, "tfFieldNameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "tfFieldNameByte"))
			}
			fieldName := string(nameBytes) + fmt.Sprintf("_%d", i) // ensure uniqueness
			tfFieldNames[i] = fieldName

			fieldType := rapid.SampledFrom([]string{"json_document", "iam_policy"}).Draw(t, "fieldType")
			confidenceInt := rapid.IntRange(0, 100).Draw(t, "confidence")

			tfJSONFields[i] = types.JSONFieldInfo{
				FieldName:  fieldName,
				FieldType:  fieldType,
				Confidence: float64(confidenceInt) / 100.0,
				Reasoning:  "test reasoning",
			}
		}

		// Generate a set of ACK field names for matches
		numACKFields := rapid.IntRange(1, 10).Draw(t, "numACKFields")
		ackFieldNames := make([]string, numACKFields)
		for i := range ackFieldNames {
			nameLen := rapid.IntRange(3, 15).Draw(t, "ackNameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('A', 'Z').Draw(t, "ackNameByte"))
			}
			ackFieldNames[i] = string(nameBytes)
		}

		// Randomly partition TF fields into matched and unmatched
		var matches []types.FieldMatch
		var unmatched []string

		for _, tfField := range tfJSONFields {
			isMatched := rapid.Bool().Draw(t, "isMatched")
			if isMatched && numACKFields > 0 {
				ackIdx := rapid.IntRange(0, numACKFields-1).Draw(t, "matchACKIdx")
				confidenceInt := rapid.IntRange(50, 100).Draw(t, "matchConfidence")
				matches = append(matches, types.FieldMatch{
					TFFieldName:  tfField.FieldName,
					ACKFieldName: ackFieldNames[ackIdx],
					ACKFieldPath: "spec." + ackFieldNames[ackIdx],
					Confidence:   float64(confidenceInt) / 100.0,
				})
			} else {
				unmatched = append(unmatched, tfField.FieldName)
			}
		}

		output := &MatchFieldsOutput{
			Matches:   matches,
			Unmatched: unmatched,
		}

		// Property: every TF field must appear in either matches or unmatched
		err := ValidateMatchCompleteness(output, tfJSONFields)
		if err != nil {
			t.Fatalf("completeness check failed: %v", err)
		}
	})
}

// TestProperty9_IncompleteMappingDetected verifies that ValidateMatchCompleteness
// correctly detects when a TF field is silently dropped.
func TestProperty9_IncompleteMappingDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate TF fields
		numTFFields := rapid.IntRange(2, 10).Draw(t, "numTFFields")
		tfJSONFields := make([]types.JSONFieldInfo, numTFFields)
		for i := range tfJSONFields {
			nameLen := rapid.IntRange(3, 15).Draw(t, "nameLen")
			nameBytes := make([]byte, nameLen)
			for j := range nameBytes {
				nameBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "nameByte"))
			}
			tfJSONFields[i] = types.JSONFieldInfo{
				FieldName:  string(nameBytes) + fmt.Sprintf("_%d", i),
				FieldType:  "json_document",
				Confidence: 0.9,
			}
		}

		// Deliberately drop one field from the output
		dropIdx := rapid.IntRange(0, numTFFields-1).Draw(t, "dropIdx")

		var matches []types.FieldMatch
		var unmatched []string
		for i, tf := range tfJSONFields {
			if i == dropIdx {
				continue // silently drop this one
			}
			unmatched = append(unmatched, tf.FieldName)
		}

		output := &MatchFieldsOutput{
			Matches:   matches,
			Unmatched: unmatched,
		}

		// Property: ValidateMatchCompleteness must detect the dropped field
		err := ValidateMatchCompleteness(output, tfJSONFields)
		if err == nil {
			t.Fatalf("expected completeness error for dropped field %q, got nil", tfJSONFields[dropIdx].FieldName)
		}
	})
}
