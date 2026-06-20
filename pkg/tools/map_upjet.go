package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/agent"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/cache"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/framework"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/logger"
	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

const mapUpjetTool = "map_upjet"

// UpjetMapping is the agent's mapping result for a single ACK controller
// to Upjet config files.
type UpjetMapping struct {
	ServiceName   string              `json:"service_name"`
	UpjetConfigs  []UpjetMappingEntry `json:"upjet_configs"`
	NoMatchReason string              `json:"no_match_reason,omitempty"`
}

// UpjetMappingEntry is a single controller-to-Upjet-config association.
type UpjetMappingEntry struct {
	UpjetService string  `json:"upjet_service"`
	FilePath     string  `json:"file_path"`
	Confidence   float64 `json:"confidence"`
}

// MapAllUpjetOutput is the aggregated mapping result for all controllers.
type MapAllUpjetOutput struct {
	Mappings []UpjetMapping `json:"mappings"`
	Skipped  []string       `json:"skipped,omitempty"`
}

// MapControllerToUpjet invokes the agent to map a single ACK controller to its
// corresponding Upjet configuration files. It uses the framework.MapOne generic
// function with a mapping config specific to Upjet.
func MapControllerToUpjet(
	ctx context.Context,
	ag *agent.Agent,
	controller types.ControllerInfo,
	upjetConfigs []UpjetConfigInfo,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	log ...*logger.Logger,
) (*UpjetMapping, error) {
	l := resolveLogger(log)

	config := buildUpjetMappingConfig()

	result, err := framework.MapOne(ctx, config, ag, controller, upjetConfigs, resultCache, validator, l)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// MapAllControllersToUpjet orchestrates mapping all controllers to Upjet configs
// with bounded concurrency. It uses the framework.MapAll generic function.
func MapAllControllersToUpjet(
	ctx context.Context,
	ag *agent.Agent,
	controllers []types.ControllerInfo,
	upjetConfigs []UpjetConfigInfo,
	resultCache *cache.ResultCache,
	validator agent.ResponseValidator,
	maxParallel int,
	log ...*logger.Logger,
) (*MapAllUpjetOutput, error) {
	l := resolveLogger(log)

	config := buildUpjetMappingConfig()

	frameworkResult, err := framework.MapAll(ctx, config, ag, controllers, upjetConfigs, resultCache, validator, maxParallel, l)
	if err != nil {
		return nil, err
	}

	// Convert framework result to our output type
	output := &MapAllUpjetOutput{
		Skipped: frameworkResult.Skipped,
	}

	// Preserve ordering from input controllers
	for _, ctrl := range controllers {
		if mapping, ok := frameworkResult.Results[ctrl.ServiceName]; ok {
			output.Mappings = append(output.Mappings, mapping)
		}
	}

	return output, nil
}

// buildUpjetMappingConfig returns the framework.MappingConfig for Controller-to-Upjet mapping.
func buildUpjetMappingConfig() framework.MappingConfig[UpjetConfigInfo, UpjetMapping] {
	return framework.MappingConfig[UpjetConfigInfo, UpjetMapping]{
		ToolName:    mapUpjetTool,
		BuildPrompt: buildMapUpjetPrompt,
		ParseResult: parseUpjetMappingResult,
		ItemKey: func(controller types.ControllerInfo) string {
			return controller.ServiceName
		},
		InputParams: buildUpjetInputParams,
	}
}

// buildMapUpjetPrompt constructs the prompt sent to the agent for mapping a
// single ACK controller to Upjet configuration files.
func buildMapUpjetPrompt(controller types.ControllerInfo, upjetConfigs []UpjetConfigInfo) string {
	var sb strings.Builder

	sb.WriteString("You are mapping an ACK (AWS Controllers for Kubernetes) controller to its corresponding Upjet/Crossplane AWS provider configuration files.\n\n")

	sb.WriteString("## ACK Controller\n")
	sb.WriteString(fmt.Sprintf("Service Name: %s\n", controller.ServiceName))
	sb.WriteString("Resource Kinds:\n")
	for _, r := range controller.Resources {
		sb.WriteString(fmt.Sprintf("  - %s\n", r.Kind))
	}

	sb.WriteString("\n## Upjet Config Services\n")
	sb.WriteString("Below is the complete list of Upjet AWS provider config directory names (each corresponds to a service):\n")
	for _, cfg := range upjetConfigs {
		sb.WriteString(fmt.Sprintf("  - %s (file: %s)\n", cfg.ServiceName, cfg.FilePath))
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Map the ACK controller to the Upjet config files that correspond to the same AWS service.\n")
	sb.WriteString("Use semantic understanding to resolve naming convention differences between ACK service names and Upjet directory names.\n")
	sb.WriteString("Common naming differences include:\n")
	sb.WriteString("  - ACK 'applicationautoscaling' → Upjet 'autoscaling'\n")
	sb.WriteString("  - ACK 'elasticloadbalancingv2' → Upjet 'elbv2' or 'elb'\n")
	sb.WriteString("  - ACK 'opensearchservice' → Upjet 'opensearch'\n")
	sb.WriteString("  - ACK 'sfn' → Upjet 'sfn' (same)\n")
	sb.WriteString("  - ACK 'sagemaker' → Upjet 'sagemaker' (same)\n")
	sb.WriteString("A controller may map to multiple Upjet configs if the service spans multiple Upjet directories.\n")
	sb.WriteString("If there is no corresponding Upjet config, leave upjet_configs as an empty array and provide a no_match_reason.\n")
	sb.WriteString("Only include Upjet configs that genuinely correspond to this ACK controller's service.\n\n")

	sb.WriteString("## Required Output Format\n")
	sb.WriteString("Respond with ONLY valid JSON (no markdown fences, no explanation, no extra text).\n")
	sb.WriteString("The JSON must match this schema:\n")
	sb.WriteString(`{"service_name":"<the ACK controller service name>","upjet_configs":[{"upjet_service":"<upjet directory/service name>","file_path":"<exact file path from the list above>","confidence":<0.0 to 1.0>}],"no_match_reason":"<optional: reason if no Upjet configs match>"}`)
	sb.WriteString("\n")

	return sb.String()
}

// parseUpjetMappingResult parses the agent's JSON response into an UpjetMapping.
func parseUpjetMappingResult(response string) (UpjetMapping, error) {
	var result UpjetMapping
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return result, fmt.Errorf("parsing upjet mapping response: %w", err)
	}
	return result, nil
}

// buildUpjetInputParams creates the input parameters used for cache hashing.
func buildUpjetInputParams(controller types.ControllerInfo, upjetConfigs []UpjetConfigInfo) map[string]any {
	kinds := make([]string, 0, len(controller.Resources))
	for _, r := range controller.Resources {
		kinds = append(kinds, r.Kind)
	}

	upjetServices := make([]string, 0, len(upjetConfigs))
	for _, cfg := range upjetConfigs {
		upjetServices = append(upjetServices, cfg.ServiceName)
	}

	return map[string]any{
		"service_name":       controller.ServiceName,
		"resource_kinds":     kinds,
		"upjet_config_count": len(upjetConfigs),
	}
}
