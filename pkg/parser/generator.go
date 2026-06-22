package parser

import (
	"fmt"
	"os"
	"strings"

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
	IsDocument   bool                `yaml:"is_document"`
	IsIAMPolicy  bool                `yaml:"is_iam_policy"`
	IsReadOnly   bool                `yaml:"is_read_only"`
	IsImmutable  bool                `yaml:"is_immutable"`
	IsPrimaryKey bool                `yaml:"is_primary_key"`
	References   *GeneratorReference `yaml:"references"`
}

// GeneratorReference describes a cross-resource reference configuration in generator.yaml.
type GeneratorReference struct {
	Resource    string `yaml:"resource"`
	ServiceName string `yaml:"service_name"`
	Path        string `yaml:"path"`
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
			if v, ok := fieldMap["is_primary_key"]; ok {
				field.IsPrimaryKey, _ = v.(bool)
			}
			if v, ok := fieldMap["references"]; ok {
				if refMap, ok := v.(map[string]interface{}); ok {
					ref := &GeneratorReference{}
					if r, ok := refMap["resource"]; ok {
						ref.Resource, _ = r.(string)
					}
					if s, ok := refMap["service_name"]; ok {
						ref.ServiceName, _ = s.(string)
					}
					if p, ok := refMap["path"]; ok {
						ref.Path, _ = p.(string)
					}
					field.References = ref
				}
			}
			genRes.Fields[fieldName] = field
		}
		config.Resources[resName] = genRes
	}

	return config, nil
}

// HasAnnotation checks if a specific field of a resource has is_document or is_iam_policy annotation.
// The lookup is case-insensitive for the field name since CRD schemas use camelCase
// (e.g., "policy") while generator.yaml uses PascalCase (e.g., "Policy").
func (gc *GeneratorConfig) HasAnnotation(resourceName, fieldName string) (isDocument, isIAMPolicy bool) {
	res, ok := gc.Resources[resourceName]
	if !ok {
		return false, false
	}
	// Try exact match first
	field, ok := res.Fields[fieldName]
	if ok {
		return field.IsDocument, field.IsIAMPolicy
	}
	// Fall back to case-insensitive match
	fieldLower := strings.ToLower(fieldName)
	for key, f := range res.Fields {
		if strings.ToLower(key) == fieldLower {
			return f.IsDocument, f.IsIAMPolicy
		}
	}
	return false, false
}

// HasReference checks if a specific field of a resource has a references configuration.
// The lookup is case-insensitive for the field name since CRD schemas use camelCase
// while generator.yaml uses PascalCase.
// Returns nil if the field has no references block.
func (gc *GeneratorConfig) HasReference(resourceName, fieldName string) *GeneratorReference {
	res, ok := gc.Resources[resourceName]
	if !ok {
		return nil
	}
	// Try exact match first
	field, ok := res.Fields[fieldName]
	if ok {
		return field.References
	}
	// Fall back to case-insensitive match
	fieldLower := strings.ToLower(fieldName)
	for key, f := range res.Fields {
		if strings.ToLower(key) == fieldLower {
			return f.References
		}
	}
	return nil
}

// HasReferenceByPath checks if a field (identified by its dot-separated CRD path)
// has a references configuration in generator.yaml. Generator.yaml uses PascalCase
// dot-paths (e.g., "Routes.GatewayId") while CRD schemas use camelCase
// (e.g., "routes.gatewayID"). This method normalizes both to lowercase for comparison.
//
// It first tries matching by leaf name (for top-level fields), then by full path.
func (gc *GeneratorConfig) HasReferenceByPath(resourceName, fieldPath string) *GeneratorReference {
	res, ok := gc.Resources[resourceName]
	if !ok {
		return nil
	}

	pathLower := strings.ToLower(fieldPath)

	for key, f := range res.Fields {
		if f.References == nil {
			continue
		}
		if strings.ToLower(key) == pathLower {
			return f.References
		}
	}
	return nil
}

// IsPrimaryKey checks if a specific field of a resource is marked as is_primary_key.
// The lookup is case-insensitive for the field name.
func (gc *GeneratorConfig) IsPrimaryKey(resourceName, fieldName string) bool {
	res, ok := gc.Resources[resourceName]
	if !ok {
		return false
	}
	// Try exact match first
	field, ok := res.Fields[fieldName]
	if ok {
		return field.IsPrimaryKey
	}
	// Fall back to case-insensitive match
	fieldLower := strings.ToLower(fieldName)
	for key, f := range res.Fields {
		if strings.ToLower(key) == fieldLower {
			return f.IsPrimaryKey
		}
	}
	return false
}
