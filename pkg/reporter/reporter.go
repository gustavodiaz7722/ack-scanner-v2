// Package reporter provides output formatting for gap reports.
package reporter

import (
	"fmt"
	"io"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// Format writes the gap report to the writer in the specified format.
// Supported formats: "json", "markdown", "table".
func Format(report *types.GapReport, format string, w io.Writer) error {
	if report == nil {
		return fmt.Errorf("report is nil")
	}

	switch format {
	case "json":
		return FormatJSON(report, w)
	case "markdown", "md":
		return FormatMarkdown(report, w)
	case "table", "text":
		return FormatTable(report, w)
	default:
		return fmt.Errorf("unsupported format: %q (supported: json, markdown, table)", format)
	}
}

// FormatReference writes the reference gap report to the writer in the specified format.
// Supported formats: "json", "markdown".
func FormatReference(report *types.ReferenceGapReport, format string, w io.Writer) error {
	if report == nil {
		return fmt.Errorf("report is nil")
	}

	switch format {
	case "json":
		return FormatReferenceJSON(report, w)
	case "markdown", "md":
		return FormatReferenceMarkdown(report, w)
	default:
		return fmt.Errorf("unsupported format: %q (supported: json, markdown)", format)
	}
}
