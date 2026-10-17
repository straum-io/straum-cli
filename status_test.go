package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/straum-io/straum-cli/metrics"
)

var update = flag.Bool("update", false, "rewrite golden files")

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// serve answers every request with body, as the core does, and counts them.
func serve(t *testing.T, status int, body []byte) (addr string, hits *atomic.Int32) {
	t.Helper()
	hits = new(atomic.Int32)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	return strings.TrimPrefix(s.URL, "http://"), hits
}

// deadAddr is a port with nothing listening.
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

func runCLI(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(context.Background(), args, func(k string) string { return env[k] }, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestStatusExitCodes(t *testing.T) {
	ok, _ := serve(t, 200, fixture(t, "node0.prom"))
	ok2, _ := serve(t, 200, fixture(t, "node1.prom"))
	dead := deadAddr(t)

	tests := []struct {
		name  string
		nodes string
		want  int
	}{
		{"all observed", ok + "," + ok2, exitOK},
		{"one unknown", ok + "," + dead, exitPartial},
		{"all unknown", dead, exitNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, nil, "status", "--node", tt.nodes, "-o", "json")
			if code != tt.want {
				t.Fatalf("exit %d, want %d; stderr: %s", code, tt.want, stderr)
			}
			var r report
			if err := json.Unmarshal([]byte(stdout), &r); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
			}
			if (tt.want == exitOK) != (stderr == "") {
				t.Errorf("stderr = %q", stderr)
			}
		})
	}
}

func TestStatusFailures(t *testing.T) {
	full := string(fixture(t, "node0.prom"))
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"http error", 500, "", "HTTP 500"},
		{"not metrics", 200, "<html>login</html>", "malformed metrics"},
		{"stable metric missing", 200, strings.Replace(full, "straum_lag 0\n", "", 1), "missing stable metric straum_lag"},
		{"stable metric not integer", 200, strings.Replace(full, "straum_commit 10\n", "straum_commit 1e3\n", 1), "straum_commit"},
		{"too large", 200, full + strings.Repeat("# padding\n", 1<<20/10), "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, _ := serve(t, tt.status, []byte(tt.body))
			code, _, stderr := runCLI(t, nil, "status", "--node", addr)
			if code != exitNone || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("exit %d, stderr %q; want exit %d and %q", code, stderr, exitNone, tt.wantErr)
			}
		})
	}
}

func TestStatusTimeoutAndNoRetry(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer s.Close()
	defer close(release)

	start := time.Now()
	code, _, stderr := runCLI(t, nil, "status", "--node", strings.TrimPrefix(s.URL, "http://"), "--timeout", "100ms")
	if code != exitNone || !strings.Contains(stderr, "no answer within 100ms") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %s with a 100ms timeout", took)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("%d requests, want exactly 1", n)
	}
}

func TestStatusCancelled(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"status", "--node", strings.TrimPrefix(s.URL, "http://"), "--timeout", "10s"}, func(string) string { return "" }, nil, &out, &errOut)
	if code != exitNone {
		t.Errorf("exit %d after cancel; stderr %q", code, errOut.String())
	}
}

func TestStatusConfiguration(t *testing.T) {
	flagNode, flagHits := serve(t, 200, fixture(t, "node0.prom"))
	envNode, envHits := serve(t, 200, fixture(t, "node1.prom"))
	env := map[string]string{"STRAUM_CLI_NODES": envNode}

	if code, _, stderr := runCLI(t, env, "status", "--node", flagNode); code != exitOK || flagHits.Load() != 1 || envHits.Load() != 0 {
		t.Errorf("flag over env: exit %d, flag hits %d, env hits %d, %s", code, flagHits.Load(), envHits.Load(), stderr)
	}
	if code, _, stderr := runCLI(t, env, "status"); code != exitOK || envHits.Load() != 1 {
		t.Errorf("env alone: exit %d, env hits %d, %s", code, envHits.Load(), stderr)
	}

	usage := []struct {
		name    string
		env     map[string]string
		args    []string
		wantErr string
	}{
		{"no nodes", nil, nil, "no nodes"},
		{"empty env", map[string]string{"STRAUM_CLI_NODES": ""}, nil, "no nodes"},
		{"scheme", nil, []string{"--node", "http://127.0.0.1:9100"}, "want host:port"},
		{"no port", nil, []string{"--node", "127.0.0.1"}, "want host:port"},
		{"path", nil, []string{"--node", "127.0.0.1:9100/x"}, "want host:port"},
		{"bad env", map[string]string{"STRAUM_CLI_NODES": "127.0.0.1:1,,"}, nil, "want host:port"},
		{"twice", nil, []string{"--node", "127.0.0.1:1", "--node", "127.0.0.1:1"}, "given twice"},
		{"timeout", nil, []string{"--node", "127.0.0.1:1", "--timeout", "0s"}, "--timeout"},
		{"output", nil, []string{"--node", "127.0.0.1:1", "-o", "yaml"}, "want table or json"},
		{"argument", nil, []string{"--node", "127.0.0.1:1", "extra"}, "unknown command"},
		{"unknown flag", nil, []string{"--nodes", "127.0.0.1:1"}, "unknown flag"},
	}
	for _, tt := range usage {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.env, append([]string{"status"}, tt.args...)...)
			if code != exitUsage || stdout != "" || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit 1 and %q", code, stdout, stderr, tt.wantErr)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	code, stdout, _ := runCLI(t, nil, "version")
	if code != exitOK || !strings.HasPrefix(stdout, "straum-cli dev (commit ") {
		t.Errorf("exit %d, %q", code, stdout)
	}
}

// goldenReport is fixed so the output formats can be compared byte for byte.
func goldenReport() report {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	observed := at.Add(-15 * time.Millisecond)
	age := int64(15)
	values := make(map[string]uint64)
	for i, name := range metrics.Stable() {
		values[name] = uint64(i)
	}
	return report{SchemaVersion: schemaVersion, Report: metrics.Report{
		GeneratedAt: at,
		Nodes: []metrics.Node{
			{Address: "127.0.0.1:9100", State: metrics.Observed, ObservedAt: &observed, AgeMS: &age, LatencyMS: 3, Metrics: values},
			{Address: "127.0.0.1:9101", State: metrics.Unknown, LatencyMS: 2000, Error: "no answer within 2s"},
		},
	}}
}

func TestGolden(t *testing.T) {
	for file, write := range map[string]func(*bytes.Buffer, report) error{
		"status.golden.json": func(b *bytes.Buffer, r report) error { return writeJSON(b, r) },
		"status.golden.txt":  func(b *bytes.Buffer, r report) error { return writeTable(b, r) },
	} {
		var got bytes.Buffer
		if err := write(&got, goldenReport()); err != nil {
			t.Fatal(err)
		}
		path := "testdata/" + file
		if *update {
			if err := os.WriteFile(path, got.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if want := fixture(t, file); !bytes.Equal(got.Bytes(), want) {
			t.Errorf("%s changed; if intended, run go test -update and check docs/status-json.md\ngot:\n%s", file, got.String())
		}
	}
}
