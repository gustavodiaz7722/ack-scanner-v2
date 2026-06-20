package reporter

import (
	"encoding/json"
	"io"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// FormatReferenceJSON writes the reference gap report as indented JSON to the writer.
func FormatReferenceJSON(report *types.ReferenceGapReport, w io.Writer) error {
	// Ensure entries is serialized as [] not null when empty
	if report.Entries == nil {
		report.Entries = []types.ReferenceGapEntry{}
	}
	if report.Summary.GapsPerService == nil {
		report.Summary.GapsPerService = make(map[string]int)
	}
	if report.Summary.ServicesByPriority == nil {
		report.Summary.ServicesByPriority = []types.ServicePriority{}
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}
