package reporter

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
)

// FormatReferenceMarkdown writes the reference gap report as a markdown document
// with summary tables, source breakdown, and per-service detail sections.
func FormatReferenceMarkdown(report *types.ReferenceGapReport, w io.Writer) error {
	if report == nil {
		return fmt.Errorf("report is nil")
	}

	// Title
	if _, err := fmt.Fprintf(w, "# ACK Reference Gap Report\n\n"); err != nil {
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
	if _, err := fmt.Fprintf(w, "| Total References | %d |\n", report.Summary.TotalReferences); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Gaps | %d |\n", report.Summary.GapCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Correctly Annotated | %d |\n", report.Summary.AnnotatedCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Ambiguous | %d |\n", report.Summary.AmbiguousCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\n"); err != nil {
		return err
	}

	// Source breakdown section
	if _, err := fmt.Fprintf(w, "## Source Breakdown\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Source Combination | Count |\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| --- | --- |\n"); err != nil {
		return err
	}
	sb := report.Summary.SourceBreakdown
	if _, err := fmt.Fprintf(w, "| Upjet Only | %d |\n", sb.UpjetOnly); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Terraform Docs Only | %d |\n", sb.TerraformOnly); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| API Model Only | %d |\n", sb.ModelOnly); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| Two Sources | %d |\n", sb.TwoSources); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "| All Three Sources | %d |\n", sb.AllThreeSources); err != nil {
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
		if _, err := fmt.Fprintf(w, "No reference gaps identified.\n\n"); err != nil {
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

	grouped := groupReferenceEntriesByService(report.Entries)
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
		if _, err := fmt.Fprintf(w, "| Resource | ACK Field | Target | Sources | Status | Confidence |\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "| --- | --- | --- | --- | --- | --- |\n"); err != nil {
			return err
		}
		for _, entry := range entries {
			fieldDisplay := entry.ACKFieldName
			if entry.CurrentStatus == string(types.RefCategoryGap) {
				fieldDisplay = "⚠️ " + entry.ACKFieldName
			}
			target := entry.TargetTFResource
			if entry.TargetACKService != "" {
				target = fmt.Sprintf("%s → %s/%s", entry.TargetTFResource, entry.TargetACKService, entry.TargetACKResource)
			}
			sources := strings.Join(entry.Sources, ", ")
			if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %.2f |\n",
				entry.ResourceName, fieldDisplay, target, sources,
				entry.CurrentStatus, entry.Confidence); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "\n"); err != nil {
			return err
		}
	}

	return nil
}

// groupReferenceEntriesByService groups reference report entries by service name.
func groupReferenceEntriesByService(entries []types.ReferenceGapEntry) map[string][]types.ReferenceGapEntry {
	grouped := make(map[string][]types.ReferenceGapEntry)
	for _, entry := range entries {
		grouped[entry.ServiceName] = append(grouped[entry.ServiceName], entry)
	}
	return grouped
}
