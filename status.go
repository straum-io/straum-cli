package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

// The metric names the core declares stable (D-141 A). CI checks them
// against the core: scripts/check-drift.sh.
//
//go:embed stable-metrics.txt
var stableFile string

var stableMetrics = strings.Fields(stableFile)

// The core renders about 16 KiB; anything past this is not a straum node.
const maxBody = 1 << 20

// schemaVersion is the version of the JSON document `status -o json`
// writes. Adding a field keeps it; removing or changing the meaning of one
// bumps it. See docs/status-json.md.
const schemaVersion = 1

type report struct {
	SchemaVersion int          `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Nodes         []nodeStatus `json:"nodes"`
}

type nodeStatus struct {
	Address string `json:"address"`
	// "observed": the node answered with every stable metric.
	// "unknown": it did not; Error says why. Nothing else is inferred.
	State      string            `json:"state"`
	ObservedAt *time.Time        `json:"observed_at,omitempty"`
	AgeMS      *int64            `json:"age_ms,omitempty"`
	LatencyMS  int64             `json:"latency_ms"`
	Error      string            `json:"error,omitempty"`
	Metrics    map[string]uint64 `json:"metrics,omitempty"`
}

const (
	stateObserved = "observed"
	stateUnknown  = "unknown"
)

// checkAddress accepts host:port only: the CLI speaks plain HTTP to the
// metrics listener and nothing else, so a scheme or a path is a mistake.
func checkAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || port == "" || strings.ContainsAny(addr, "/?#@") {
		return fmt.Errorf("node %q: want host:port", addr)
	}
	return nil
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:             nil, // the endpoint is local or on a trusted network; never via a proxy
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// collect asks every node once, concurrently, each bounded by timeout.
// There are no retries: a node that does not answer in time is unknown.
func collect(ctx context.Context, client *http.Client, addrs []string, timeout time.Duration, now func() time.Time) report {
	nodes := make([]nodeStatus, len(addrs))
	var wg sync.WaitGroup
	for i, addr := range addrs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nodes[i] = observe(ctx, client, addr, timeout, now)
		}()
	}
	wg.Wait()
	r := report{SchemaVersion: schemaVersion, GeneratedAt: now().UTC(), Nodes: nodes}
	for i := range nodes {
		if at := nodes[i].ObservedAt; at != nil {
			age := r.GeneratedAt.Sub(*at).Milliseconds()
			nodes[i].AgeMS = &age
		}
	}
	return r
}

func observe(ctx context.Context, client *http.Client, addr string, timeout time.Duration, now func() time.Time) nodeStatus {
	start := now()
	metrics, err := scrape(ctx, client, addr, timeout)
	end := now().UTC()
	n := nodeStatus{Address: addr, LatencyMS: end.Sub(start).Milliseconds()}
	if err != nil {
		n.State, n.Error = stateUnknown, err.Error()
		return n
	}
	n.State, n.ObservedAt, n.Metrics = stateObserved, &end, metrics
	return n
}

func scrape(ctx context.Context, client *http.Client, addr string, timeout time.Duration) (map[string]uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/metrics", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("no answer within %s", timeout)
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("no answer within %s", timeout)
		}
		return nil, err
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("response larger than %d bytes", maxBody)
	}
	samples, err := parseSamples(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("malformed metrics: %w", err)
	}
	metrics := make(map[string]uint64, len(stableMetrics))
	for _, name := range stableMetrics {
		raw, ok := samples[name]
		if !ok {
			return nil, fmt.Errorf("missing stable metric %s", name)
		}
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a non-negative integer", name, raw)
		}
		metrics[name] = v
	}
	return metrics, nil
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
	fmt.Fprint(tw, "ADDRESS\tSTATE")
	for _, name := range stableMetrics {
		fmt.Fprint(tw, "\t", strings.ToUpper(strings.TrimPrefix(name, "straum_")))
	}
	fmt.Fprintln(tw, "\tAGE")
	for _, n := range r.Nodes {
		fmt.Fprint(tw, n.Address, "\t", n.State)
		for _, name := range stableMetrics {
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
