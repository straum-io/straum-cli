package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/straum-io/straum-cli/metrics"
)

// schemaVersion is the version of the JSON document `status -o json`
// writes. Adding a field keeps it; removing or changing the meaning of one
// bumps it. See docs/status-json.md.
const schemaVersion = 1

// report is the `status -o json` document: the library's Report, versioned.
type report struct {
	SchemaVersion int `json:"schema_version"`
	metrics.Report
}

func writeJSON(w io.Writer, r report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// writeTable prints one row per node, in the order given. Values are the
// node's own, unaltered; an unknown node shows "-" in every column.
func writeTable(w io.Writer, r report) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	stable := metrics.Stable()
	fmt.Fprint(tw, "ADDRESS\tSTATE")
	for _, name := range stable {
		fmt.Fprint(tw, "\t", strings.ToUpper(strings.TrimPrefix(name, "straum_")))
	}
	fmt.Fprintln(tw, "\tAGE")
	for _, n := range r.Nodes {
		fmt.Fprint(tw, n.Address, "\t", n.State)
		for _, name := range stable {
			if v, ok := n.Metrics[name]; ok {
				fmt.Fprint(tw, "\t", v)
			} else {
				fmt.Fprint(tw, "\t-")
			}
		}
		if n.AgeMS != nil {
			fmt.Fprintf(tw, "\t%dms\n", *n.AgeMS)
		} else {
			fmt.Fprintln(tw, "\t-")
		}
	}
	return tw.Flush()
}
