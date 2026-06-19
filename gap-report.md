# ACK Scanner v2 Gap Analysis Report

## Summary

| Metric | Count |
| --- | --- |
| Total Matches | 42 |
| Gaps | 25 |
| Correctly Annotated | 14 |
| Incorrectly Annotated | 3 |

## Priority Services

| Service | Gap Count |
| --- | --- |
| wafv2 | 4 |
| pipes | 3 |
| eventbridge | 2 |
| apigateway | 1 |
| apigatewayv2 | 1 |
| autoscaling | 1 |
| cloudwatch | 1 |
| ec2 | 1 |
| ecs | 1 |
| iam | 1 |
| kms | 1 |
| lambda | 1 |
| opensearchserverless | 1 |
| ram | 1 |
| sagemaker | 1 |
| secretsmanager | 1 |
| sfn | 1 |
| sns | 1 |
| ssm | 1 |

## Details

### apigateway

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| RestAPI | ⚠️ policy | policy | is_iam_policy | gap |

### apigatewayv2

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| API | ⚠️ body | body | is_document | gap |

### autoscaling

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| AutoScalingGroup | ⚠️ notificationMetadata | notification_metadata | is_document | gap |

### cloudwatch

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Dashboard | ⚠️ dashboardBody | dashboard_body | is_document | gap |

### ec2

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| VPCEndpoint | ⚠️ policyDocument | policy | is_iam_policy | gap |

### ecr

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| RepositoryCreationTemplate | lifecyclePolicy | lifecycle_policy | is_document | annotated |
| RepositoryCreationTemplate | repositoryPolicy | repository_policy | is_document | incorrect |
| Repository | lifecyclePolicy | lifecycle_policy | is_document | annotated |
| Repository | policy | repository_policy | is_document | incorrect |

### ecs

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| TaskDefinition | ⚠️ containerDefinitions | container_definitions | is_document | gap |

### eks

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Addon | configurationValues | configuration_values | is_document | annotated |

### eventbridge

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Archive | ⚠️ eventPattern | event_pattern | is_document | gap |
| Rule | ⚠️ eventPattern | event_pattern | is_document | gap |

### iam

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Group | ⚠️ policies | policy | is_iam_policy | gap |
| Role | assumeRolePolicyDocument | assume_role_policy | is_iam_policy | annotated |
| Policy | policyDocument | policy | is_iam_policy | annotated |

### kms

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Key | ⚠️ policy | policy | is_iam_policy | gap |

### lambda

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| EventSourceMapping | ⚠️ pattern | pattern | is_document | gap |

### opensearchserverless

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| SecurityPolicy | ⚠️ policy | policy | is_iam_policy | gap |

### opensearchservice

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Domain | accessPolicies | access_policies | is_document | incorrect |

### pipes

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Pipe | ⚠️ inputTemplate | enrichment_parameters.input_template | is_document | gap |
| Pipe | ⚠️ inputTemplate | target_parameters.input_template | is_document | gap |
| Pipe | ⚠️ pattern | source_parameters.filter_criteria.filter.pattern | is_document | gap |

### ram

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Permission | ⚠️ policyTemplate | policy_template | is_document | gap |

### s3

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Bucket | policy | policy | is_iam_policy | annotated |

### sagemaker

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Pipeline | ⚠️ pipelineDefinition | pipeline_definition | is_document | gap |

### secretsmanager

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Secret | ⚠️ key | secret_string | is_document | gap |

### sfn

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| StateMachine | ⚠️ definition | definition | is_document | gap |

### sns

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Topic | ⚠️ deliveryPolicy | delivery_policy | is_document | gap |
| Topic | policy | policy | is_iam_policy | annotated |
| Subscription | deliveryPolicy | delivery_policy | is_document | annotated |
| Subscription | filterPolicy | filter_policy | is_document | annotated |
| Subscription | redrivePolicy | redrive_policy | is_document | annotated |

### sqs

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Queue | policy | policy | is_iam_policy | annotated |
| Queue | redriveAllowPolicy | redrive_allow_policy | is_document | annotated |
| Queue | redrivePolicy | redrive_policy | is_document | annotated |

### ssm

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| Document | content | content | is_document | annotated |
| Parameter | ⚠️ value | content | is_document | gap |

### wafv2

| Resource | ACK Field | TF Field | Recommended | Status |
| --- | --- | --- | --- | --- |
| WebACL | ⚠️ rules | rules_json | is_document | gap |
| WebACL | ⚠️ rules | rule_json | is_document | gap |
| RuleGroup | ⚠️ andStatement | rules_json | is_document | gap |
| RuleGroup | ⚠️ andStatement | rule_json | is_document | gap |

