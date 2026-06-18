package parser_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
)

// Property 2: CRD string field extraction completeness
// For any valid CRD YAML with string fields at arbitrary nesting under spec,
// the parser extracts every string field with correct path.
//
// **Validates: Requirements 1.2, 1.3**
func TestProperty2_CRDStringFieldExtractionCompleteness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random CRD structure with known string fields
		kind := rapid.StringMatching(`[A-Z][a-zA-Z]{3,12}`).Draw(t, "kind")

		// Generate nested spec properties with string fields at various depths
		spec, expectedPaths := generateSpecProperties(t, "", 0)

		// Build the CRD YAML
		crdYAML := buildCRDYAML(kind, spec)

		// Parse the CRD
		p := parser.NewCRDParser()
		resource, err := p.ParseCRDYAML([]byte(crdYAML))
		if err != nil {
			t.Fatalf("failed to parse generated CRD YAML: %v\nYAML:\n%s", err, crdYAML)
		}

		if resource == nil {
			if len(expectedPaths) > 0 {
				t.Fatalf("parser returned nil resource but expected %d string fields", len(expectedPaths))
			}
			return
		}

		// Collect actual paths from the parser
		actualPaths := make(map[string]bool)
		for _, field := range resource.StringFields {
			actualPaths[field.Path] = true
		}

		// Verify: every expected string field is extracted
		sort.Strings(expectedPaths)
		for _, path := range expectedPaths {
			if !actualPaths[path] {
				t.Fatalf("expected string field at path %q not found in parser output.\nExpected: %v\nActual: %v\nYAML:\n%s",
					path, expectedPaths, keysOf(actualPaths), crdYAML)
			}
		}

		// Verify: no extra string fields beyond what we generated
		expectedSet := make(map[string]bool)
		for _, p := range expectedPaths {
			expectedSet[p] = true
		}
		for path := range actualPaths {
			if !expectedSet[path] {
				t.Fatalf("unexpected string field at path %q in parser output.\nExpected: %v\nActual: %v",
					path, expectedPaths, keysOf(actualPaths))
			}
		}
	})
}

// specProperty represents a generated property in the spec
type specProperty struct {
	name       string
	fieldType  string // "string", "integer", "boolean", "object", "array"
	children   []specProperty
	arrayItems string // type of array items (for array type)
}

// generateSpecProperties generates random spec properties and returns the YAML
// fragment and expected string field paths.
func generateSpecProperties(t *rapid.T, prefix string, depth int) ([]specProperty, []string) {
	maxDepth := 3
	numProps := rapid.IntRange(1, 4).Draw(t, fmt.Sprintf("numProps_d%d", depth))

	var props []specProperty
	var expectedPaths []string

	usedNames := make(map[string]bool)
	for i := 0; i < numProps; i++ {
		// Generate a unique property name for this level
		name := rapid.StringMatching(`[a-z][a-zA-Z]{2,10}`).Draw(t, fmt.Sprintf("propName_d%d_%d", depth, i))
		if usedNames[name] {
			continue
		}
		usedNames[name] = true

		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		if depth >= maxDepth {
			// At max depth, only generate leaf types
			leafType := rapid.SampledFrom([]string{"string", "integer", "boolean"}).Draw(t, "leafType")
			props = append(props, specProperty{name: name, fieldType: leafType})
			if leafType == "string" {
				expectedPaths = append(expectedPaths, path)
			}
		} else {
			// Mix of types
			fieldType := rapid.SampledFrom([]string{"string", "integer", "boolean", "object"}).Draw(t, fmt.Sprintf("type_d%d_%d", depth, i))

			switch fieldType {
			case "string":
				props = append(props, specProperty{name: name, fieldType: "string"})
				expectedPaths = append(expectedPaths, path)
			case "integer", "boolean":
				props = append(props, specProperty{name: name, fieldType: fieldType})
			case "object":
				children, childPaths := generateSpecProperties(t, path, depth+1)
				props = append(props, specProperty{name: name, fieldType: "object", children: children})
				expectedPaths = append(expectedPaths, childPaths...)
			}
		}
	}

	return props, expectedPaths
}

// buildCRDYAML constructs a valid CRD YAML document from the generated spec properties.
func buildCRDYAML(kind string, specProps []specProperty) string {
	var sb strings.Builder
	sb.WriteString("apiVersion: apiextensions.k8s.io/v1\n")
	sb.WriteString("kind: CustomResourceDefinition\n")
	sb.WriteString("metadata:\n")
	sb.WriteString("  name: test.example.com\n")
	sb.WriteString("spec:\n")
	sb.WriteString("  names:\n")
	sb.WriteString(fmt.Sprintf("    kind: %s\n", kind))
	sb.WriteString("  versions:\n")
	sb.WriteString("  - name: v1\n")
	sb.WriteString("    schema:\n")
	sb.WriteString("      openAPIV3Schema:\n")
	sb.WriteString("        type: object\n")
	sb.WriteString("        properties:\n")
	sb.WriteString("          spec:\n")
	sb.WriteString("            type: object\n")
	sb.WriteString("            properties:\n")
	writeProperties(&sb, specProps, 14)
	return sb.String()
}

// writeProperties writes YAML properties at the given indentation level.
func writeProperties(sb *strings.Builder, props []specProperty, indent int) {
	prefix := strings.Repeat(" ", indent)
	for _, prop := range props {
		sb.WriteString(fmt.Sprintf("%s%s:\n", prefix, prop.name))
		switch prop.fieldType {
		case "string":
			sb.WriteString(fmt.Sprintf("%s  type: string\n", prefix))
		case "integer":
			sb.WriteString(fmt.Sprintf("%s  type: integer\n", prefix))
		case "boolean":
			sb.WriteString(fmt.Sprintf("%s  type: boolean\n", prefix))
		case "object":
			sb.WriteString(fmt.Sprintf("%s  type: object\n", prefix))
			if len(prop.children) > 0 {
				sb.WriteString(fmt.Sprintf("%s  properties:\n", prefix))
				writeProperties(sb, prop.children, indent+4)
			}
		case "array":
			sb.WriteString(fmt.Sprintf("%s  type: array\n", prefix))
			sb.WriteString(fmt.Sprintf("%s  items:\n", prefix))
			sb.WriteString(fmt.Sprintf("%s    type: %s\n", prefix, prop.arrayItems))
		}
	}
}

func keysOf(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
