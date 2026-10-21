//go:build e2e

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
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
func startCluster(t *testing.T, extra ...string) *cluster {
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
			"NO_COLOR=1",
		}
		cmd.Env = append(cmd.Env, extra...)
		// Each node's log goes to a file, printed if the test fails: a node
		// that dies or answers EOF is diagnosable from CI (straum#13).
		logPath := fmt.Sprintf("%s/n%d.log", data, i)
		log, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		// The server logs to stdout; configuration errors and panics go to
		// stderr. Both land in the file.
		var out io.Writer = log
		if testing.Verbose() {
			out = io.MultiWriter(log, os.Stderr)
		}
		cmd.Stdout, cmd.Stderr = out, out
		// Registered before the kill below, so it runs after it: the log
		// is complete when printed.
		t.Cleanup(func() {
			log.Close()
			if t.Failed() {
				if b, err := os.ReadFile(logPath); err == nil {
					t.Logf("node %d log:\n%s", i, b)
				}
			}
		})
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

// TestE2ECredentialsAndTLS runs the cluster with client TLS and a password
// file (core D-158 A, D-164 A) and publishes through them.
func TestE2ECredentialsAndTLS(t *testing.T) {
	dir := t.TempDir()
	cert, key := dir+"/cert.pem", dir+"/key.pem"
	writeSelfSigned(t, cert, key)
	bin := os.Getenv("STRAUM_SERVER_BIN")
	if bin == "" {
		t.Fatal("STRAUM_SERVER_BIN is not set")
	}
	hashCmd := exec.Command(bin, "hash-password")
	hashCmd.Stdin = strings.NewReader("secret\n")
	hash, err := hashCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	passwords, acl := dir+"/passwords", dir+"/acl"
	if err := os.WriteFile(passwords, []byte("alice:"+strings.TrimSpace(string(hash))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acl, []byte("user alice\npublish e2e/auth\nreadwrite e2e/ok/#\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startCluster(t, "STRAUM_TLS_CERT_PATH="+cert, "STRAUM_TLS_KEY_PATH="+key, "STRAUM_AUTH_PASSWORD_FILE="+passwords, "STRAUM_AUTH_ACL_FILE="+acl)
	broker := c.mqtt[0]
	pub := func(env map[string]string, args ...string) (int, string) {
		code, _, stderr := runCLI(t, env, append([]string{"pub", "--broker", broker, "-t", "e2e/auth", "-m", "hi", "-q", "1"}, args...)...)
		return code, stderr
	}
	tlsArgs := []string{"--tls-ca", cert, "--username", "alice"}

	if code, stderr := pub(map[string]string{"STRAUM_CLI_PASSWORD": "secret"}, tlsArgs...); code != exitOK {
		t.Fatalf("with TLS and the right password: exit %d, %s", code, stderr)
	}
	if code, stderr := pub(map[string]string{"STRAUM_CLI_PASSWORD": "wrong"}, tlsArgs...); code != exitRefused || !strings.Contains(stderr, "bad user name or password") {
		t.Fatalf("a wrong password: exit %d, %s", code, stderr)
	}
	if code, stderr := pub(nil, "--tls-ca", cert); code != exitRefused || !strings.Contains(stderr, "not authorized") {
		t.Fatalf("no credentials, anonymous refused: exit %d, %s", code, stderr)
	}
	if code, _ := pub(map[string]string{"STRAUM_CLI_PASSWORD": "secret"}, "--username", "alice"); code != exitRefused {
		t.Fatalf("plain TCP to a TLS listener: exit %d", code)
	}
	if code, stderr := pub(map[string]string{"STRAUM_CLI_PASSWORD": "secret"}, append(tlsArgs, "--tls-server-name", "other.example")...); code != exitRefused || !strings.Contains(stderr, "certificate") {
		t.Fatalf("a certificate for another name: exit %d, %s", code, stderr)
	}
	if code, stderr := pub(map[string]string{"STRAUM_CLI_PASSWORD": "secret"}, append(tlsArgs, "-t", "e2e/denied")...); code != exitRefused || !strings.Contains(stderr, "not authorized") {
		t.Fatalf("a publish a topic rule refuses: exit %d, %s", code, stderr)
	}
	sub := func(topic string) (int, string) {
		code, _, stderr := runCLI(t, map[string]string{"STRAUM_CLI_PASSWORD": "secret"}, append([]string{"sub", "--broker", broker, "-t", topic, "--wait", "500ms"}, tlsArgs...)...)
		return code, stderr
	}
	if code, stderr := sub("e2e/ok/x"); code != exitOK {
		t.Fatalf("a subscription a topic rule allows: exit %d, %s", code, stderr)
	}
	if code, stderr := sub("e2e/denied"); code != exitRefused || !strings.Contains(stderr, "not authorized") {
		t.Fatalf("a subscription a topic rule refuses: exit %d, %s", code, stderr)
	}
}

// writeSelfSigned writes a certificate for 127.0.0.1, valid for an hour.
func writeSelfSigned(t *testing.T, certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "straum e2e"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
}
