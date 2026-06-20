package reporter

import (
	"fmt"
	"io"
	"sort"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// FormatMarkdown writes the gap report as a markdown document with:
// - Summary section with counts
// - Priority table (services sorted by gaps desc)
// - Per-service detail sections with field tables
func FormatMarkdown(report *types.GapReport, w io.Writer) error {
	// Title
	if _, err := fmt.Fprintf(w, "# ACK Scanner v2 Gap Analysis Report\n\n"); err != nil {
		return err
	}

	// Summary section
	if _, err := fmt.Fprintf(w, "## Summary\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Metric | Count |\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| --- | --- |\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Total Matches | %d |\n", report.Summary.TotalMatches); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Gaps | %d |\n", report.Summary.GapCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Correctly Annotated | %d |\n", report.Summary.AnnotatedCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Incorrectly Annotated | %d |\n", report.Summary.IncorrectCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\n"); err != nil {
		return err
	}

	// Priority table section
	if _, err := fmt.Fprintf(w, "## Priority Services\n\n"); err != nil {
		return err
	}
	if len(report.Summary.ServicesByPriority) == 0 {
		if _, err := fmt.Fprintf(w, "No gaps identified.\n\n"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "| Service | Gap Count |\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "| --- | --- |\n"); err != nil {
			return err
		}
		for _, sp := range report.Summary.ServicesByPriority {
			if _, err := fmt.Fprintf(w, "| %s | %d |\n", sp.ServiceName, sp.GapCount); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "\n"); err != nil {
			return err
		}
	}

	// Per-service detail sections
	if _, err := fmt.Fprintf(w, "## Details\n\n"); err != nil {
		return err
	}

	// Group entries by service
	grouped := groupEntriesByService(report.Entries)
	services := make([]string, 0, len(grouped))
	for svc := range grouped {
		services = append(services, svc)
	}
	sort.Strings(services)

	for _, svc := range services {
		entries := grouped[svc]
		if _, err := fmt.Fprintf(w, "### %s\n\n", svc); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "| Resource | ACK Field | TF Field | Recommended | Status |\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "| --- | --- | --- | --- | --- |\n"); err != nil {
			return err
		}
		for _, entry := range entries {
			fieldDisplay := entry.ACKFieldName
			if entry.CurrentStatus == string(types.CategoryGap) {
				fieldDisplay = "⚠️ " + entry.ACKFieldName
			}
			if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n",
				entry.ResourceName, fieldDisplay, entry.TFFieldName,
				entry.RecommendedAnnotation, entry.CurrentStatus); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "\n"); err != nil {
			return err
		}
	}

	return nil
}

// groupEntriesByService groups report entries by their service name.
func groupEntriesByService(entries []types.GapReportEntry) map[string][]types.GapReportEntry {
	grouped := make(map[string][]types.GapReportEntry)
	for _, entry := range entries {
		grouped[entry.ServiceName] = append(grouped[entry.ServiceName], entry)
	}
	return grouped
}
