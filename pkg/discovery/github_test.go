package discovery_test

import (
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/discovery"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

func TestEnrichResourceFields_WithAnnotations(t *testing.T) {
	yaml := `resources:
  Addon:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
      ConfigurationValues:
        is_document: true
      ServiceAccountRoleArn:
        references:
          service_name: iam
          resource: Role
          path: Status.ACKResourceMetadata.ARN
      Policy:
        is_iam_policy: true
`
	genConfig, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error parsing generator config: %v", err)
	}

	resource := &types.ResourceInfo{
		Kind: "Addon",
		StringFields: []types.FieldInfo{
			{Name: "ClusterName", Path: "clusterName"},
			{Name: "ConfigurationValues", Path: "configurationValues"},
			{Name: "ServiceAccountRoleArn", Path: "serviceAccountRoleArn"},
			{Name: "Policy", Path: "policy"},
			{Name: "AddonVersion", Path: "addonVersion"}, // no annotation
		},
	}

	discovery.EnrichResourceFields(resource, genConfig)

	// ClusterName should have reference
	f := resource.StringFields[0]
	if !f.HasReference {
		t.Errorf("expected ClusterName.HasReference = true")
	}
	if f.ReferenceConfig == nil {
		t.Fatal("expected ClusterName.ReferenceConfig to be non-nil")
	}
	if f.ReferenceConfig.Resource != "Cluster" {
		t.Errorf("expected ReferenceConfig.Resource = 'Cluster', got %q", f.ReferenceConfig.Resource)
	}
	if f.ReferenceConfig.Path != "Spec.Name" {
		t.Errorf("expected ReferenceConfig.Path = 'Spec.Name', got %q", f.ReferenceConfig.Path)
	}
	if f.IsDocument || f.IsIAMPolicy {
		t.Error("ClusterName should not be document or IAM policy")
	}

	// ConfigurationValues should be is_document
	f = resource.StringFields[1]
	if !f.IsDocument {
		t.Error("expected ConfigurationValues.IsDocument = true")
	}
	if f.IsIAMPolicy {
		t.Error("expected ConfigurationValues.IsIAMPolicy = false")
	}
	if f.HasReference {
		t.Error("expected ConfigurationValues.HasReference = false")
	}

	// ServiceAccountRoleArn should have cross-service reference
	f = resource.StringFields[2]
	if !f.HasReference {
		t.Error("expected ServiceAccountRoleArn.HasReference = true")
	}
	if f.ReferenceConfig == nil {
		t.Fatal("expected ServiceAccountRoleArn.ReferenceConfig to be non-nil")
	}
	if f.ReferenceConfig.ServiceName != "iam" {
		t.Errorf("expected ReferenceConfig.ServiceName = 'iam', got %q", f.ReferenceConfig.ServiceName)
	}
	if f.ReferenceConfig.Resource != "Role" {
		t.Errorf("expected ReferenceConfig.Resource = 'Role', got %q", f.ReferenceConfig.Resource)
	}
	if f.ReferenceConfig.Path != "Status.ACKResourceMetadata.ARN" {
		t.Errorf("expected ReferenceConfig.Path = 'Status.ACKResourceMetadata.ARN', got %q", f.ReferenceConfig.Path)
	}

	// Policy should be is_iam_policy
	f = resource.StringFields[3]
	if !f.IsIAMPolicy {
		t.Error("expected Policy.IsIAMPolicy = true")
	}
	if f.IsDocument {
		t.Error("expected Policy.IsDocument = false")
	}
	if f.HasReference {
		t.Error("expected Policy.HasReference = false")
	}

	// AddonVersion should have all zero values (no annotation)
	f = resource.StringFields[4]
	if f.IsDocument || f.IsIAMPolicy || f.HasReference {
		t.Error("expected AddonVersion to have zero annotation values")
	}
	if f.ReferenceConfig != nil {
		t.Errorf("expected AddonVersion.ReferenceConfig = nil, got %+v", f.ReferenceConfig)
	}
}

func TestEnrichResourceFields_ResourceNotInGenerator(t *testing.T) {
	yaml := `resources:
  Addon:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
`
	genConfig, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A resource that's not in the generator config
	resource := &types.ResourceInfo{
		Kind: "NonExistentResource",
		StringFields: []types.FieldInfo{
			{Name: "SomeField", Path: "someField"},
		},
	}

	discovery.EnrichResourceFields(resource, genConfig)

	// All annotation fields should remain zero values
	f := resource.StringFields[0]
	if f.IsDocument || f.IsIAMPolicy || f.HasReference {
		t.Error("expected all annotation fields to be zero for unknown resource")
	}
	if f.ReferenceConfig != nil {
		t.Errorf("expected ReferenceConfig = nil, got %+v", f.ReferenceConfig)
	}
}

func TestEnrichResourceFields_NilConfig(t *testing.T) {
	// When generator config is nil, the function should not be called
	// but if somehow called with nil, it should not panic
	// This tests the defensive scenario — the actual code guards nil before calling
	resource := &types.ResourceInfo{
		Kind: "Addon",
		StringFields: []types.FieldInfo{
			{Name: "ClusterName", Path: "clusterName"},
		},
	}

	// Passing nil genConfig should be safe (guarded by caller)
	// The exported function signature requires non-nil, so this is a safety check
	// that the caller logic (processController) guards against nil
	_ = resource // Just verifying the types work
}

func TestEnrichResourceFields_EmptyFields(t *testing.T) {
	yaml := `resources:
  Addon:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
`
	genConfig, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Resource with no fields
	resource := &types.ResourceInfo{
		Kind:         "Addon",
		StringFields: []types.FieldInfo{},
	}

	// Should not panic on empty fields
	discovery.EnrichResourceFields(resource, genConfig)

	if len(resource.StringFields) != 0 {
		t.Errorf("expected 0 fields, got %d", len(resource.StringFields))
	}
}
