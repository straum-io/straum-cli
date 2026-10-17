//go:build e2e

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/straum-io/straum-cli/metrics"
)

type cluster struct {
	metrics, mqtt []string
	procs         []*exec.Cmd
}

// startCluster runs three straum-server processes on loopback and waits
// until every node serves and one sequences. STRAUM_SERVER_BIN is the
// core's binary (make e2e builds it from ../straum).
func startCluster(t *testing.T) *cluster {
	t.Helper()
	bin := os.Getenv("STRAUM_SERVER_BIN")
	if bin == "" {
		t.Fatal("STRAUM_SERVER_BIN is not set")
	}
	data := t.TempDir()
	c := &cluster{}
	var peers []string
	for i := range 3 {
		peers = append(peers, fmt.Sprintf("%d=%s", i, deadAddr(t)))
		c.metrics = append(c.metrics, deadAddr(t))
		c.mqtt = append(c.mqtt, deadAddr(t))
	}
	for i := range 3 {
		cmd := exec.Command(bin)
		cmd.Env = []string{
			"STRAUM_EXPERIMENTAL_CLUSTER=1",
			fmt.Sprintf("STRAUM_CLUSTER_NODE_ID=%d", i),
			"STRAUM_CLUSTER_PEERS=" + strings.Join(peers, ","),
			"STRAUM_CLUSTER_MQTT_LISTEN=" + c.mqtt[i],
			"STRAUM_CLUSTER_METRICS_LISTEN=" + c.metrics[i],
			fmt.Sprintf("STRAUM_CLUSTER_DATA_DIR=%s/n%d", data, i),
			"STRAUM_LOG_LEVEL=warn",
		}
		if testing.Verbose() {
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		c.procs = append(c.procs, cmd)
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		code, r := c.status(t)
		leading, serving := 0, 0
		for _, n := range r.Nodes {
			leading += int(n.Metrics["straum_leading"])
			serving += int(n.Metrics["straum_serving"])
		}
		if code == exitOK && leading == 1 && serving == 3 {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("cluster not up: exit %d, %+v", code, r)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (c *cluster) status(t *testing.T) (int, report) {
	t.Helper()
	code, stdout, _ := runCLI(t, nil, "status", "--node", strings.Join(c.metrics, ","), "-o", "json")
	var r report
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	return code, r
}

func TestE2EStatus(t *testing.T) {
	c := startCluster(t)
	_, r := c.status(t)
	for i, n := range r.Nodes {
		if n.Address != c.metrics[i] || n.Metrics["straum_node"] != uint64(i) {
			t.Errorf("node %d answered for another: %+v", i, n)
		}
	}

	// One node down: it is unknown, the others still observed.
	c.procs[2].Process.Kill()
	c.procs[2].Wait()
	code, r := c.status(t)
	if code != exitPartial || r.Nodes[2].State != metrics.Unknown || r.Nodes[0].State != metrics.Observed || r.Nodes[1].State != metrics.Observed {
		t.Fatalf("one down: exit %d, %+v", code, r)
	}

	for _, p := range c.procs[:2] {
		p.Process.Kill()
		p.Wait()
	}
	if code, r := c.status(t); code != exitNone {
		t.Fatalf("all down: exit %d, %+v", code, r)
	}
}

// syncBuffer is written by a running sub and read by the test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type subRun struct {
	out, errOut syncBuffer
	done        chan int
}

// startSub runs `sub` in the background. It also subscribes to a marker
// topic holding a retained message: once that arrives, the subscription
// is committed and live publishes reach it.
func startSub(t *testing.T, broker, marker string, args ...string) *subRun {
	t.Helper()
	s := &subRun{done: make(chan int, 1)}
	args = append([]string{"sub", "--broker", broker, "-o", "json", "-t", marker, "--wait", "20s"}, args...)
	go func() {
		s.done <- run(context.Background(), args, func(string) string { return "" }, nil, &s.out, &s.errOut)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(s.out.String(), `"topic":"`+marker+`"`) {
		if time.Now().After(deadline) {
			t.Fatalf("sub never got the marker; stderr: %s", s.errOut.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	return s
}

func (s *subRun) wait(t *testing.T) (int, []message) {
	t.Helper()
	select {
	case code := <-s.done:
		var msgs []message
		dec := json.NewDecoder(strings.NewReader(s.out.String()))
		for dec.More() {
			var m message
			if err := dec.Decode(&m); err != nil {
				t.Fatalf("sub output is not JSON lines: %v\n%s", err, s.out.String())
			}
			msgs = append(msgs, m)
		}
		return code, msgs
	case <-time.After(30 * time.Second):
		t.Fatal("sub did not stop")
		return 0, nil
	}
}

func pub(t *testing.T, want int, broker string, args ...string) string {
	t.Helper()
	code, stdout, stderr := runCLI(t, nil, append([]string{"pub", "--broker", broker}, args...)...)
	if code != want || stdout != "" {
		t.Fatalf("pub %v: exit %d, want %d; stdout %q, stderr %q", args, code, want, stdout, stderr)
	}
	return stderr
}

func topics(msgs []message) []string {
	var ts []string
	for _, m := range msgs {
		ts = append(ts, fmt.Sprintf("%s qos=%d retain=%t %s", m.Topic, m.QoS, m.Retain, m.Payload))
	}
	slices.Sort(ts)
	return ts
}

// Across nodes: published on one node, delivered on another, so each
// message went through the replicated log.
func TestE2EPubSubWildcardsAndQoS(t *testing.T) {
	c := startCluster(t)
	pub(t, exitOK, c.mqtt[0], "-t", "e2e/marker", "-m", "ready", "-r")
	s := startSub(t, c.mqtt[1], "e2e/marker", "-t", "e2e/a/+", "-t", "e2e/b/#", "-c", "4")

	pub(t, exitOK, c.mqtt[2], "-t", "e2e/a/x", "-m", "one", "-q", "1")
	pub(t, exitOK, c.mqtt[0], "-t", "e2e/b/y/z", "-m", "two", "-q", "0")
	pub(t, exitOK, c.mqtt[0], "-t", "e2e/a/x/deep", "-m", "+ is one level")
	pub(t, exitOK, c.mqtt[2], "-t", "e2e/c", "-m", "no filter")
	var out, errOut bytes.Buffer
	args := []string{"pub", "--broker", c.mqtt[1], "-t", "e2e/b", "--stdin"}
	if code := run(context.Background(), args, func(string) string { return "" }, strings.NewReader("three"), &out, &errOut); code != exitOK {
		t.Fatalf("pub --stdin: exit %d, %s", code, errOut.String())
	}

	code, msgs := s.wait(t)
	want := []string{
		"e2e/a/x qos=1 retain=false one",
		"e2e/b qos=1 retain=false three", // # matches its parent level too
		"e2e/b/y/z qos=0 retain=false two",
		"e2e/marker qos=1 retain=true ready",
	}
	if got := topics(msgs); code != exitOK || !slices.Equal(got, want) {
		t.Errorf("exit %d, got\n%s\nwant\n%s\nstderr: %s", code, strings.Join(got, "\n"), strings.Join(want, "\n"), s.errOut.String())
	}
	if strings.Contains(s.errOut.String(), "unsubscribe") {
		t.Errorf("unsubscribe failed: %s", s.errOut.String())
	}
}

func TestE2ERetained(t *testing.T) {
	c := startCluster(t)
	pub(t, exitOK, c.mqtt[0], "-t", "e2e/r", "-m", "kept", "-r")

	// A new subscription gets it, from any node.
	for _, broker := range c.mqtt {
		code, stdout, stderr := runCLI(t, nil, "sub", "--broker", broker, "-t", "e2e/r", "-c", "1", "--wait", "10s", "-v")
		if code != exitOK || stdout != "e2e/r kept\n" {
			t.Errorf("%s: exit %d, %q, %s", broker, code, stdout, stderr)
		}
	}

	// Retain handling 2: never sent at subscribe.
	code, stdout, _ := runCLI(t, nil, "sub", "--broker", c.mqtt[1], "-t", "e2e/r", "--retain-handling", "2", "-c", "1", "--wait", "1s")
	if code != exitPartial || stdout != "" {
		t.Errorf("retain-handling 2: exit %d, %q", code, stdout)
	}

	// An empty retained payload clears it.
	pub(t, exitOK, c.mqtt[2], "-t", "e2e/r", "-m", "", "-r")
	code, stdout, _ = runCLI(t, nil, "sub", "--broker", c.mqtt[1], "-t", "e2e/r", "-c", "1", "--wait", "1s")
	if code != exitPartial || stdout != "" {
		t.Errorf("after clear: exit %d, %q", code, stdout)
	}
}

func TestE2ESubscriptionOptions(t *testing.T) {
	c := startCluster(t)
	pub(t, exitOK, c.mqtt[0], "-t", "e2e/m1", "-m", "ready", "-r")
	pub(t, exitOK, c.mqtt[0], "-t", "e2e/m2", "-m", "ready", "-r")

	// A live retained publish reaches a subscriber with retain cleared,
	// unless it asked for retain as published. No local is accepted; it
	// only filters this connection's own publishes, and sub publishes none.
	asIs := startSub(t, c.mqtt[1], "e2e/m1", "-t", "e2e/rap", "--retain-as-published", "--no-local", "-c", "2")
	plain := startSub(t, c.mqtt[2], "e2e/m2", "-t", "e2e/rap", "-c", "2")
	pub(t, exitOK, c.mqtt[0], "-t", "e2e/rap", "-m", "live", "-r")

	for name, tt := range map[string]struct {
		s    *subRun
		want string
	}{
		"retain as published": {asIs, "e2e/rap qos=1 retain=true live"},
		"default":             {plain, "e2e/rap qos=1 retain=false live"},
	} {
		code, msgs := tt.s.wait(t)
		if got := topics(msgs); code != exitOK || len(got) != 2 || !slices.Contains(got, tt.want) {
			t.Errorf("%s: exit %d, %v", name, code, got)
		}
	}
}

func TestE2ERefusals(t *testing.T) {
	c := startCluster(t)
	code, _, stderr := runCLI(t, nil, "sub", "--broker", c.mqtt[0], "-t", "$share/g/e2e/x", "--wait", "1s")
	// The core says so in CONNACK (shared subscriptions unavailable); the
	// client refuses before sending, and the core would refuse it too.
	if code != exitRefused || !strings.Contains(stderr, "shared subscriptions") {
		t.Errorf("shared subscription: exit %d, %s", code, stderr)
	}
	code, _, stderr = runCLI(t, nil, "pub", "--broker", c.mqtt[0], "-t", "e2e/x", "-m", "x", "-q", "2")
	if code != exitUsage || !strings.Contains(stderr, "QoS 2") {
		t.Errorf("QoS 2: exit %d, %s", code, stderr)
	}
	// No majority: the PUBLISH is sent, no PUBACK comes, and the CLI
	// cannot know whether it will be committed.
	for _, p := range c.procs[1:] {
		p.Process.Kill()
		p.Wait()
	}
	if stderr := pub(t, exitUnknown, c.mqtt[0], "-t", "e2e/x", "-m", "x", "--timeout", "2s"); !strings.Contains(stderr, "outcome unknown") {
		t.Errorf("no majority: %s", stderr)
	}

	// A node that is down: nothing was sent.
	c.procs[0].Process.Kill()
	c.procs[0].Wait()
	pub(t, exitRefused, c.mqtt[0], "-t", "e2e/x", "-m", "x")
}

// The node disconnects a client with a reason: a second connection with
// the same client id takes the session over (MQTT 5 0x8e).
func TestE2EDisconnectedByTheNode(t *testing.T) {
	c := startCluster(t)
	pub(t, exitOK, c.mqtt[0], "-t", "e2e/marker", "-m", "ready", "-r")
	first := startSub(t, c.mqtt[0], "e2e/marker", "--client-id", "e2e-dup")
	if code, _, stderr := runCLI(t, nil, "sub", "--broker", c.mqtt[0], "--client-id", "e2e-dup", "-t", "e2e/x", "--wait", "500ms"); code != exitOK {
		t.Fatalf("second sub: exit %d, %s", code, stderr)
	}
	if code, _ := first.wait(t); code != exitRefused || !strings.Contains(first.errOut.String(), "the node disconnected: reason 0x8e") {
		t.Errorf("first sub: exit %d, stderr %q", code, first.errOut.String())
	}
}
