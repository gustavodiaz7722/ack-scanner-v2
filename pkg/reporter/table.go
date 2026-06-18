package reporter

import (
	"fmt"
	"io"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// FormatTable writes the gap report as a simple text table format.
func FormatTable(report *types.GapReport, w io.Writer) error {
	// Header
	header := fmt.Sprintf("%-20s %-20s %-30s %-30s %-15s %-12s",
		"SERVICE", "RESOURCE", "ACK FIELD", "TF FIELD", "RECOMMENDED", "STATUS")
	if _, err := fmt.Fprintln(w, header); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, strings.Repeat("-", len(header))); err != nil {
		return err
	}

	// Entries
	for _, entry := range report.Entries {
		line := fmt.Sprintf("%-20s %-20s %-30s %-30s %-15s %-12s",
			truncate(entry.ServiceName, 20),
			truncate(entry.ResourceName, 20),
			truncate(entry.ACKFieldName, 30),
			truncate(entry.TFFieldName, 30),
			truncate(entry.RecommendedAnnotation, 15),
			truncate(entry.CurrentStatus, 12))
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}

	// Summary footer
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Total: %d | Gaps: %d | Annotated: %d | Incorrect: %d\n",
		report.Summary.TotalMatches, report.Summary.GapCount,
		report.Summary.AnnotatedCount, report.Summary.IncorrectCount); err != nil {
		return err
	}

	return nil
}

// truncate shortens a string to maxLen, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}
