package parser_test

import (
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/parser"
)

func TestParseGeneratorConfigBytes_ReferencesBlock(t *testing.T) {
	yaml := `resources:
  Addon:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
      ServiceAccountRoleArn:
        references:
          service_name: iam
          resource: Role
          path: Status.ACKResourceMetadata.ARN
      ConfigurationValues:
        is_document: true
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	addon, ok := config.Resources["Addon"]
	if !ok {
		t.Fatal("expected 'Addon' resource")
	}

	// ClusterName should have a reference with no service_name (same-service reference)
	clusterField := addon.Fields["ClusterName"]
	if clusterField.References == nil {
		t.Fatal("expected ClusterName to have a references block")
	}
	if clusterField.References.Resource != "Cluster" {
		t.Errorf("expected ClusterName.References.Resource = 'Cluster', got %q", clusterField.References.Resource)
	}
	if clusterField.References.ServiceName != "" {
		t.Errorf("expected ClusterName.References.ServiceName = '', got %q", clusterField.References.ServiceName)
	}
	if clusterField.References.Path != "Spec.Name" {
		t.Errorf("expected ClusterName.References.Path = 'Spec.Name', got %q", clusterField.References.Path)
	}

	// ServiceAccountRoleArn should have a cross-service reference
	roleField := addon.Fields["ServiceAccountRoleArn"]
	if roleField.References == nil {
		t.Fatal("expected ServiceAccountRoleArn to have a references block")
	}
	if roleField.References.Resource != "Role" {
		t.Errorf("expected ServiceAccountRoleArn.References.Resource = 'Role', got %q", roleField.References.Resource)
	}
	if roleField.References.ServiceName != "iam" {
		t.Errorf("expected ServiceAccountRoleArn.References.ServiceName = 'iam', got %q", roleField.References.ServiceName)
	}
	if roleField.References.Path != "Status.ACKResourceMetadata.ARN" {
		t.Errorf("expected ServiceAccountRoleArn.References.Path = 'Status.ACKResourceMetadata.ARN', got %q", roleField.References.Path)
	}

	// ConfigurationValues should have no reference
	configField := addon.Fields["ConfigurationValues"]
	if configField.References != nil {
		t.Errorf("expected ConfigurationValues to have no references block, got %+v", configField.References)
	}
	if !configField.IsDocument {
		t.Error("expected ConfigurationValues.IsDocument to be true")
	}
}

func TestParseGeneratorConfigBytes_FieldWithBothAnnotationsAndReferences(t *testing.T) {
	yaml := `resources:
  PodIdentityAssociation:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
        is_immutable: true
      Policy:
        is_iam_policy: true
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pia := config.Resources["PodIdentityAssociation"]

	// ClusterName has both references and is_immutable
	clusterField := pia.Fields["ClusterName"]
	if clusterField.References == nil {
		t.Fatal("expected ClusterName to have a references block")
	}
	if clusterField.References.Resource != "Cluster" {
		t.Errorf("expected resource 'Cluster', got %q", clusterField.References.Resource)
	}
	if !clusterField.IsImmutable {
		t.Error("expected ClusterName.IsImmutable to be true")
	}

	// Policy has is_iam_policy but no reference
	policyField := pia.Fields["Policy"]
	if policyField.References != nil {
		t.Errorf("expected Policy to have no references, got %+v", policyField.References)
	}
	if !policyField.IsIAMPolicy {
		t.Error("expected Policy.IsIAMPolicy to be true")
	}
}

func TestParseGeneratorConfigBytes_NoReferences(t *testing.T) {
	yaml := `resources:
  Role:
    fields:
      AssumeRolePolicyDocument:
        is_document: true
      Tags:
        is_read_only: true
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	role := config.Resources["Role"]
	for fieldName, field := range role.Fields {
		if field.References != nil {
			t.Errorf("expected field %q to have no references, got %+v", fieldName, field.References)
		}
	}
}

func TestHasReference_ExactMatch(t *testing.T) {
	yaml := `resources:
  Addon:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
      ServiceAccountRoleArn:
        references:
          service_name: iam
          resource: Role
          path: Status.ACKResourceMetadata.ARN
      ConfigurationValues:
        is_document: true
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Exact match — field with reference
	ref := config.HasReference("Addon", "ClusterName")
	if ref == nil {
		t.Fatal("expected HasReference to return non-nil for Addon.ClusterName")
	}
	if ref.Resource != "Cluster" {
		t.Errorf("expected Resource = 'Cluster', got %q", ref.Resource)
	}
	if ref.Path != "Spec.Name" {
		t.Errorf("expected Path = 'Spec.Name', got %q", ref.Path)
	}

	// Exact match — field with cross-service reference
	ref = config.HasReference("Addon", "ServiceAccountRoleArn")
	if ref == nil {
		t.Fatal("expected HasReference to return non-nil for Addon.ServiceAccountRoleArn")
	}
	if ref.ServiceName != "iam" {
		t.Errorf("expected ServiceName = 'iam', got %q", ref.ServiceName)
	}
	if ref.Resource != "Role" {
		t.Errorf("expected Resource = 'Role', got %q", ref.Resource)
	}

	// Exact match — field without reference
	ref = config.HasReference("Addon", "ConfigurationValues")
	if ref != nil {
		t.Errorf("expected HasReference to return nil for Addon.ConfigurationValues, got %+v", ref)
	}
}

func TestHasReference_CaseInsensitiveMatch(t *testing.T) {
	yaml := `resources:
  Addon:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Case-insensitive match (camelCase query for PascalCase key)
	ref := config.HasReference("Addon", "clusterName")
	if ref == nil {
		t.Fatal("expected HasReference to find reference with case-insensitive match")
	}
	if ref.Resource != "Cluster" {
		t.Errorf("expected Resource = 'Cluster', got %q", ref.Resource)
	}

	// All lowercase
	ref = config.HasReference("Addon", "clustername")
	if ref == nil {
		t.Fatal("expected HasReference to find reference with all-lowercase match")
	}
}

func TestHasReference_ResourceNotFound(t *testing.T) {
	yaml := `resources:
  Addon:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Resource doesn't exist
	ref := config.HasReference("NonExistent", "ClusterName")
	if ref != nil {
		t.Errorf("expected nil for non-existent resource, got %+v", ref)
	}
}

func TestHasReference_FieldNotFound(t *testing.T) {
	yaml := `resources:
  Addon:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Field doesn't exist
	ref := config.HasReference("Addon", "NonExistentField")
	if ref != nil {
		t.Errorf("expected nil for non-existent field, got %+v", ref)
	}
}

func TestParseGeneratorConfigBytes_RealWorldEKSStructure(t *testing.T) {
	// Simulates a realistic EKS controller generator.yaml structure
	yaml := `operations:
  AssociateIdentityProviderConfig:
    operation_type:
    - Create
resources:
  Addon:
    hooks:
      delta_pre_compare:
        code: customPreCompare(delta, a, b)
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
  PodIdentityAssociation:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
        is_immutable: true
      Policy:
        is_iam_policy: true
  FargateProfile:
    fields:
      ClusterName:
        references:
          resource: Cluster
          path: Spec.Name
      PodExecutionRoleARN:
        references:
          service_name: iam
          resource: Role
          path: Status.ACKResourceMetadata.ARN
      Subnets:
        references:
          service_name: ec2
          resource: Subnet
          path: Status.SubnetID
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify 3 resources parsed
	if len(config.Resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(config.Resources))
	}

	// Verify Addon
	addon := config.Resources["Addon"]
	if addon.Fields["ConfigurationValues"].References != nil {
		t.Error("ConfigurationValues should have no reference")
	}
	if !addon.Fields["ConfigurationValues"].IsDocument {
		t.Error("ConfigurationValues should be is_document")
	}
	ref := config.HasReference("Addon", "ClusterName")
	if ref == nil || ref.Resource != "Cluster" {
		t.Errorf("Addon.ClusterName reference not parsed correctly")
	}
	ref = config.HasReference("Addon", "ServiceAccountRoleArn")
	if ref == nil || ref.ServiceName != "iam" || ref.Resource != "Role" {
		t.Errorf("Addon.ServiceAccountRoleArn reference not parsed correctly")
	}

	// Verify FargateProfile subnet reference
	ref = config.HasReference("FargateProfile", "Subnets")
	if ref == nil {
		t.Fatal("expected FargateProfile.Subnets to have a reference")
	}
	if ref.ServiceName != "ec2" {
		t.Errorf("expected service_name 'ec2', got %q", ref.ServiceName)
	}
	if ref.Resource != "Subnet" {
		t.Errorf("expected resource 'Subnet', got %q", ref.Resource)
	}
	if ref.Path != "Status.SubnetID" {
		t.Errorf("expected path 'Status.SubnetID', got %q", ref.Path)
	}

	// Verify PodIdentityAssociation
	ref = config.HasReference("PodIdentityAssociation", "ClusterName")
	if ref == nil || ref.Resource != "Cluster" {
		t.Error("PodIdentityAssociation.ClusterName reference not parsed correctly")
	}
	piaCluster := config.Resources["PodIdentityAssociation"].Fields["ClusterName"]
	if !piaCluster.IsImmutable {
		t.Error("PodIdentityAssociation.ClusterName should be is_immutable")
	}
	ref = config.HasReference("PodIdentityAssociation", "Policy")
	if ref != nil {
		t.Errorf("PodIdentityAssociation.Policy should have no reference, got %+v", ref)
	}
}

func TestParseGeneratorConfigBytes_EmptyResources(t *testing.T) {
	yaml := `resources: {}`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(config.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(config.Resources))
	}
}

func TestParseGeneratorConfigBytes_EmptyFields(t *testing.T) {
	yaml := `resources:
  Bucket:
    fields: {}
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bucket := config.Resources["Bucket"]
	if len(bucket.Fields) != 0 {
		t.Errorf("expected 0 fields, got %d", len(bucket.Fields))
	}

	// HasReference should return nil
	ref := config.HasReference("Bucket", "Anything")
	if ref != nil {
		t.Errorf("expected nil for empty resource fields, got %+v", ref)
	}
}

func TestParseGeneratorConfigBytes_ReferenceWithOnlyResource(t *testing.T) {
	// Some references only have resource (same-service, same-path implied)
	yaml := `resources:
  SecurityGroup:
    fields:
      VPCID:
        references:
          resource: VPC
          path: Status.VPCID
`
	config, err := parser.ParseGeneratorConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ref := config.HasReference("SecurityGroup", "VPCID")
	if ref == nil {
		t.Fatal("expected reference for SecurityGroup.VPCID")
	}
	if ref.Resource != "VPC" {
		t.Errorf("expected resource 'VPC', got %q", ref.Resource)
	}
	if ref.ServiceName != "" {
		t.Errorf("expected empty service_name, got %q", ref.ServiceName)
	}
	if ref.Path != "Status.VPCID" {
		t.Errorf("expected path 'Status.VPCID', got %q", ref.Path)
	}
}
