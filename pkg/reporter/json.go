package reporter

import (
	"encoding/json"
	"io"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// FormatJSON writes the gap report as indented JSON to the writer.
func FormatJSON(report *types.GapReport, w io.Writer) error {
	// Ensure entries is serialized as [] not null when empty
	if report.Entries == nil {
		report.Entries = []types.GapReportEntry{}
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
