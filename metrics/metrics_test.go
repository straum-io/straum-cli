package metrics

import (
	"context"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// serve answers every request with status and body, and counts them.
func serve(t testing.TB, status int, body string) (addr string, hits *atomic.Int32) {
	t.Helper()
	hits = new(atomic.Int32)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return strings.TrimPrefix(s.URL, "http://"), hits
}

// hang answers nothing until the request is cancelled.
func hang(t *testing.T) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	t.Cleanup(s.Close)
	return strings.TrimPrefix(s.URL, "http://")
}

func deadAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func collectOne(t *testing.T, c *Client, addr string) Node {
	t.Helper()
	r, err := c.Collect(context.Background(), []string{addr})
	if err != nil {
		t.Fatal(err)
	}
	return r.Nodes[0]
}

func TestStableContract(t *testing.T) {
	want := []string{"straum_node", "straum_slot", "straum_leading", "straum_serving", "straum_commit", "straum_applied",
		"straum_log_end", "straum_log_durable", "straum_lag", "straum_outbox_entries", "straum_outbox_capacity"}
	if got := Stable(); !slices.Equal(got, want) {
		t.Fatalf("Stable() = %v", got)
	}
	Stable()[0] = "changed"
	if Stable()[0] != "straum_node" {
		t.Error("Stable() returned the package's own slice")
	}
}

func TestCollectObserved(t *testing.T) {
	// The fixture carries many unstable series too; only the stable set is kept.
	addr, hits := serve(t, 200, fixture(t, "node1.prom"))
	before := time.Now().UTC()
	r, err := (&Client{Timeout: time.Second}).Collect(context.Background(), []string{addr})
	if err != nil {
		t.Fatal(err)
	}
	n := r.Nodes[0]
	if n.State != Observed || n.Address != addr || n.Err != nil || n.Error != "" || n.ObservedAt == nil || n.AgeMS == nil {
		t.Fatalf("%+v", n)
	}
	if n.ObservedAt.Before(before) || r.GeneratedAt.Before(*n.ObservedAt) || n.ObservedAt.Location() != time.UTC {
		t.Errorf("observed at %v, generated at %v, started %v", n.ObservedAt, r.GeneratedAt, before)
	}
	if keys := slices.Sorted(maps.Keys(n.Metrics)); !slices.Equal(keys, slices.Sorted(slices.Values(Stable()))) {
		t.Errorf("metrics %v, want exactly the stable set", keys)
	}
	if n.Metrics["straum_node"] != 1 || n.Metrics["straum_outbox_capacity"] != 341 {
		t.Errorf("values %v", n.Metrics)
	}
	if hits.Load() != 1 {
		t.Errorf("%d requests, want 1", hits.Load())
	}
}

func TestCollectKeepsOrderAndIsolatesFailures(t *testing.T) {
	ok, _ := serve(t, 200, fixture(t, "node0.prom"))
	dead := deadAddr(t)
	r, err := (&Client{Timeout: time.Second}).Collect(context.Background(), []string{dead, ok})
	if err != nil {
		t.Fatal(err)
	}
	if n := r.Nodes[0]; n.State != Unknown || n.Address != dead || n.Err == nil || n.Error != n.Err.Error() ||
		n.Metrics != nil || n.ObservedAt != nil || n.AgeMS != nil {
		t.Errorf("unknown node: %+v", n)
	}
	var op *net.OpError
	if !errors.As(r.Nodes[0].Err, &op) {
		t.Errorf("unreachable node: %T %v, want a *net.OpError inside", r.Nodes[0].Err, r.Nodes[0].Err)
	}
	if n := r.Nodes[1]; n.State != Observed || n.Address != ok {
		t.Errorf("observed node: %+v", n)
	}
}

func TestCollectBadAnswers(t *testing.T) {
	full := fixture(t, "node0.prom")
	tests := []struct {
		name    string
		status  int
		body    string
		content bool // a *ContentError, else a *StatusError
		wantErr string
	}{
		{"http error", 500, "", false, "HTTP 500 Internal Server Error"},
		{"not metrics", 200, "<html>login</html>", true, "malformed metrics: line 1: no metric name"},
		{"stable metric missing", 200, strings.Replace(full, "straum_lag 0\n", "", 1), true, "missing stable metric straum_lag"},
		{"not an integer", 200, strings.Replace(full, "straum_commit 10\n", "straum_commit 1e3\n", 1), true, `straum_commit: "1e3" is not a non-negative integer`},
		{"negative", 200, strings.Replace(full, "straum_lag 0\n", "straum_lag -1\n", 1), true, `straum_lag: "-1" is not a non-negative integer`},
		{"duplicate", 200, full + "straum_lag 0\n", true, "appears twice"},
		{"too large", 200, full + strings.Repeat("# padding\n", maxBody/10), true, "response larger than 1048576 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, _ := serve(t, tt.status, tt.body)
			n := collectOne(t, &Client{Timeout: time.Second}, addr)
			if n.State != Unknown || !strings.Contains(n.Error, tt.wantErr) {
				t.Fatalf("%s %q, want unknown and %q", n.State, n.Error, tt.wantErr)
			}
			var ce *ContentError
			var se *StatusError
			if tt.content && !errors.As(n.Err, &ce) || !tt.content && (!errors.As(n.Err, &se) || se.Code != tt.status) {
				t.Errorf("error %T %v", n.Err, n.Err)
			}
		})
	}
}

func TestCollectDoesNotFollowRedirects(t *testing.T) {
	target, targetHits := serve(t, 200, fixture(t, "node0.prom"))
	s := httptest.NewServer(http.RedirectHandler("http://"+target+"/metrics", http.StatusFound))
	t.Cleanup(s.Close)
	n := collectOne(t, &Client{}, strings.TrimPrefix(s.URL, "http://"))
	var se *StatusError
	if !errors.As(n.Err, &se) || se.Code != http.StatusFound || targetHits.Load() != 0 {
		t.Errorf("%+v, %d hits on the target", n, targetHits.Load())
	}
}

func TestCollectTimeout(t *testing.T) {
	start := time.Now()
	n := collectOne(t, &Client{Timeout: 100 * time.Millisecond}, hang(t))
	if n.Error != "no answer within 100ms" || !errors.Is(n.Err, context.DeadlineExceeded) {
		t.Errorf("%q (%T)", n.Error, n.Err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %s", took)
	}
}

func TestCollectCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	// No Timeout: only ctx bounds the request.
	r, err := (&Client{}).Collect(ctx, []string{hang(t)})
	if err != nil {
		t.Fatal(err)
	}
	if n := r.Nodes[0]; n.State != Unknown || !errors.Is(n.Err, context.Canceled) {
		t.Errorf("%+v", n)
	}
}

func TestCollectUsesTheGivenHTTPClient(t *testing.T) {
	addr, _ := serve(t, 200, fixture(t, "node0.prom"))
	var used atomic.Bool
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		used.Store(true)
		return http.DefaultTransport.RoundTrip(r)
	})}
	if n := collectOne(t, &Client{HTTP: hc}, addr); n.State != Observed || !used.Load() {
		t.Errorf("%+v, used %v", n, used.Load())
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCollectConfiguration(t *testing.T) {
	addr, hits := serve(t, 200, fixture(t, "node0.prom"))
	for _, tt := range []struct {
		name string
		c    Client
		addr string
	}{
		{"negative timeout", Client{Timeout: -time.Second}, "127.0.0.1:1"},
		{"scheme", Client{}, "http://127.0.0.1:9100"},
		{"no port", Client{}, "127.0.0.1"},
		{"no host", Client{}, ":9100"},
		{"path", Client{}, "127.0.0.1:9100/x"},
		{"credentials", Client{}, "u@127.0.0.1:9100"},
		{"empty", Client{}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// A bad entry fails the whole call before any request.
			if _, err := tt.c.Collect(context.Background(), []string{addr, tt.addr}); err == nil {
				t.Errorf("accepted %q", tt.addr)
			}
		})
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests made despite a configuration error", hits.Load())
	}
	for _, ok := range []string{"127.0.0.1:9100", "[::1]:9100", "node-0.local:9100"} {
		if err := CheckAddress(ok); err != nil {
			t.Errorf("CheckAddress(%q): %v", ok, err)
		}
	}
	if r, err := (&Client{}).Collect(context.Background(), nil); err != nil || len(r.Nodes) != 0 {
		t.Errorf("no addresses: %+v, %v", r, err)
	}
}

// A node that sends its headers and part of the body, then stalls: the
// deadline hits while the body is being parsed, and the node is Unknown
// for a timeout, not for malformed content.
func TestCollectTimeoutMidBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "# HELP straum_node This node's id.\nstraum_node 0\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	c := Client{Timeout: 200 * time.Millisecond}
	r, err := c.Collect(context.Background(), []string{strings.TrimPrefix(srv.URL, "http://")})
	if err != nil {
		t.Fatal(err)
	}
	n := r.Nodes[0]
	var content *ContentError
	if n.State != Unknown || !errors.Is(n.Err, context.DeadlineExceeded) || errors.As(n.Err, &content) {
		t.Fatalf("want an Unknown node for a timeout, got %+v (%v)", n, n.Err)
	}
}
