package parser

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GeneratorConfig represents a parsed generator.yaml file from an ACK controller.
type GeneratorConfig struct {
	Resources map[string]GeneratorResource `yaml:"resources"`
}

// GeneratorResource represents a resource entry in generator.yaml.
type GeneratorResource struct {
	Fields map[string]GeneratorField `yaml:"fields"`
}

// GeneratorField represents a field configuration in generator.yaml.
type GeneratorField struct {
	IsDocument  bool `yaml:"is_document"`
	IsIAMPolicy bool `yaml:"is_iam_policy"`
	IsReadOnly  bool `yaml:"is_read_only"`
	IsImmutable bool `yaml:"is_immutable"`
}

// ParseGeneratorConfig parses a generator.yaml file and extracts field annotations.
func ParseGeneratorConfig(path string) (*GeneratorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading generator.yaml: %w", err)
	}

	return ParseGeneratorConfigBytes(data)
}

// ParseGeneratorConfigBytes parses generator.yaml bytes and extracts field annotations.
// This is exported for testing.
func ParseGeneratorConfigBytes(data []byte) (*GeneratorConfig, error) {
	var raw struct {
		Resources map[string]struct {
			Fields map[string]map[string]interface{} `yaml:"fields"`
		} `yaml:"resources"`
	}

	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing generator.yaml: %w", err)
	}

	config := &GeneratorConfig{
		Resources: make(map[string]GeneratorResource),
	}

	for resName, res := range raw.Resources {
		genRes := GeneratorResource{
			Fields: make(map[string]GeneratorField),
		}
		for fieldName, fieldMap := range res.Fields {
			field := GeneratorField{}
			if v, ok := fieldMap["is_document"]; ok {
				field.IsDocument, _ = v.(bool)
			}
			if v, ok := fieldMap["is_iam_policy"]; ok {
				field.IsIAMPolicy, _ = v.(bool)
			}
			if v, ok := fieldMap["is_read_only"]; ok {
				field.IsReadOnly, _ = v.(bool)
			}
			if v, ok := fieldMap["is_immutable"]; ok {
				field.IsImmutable, _ = v.(bool)
			}
			genRes.Fields[fieldName] = field
		}
		config.Resources[resName] = genRes
	}

	return config, nil
}

// HasAnnotation checks if a specific field of a resource has is_document or is_iam_policy annotation.
func (gc *GeneratorConfig) HasAnnotation(resourceName, fieldName string) (isDocument, isIAMPolicy bool) {
	res, ok := gc.Resources[resourceName]
	if !ok {
		return false, false
	}
	field, ok := res.Fields[fieldName]
	if !ok {
		return false, false
	}
	return field.IsDocument, field.IsIAMPolicy
}
