// Package metrics reads the stable metrics a Straum node reports on its
// metrics endpoint (the node's STRAUM_CLUSTER_METRICS_LISTEN), the core's
// read-only client contract under D-141 A.
//
// It reads only the metric names the core declares stable (see Stable),
// draws no health verdict, and never retries: a node that does not answer,
// answers with an error, or omits a stable metric is Unknown.
//
// The module is before 1.0: this API may still change in a minor version.
package metrics

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The metric names the core declares stable (D-141 A). CI checks them
// against the core: scripts/check-drift.sh.
//
//go:embed stable-metrics.txt
var stableFile string

var stable = strings.Fields(stableFile)

// Stable returns the names of the stable metrics, in display order. Every
// observed Node carries exactly these.
func Stable() []string { return slices.Clone(stable) }

// The core renders about 16 KiB; anything past this is not a straum node.
const maxBody = 1 << 20

// State is what is known about a node. Neither value is a health verdict.
type State string

const (
	// Observed: the node answered HTTP 200 with every stable metric as a
	// non-negative integer.
	Observed State = "observed"
	// Unknown: anything else; Node.Err says why.
	Unknown State = "unknown"
)

// Report is one round of requests, one Node per address, in the order given.
type Report struct {
	// When the last request finished, UTC.
	GeneratedAt time.Time `json:"generated_at"`
	Nodes       []Node    `json:"nodes"`
}

// Node is what one address answered. The JSON form is the node entry of
// `straum-cli status -o json` (docs/status-json.md).
type Node struct {
	Address string `json:"address"`
	State   State  `json:"state"`
	// Only when observed: when the response was read, UTC, by the local
	// clock (the core does not timestamp its metrics), and how old that was
	// at Report.GeneratedAt.
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	AgeMS      *int64     `json:"age_ms,omitempty"`
	// How long the request took, whether it succeeded or not.
	LatencyMS int64 `json:"latency_ms"`
	// Only when unknown. Error is Err's text, for a person to read; its
	// wording is not stable. Err is nil in a Node decoded from JSON.
	Error string `json:"error,omitempty"`
	Err   error  `json:"-"`
	// Only when observed: every stable metric, as the node rendered it.
	Metrics map[string]uint64 `json:"metrics,omitempty"`
}

// StatusError is an answer other than HTTP 200.
type StatusError struct {
	Code   int
	Status string // e.g. "500 Internal Server Error"
}

func (e *StatusError) Error() string { return "HTTP " + e.Status }

// ContentError is an HTTP 200 answer that is not a straum node's stable
// metrics: too large, malformed, or a stable metric missing or not a
// non-negative integer.
type ContentError struct{ msg string }

func (e *ContentError) Error() string { return e.msg }

// timeoutError is Client.Timeout passing; it is context.DeadlineExceeded.
type timeoutError time.Duration

func (e timeoutError) Error() string { return fmt.Sprintf("no answer within %s", time.Duration(e)) }
func (e timeoutError) Unwrap() error { return context.DeadlineExceeded }

// readErr remembers the first error other than io.EOF from r.
type readErr struct {
	r   io.Reader
	err error
}

func (e *readErr) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}

// CheckAddress accepts host:port only: the endpoint is plain HTTP at a
// fixed path, so a scheme, a path or credentials are a mistake.
func CheckAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || port == "" || strings.ContainsAny(addr, "/?#@") {
		return fmt.Errorf("node %q: want host:port", addr)
	}
	return nil
}

// Client asks nodes for their stable metrics. The zero value is ready to
// use. A Client holds no connections of its own; one HTTP client may be
// shared by many goroutines.
type Client struct {
	// HTTP sends the requests; the caller owns it. Nil uses a client that
	// never goes through a proxy, never follows a redirect and keeps no
	// idle connections, so nothing is left open after Collect returns.
	HTTP *http.Client
	// Timeout bounds each node's request. Zero means only ctx bounds it.
	Timeout time.Duration
}

func defaultHTTP() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:             nil, // the endpoint is local or on a trusted network; never via a proxy
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Collect asks every address once, concurrently, and returns when all have
// answered or failed. A node's failure is in its Node, not the error: the
// error is only for a bad address or a negative Timeout, before any request.
// Cancelling ctx makes the nodes still pending Unknown.
func (c *Client) Collect(ctx context.Context, addrs []string) (Report, error) {
	if c.Timeout < 0 {
		return Report{}, errors.New("timeout must not be negative")
	}
	for _, a := range addrs {
		if err := CheckAddress(a); err != nil {
			return Report{}, err
		}
	}
	hc := c.HTTP
	if hc == nil {
		hc = defaultHTTP()
	}
	nodes := make([]Node, len(addrs))
	var wg sync.WaitGroup
	for i, addr := range addrs {
		wg.Go(func() { nodes[i] = c.observe(ctx, hc, addr) })
	}
	wg.Wait()
	r := Report{GeneratedAt: time.Now().UTC(), Nodes: nodes}
	for i := range nodes {
		if at := nodes[i].ObservedAt; at != nil {
			age := r.GeneratedAt.Sub(*at).Milliseconds()
			nodes[i].AgeMS = &age
		}
	}
	return r, nil
}

func (c *Client) observe(ctx context.Context, hc *http.Client, addr string) Node {
	start := time.Now()
	metrics, err := c.scrape(ctx, hc, addr)
	end := time.Now().UTC()
	n := Node{Address: addr, LatencyMS: end.Sub(start).Milliseconds()}
	if err != nil {
		n.State, n.Error, n.Err = Unknown, err.Error(), err
		return n
	}
	n.State, n.ObservedAt, n.Metrics = Observed, &end, metrics
	return n
}

func (c *Client) scrape(ctx context.Context, hc *http.Client, addr string) (map[string]uint64, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	timedOut := func(err error) error {
		if c.Timeout > 0 && errors.Is(err, context.DeadlineExceeded) {
			return timeoutError(c.Timeout)
		}
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/metrics", nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, timedOut(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{resp.StatusCode, resp.Status}
	}
	// Parsed as it arrives, not read whole first: one line buffer, not a
	// copy of the body. A read error is the network's; anything else the
	// parser refuses is the content's.
	limited := &io.LimitedReader{R: resp.Body, N: maxBody + 1}
	body := &readErr{r: limited}
	samples, err := parseSamples(body)
	if body.err != nil {
		return nil, timedOut(body.err)
	}
	if limited.N <= 0 {
		return nil, &ContentError{fmt.Sprintf("response larger than %d bytes", maxBody)}
	}
	if err != nil {
		return nil, &ContentError{"malformed metrics: " + err.Error()}
	}
	metrics := make(map[string]uint64, len(stable))
	for _, name := range stable {
		raw, ok := samples[name]
		if !ok {
			return nil, &ContentError{"missing stable metric " + name}
		}
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, &ContentError{fmt.Sprintf("%s: %q is not a non-negative integer", name, raw)}
		}
		metrics[name] = v
	}
	return metrics, nil
}
