// Package parser provides parsing utilities for CRD YAML files and generator.yaml configurations.
package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// CRDParser parses CRD YAML files to extract resource info and string fields.
type CRDParser struct{}

// NewCRDParser creates a new CRDParser.
func NewCRDParser() *CRDParser {
	return &CRDParser{}
}

// crdDocument represents the top-level structure of a CRD YAML file.
type crdDocument struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Spec       struct {
		Names struct {
			Kind string `yaml:"kind"`
		} `yaml:"names"`
		Versions []struct {
			Name   string `yaml:"name"`
			Schema struct {
				OpenAPIV3Schema map[string]interface{} `yaml:"openAPIV3Schema"`
			} `yaml:"schema"`
		} `yaml:"versions"`
	} `yaml:"spec"`
}

// ParseCRDs parses all CRD YAML files in the given directory and extracts
// resource information including string fields under spec.
func (p *CRDParser) ParseCRDs(crdDir string) ([]types.ResourceInfo, error) {
	entries, err := os.ReadDir(crdDir)
	if err != nil {
		return nil, fmt.Errorf("reading CRD directory %s: %w", crdDir, err)
	}

	var resources []types.ResourceInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		filePath := filepath.Join(crdDir, entry.Name())
		resource, err := p.parseCRDFile(filePath)
		if err != nil {
			// Skip unparseable CRD files but continue with others
			continue
		}
		if resource != nil {
			resources = append(resources, *resource)
		}
	}

	return resources, nil
}

// parseCRDFile parses a single CRD YAML file and extracts resource info.
func (p *CRDParser) parseCRDFile(filePath string) (*types.ResourceInfo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", filePath, err)
	}

	return p.ParseCRDYAML(data)
}

// ParseCRDYAML parses CRD YAML bytes and extracts resource info with string fields.
// This is exported for testing.
func (p *CRDParser) ParseCRDYAML(data []byte) (*types.ResourceInfo, error) {
	var crd crdDocument
	if err := yaml.Unmarshal(data, &crd); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	if crd.Kind != "CustomResourceDefinition" {
		return nil, fmt.Errorf("not a CRD: kind is %q", crd.Kind)
	}

	kind := crd.Spec.Names.Kind
	if kind == "" {
		return nil, fmt.Errorf("CRD has no kind defined")
	}

	// Use the first version's schema
	if len(crd.Spec.Versions) == 0 {
		return &types.ResourceInfo{Kind: kind}, nil
	}

	schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema
	if schema == nil {
		return &types.ResourceInfo{Kind: kind}, nil
	}

	// Navigate to spec.properties
	specSchema := navigateToSpec(schema)
	if specSchema == nil {
		return &types.ResourceInfo{Kind: kind}, nil
	}

	// Extract all string fields recursively
	var fields []types.FieldInfo
	extractStringFields(specSchema, "", &fields)

	// Deduplicate by path
	fields = deduplicateFields(fields)

	// Sort for deterministic output
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Path < fields[j].Path
	})

	return &types.ResourceInfo{
		Kind:         kind,
		StringFields: fields,
	}, nil
}

// navigateToSpec finds the spec properties within the OpenAPI schema.
func navigateToSpec(schema map[string]interface{}) map[string]interface{} {
	// Schema structure: properties.spec.properties
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return nil
	}
	specNode, ok := props["spec"].(map[string]interface{})
	if !ok {
		return nil
	}
	specProps, ok := specNode["properties"].(map[string]interface{})
	if !ok {
		return nil
	}
	return specProps
}

// extractStringFields recursively walks the OpenAPI schema properties,
// finding all fields of type "string" at any depth and recording their
// full dot-separated path.
func extractStringFields(properties map[string]interface{}, prefix string, fields *[]types.FieldInfo) {
	for name, value := range properties {
		propMap, ok := value.(map[string]interface{})
		if !ok {
			continue
		}

		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		fieldType, _ := propMap["type"].(string)

		if fieldType == "string" {
			*fields = append(*fields, types.FieldInfo{
				Name: name,
				Path: path,
			})
		}

		// Recurse into nested object properties
		if fieldType == "object" || fieldType == "" {
			if nestedProps, ok := propMap["properties"].(map[string]interface{}); ok {
				extractStringFields(nestedProps, path, fields)
			}
		}

		// Recurse into array item properties (for arrays of objects)
		if fieldType == "array" {
			if items, ok := propMap["items"].(map[string]interface{}); ok {
				itemType, _ := items["type"].(string)
				if itemType == "string" {
					// The array items are strings; record the array field itself
					*fields = append(*fields, types.FieldInfo{
						Name: name,
						Path: path,
					})
				} else if itemProps, ok := items["properties"].(map[string]interface{}); ok {
					extractStringFields(itemProps, path, fields)
				}
			}
		}
	}
}

// deduplicateFields removes duplicate fields based on path.
func deduplicateFields(fields []types.FieldInfo) []types.FieldInfo {
	seen := make(map[string]bool)
	var result []types.FieldInfo
	for _, f := range fields {
		if !seen[f.Path] {
			seen[f.Path] = true
			result = append(result, f)
		}
	}
	return result
}
