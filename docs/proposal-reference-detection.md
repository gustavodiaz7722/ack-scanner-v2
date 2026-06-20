# Proposal: Discovering Cross-Resource References from External Artifacts

## Problem

ACK CRD fields that reference other AWS resources (e.g., `SubnetID` → EC2 Subnet, `RoleARN` → IAM Role) need explicit `references:` configuration in `generator.yaml`. Many fields lack this configuration today. We need a method to identify which fields are references and what they reference.

## External Artifacts for Reference Discovery

### Source 1: Upjet/Crossplane AWS Provider Reference Configs (Highest Value)

**Repo:** `github.com/upbound/provider-aws`  
**Path:** `config/*/config.go`  
**License:** Apache-2.0  
**Format:** Go source files  

Upjet's AWS provider contains human-verified reference declarations for hundreds of AWS resources. Each `config.go` file explicitly maps fields to their referenced Terraform resources:

```go
r.References["parameter_group_name"] = config.Reference{
    TerraformName: "aws_elasticache_parameter_group",
}
r.References["kms_key_id"] = config.Reference{
    TerraformName: "aws_kms_key",
}
r.References["subnet_group_name"] = config.Reference{
    TerraformName: "aws_elasticache_subnet_group",
}
```

**How to extract:** Regex parsing of Go source files. The pattern is consistent:
```
r.References["<field_name>"] = config.Reference{...TerraformName: "<aws_resource_type>"...}
```

Also detect `delete(r.References, "<field>")` — this indicates the field was auto-generated as a reference but intentionally removed because it can reference multiple resource types (flag as ambiguous).

**What this gives you:** A definitive mapping of `terraform_field → referenced_terraform_resource` for ~600+ fields across all AWS services. No LLM or heuristics needed.

**Translation to ACK format:** Map Terraform resource names to ACK service/resource pairs:
- `aws_iam_role` → `service_name: iam`, `resource: Role`
- `aws_kms_key` → `service_name: kms`, `resource: Key`
- `aws_ec2_subnet` → `service_name: ec2`, `resource: Subnet`

---

### Source 2: Terraform HCL Examples in Documentation

**Repo:** `github.com/hashicorp/terraform-provider-aws`  
**Path:** `website/docs/r/*.html.markdown`  
**License:** MPL-2.0  
**Format:** Markdown with embedded HCL code blocks  

Terraform documentation contains HCL examples where cross-resource references are encoded explicitly in syntax:

```hcl
resource "aws_ebs_snapshot" "example" {
  volume_id = aws_ebs_volume.example.id    # ← reference to aws_ebs_volume
}

resource "aws_autoscaling_group" "example" {
  vpc_zone_identifier = [aws_subnet.example.id]  # ← reference to aws_subnet
  service_linked_role_arn = aws_iam_service_linked_role.example.arn  # ← reference
}
```

**How to extract:** Parse HCL code blocks within markdown files. Match the pattern `<field> = aws_<resource>.<name>.<attribute>`. This unambiguously identifies:
- Which field is a reference
- What resource type it references
- What attribute it resolves to (`.id`, `.arn`, `.name`)

**What this gives you:** An independent confirmation of references, plus the resolution attribute (ARN vs ID vs Name). Covers ~70-80% of reference fields. This is the same source Upjet's auto-generator uses.

---

### Source 3: AWS Smithy JSON API Models

**Repo:** `github.com/aws/aws-sdk-go-v2`  
**Path:** `codegen/sdk-codegen/aws-models/*.json`  
**License:** Apache-2.0  
**Format:** Smithy JSON AST  

The same models ACK's code-generator uses. They contain per-field documentation and traits:

```json
{
  "com.amazonaws.autoscaling#CreateAutoScalingGroupType": {
    "type": "structure",
    "members": {
      "ServiceLinkedRoleARN": {
        "target": "com.amazonaws.autoscaling#ResourceName",
        "traits": {
          "smithy.api#documentation": "The Amazon Resource Name (ARN) of the service-linked role that the Auto Scaling group uses to call other AWS service on your behalf."
        }
      },
      "VPCZoneIdentifier": {
        "target": "com.amazonaws.autoscaling#XmlStringMaxLen2048",
        "traits": {
          "smithy.api#documentation": "A comma-separated list of subnet IDs for a virtual private cloud (VPC)..."
        }
      }
    }
  }
}
```

**Signals to extract:**

| Signal | Confidence | Example |
|--------|-----------|---------|
| `aws.api#arnReference` trait present | Definitive | Explicitly marks ARN reference fields |
| Field name ends in `ARN`/`Arn` + doc mentions another service | HIGH | `ServiceLinkedRoleARN`, doc says "IAM role" |
| Field name ends in `Id`/`ID` + doc says "The ID of the" | HIGH | `SubnetId`, doc says "subnet IDs" |
| Field name ends in `Name` + doc says "The name of the" | MEDIUM | `PlacementGroup`, doc says "name of the placement group" |
| Doc contains "use the X API to get this value" | HIGH | Explicitly names the source API |

**What this gives you:** 100% field coverage (every field has a model entry), ~40% yield definitive reference signals. Fills gaps where Upjet doesn't have config for a resource.

---

### Source 4: Terraform Argument Reference Descriptions

**Repo:** Same as Source 2 (`hashicorp/terraform-provider-aws`)  
**Path:** `website/docs/r/*.html.markdown` — the "Argument Reference" section  
**Format:** Markdown bullet lists  

```markdown
* `vpc_zone_identifier` (Optional) A list of subnet IDs to launch resources in.
* `target_group_arns` (Optional) A list of `aws_alb_target_group` ARNs
* `service_linked_role_arn` (Optional) The ARN of the service-linked role
* `placement_group` (Optional) The name of the placement group
```

**How to extract:** Parse the "Argument Reference" section. Look for:
- Explicit resource type mentions: `` `aws_alb_target_group` `` in backticks
- Descriptions containing "ARN", "ID of", "name of" + a resource type

**What this gives you:** Human-written descriptions that often explicitly name the referenced resource type. Good for confirming signals from other sources.

---

## Recommended Approach

### Step 1: Parse Upjet configs (covers ~60-70% of fields)

This is the highest-value, lowest-effort step. Clone `upbound/provider-aws`, parse `config/*/config.go` with regex, extract all `r.References[...]` declarations. This gives you a confirmed ground-truth mapping.

### Step 2: Parse Smithy models (covers remaining gaps)

For fields not covered by Upjet (newer fields, resources Upjet hasn't configured), parse the Smithy models for `aws.api#arnReference` traits and documentation string analysis. This catches fields that Upjet missed or that are too new.

### Step 3: Confirm with Terraform HCL examples (validation)

Cross-reference findings from Steps 1-2 against Terraform HCL examples. If a field appears as a reference in both Upjet AND Terraform examples, confidence is maximum. If only one source has it, flag for review.

### Step 4: Agent review for ambiguous cases (optional, ~10% of fields)

For fields where:
- Upjet has `delete(r.References, ...)` (multi-target reference)
- Smithy doc is unclear
- Name pattern matches but doc doesn't clearly identify the target

Use an LLM agent to review the field documentation and determine the correct reference target.

## Translation: Terraform Resource → ACK Reference Config

The final output needs to map from Terraform naming to ACK's `generator.yaml` format:

| Terraform Resource | ACK Equivalent |
|---|---|
| `aws_iam_role` | `service_name: iam`, `resource: Role`, `path: Status.ACKResourceMetadata.ARN` |
| `aws_ec2_subnet` | `service_name: ec2`, `resource: Subnet`, `path: Status.SubnetID` |
| `aws_kms_key` | `service_name: kms`, `resource: Key`, `path: Status.ACKResourceMetadata.ARN` |
| `aws_s3_bucket` | `service_name: s3`, `resource: Bucket`, `path: Spec.Name` |

Resolution path conventions:
- **ARN fields** → `Status.ACKResourceMetadata.ARN` (universal for all ACK resources)
- **ID fields** → `Status.<ResourceType>ID` (e.g., `Status.SubnetID`, `Status.VPCID`)
- **Name fields** → `Spec.Name` or the renamed primary key field

This translation table can be built programmatically from the list of existing ACK controllers and their generated API types.

## Fields to Exclude

Not all string fields that match patterns are references:

- **JSON/document fields** — policy documents, configuration blobs
- **Tags** — `Tags`, `TagList`, `TagSpecifications`
- **Enum fields** — fields with fixed allowed values (e.g., `HealthCheckType: "EC2"|"ELB"`)
- **Self-referential** — the resource's own name/ID (primary key)
- **Free-form strings** — descriptions, comments, metadata
- **Multi-value references** — fields that can reference multiple resource types (flag separately)

## Expected Results

Based on the ~50 ACK controllers currently available:
- ~600-800 fields identified as references from Upjet configs alone
- ~100-200 additional from Smithy model analysis
- ~50-100 gaps (fields that are references in Upjet but missing in ACK `generator.yaml`)
- False positive rate <10% when using Upjet as primary source (it's human-verified)
