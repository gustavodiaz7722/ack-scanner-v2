# ACK Reference Gap Report

## Summary

| Metric | Count |
| --- | --- |
| Total References | 510 |
| Gaps | 252 |
| Correctly Annotated | 258 |
| Ambiguous | 0 |

## Source Breakdown

| Source Combination | Count |
| --- | --- |
| Upjet Only | 0 |
| Terraform Docs Only | 0 |
| API Model Only | 261 |
| Already Annotated (generator.yaml) | 249 |
| Two Sources | 0 |
| All Three Sources | 0 |

## Priority Services

| Service | Gap Count |
| --- | --- |
| ec2 | 39 |
| sagemaker | 22 |
| apigateway | 14 |
| cognitoidentityprovider | 13 |
| sns | 13 |
| pipes | 9 |
| quicksight | 9 |
| elasticache | 7 |
| rds | 7 |
| autoscaling | 6 |
| bedrockagentcorecontrol | 6 |
| cloudwatch | 6 |
| eks | 6 |
| networkfirewall | 6 |
| apigatewayv2 | 5 |
| athena | 5 |
| lambda | 5 |
| ecr | 4 |
| elbv2 | 4 |
| eventbridge | 4 |
| kafka | 4 |
| route53resolver | 4 |
| backup | 3 |
| cloudfront | 3 |
| documentdb | 3 |
| opensearchservice | 3 |
| ram | 3 |
| route53 | 3 |
| s3 | 3 |
| s3control | 3 |
| acmpca | 2 |
| bedrockagent | 2 |
| codeartifact | 2 |
| dms | 2 |
| dynamodb | 2 |
| emrcontainers | 2 |
| memorydb | 2 |
| sfn | 2 |
| wafv2 | 2 |
| acm | 1 |
| applicationautoscaling | 1 |
| bedrock | 1 |
| efs | 1 |
| emrserverless | 1 |
| glue | 1 |
| keyspaces | 1 |
| kms | 1 |
| mq | 1 |
| organizations | 1 |
| prometheusservice | 1 |
| ssm | 1 |

## Details

### acm

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Certificate | ⚠️ certificateARN | certificateARN | Certificate | api_model | gap | 0.95 |
| Certificate | certificateAuthorityARN | certificateAuthorityARN | aws_acmpca_certificate_authority → acmpca/CertificateAuthority | generator_yaml | annotated | 1.00 |

### acmpca

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Certificate | certificateAuthorityARN | certificateAuthorityARN | aws_acmpca_certificate_authority → acmpca/CertificateAuthority | generator_yaml | annotated | 1.00 |
| Certificate | certificateSigningRequest | certificateSigningRequest | aws_acmpca_certificate_authority → acmpca/CertificateAuthority | generator_yaml | annotated | 1.00 |
| Certificate | ⚠️ templateARN | templateARN | Template | api_model | gap | 0.85 |
| CertificateAuthority | ⚠️ s3BucketName | revocationConfiguration.crlConfiguration.s3BucketName | Bucket | api_model | gap | 0.95 |

### apigateway

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| APIIntegrationResponse | ⚠️ resourceID | resourceID | Resource | api_model | gap | 0.95 |
| APIIntegrationResponse | ⚠️ restAPIID | restAPIID | RestApi | api_model | gap | 0.95 |
| APIKey | ⚠️ restAPIID | stageKeys.restAPIID | RestApi | api_model | gap | 0.80 |
| APIMethodResponse | ⚠️ resourceID | resourceID | Resource | api_model | gap | 0.80 |
| APIMethodResponse | ⚠️ restAPIID | restAPIID | RestApi | api_model | gap | 0.80 |
| Authorizer | ⚠️ authorizerCredentials | authorizerCredentials | Role | api_model | gap | 0.80 |
| Authorizer | ⚠️ authorizerURI | authorizerURI | Function | api_model | gap | 0.80 |
| Authorizer | ⚠️ providerARNs | providerARNs | UserPool | api_model | gap | 0.85 |
| Authorizer | restAPIID | restAPIID | aws_apigateway_rest_a_p_i → apigateway/RestAPI | generator_yaml | annotated | 1.00 |
| Deployment | restAPIID | restAPIID | aws_apigateway_rest_a_p_i → apigateway/RestAPI | generator_yaml | annotated | 1.00 |
| Integration | connectionID | connectionID | aws_apigateway_v_p_c_link → apigateway/VPCLink | generator_yaml | annotated | 1.00 |
| Integration | ⚠️ credentials | credentials | Role | api_model | gap | 0.80 |
| Integration | restAPIID | restAPIID | aws_apigateway_rest_a_p_i → apigateway/RestAPI | generator_yaml | annotated | 1.00 |
| Method | ⚠️ authorizerID | authorizerID | Authorizer | api_model | gap | 0.95 |
| Method | ⚠️ requestValidatorID | requestValidatorID | RequestValidator | api_model | gap | 0.90 |
| Method | restAPIID | restAPIID | aws_apigateway_rest_a_p_i → apigateway/RestAPI | generator_yaml | annotated | 1.00 |
| Resource | parentID | parentID | aws_apigateway_resource → apigateway/Resource | generator_yaml | annotated | 1.00 |
| Resource | parentId | parentId | Resource | api_model | annotated | 0.80 |
| Resource | restAPIID | restAPIID | aws_apigateway_rest_a_p_i → apigateway/RestAPI | generator_yaml | annotated | 1.00 |
| RestAPI | ⚠️ cloneFrom | cloneFrom | RestApi | api_model | gap | 0.70 |
| Stage | ⚠️ deploymentID | deploymentID | Deployment | api_model | gap | 0.95 |
| Stage | restAPIID | restAPIID | aws_apigateway_rest_a_p_i → apigateway/RestAPI | generator_yaml | annotated | 1.00 |
| VPCLink | ⚠️ targetARNs | targetARNs | LoadBalancer | api_model | gap | 0.95 |

### apigatewayv2

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| API | ⚠️ credentialsARN | credentialsARN | Role | api_model | gap | 0.95 |
| Authorizer | apiID | apiID | aws_apigatewayv2_a_p_i → apigatewayv2/API | generator_yaml | annotated | 1.00 |
| Authorizer | ⚠️ authorizerCredentialsARN | authorizerCredentialsARN | Role | api_model | gap | 0.95 |
| Deployment | apiID | apiID | aws_apigatewayv2_a_p_i → apigatewayv2/API | generator_yaml | annotated | 1.00 |
| DomainName | ⚠️ certificateARN | domainNameConfigurations.certificateARN | Certificate | api_model | gap | 0.95 |
| Integration | apiID | apiID | aws_apigatewayv2_a_p_i → apigatewayv2/API | generator_yaml | annotated | 1.00 |
| Integration | connectionID | connectionID | aws_apigatewayv2_v_p_c_link → apigatewayv2/VPCLink | generator_yaml | annotated | 1.00 |
| Integration | ⚠️ credentialsARN | credentialsARN | Role | api_model | gap | 0.90 |
| Route | apiID | apiID | aws_apigatewayv2_a_p_i → apigatewayv2/API | generator_yaml | annotated | 1.00 |
| Route | authorizerID | authorizerID | aws_apigatewayv2_authorizer → apigatewayv2/Authorizer | generator_yaml | annotated | 1.00 |
| Route | target | target | aws_apigatewayv2_integration → apigatewayv2/Integration | generator_yaml | annotated | 1.00 |
| Stage | apiID | apiID | aws_apigatewayv2_a_p_i → apigatewayv2/API | generator_yaml | annotated | 1.00 |
| Stage | deploymentID | deploymentID | aws_apigatewayv2_deployment → apigatewayv2/Deployment | generator_yaml | annotated | 1.00 |
| Stage | ⚠️ destinationARN | accessLogSettings.destinationARN | LogGroup | api_model | gap | 0.95 |

### applicationautoscaling

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| ScalableTarget | ⚠️ roleARN | roleARN | Role | api_model | gap | 0.85 |

### athena

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| WorkGroup | ⚠️ executionRole | configuration.executionRole | Role | api_model | gap | 0.80 |
| WorkGroup | ⚠️ expectedBucketOwner | configuration.resultConfiguration.expectedBucketOwner | Account | api_model | gap | 0.80 |
| WorkGroup | ⚠️ identityCenterInstanceARN | configuration.identityCenterConfiguration.identityCenterInstanceARN | Instance | api_model | gap | 0.85 |
| WorkGroup | ⚠️ kmsKey | configuration.customerContentEncryptionConfiguration.kmsKey | Key | api_model | gap | 0.80 |
| WorkGroup | ⚠️ outputLocation | configuration.resultConfiguration.outputLocation | Bucket | api_model | gap | 0.60 |

### autoscaling

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| AutoScalingGroup | ⚠️ availabilityZones | availabilityZones | AvailabilityZone | api_model | gap | 0.95 |
| AutoScalingGroup | instanceID | instanceID | aws_ec2_instance → ec2/Instance | generator_yaml | annotated | 1.00 |
| AutoScalingGroup | ⚠️ launchConfigurationName | launchConfigurationName | LaunchConfiguration | api_model | gap | 0.95 |
| AutoScalingGroup | ⚠️ launchTemplateName | launchTemplate.launchTemplateName | LaunchTemplate | api_model | gap | 0.95 |
| AutoScalingGroup | ⚠️ loadBalancerNames | loadBalancerNames | LoadBalancer | api_model | gap | 0.95 |
| AutoScalingGroup | ⚠️ placementGroup | placementGroup | PlacementGroup | api_model | gap | 0.95 |
| AutoScalingGroup | roleARN | lifecycleHookSpecificationList.roleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| AutoScalingGroup | serviceLinkedRoleARN | serviceLinkedRoleARN | aws_iam_service_linked_role → iam/ServiceLinkedRole | generator_yaml | annotated | 1.00 |
| AutoScalingGroup | targetGroupARNs | targetGroupARNs | aws_elbv2_target_group → elbv2/TargetGroup | generator_yaml | annotated | 1.00 |
| AutoScalingGroup | ⚠️ vpcZoneIdentifier | vpcZoneIdentifier | Subnet | api_model | gap | 0.95 |

### backup

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| BackupPlan | destinationBackupVaultARN | rules.copyActions.destinationBackupVaultARN | aws_backup_backup_vault → backup/BackupVault | generator_yaml | annotated | 1.00 |
| BackupPlan | ⚠️ scannerRoleARN | scanSettings.scannerRoleARN | Role | api_model | gap | 0.80 |
| BackupPlan | targetBackupVaultName | rules.targetBackupVaultName | aws_backup_backup_vault → backup/BackupVault | generator_yaml | annotated | 1.00 |
| BackupPlan | ⚠️ targetLogicallyAirGappedBackupVaultARN | rules.targetLogicallyAirGappedBackupVaultARN | backup vault | api_model | gap | 0.80 |
| BackupSelection | backupPlanID | backupPlanID | aws_backup_backup_plan → backup/BackupPlan | generator_yaml | annotated | 1.00 |
| BackupSelection | iamRoleARN | iamRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| BackupVault | EncryptionKeyArn | EncryptionKeyArn | Key | api_model | annotated | 0.85 |
| BackupVault | ⚠️ IamRoleArn | IamRoleArn | Role | api_model | gap | 0.85 |
| BackupVault | encryptionKeyARN | encryptionKeyARN | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |

### bedrock

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| InferenceProfile | ⚠️ copyFrom | modelSource.copyFrom | FoundationModel | api_model | gap | 0.80 |

### bedrockagent

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Agent | agentResourceRoleARN | agentResourceRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Agent | ⚠️ customerEncryptionKeyARN | customerEncryptionKeyARN | Key | api_model | gap | 0.85 |
| Agent | ⚠️ lambda | customOrchestration.executor.lambda | Function | api_model | gap | 0.85 |

### bedrockagentcorecontrol

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| AgentRuntime | ⚠️ roleARN | roleARN | Role | api_model | gap | 0.95 |
| AgentRuntime | securityGroups | networkConfiguration.networkModeConfig.securityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| AgentRuntime | subnets | networkConfiguration.networkModeConfig.subnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| AgentRuntimeEndpoint | ⚠️ agentRuntimeID | agentRuntimeID | Runtime | api_model | gap | 0.75 |
| Browser | executionRoleARN | executionRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Browser | secretARN | certificates.location.secretsManager.secretARN | aws_secretsmanager_secret → secretsmanager/Secret | generator_yaml | annotated | 1.00 |
| Browser | securityGroups | networkConfiguration.vpcConfig.securityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Browser | subnets | networkConfiguration.vpcConfig.subnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| CodeInterpreter | executionRoleARN | executionRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| CodeInterpreter | secretARN | certificates.location.secretsManager.secretARN | aws_secretsmanager_secret → secretsmanager/Secret | generator_yaml | annotated | 1.00 |
| CodeInterpreter | securityGroups | networkConfiguration.vpcConfig.securityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| CodeInterpreter | subnets | networkConfiguration.vpcConfig.subnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| Gateway | ⚠️ arn | interceptorConfigurations.interceptor.lambda.arn | Role | api_model | gap | 0.60 |
| Gateway | kmsKeyARN | kmsKeyARN | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Gateway | roleARN | roleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| GatewayTarget | gatewayIdentifier | gatewayIdentifier | aws_bedrockagentcorecontrol_gateway → bedrockagentcorecontrol/Gateway | generator_yaml | annotated | 1.00 |
| GatewayTarget | ⚠️ lambdaARN | targetConfiguration.mcp.lambda.lambdaARN | Function | api_model | gap | 0.70 |
| GatewayTarget | ⚠️ providerARN | credentialProviderConfigurations.credentialProvider.apiKeyCredentialProvider.providerARN | APIKeyCredentialProvider | api_model | gap | 0.95 |
| GatewayTarget | restAPIID | targetConfiguration.mcp.apiGateway.restAPIID | aws_apigateway_rest_a_p_i → apigateway/RestAPI | generator_yaml | annotated | 1.00 |
| Memory | ⚠️ dataStreamARN | streamDeliveryResources.resources.kinesis.dataStreamARN | Stream | api_model | gap | 0.95 |
| Memory | encryptionKeyARN | encryptionKeyARN | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Memory | memoryExecutionRoleARN | memoryExecutionRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Memory | topicARN | memoryStrategies.customMemoryStrategy.configuration.selfManagedConfiguration.invocationConfiguration.topicARN | aws_sns_topic → sns/Topic | generator_yaml | annotated | 1.00 |

### cloudfront

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Distribution | acmCertificateARN | distributionConfig.viewerCertificate.acmCertificateARN | aws_acm_certificate → acm/Certificate | generator_yaml | annotated | 1.00 |
| Distribution | ⚠️ id | distributionConfig.connectionFunctionAssociation.id |  | api_model | gap | 0.75 |
| Distribution | ⚠️ webACLID | distributionConfig.webACLID | WebACL | api_model | gap | 0.85 |
| VPCOrigin | ⚠️ arn | vpcOriginEndpointConfig.arn |  | api_model | gap | 0.75 |

### cloudwatch

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| MetricAlarm | ⚠️ alarmActions | alarmActions |  | api_model | gap | 0.95 |
| MetricAlarm | ⚠️ insufficientDataActions | insufficientDataActions |  | api_model | gap | 0.95 |
| MetricAlarm | ⚠️ oKActions | oKActions |  | api_model | gap | 0.95 |
| MetricAlarm | ⚠️ thresholdMetricID | thresholdMetricID | AnomalyDetectionBand | api_model | gap | 0.90 |
| MetricStream | ⚠️ firehoseARN | firehoseARN | DeliveryStream | api_model | gap | 0.95 |
| MetricStream | ⚠️ roleARN | roleARN | Role | api_model | gap | 0.95 |

### cloudwatchlogs

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| LogGroup | kmsKeyID | kmsKeyID | Key | api_model | annotated | 0.80 |

### codeartifact

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Domain | ⚠️ encryptionKey | encryptionKey | Key | api_model | gap | 0.85 |
| PackageGroup | ⚠️ domainOwner | domainOwner | Account | api_model | gap | 0.80 |

### cognitoidentityprovider

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| UserPool | ⚠️ customMessage | lambdaConfig.customMessage | Function | api_model | gap | 0.95 |
| UserPool | ⚠️ defineAuthChallenge | lambdaConfig.defineAuthChallenge | Function | api_model | gap | 0.95 |
| UserPool | ⚠️ kmsKeyID | lambdaConfig.kmsKeyID | Key | api_model | gap | 0.95 |
| UserPool | ⚠️ lambdaARN | lambdaConfig.customEmailSender.lambdaARN | Function | api_model | gap | 0.90 |
| UserPool | ⚠️ postAuthentication | lambdaConfig.postAuthentication | Function | api_model | gap | 0.95 |
| UserPool | ⚠️ postConfirmation | lambdaConfig.postConfirmation | Function | api_model | gap | 0.95 |
| UserPool | ⚠️ preAuthentication | lambdaConfig.preAuthentication | Function | api_model | gap | 0.95 |
| UserPool | ⚠️ preSignUp | lambdaConfig.preSignUp | Function | api_model | gap | 0.95 |
| UserPool | ⚠️ preTokenGeneration | lambdaConfig.preTokenGeneration | Function | api_model | gap | 0.95 |
| UserPool | ⚠️ snsCallerARN | smsConfiguration.snsCallerARN | Role | api_model | gap | 0.95 |
| UserPool | ⚠️ sourceARN | emailConfiguration.sourceARN | Identity | api_model | gap | 0.95 |
| UserPool | ⚠️ userMigration | lambdaConfig.userMigration | Function | api_model | gap | 0.95 |
| UserPool | ⚠️ verifyAuthChallengeResponse | lambdaConfig.verifyAuthChallengeResponse | Function | api_model | gap | 0.95 |

### dms

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Certificate | kmsKeyID | kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Endpoint | bucketName | redshiftSettings.bucketName | aws_s3_bucket → s3/Bucket | generator_yaml | annotated | 1.00 |
| Endpoint | certificateARN | certificateARN | aws_dms_certificate → dms/Certificate | generator_yaml | annotated | 1.00 |
| Endpoint | kmsKeyID | docDBSettings.kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Endpoint | s3BucketName | neptuneSettings.s3BucketName | aws_s3_bucket → s3/Bucket | generator_yaml | annotated | 1.00 |
| Endpoint | secretsManagerAccessRoleARN | docDBSettings.secretsManagerAccessRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Endpoint | ⚠️ secretsManagerOracleAsmAccessRoleARN | oracleSettings.secretsManagerOracleAsmAccessRoleARN | Role | api_model | gap | 0.85 |
| Endpoint | ⚠️ secretsManagerOracleAsmSecretID | oracleSettings.secretsManagerOracleAsmSecretID | Secret | api_model | gap | 0.80 |
| Endpoint | secretsManagerSecretID | docDBSettings.secretsManagerSecretID | aws_secretsmanager_secret → secretsmanager/Secret | generator_yaml | annotated | 1.00 |
| Endpoint | serverSideEncryptionKMSKeyID | redshiftSettings.serverSideEncryptionKMSKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Endpoint | serviceAccessRoleARN | dmsTransferSettings.serviceAccessRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Endpoint | sslCaCertificateARN | kafkaSettings.sslCaCertificateARN | aws_dms_certificate → dms/Certificate | generator_yaml | annotated | 1.00 |
| Endpoint | sslClientCertificateARN | kafkaSettings.sslClientCertificateARN | aws_dms_certificate → dms/Certificate | generator_yaml | annotated | 1.00 |
| Endpoint | sslClientKeyARN | kafkaSettings.sslClientKeyARN | aws_dms_certificate → dms/Certificate | generator_yaml | annotated | 1.00 |
| Endpoint | streamARN | kinesisSettings.streamARN | aws_kinesis_stream → kinesis/Stream | generator_yaml | annotated | 1.00 |
| ReplicationSubnetGroup | subnetIDs | subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |

### documentdb

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| DBCluster | ⚠️ dbClusterParameterGroupName | dbClusterParameterGroupName | DBClusterParameterGroup | api_model | gap | 0.95 |
| DBCluster | dbSubnetGroupName | dbSubnetGroupName | aws_documentdb_d_b_subnet_group → documentdb/DBSubnetGroup | generator_yaml | annotated | 1.00 |
| DBCluster | ⚠️ globalClusterIdentifier | globalClusterIdentifier | GlobalCluster | api_model | gap | 0.95 |
| DBCluster | kmsKeyID | kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| DBCluster | masterUserSecretKMSKeyID | masterUserSecretKMSKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| DBCluster | vpcSecurityGroupIDs | vpcSecurityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| DBInstance | ⚠️ caCertificateIdentifier | caCertificateIdentifier | Certificate | api_model | gap | 0.95 |
| DBInstance | performanceInsightsKMSKeyID | performanceInsightsKMSKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| DBSubnetGroup | subnetIDs | subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |

### dsql

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Cluster | kmsEncryptionKey | kmsEncryptionKey | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |

### dynamodb

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Backup | ⚠️ tableName | tableName | Table | api_model | gap | 0.75 |
| Table | ⚠️ kmsMasterKeyID | tableReplicas.kmsMasterKeyID | Key | api_model | gap | 0.90 |

### ec2

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| CapacityReservation | ⚠️ outpostARN | outpostARN |  | api_model | gap | 0.95 |
| CapacityReservation | ⚠️ placementGroupARN | placementGroupARN |  | api_model | gap | 0.95 |
| DHCPOptions | ⚠️ vpc | vpc | Vpc | api_model | gap | 0.85 |
| EgressOnlyInternetGateway | vpcID | vpcID | aws_ec2_v_p_c → ec2/VPC | generator_yaml | annotated | 1.00 |
| FlowLog | deliverLogsPermissionARN | deliverLogsPermissionARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| FlowLog | ⚠️ resourceID | resourceID | Vpc | api_model | gap | 0.75 |
| Instance | ⚠️ capacityReservationID | capacityReservationSpecification.capacityReservationTarget.capacityReservationID | CapacityReservation | api_model | gap | 0.95 |
| Instance | ⚠️ imageID | imageID | Instance | api_model | gap | 0.60 |
| Instance | launchTemplateID | launchTemplate.launchTemplateID | aws_ec2_launch_template → ec2/LaunchTemplate | generator_yaml | annotated | 1.00 |
| Instance | ⚠️ networkInterfaceID | networkInterfaces.networkInterfaceID | NetworkInterface | api_model | gap | 0.95 |
| Instance | ⚠️ securityGroupIDs | securityGroupIDs | SecurityGroup | api_model | gap | 0.95 |
| Instance | subnetID | networkInterfaces.subnetID | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| InternetGateway | routeTables | routeTables | aws_ec2_route_table → ec2/RouteTable | generator_yaml | annotated | 1.00 |
| InternetGateway | vpc | vpc | aws_ec2_v_p_c → ec2/VPC | generator_yaml | annotated | 1.00 |
| LaunchTemplate | ⚠️ capacityReservationID | data.capacityReservationSpecification.capacityReservationTarget.capacityReservationID | CapacityReservation | api_model | gap | 0.95 |
| LaunchTemplate | ⚠️ groupID | data.placement.groupID | SecurityGroup | api_model | gap | 0.90 |
| LaunchTemplate | ⚠️ networkInterfaceID | data.networkInterfaces.networkInterfaceID | NetworkInterface | api_model | gap | 0.95 |
| LaunchTemplate | ⚠️ securityGroupIDs | data.securityGroupIDs | SecurityGroup | api_model | gap | 0.95 |
| LaunchTemplate | ⚠️ subnetID | data.networkInterfaces.subnetID | Subnet | api_model | gap | 0.95 |
| NATGateway | ⚠️ allocationID | allocationID | ElasticIp | api_model | gap | 0.85 |
| NATGateway | ⚠️ subnetID | subnetID | Subnet | api_model | gap | 0.95 |
| NATGateway | ⚠️ vpcID | vpcID | Vpc | api_model | gap | 0.95 |
| NetworkACL | ⚠️ subnetID | associations.subnetID | Subnet | api_model | gap | 0.95 |
| NetworkACL | ⚠️ vpcID | vpcID | Vpc | api_model | gap | 0.95 |
| RouteTable | ⚠️ carrierGatewayID | routes.carrierGatewayID | CarrierGateway | api_model | gap | 0.95 |
| RouteTable | ⚠️ coreNetworkARN | routes.coreNetworkARN | CoreNetwork | api_model | gap | 0.95 |
| RouteTable | ⚠️ destinationPrefixListID | routes.destinationPrefixListID | PrefixList | api_model | gap | 0.85 |
| RouteTable | ⚠️ egressOnlyInternetGatewayID | routes.egressOnlyInternetGatewayID | EgressOnlyInternetGateway | api_model | gap | 0.95 |
| RouteTable | gatewayID | routes.gatewayID | aws_ec2_internet_gateway → ec2/InternetGateway | generator_yaml | annotated | 1.00 |
| RouteTable | ⚠️ instanceID | routes.instanceID | Instance | api_model | gap | 0.95 |
| RouteTable | ⚠️ localGatewayID | routes.localGatewayID | LocalGateway | api_model | gap | 0.95 |
| RouteTable | natGatewayID | routes.natGatewayID | aws_ec2_n_a_t_gateway → ec2/NATGateway | generator_yaml | annotated | 1.00 |
| RouteTable | ⚠️ networkInterfaceID | routes.networkInterfaceID | NetworkInterface | api_model | gap | 0.95 |
| RouteTable | transitGatewayID | routes.transitGatewayID | aws_ec2_transit_gateway → ec2/TransitGateway | generator_yaml | annotated | 1.00 |
| RouteTable | vpcEndpointID | routes.vpcEndpointID | aws_ec2_v_p_c_endpoint → ec2/VPCEndpoint | generator_yaml | annotated | 1.00 |
| RouteTable | vpcID | vpcID | aws_ec2_v_p_c → ec2/VPC | generator_yaml | annotated | 1.00 |
| RouteTable | vpcPeeringConnectionID | routes.vpcPeeringConnectionID | aws_ec2_v_p_c_peering_connection → ec2/VPCPeeringConnection | generator_yaml | annotated | 1.00 |
| SecurityGroup | groupID | egressRules.userIDGroupPairs.groupID | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| SecurityGroup | ⚠️ prefixListID | egressRules.prefixListIDs.prefixListID | PrefixList | api_model | gap | 0.95 |
| SecurityGroup | vpcID | egressRules.userIDGroupPairs.vpcID | aws_ec2_v_p_c → ec2/VPC | generator_yaml | annotated | 1.00 |
| SecurityGroup | ⚠️ vpcPeeringConnectionID | egressRules.userIDGroupPairs.vpcPeeringConnectionID | VpcPeeringConnection | api_model | gap | 0.95 |
| Subnet | routeTables | routeTables | aws_ec2_route_table → ec2/RouteTable | generator_yaml | annotated | 1.00 |
| Subnet | vpcID | vpcID | aws_ec2_v_p_c → ec2/VPC | generator_yaml | annotated | 1.00 |
| TransitGatewayVPCAttachment | ⚠️ subnetIDs | subnetIDs | Subnet | api_model | gap | 0.95 |
| TransitGatewayVPCAttachment | ⚠️ transitGatewayID | transitGatewayID | TransitGateway | api_model | gap | 0.95 |
| TransitGatewayVPCAttachment | ⚠️ vpcID | vpcID | Vpc | api_model | gap | 0.95 |
| VPC | ⚠️ ipv6IPAMPoolID | ipv6IPAMPoolID | IpamPool | api_model | gap | 0.95 |
| VPCEndpoint | ⚠️ routeTableIDs | routeTableIDs | RouteTable | api_model | gap | 0.85 |
| VPCEndpoint | ⚠️ securityGroupIDs | securityGroupIDs | SecurityGroup | api_model | gap | 0.95 |
| VPCEndpoint | ⚠️ subnetIDs | subnetIDs | Subnet | api_model | gap | 0.95 |
| VPCEndpoint | ⚠️ vpcID | vpcID | Vpc | api_model | gap | 0.95 |
| VPCEndpointServiceConfiguration | ⚠️ gatewayLoadBalancerARNs | gatewayLoadBalancerARNs | LoadBalancer | api_model | gap | 0.95 |
| VPCEndpointServiceConfiguration | ⚠️ networkLoadBalancerARNs | networkLoadBalancerARNs | LoadBalancer | api_model | gap | 0.95 |
| VPCPeeringConnection | ⚠️ peerVPCID | peerVPCID | Vpc | api_model | gap | 0.80 |
| VPCPeeringConnection | ⚠️ vpcID | vpcID | Vpc | api_model | gap | 0.85 |

### ecr

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| PullThroughCacheRule | credentialARN | credentialARN | aws_secretsmanager_secret → secretsmanager/Secret | generator_yaml | annotated | 1.00 |
| PullThroughCacheRule | customRoleARN | customRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| PullThroughCacheRule | ⚠️ registryID | registryID | Account | api_model | gap | 0.95 |
| Repository | ⚠️ kmsKey | encryptionConfiguration.kmsKey | Key | api_model | gap | 0.80 |
| Repository | ⚠️ registryID | registryID | Account | api_model | gap | 0.80 |
| RepositoryCreationTemplate | customRoleARN | customRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| RepositoryCreationTemplate | ⚠️ kmsKey | encryptionConfiguration.kmsKey | Key | api_model | gap | 0.80 |

### ecs

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| CapacityProvider | autoScalingGroupARN | autoScalingGroupProvider.autoScalingGroupARN | aws_autoscaling_auto_scaling_group → autoscaling/AutoScalingGroup | generator_yaml | annotated | 1.00 |
| CapacityProvider | cluster | cluster | aws_ecs_cluster → ecs/Cluster | generator_yaml | annotated | 1.00 |
| CapacityProvider | ec2InstanceProfileARN | managedInstancesProvider.instanceLaunchTemplate.ec2InstanceProfileARN | aws_iam_instance_profile → iam/InstanceProfile | generator_yaml | annotated | 1.00 |
| CapacityProvider | infrastructureRoleARN | managedInstancesProvider.infrastructureRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| CapacityProvider | securityGroups | managedInstancesProvider.instanceLaunchTemplate.networkConfiguration.securityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| CapacityProvider | subnets | managedInstancesProvider.instanceLaunchTemplate.networkConfiguration.subnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| Service | cluster | cluster | aws_ecs_cluster → ecs/Cluster | generator_yaml | annotated | 1.00 |
| Service | loadBalancerName | loadBalancers.loadBalancerName | aws_elbv2_load_balancer → elbv2/LoadBalancer | generator_yaml | annotated | 1.00 |
| Service | role | role | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Service | securityGroups | networkConfiguration.awsVPCConfiguration.securityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Service | subnets | networkConfiguration.awsVPCConfiguration.subnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| Service | targetGroupARN | loadBalancers.targetGroupARN | aws_elbv2_target_group → elbv2/TargetGroup | generator_yaml | annotated | 1.00 |
| Service | taskDefinition | taskDefinition | aws_ecs_task_definition → ecs/TaskDefinition | generator_yaml | annotated | 1.00 |
| TaskDefinition | taskRoleARN | taskRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |

### efs

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| AccessPoint | fileSystemID | fileSystemID | aws_efs_file_system → efs/FileSystem | generator_yaml | annotated | 1.00 |
| FileSystem | ⚠️ availabilityZoneName | availabilityZoneName | AvailabilityZone | api_model | gap | 0.90 |
| FileSystem | fileSystemID | replicationConfiguration.fileSystemID | aws_efs_file_system → efs/FileSystem | generator_yaml | annotated | 1.00 |
| FileSystem | kmsKeyID | kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| FileSystem | roleARN | replicationConfiguration.roleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| MountTarget | fileSystemID | fileSystemID | aws_efs_file_system → efs/FileSystem | generator_yaml | annotated | 1.00 |
| MountTarget | securityGroups | securityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| MountTarget | subnetID | subnetID | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |

### eks

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| AccessEntry | ⚠️ policyARN | accessPolicies.policyARN |  | api_model | gap | 0.90 |
| AccessEntry | ⚠️ principalARN | principalARN | Principal | api_model | gap | 0.95 |
| Addon | clusterName | clusterName | aws_eks_cluster → eks/Cluster | generator_yaml | annotated | 1.00 |
| Addon | ⚠️ roleARN | podIdentityAssociations.roleARN | Role | api_model | gap | 0.85 |
| Addon | serviceAccountRoleARN | serviceAccountRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Capability | ⚠️ role | configuration.argoCD.rbacRoleMappings.role | Role | api_model | gap | 0.75 |
| Capability | roleARN | roleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Cluster | keyARN | encryptionConfig.provider.keyARN | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Cluster | ⚠️ nodeRoleARN | computeConfig.nodeRoleARN | Role | api_model | gap | 0.95 |
| Cluster | roleARN | roleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Cluster | securityGroupIDs | resourcesVPCConfig.securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Cluster | subnetIDs | resourcesVPCConfig.subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| FargateProfile | clusterName | clusterName | aws_eks_cluster → eks/Cluster | generator_yaml | annotated | 1.00 |
| FargateProfile | podExecutionRoleARN | podExecutionRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| FargateProfile | subnets | subnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| Nodegroup | clusterName | clusterName | aws_eks_cluster → eks/Cluster | generator_yaml | annotated | 1.00 |
| Nodegroup | nodeRole | nodeRole | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Nodegroup | sourceSecurityGroups | remoteAccess.sourceSecurityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Nodegroup | subnets | subnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| PodIdentityAssociation | roleARN | roleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| PodIdentityAssociation | ⚠️ targetRoleARN | targetRoleARN | Role | api_model | gap | 0.85 |

### elasticache

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| CacheCluster | cacheParameterGroupName | cacheParameterGroupName | aws_elasticache_cache_parameter_group → elasticache/CacheParameterGroup | generator_yaml | annotated | 1.00 |
| CacheCluster | cacheSubnetGroupName | cacheSubnetGroupName | aws_elasticache_cache_subnet_group → elasticache/CacheSubnetGroup | generator_yaml | annotated | 1.00 |
| CacheCluster | notificationTopicARN | notificationTopicARN | aws_sns_topic → sns/Topic | generator_yaml | annotated | 1.00 |
| CacheCluster | ⚠️ preferredOutpostARN | preferredOutpostARN | Outpost | api_model | gap | 0.85 |
| CacheCluster | replicationGroupID | replicationGroupID | aws_elasticache_replication_group → elasticache/ReplicationGroup | generator_yaml | annotated | 1.00 |
| CacheCluster | securityGroupIDs | securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| CacheCluster | snapshotName | snapshotName | aws_elasticache_snapshot → elasticache/Snapshot | generator_yaml | annotated | 1.00 |
| CacheSubnetGroup | subnetIDs | subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| ReplicationGroup | cacheParameterGroupName | cacheParameterGroupName | aws_elasticache_cache_parameter_group → elasticache/CacheParameterGroup | generator_yaml | annotated | 1.00 |
| ReplicationGroup | cacheSubnetGroupName | cacheSubnetGroupName | aws_elasticache_cache_subnet_group → elasticache/CacheSubnetGroup | generator_yaml | annotated | 1.00 |
| ReplicationGroup | ⚠️ kmsKeyID | kmsKeyID | Key | api_model | gap | 0.90 |
| ReplicationGroup | ⚠️ primaryOutpostARN | nodeGroupConfiguration.primaryOutpostARN | Outpost | api_model | gap | 0.75 |
| ReplicationGroup | securityGroupIDs | securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| ServerlessCache | ⚠️ kmsKeyID | kmsKeyID | Key | api_model | gap | 0.80 |
| ServerlessCache | securityGroupIDs | securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| ServerlessCache | subnetIDs | subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| ServerlessCache | ⚠️ userGroupID | userGroupID | UserGroup | api_model | gap | 0.75 |
| ServerlessCacheSnapshot | kmsKeyID | kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| ServerlessCacheSnapshot | serverlessCacheName | serverlessCacheName | aws_elasticache_serverless_cache → elasticache/ServerlessCache | generator_yaml | annotated | 1.00 |
| Snapshot | ⚠️ kmsKeyID | kmsKeyID | Key | api_model | gap | 0.95 |
| Snapshot | ⚠️ replicationGroupID | replicationGroupID | ReplicationGroup | api_model | gap | 0.95 |

### elbv2

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Listener | ⚠️ certificateARN | certificates.certificateARN | Certificate | api_model | gap | 0.95 |
| Listener | targetGroupARN | defaultActions.forwardConfig.targetGroups.targetGroupARN | aws_elbv2_target_group → elbv2/TargetGroup | generator_yaml | annotated | 1.00 |
| Listener | ⚠️ trustStoreARN | mutualAuthentication.trustStoreARN | TrustStore | api_model | gap | 0.95 |
| LoadBalancer | ⚠️ allocationID | subnetMappings.allocationID | ElasticIp | api_model | gap | 0.80 |
| LoadBalancer | ⚠️ customerOwnedIPv4Pool | customerOwnedIPv4Pool | CoipPool | api_model | gap | 0.80 |
| LoadBalancer | securityGroups | securityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| LoadBalancer | subnetID | subnetMappings.subnetID | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| LoadBalancer | subnets | subnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| Rule | listenerARN | listenerARN | aws_elbv2_listener → elbv2/Listener | generator_yaml | annotated | 1.00 |
| Rule | targetGroupARN | actions.forwardConfig.targetGroups.targetGroupARN | TargetGroup | api_model | annotated | 0.85 |
| TargetGroup | vpcID | vpcID | aws_ec2_v_p_c → ec2/VPC | generator_yaml | annotated | 1.00 |

### emrcontainers

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| JobRun | ⚠️ executionRoleARN | executionRoleARN | Role | api_model | gap | 0.95 |
| JobRun | virtualClusterID | virtualClusterID | aws_emrcontainers_virtual_cluster → emrcontainers/VirtualCluster | generator_yaml | annotated | 1.00 |
| VirtualCluster | ⚠️ id | containerProvider.id | Cluster | api_model | gap | 0.70 |

### emrserverless

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Application | encryptionKeyARN | diskEncryptionConfiguration.encryptionKeyARN | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Application | ⚠️ identityCenterInstanceARN | identityCenterConfiguration.identityCenterInstanceARN | Instance | api_model | gap | 0.95 |
| Application | securityGroupIDs | networkConfiguration.securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Application | subnetIDs | networkConfiguration.subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |

### eventbridge

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Archive | eventSourceARN | eventSourceARN | aws_eventbridge_event_bus → eventbridge/EventBus | generator_yaml | annotated | 1.00 |
| Endpoint | ⚠️ eventBusARN | eventBuses.eventBusARN | EventBus | api_model | gap | 0.85 |
| Endpoint | ⚠️ roleARN | roleARN | Role | api_model | gap | 0.85 |
| Rule | ⚠️ arn | targets.arn | Queue | api_model | gap | 0.85 |
| Rule | eventBusName | eventBusName | aws_eventbridge_event_bus → eventbridge/EventBus | generator_yaml | annotated | 1.00 |
| Rule | ⚠️ roleARN | roleARN | Role | api_model | gap | 0.90 |

### firehose

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| DeliveryStream | awsKMSKeyARN | httpEndpointDestinationConfiguration.s3Configuration.encryptionConfiguration.kmsEncryptionConfig.awsKMSKeyARN | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| DeliveryStream | bucketARN | httpEndpointDestinationConfiguration.s3Configuration.bucketARN | aws_s3_bucket → s3/Bucket | generator_yaml | annotated | 1.00 |
| DeliveryStream | keyARN | deliveryStreamEncryptionConfiguration.keyARN | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| DeliveryStream | roleARN | httpEndpointDestinationConfiguration.roleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| DeliveryStream | secretARN | httpEndpointDestinationConfiguration.secretsManagerConfiguration.secretARN | aws_secretsmanager_secret → secretsmanager/Secret | generator_yaml | annotated | 1.00 |

### glue

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Job | role | role | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Job | ⚠️ securityConfiguration | securityConfiguration | SecurityConfiguration | api_model | gap | 0.95 |

### iam

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Group | policies | policies | aws_iam_policy → iam/Policy | generator_yaml | annotated | 1.00 |
| InstanceProfile | role | role | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Role | permissionsBoundary | permissionsBoundary | aws_iam_policy → iam/Policy | generator_yaml | annotated | 1.00 |
| Role | policies | policies | aws_iam_policy → iam/Policy | generator_yaml | annotated | 1.00 |
| User | permissionsBoundary | permissionsBoundary | aws_iam_policy → iam/Policy | generator_yaml | annotated | 1.00 |
| User | policies | policies | aws_iam_policy → iam/Policy | generator_yaml | annotated | 1.00 |

### kafka

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Cluster | ⚠️ arn | configurationInfo.arn | Configuration | api_model | gap | 0.85 |
| Cluster | associatedSCRAMSecrets | associatedSCRAMSecrets | aws_secretsmanager_secret → secretsmanager/Secret | generator_yaml | annotated | 1.00 |
| Cluster | clientSubnets | brokerNodeGroupInfo.clientSubnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| Cluster | ⚠️ securityGroups | brokerNodeGroupInfo.securityGroups | SecurityGroup | api_model | gap | 0.95 |
| ServerlessCluster | ⚠️ arn | provisioned.configurationInfo.arn | Configuration | api_model | gap | 0.80 |
| ServerlessCluster | associatedSCRAMSecrets | associatedSCRAMSecrets | aws_secretsmanager_secret → secretsmanager/Secret | generator_yaml | annotated | 1.00 |
| ServerlessCluster | clientSubnets | provisioned.brokerNodeGroupInfo.clientSubnets | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| ServerlessCluster | ⚠️ securityGroups | provisioned.brokerNodeGroupInfo.securityGroups | SecurityGroup | api_model | gap | 0.80 |

### keyspaces

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Table | ⚠️ kmsKeyIdentifier | encryptionSpecification.kmsKeyIdentifier | Key | api_model | gap | 0.95 |

### kms

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Alias | TargetKeyID | spec.targetKeyID | Key | api_model | annotated | 0.95 |
| Alias | targetKeyID | targetKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Grant | keyID | keyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Key | ⚠️ customKeyStoreID | customKeyStoreID | CustomKeyStore | api_model | gap | 0.80 |

### lambda

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Alias | functionName | functionEventInvokeConfig.functionName | aws_lambda_function → lambda/Function | generator_yaml | annotated | 1.00 |
| Alias | ⚠️ sourceARN | permissions.sourceARN |  | api_model | gap | 0.85 |
| CodeSigningConfig | ⚠️ signingProfileVersionARNs | allowedPublishers.signingProfileVersionARNs | SigningProfile | api_model | gap | 0.95 |
| EventSourceMapping | eventSourceARN | eventSourceARN | aws_kafka_cluster → kafka/Cluster | generator_yaml | annotated | 1.00 |
| EventSourceMapping | functionName | functionName | aws_lambda_function → lambda/Function | generator_yaml | annotated | 1.00 |
| EventSourceMapping | queues | queues | aws_mq_broker → mq/Broker | generator_yaml | annotated | 1.00 |
| EventSourceMapping | uri | amazonManagedKafkaEventSourceConfig.schemaRegistryConfig.accessConfigs.uri | aws_secretsmanager_secret → secretsmanager/Secret | generator_yaml | annotated | 1.00 |
| Function | ⚠️ codeSigningConfigARN | codeSigningConfigARN | CodeSigningConfig | api_model | gap | 0.95 |
| Function | ⚠️ functionName | functionEventInvokeConfig.functionName | Function | api_model | gap | 0.85 |
| Function | kmsKeyARN | kmsKeyARN | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Function | role | role | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Function | s3Bucket | code.s3Bucket | aws_s3_bucket → s3/Bucket | generator_yaml | annotated | 1.00 |
| Function | securityGroupIDs | vpcConfig.securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Function | subnetIDs | vpcConfig.subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| FunctionURLConfig | ⚠️ functionName | functionName | Function | api_model | gap | 0.95 |

### memorydb

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| ACL | userNames | userNames | aws_memorydb_user → memorydb/User | generator_yaml | annotated | 1.00 |
| Cluster | aclName | aclName | aws_memorydb_a_c_l → memorydb/ACL | generator_yaml | annotated | 1.00 |
| Cluster | ⚠️ kmsKeyID | kmsKeyID | Key | api_model | gap | 0.95 |
| Cluster | parameterGroupName | parameterGroupName | aws_memorydb_parameter_group → memorydb/ParameterGroup | generator_yaml | annotated | 1.00 |
| Cluster | securityGroupIDs | securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Cluster | ⚠️ snapshotARNs | snapshotARNs | Snapshot | api_model | gap | 0.95 |
| Cluster | snapshotName | snapshotName | aws_memorydb_snapshot → memorydb/Snapshot | generator_yaml | annotated | 1.00 |
| Cluster | snsTopicARN | snsTopicARN | aws_sns_topic → sns/Topic | generator_yaml | annotated | 1.00 |
| Cluster | subnetGroupName | subnetGroupName | aws_memorydb_subnet_group → memorydb/SubnetGroup | generator_yaml | annotated | 1.00 |
| Snapshot | clusterName | clusterName | aws_memorydb_cluster → memorydb/Cluster | generator_yaml | annotated | 1.00 |
| Snapshot | kmsKeyID | kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| SubnetGroup | subnetIDs | subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |

### mq

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Broker | ⚠️ kmsKeyID | encryptionOptions.kmsKeyID | Key | api_model | gap | 0.95 |
| Broker | securityGroups | securityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Broker | subnetIDs | subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |

### mwaa

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Environment | executionRoleARN | executionRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| Environment | kmsKey | kmsKey | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Environment | securityGroupIDs | networkConfiguration.securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Environment | sourceBucketARN | sourceBucketARN | aws_s3_bucket → s3/Bucket | generator_yaml | annotated | 1.00 |
| Environment | subnetIDs | networkConfiguration.subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |

### networkfirewall

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Firewall | ⚠️ firewallPolicyARN | firewallPolicyARN | FirewallPolicy | api_model | gap | 0.95 |
| Firewall | ⚠️ subnetID | subnetMappings.subnetID | Subnet | api_model | gap | 0.90 |
| Firewall | ⚠️ vpcID | vpcID | Vpc | api_model | gap | 0.95 |
| FirewallPolicy | ⚠️ resourceARN | firewallPolicy.statefulRuleGroupReferences.resourceARN | FirewallPolicy | api_model | gap | 0.75 |
| FirewallPolicy | ⚠️ tlsInspectionConfigurationARN | firewallPolicy.tlsInspectionConfigurationARN | ProxyConfiguration | api_model | gap | 0.70 |
| RuleGroup | ⚠️ sourceARN | sourceMetadata.sourceARN | Firewall | api_model | gap | 0.70 |

### opensearchservice

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Domain | ⚠️ identityPoolID | cognitoOptions.identityPoolID | IdentityPool | api_model | gap | 0.80 |
| Domain | ⚠️ roleARN | cognitoOptions.roleARN | Role | api_model | gap | 0.85 |
| Domain | securityGroupIDs | vpcOptions.securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| Domain | subnetIDs | vpcOptions.subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| Domain | ⚠️ userPoolID | cognitoOptions.userPoolID | UserPool | api_model | gap | 0.80 |

### organizations

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| OrganizationalUnit | ⚠️ parentID | parentID | Root/OrganizationalUnit | api_model | gap | 0.95 |

### pipes

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Pipe | ⚠️ arn | sourceParameters.dynamoDBStreamParameters.deadLetterConfig.arn |  | api_model | gap | 0.85 |
| Pipe | ⚠️ enrichment | enrichment |  | api_model | gap | 0.85 |
| Pipe | ⚠️ executionRoleARN | targetParameters.ecsTaskParameters.overrides.executionRoleARN | Role | api_model | gap | 0.85 |
| Pipe | ⚠️ roleARN | roleARN | Role | api_model | gap | 0.85 |
| Pipe | ⚠️ securityGroups | targetParameters.ecsTaskParameters.networkConfiguration.awsVPCConfiguration.securityGroups | SecurityGroup | api_model | gap | 0.80 |
| Pipe | ⚠️ source | source |  | api_model | gap | 0.85 |
| Pipe | ⚠️ subnets | sourceParameters.selfManagedKafkaParameters.vpc.subnets | Subnet | api_model | gap | 0.80 |
| Pipe | ⚠️ target | target |  | api_model | gap | 0.85 |
| Pipe | ⚠️ taskRoleARN | targetParameters.ecsTaskParameters.overrides.taskRoleARN | Role | api_model | gap | 0.85 |

### prometheusservice

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| LoggingConfiguration | ⚠️ logGroupARN | logGroupARN | LogGroup | api_model | gap | 1.00 |
| RuleGroupsNamespace | workspaceID | workspaceID | aws_prometheusservice_workspace → prometheusservice/Workspace | generator_yaml | annotated | 1.00 |

### quicksight

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Analysis | ⚠️ arn | sourceEntity.sourceTemplate.arn | Template | api_model | gap | 0.85 |
| Analysis | dataSetARN | sourceEntity.sourceTemplate.dataSetReferences.dataSetARN | aws_quicksight_data_set → quicksight/DataSet | generator_yaml | annotated | 1.00 |
| Analysis | ⚠️ folderARNs | folderARNs | Folder | api_model | gap | 0.95 |
| Analysis | ⚠️ themeARN | themeARN | Theme | api_model | gap | 0.95 |
| Dashboard | ⚠️ arn | sourceEntity.sourceTemplate.arn | Template | api_model | gap | 0.85 |
| Dashboard | dataSetARN | sourceEntity.sourceTemplate.dataSetReferences.dataSetARN | aws_quicksight_data_set → quicksight/DataSet | generator_yaml | annotated | 1.00 |
| Dashboard | ⚠️ folderARNs | folderARNs | Folder | api_model | gap | 0.95 |
| Dashboard | linkEntities | linkEntities | aws_quicksight_analysis → quicksight/Analysis | generator_yaml | annotated | 1.00 |
| Dashboard | ⚠️ themeARN | themeARN | Theme | api_model | gap | 0.95 |
| DataSet | ⚠️ folderARNs | folderARNs | Folder | api_model | gap | 0.95 |
| DataSource | bucket | parameters.s3Parameters.manifestFileLocation.bucket | aws_s3_bucket → s3/Bucket | generator_yaml | annotated | 1.00 |
| DataSource | ⚠️ copySourceARN | credentials.copySourceARN | Secret | api_model | gap | 0.70 |
| DataSource | ⚠️ folderARNs | folderARNs | Folder | api_model | gap | 0.85 |
| DataSource | instanceID | parameters.rdsParameters.instanceID | aws_rds_d_b_instance → rds/DBInstance | generator_yaml | annotated | 1.00 |
| DataSource | roleARN | parameters.athenaParameters.roleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| DataSource | secretARN | credentials.secretARN | aws_secretsmanager_secret → secretsmanager/Secret | generator_yaml | annotated | 1.00 |

### ram

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| ResourceShare | permissionARNs | permissionARNs | aws_ram_permission → ram/Permission | generator_yaml | annotated | 1.00 |
| ResourceShare | ⚠️ principals | principals |  | api_model | gap | 0.95 |
| ResourceShare | ⚠️ resourceARNs | resourceARNs |  | api_model | gap | 0.95 |
| ResourceShare | ⚠️ sources | sources |  | api_model | gap | 0.95 |

### rds

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| DBCluster | dbClusterParameterGroupName | dbClusterParameterGroupName | aws_rds_d_b_cluster_parameter_group → rds/DBClusterParameterGroup | generator_yaml | annotated | 1.00 |
| DBCluster | dbSubnetGroupName | dbSubnetGroupName | aws_rds_d_b_subnet_group → rds/DBSubnetGroup | generator_yaml | annotated | 1.00 |
| DBCluster | kmsKeyID | kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| DBCluster | masterUserSecretKMSKeyID | masterUserSecretKMSKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| DBCluster | ⚠️ monitoringRoleARN | monitoringRoleARN | Role | api_model | gap | 0.85 |
| DBCluster | ⚠️ optionGroupName | optionGroupName | OptionGroup | api_model | gap | 0.60 |
| DBCluster | ⚠️ performanceInsightsKMSKeyID | performanceInsightsKMSKeyID | Key | api_model | gap | 0.80 |
| DBCluster | vpcSecurityGroupIDs | vpcSecurityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| DBClusterEndpoint | dbClusterIdentifier | dbClusterIdentifier | aws_rds_d_b_cluster → rds/DBCluster | generator_yaml | annotated | 1.00 |
| DBClusterSnapshot | dbClusterIdentifier | dbClusterIdentifier | aws_rds_d_b_cluster → rds/DBCluster | generator_yaml | annotated | 1.00 |
| DBInstance | dbParameterGroupName | dbParameterGroupName | aws_rds_d_b_parameter_group → rds/DBParameterGroup | generator_yaml | annotated | 1.00 |
| DBInstance | dbSubnetGroupName | dbSubnetGroupName | aws_rds_d_b_subnet_group → rds/DBSubnetGroup | generator_yaml | annotated | 1.00 |
| DBInstance | kmsKeyID | kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| DBInstance | masterUserSecretKMSKeyID | masterUserSecretKMSKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| DBInstance | ⚠️ monitoringRoleARN | monitoringRoleARN | Role | api_model | gap | 0.85 |
| DBInstance | ⚠️ optionGroupName | optionGroupName | OptionGroup | api_model | gap | 0.95 |
| DBInstance | ⚠️ performanceInsightsKMSKeyID | performanceInsightsKMSKeyID | Key | api_model | gap | 0.80 |
| DBInstance | vpcSecurityGroupIDs | vpcSecurityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| DBProxy | ⚠️ roleARN | roleARN | Role | api_model | gap | 0.95 |
| DBSnapshot | dbInstanceIdentifier | dbInstanceIdentifier | aws_rds_d_b_instance → rds/DBInstance | generator_yaml | annotated | 1.00 |
| DBSubnetGroup | subnetIDs | subnetIDs | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |

### route53

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| HealthCheck | ⚠️ name | healthCheckConfig.alarmIdentifier.name | Alarm | api_model | gap | 0.60 |
| HostedZone | ⚠️ delegationSetID | delegationSetID | CidrCollection | api_model | gap | 0.70 |
| RecordSet | ⚠️ collectionID | cidrRoutingConfig.collectionID | CidrCollection | api_model | gap | 0.80 |
| RecordSet | healthCheckID | healthCheckID | aws_route53_health_check → route53/HealthCheck | generator_yaml | annotated | 1.00 |
| RecordSet | hostedZoneID | aliasTarget.hostedZoneID | aws_route53_hosted_zone → route53/HostedZone | generator_yaml | annotated | 1.00 |

### route53resolver

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| ResolverEndpoint | securityGroupIDs | securityGroupIDs | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| ResolverEndpoint | subnetID | ipAddresses.subnetID | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |
| ResolverQueryLogConfig | ⚠️ destinationARN | destinationARN |  | api_model | gap | 0.85 |
| ResolverQueryLogConfigAssociation | ResolverQueryLogConfigID | spec.resolverQueryLogConfigID | ResolverQueryLogConfig | api_model | annotated | 0.95 |
| ResolverQueryLogConfigAssociation | ResourceID | spec.resourceID | VPC | api_model | annotated | 0.95 |
| ResolverQueryLogConfigAssociation | resolverQueryLogConfigID | resolverQueryLogConfigID | aws_route53resolver_resolver_query_log_config → route53resolver/ResolverQueryLogConfig | generator_yaml | annotated | 1.00 |
| ResolverQueryLogConfigAssociation | resourceID | resourceID | aws_ec2_v_p_c → ec2/VPC | generator_yaml | annotated | 1.00 |
| ResolverRule | ⚠️ resolverEndpointID | resolverEndpointID | ResolverEndpoint | api_model | gap | 0.95 |
| ResolverRule | ⚠️ resolverRuleID | associations.resolverRuleID | ResolverRule | api_model | gap | 0.90 |
| ResolverRule | ⚠️ vpcID | associations.vpcID | VPC | api_model | gap | 0.90 |
| ResolverRuleAssociation | ResolverRuleID | ResolverRuleID | ResolverRule | api_model | annotated | 0.95 |
| ResolverRuleAssociation | VPCID | VPCID | VPC | api_model | annotated | 0.95 |
| ResolverRuleAssociation | resolverRuleID | resolverRuleID | aws_route53resolver_resolver_rule → route53resolver/ResolverRule | generator_yaml | annotated | 1.00 |
| ResolverRuleAssociation | vpcID | vpcID | aws_ec2_v_p_c → ec2/VPC | generator_yaml | annotated | 1.00 |

### s3

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Bucket | ⚠️ bucket | analytics.storageClassAnalysis.dataExport.destination.s3BucketDestination.bucket | Bucket | api_model | gap | 0.60 |
| Bucket | ⚠️ bucketAccountID | analytics.storageClassAnalysis.dataExport.destination.s3BucketDestination.bucketAccountID | Account | api_model | gap | 0.80 |
| Bucket | ⚠️ role | replication.role | Role | api_model | gap | 0.60 |

### s3control

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| AccessPoint | ⚠️ accountID | accountID | Account | api_model | gap | 0.80 |
| AccessPoint | ⚠️ bucket | bucket | Bucket | api_model | gap | 0.80 |
| AccessPoint | ⚠️ bucketAccountID | bucketAccountID | Account | api_model | gap | 0.80 |

### s3files

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| AccessPoint | fileSystemID | fileSystemID | aws_s3files_file_system → s3files/FileSystem | generator_yaml | annotated | 1.00 |
| FileSystem | bucket | bucket | aws_s3_bucket → s3/Bucket | generator_yaml | annotated | 1.00 |
| FileSystem | kmsKeyID | kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| FileSystem | roleARN | roleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| MountTarget | fileSystemID | fileSystemID | aws_s3files_file_system → s3files/FileSystem | generator_yaml | annotated | 1.00 |
| MountTarget | securityGroups | securityGroups | aws_ec2_security_group → ec2/SecurityGroup | generator_yaml | annotated | 1.00 |
| MountTarget | subnetID | subnetID | aws_ec2_subnet → ec2/Subnet | generator_yaml | annotated | 1.00 |

### s3tables

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Namespace | tableBucketARN | tableBucketARN | aws_s3tables_table_bucket → s3tables/TableBucket | generator_yaml | annotated | 1.00 |
| TableBucket | kmsKeyARN | encryptionConfiguration.kmsKeyARN | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |

### sagemaker

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| DataQualityJobDefinition | ⚠️ endpointName | dataQualityJobInput.endpointInput.endpointName | Endpoint | api_model | gap | 0.80 |
| Domain | ⚠️ appImageConfigName | defaultSpaceSettings.jupyterLabAppSettings.customImages.appImageConfigName | AppImageConfig | api_model | gap | 0.75 |
| Endpoint | ⚠️ alarmName | deploymentConfig.autoRollbackConfiguration.alarms.alarmName | Algorithm | api_model | gap | 0.30 |
| EndpointConfig | ⚠️ modelName | productionVariants.modelName | ModelPackage | api_model | gap | 0.75 |
| FeatureGroup | ⚠️ kmsKeyID | offlineStoreConfig.s3StorageConfig.kmsKeyID | Key | api_model | gap | 0.85 |
| HyperParameterTuningJob | ⚠️ algorithmName | trainingJobDefinition.algorithmSpecification.algorithmName | Algorithm | api_model | gap | 0.95 |
| InferenceComponent | ⚠️ endpointName | endpointName | Endpoint | api_model | gap | 0.80 |
| LabelingJob | ⚠️ initialActiveLearningModelARN | labelingJobAlgorithmsConfig.initialActiveLearningModelARN | ModelPackage | api_model | gap | 0.85 |
| LabelingJob | ⚠️ labelingJobAlgorithmSpecificationARN | labelingJobAlgorithmsConfig.labelingJobAlgorithmSpecificationARN | Algorithm | api_model | gap | 0.60 |
| Model | ⚠️ modelPackageName | containers.modelPackageName | ModelPackage | api_model | gap | 0.75 |
| ModelBiasJobDefinition | ⚠️ endpointName | modelBiasJobInput.endpointInput.endpointName | Endpoint | api_model | gap | 0.80 |
| ModelExplainabilityJobDefinition | ⚠️ endpointName | modelExplainabilityJobInput.endpointInput.endpointName | Endpoint | api_model | gap | 0.80 |
| ModelPackage | ⚠️ algorithmName | sourceAlgorithmSpecification.sourceAlgorithms.algorithmName | Algorithm | api_model | gap | 0.60 |
| ModelQualityJobDefinition | ⚠️ endpointName | modelQualityJobInput.endpointInput.endpointName | Endpoint | api_model | gap | 0.80 |
| MonitoringSchedule | ⚠️ endpointName | monitoringScheduleConfig.monitoringJobDefinition.monitoringInputs.endpointInput.endpointName | Endpoint | api_model | gap | 0.80 |
| NotebookInstance | ⚠️ kmsKeyID | kmsKeyID |  | api_model | gap | 0.90 |
| Pipeline | ⚠️ pipelineDefinition | pipelineDefinition | ModelPackage | api_model | gap | 0.30 |
| ProcessingJob | ⚠️ imageURI | appSpecification.imageURI | ModelPackage | api_model | gap | 0.40 |
| Space | ⚠️ appImageConfigName | spaceSettings.kernelGatewayAppSettings.customImages.appImageConfigName | AppImageConfig | api_model | gap | 0.75 |
| TrainingJob | ⚠️ algorithmName | algorithmSpecification.algorithmName | Algorithm | api_model | gap | 0.60 |
| TransformJob | ⚠️ modelName | modelName | ModelPackage | api_model | gap | 0.75 |
| UserProfile | ⚠️ appImageConfigName | userSettings.jupyterLabAppSettings.customImages.appImageConfigName | AppImageConfig | api_model | gap | 0.75 |

### secretsmanager

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Secret | kmsKeyID | kmsKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |

### sfn

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| StateMachine | ⚠️ roleARN | roleARN | Role | api_model | gap | 0.95 |
| StateMachineAlias | ⚠️ stateMachineVersionARN | routingConfiguration.stateMachineVersionARN | StateMachineVersion | api_model | gap | 0.95 |

### sns

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| PlatformApplication | ⚠️ eventDeliveryFailure | eventDeliveryFailure | Topic | api_model | gap | 0.95 |
| PlatformApplication | eventEndpointCreated | eventEndpointCreated | aws_sns_topic → sns/Topic | generator_yaml | annotated | 1.00 |
| PlatformApplication | eventEndpointDeleted | eventEndpointDeleted | aws_sns_topic → sns/Topic | generator_yaml | annotated | 1.00 |
| PlatformApplication | eventEndpointUpdated | eventEndpointUpdated | aws_sns_topic → sns/Topic | generator_yaml | annotated | 1.00 |
| PlatformApplication | failureFeedbackRoleARN | failureFeedbackRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| PlatformApplication | successFeedbackRoleARN | successFeedbackRoleARN | aws_iam_role → iam/Role | generator_yaml | annotated | 1.00 |
| PlatformEndpoint | ⚠️ platformApplicationARN | platformApplicationARN | PlatformApplication | api_model | gap | 0.95 |
| Subscription | ⚠️ subscriptionRoleARN | subscriptionRoleARN | Role | api_model | gap | 0.85 |
| Subscription | topicARN | topicARN | aws_sns_topic → sns/Topic | generator_yaml | annotated | 1.00 |
| Topic | ⚠️ applicationFailureFeedbackRoleARN | applicationFailureFeedbackRoleARN | Role | api_model | gap | 0.95 |
| Topic | ⚠️ applicationSuccessFeedbackRoleARN | applicationSuccessFeedbackRoleARN | Role | api_model | gap | 0.95 |
| Topic | ⚠️ firehoseFailureFeedbackRoleARN | firehoseFailureFeedbackRoleARN | Role | api_model | gap | 0.95 |
| Topic | ⚠️ firehoseSuccessFeedbackRoleARN | firehoseSuccessFeedbackRoleARN | Role | api_model | gap | 0.95 |
| Topic | ⚠️ httpFailureFeedbackRoleARN | httpFailureFeedbackRoleARN | Role | api_model | gap | 0.95 |
| Topic | ⚠️ httpSuccessFeedbackRoleARN | httpSuccessFeedbackRoleARN | Role | api_model | gap | 0.95 |
| Topic | kmsMasterKeyID | kmsMasterKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Topic | ⚠️ lambdaFailureFeedbackRoleARN | lambdaFailureFeedbackRoleARN | Role | api_model | gap | 0.95 |
| Topic | ⚠️ lambdaSuccessFeedbackRoleARN | lambdaSuccessFeedbackRoleARN | Role | api_model | gap | 0.95 |
| Topic | policy | policy | aws_iam_policy → iam/Policy | generator_yaml | annotated | 1.00 |
| Topic | ⚠️ sqsFailureFeedbackRoleARN | sqsFailureFeedbackRoleARN | Role | api_model | gap | 0.95 |
| Topic | ⚠️ sqsSuccessFeedbackRoleARN | sqsSuccessFeedbackRoleARN | Role | api_model | gap | 0.95 |

### sqs

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Queue | kmsMasterKeyID | kmsMasterKeyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |
| Queue | policy | policy | aws_iam_policy → iam/Policy | generator_yaml | annotated | 1.00 |

### ssm

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| Document | ⚠️ name | requires.name | Document | api_model | gap | 0.60 |
| Parameter | keyID | keyID | aws_kms_key → kms/Key | generator_yaml | annotated | 1.00 |

### wafv2

| Resource | ACK Field | Field Path | Target | Sources | Status | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| RuleGroup | ⚠️ arn | rules.statement.ipSetReferenceStatement.arn | WebACL | api_model | gap | 0.75 |
| WebACL | ⚠️ resourceARN | loggingConfiguration.resourceARN |  | api_model | gap | 0.95 |

