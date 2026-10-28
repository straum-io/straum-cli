//go:build live

// Live tests: the CLI built and run as a process against straum-broker and
// straum-fake-server built from the Straum commit in CONTRACT
// (scripts/live.sh builds them and runs these tests).
//
//	STRAUM_BROKER_BIN       target/release/straum-broker
//	STRAUM_FAKE_SERVER_BIN  target/release/straum-fake-server
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/straum-io/straum-go-sdk/admin"
	"github.com/straum-io/straum-go-sdk/client"
)

var cliBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "straum-cli-live-")
	if err != nil {
		panic(err)
	}
	cliBin = filepath.Join(dir, "straum-cli")
	if out, err := exec.Command("go", "build", "-race", "-o", cliBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func liveBin(t *testing.T, env string) string {
	t.Helper()
	p := os.Getenv(env)
	if p == "" {
		t.Fatalf("%s is not set (scripts/live.sh sets it)", env)
	}
	return p
}

type broker struct {
	addr string
	cmd  *exec.Cmd
	dir  string
}

// startBroker starts straum-broker on a free loopback port with a fresh
// data directory, as the SDK's conformance runner does.
func startBroker(t *testing.T, clusterID string) *broker {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(liveBin(t, "STRAUM_BROKER_BIN"), "--data-dir", dir, "--listen", "127.0.0.1:0",
		"--cluster-id", clusterID, "--node-id", "1")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	b := &broker{cmd: cmd, dir: dir}
	t.Cleanup(b.stop)
	addr := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			if a, ok := strings.CutPrefix(s.Text(), "straum-broker: listening on "); ok {
				addr <- a
			}
		}
	}()
	select {
	case b.addr = <-addr:
	case <-time.After(30 * time.Second):
		t.Fatal("the broker did not start listening")
	}
	return b
}

func (b *broker) stop() {
	_ = b.cmd.Process.Kill()
	_ = b.cmd.Wait()
}

// proc runs the CLI binary with a private configuration and state
// directory.
type proc struct {
	t   *testing.T
	env []string
}

func newProc(t *testing.T, addr string) *proc {
	dir := t.TempDir()
	return &proc{t: t, env: append(os.Environ(),
		"STRAUM_CLI_BROKER="+addr,
		"STRAUM_CLI_CONFIG="+filepath.Join(dir, "config"),
		"XDG_STATE_HOME="+filepath.Join(dir, "state"),
	)}
}

func (p *proc) command(stdin string, args ...string) *exec.Cmd {
	cmd := exec.Command(cliBin, args...)
	cmd.Env = p.env
	cmd.Stdin = strings.NewReader(stdin)
	return cmd
}

func (p *proc) run(stdin string, args ...string) (int, string, string) {
	p.t.Helper()
	cmd := p.command(stdin, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		p.t.Fatal(err)
	}
	return cmd.ProcessState.ExitCode(), out.String(), errOut.String()
}

// ok runs a command that must exit 0 and returns its stdout.
func (p *proc) ok(stdin string, args ...string) string {
	p.t.Helper()
	code, out, errOut := p.run(stdin, args...)
	if code != 0 {
		p.t.Fatalf("%v: exit %d: %s", args, code, errOut)
	}
	return out
}

func (p *proc) exit(want int, msg string, args ...string) {
	p.t.Helper()
	code, _, errOut := p.run("", args...)
	if code != want || !strings.Contains(errOut, msg) {
		p.t.Fatalf("%v: exit %d, stderr %q; want exit %d with %q", args, code, errOut, want, msg)
	}
}

func jsonLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("%q: %v", l, err)
		}
		rows = append(rows, m)
	}
	return rows
}

func bodies(t *testing.T, out string) []string {
	var bs []string
	for _, m := range jsonLines(t, out) {
		bs = append(bs, fmt.Sprint(m["body"]))
	}
	return bs
}

func TestLiveAdmin(t *testing.T) {
	b := startBroker(t, "live")
	p := newProc(t, b.addr)

	out := p.ok("", "ping", "-c", "2", "--interval", "10ms", "-o", "json")
	if rows := jsonLines(t, out); len(rows) != 3 || rows[0]["cluster_id"] != "live" || rows[2]["type"] != "pong" {
		t.Fatalf("ping: %s", out)
	}
	if out := p.ok("", "topic", "create", "orders", "--retention", "1h"); out != "created orders\n" {
		t.Fatalf("create: %q", out)
	}
	p.exit(exitRefused, "TopicExists", "topic", "create", "orders")
	if out := p.ok("", "topic", "create", "orders", "--if-not-exists"); out != "exists orders\n" {
		t.Fatalf("if-not-exists: %q", out)
	}
	p.exit(exitRefused, "Forbidden", "topic", "create", "__internal")
	p.exit(exitRefused, "Unsupported", "topic", "create", "wide", "--partitions", "2")
	p.exit(exitRefused, "MalformedRequest", "topic", "create", "bad/name")

	d := jsonLines(t, p.ok("", "topic", "describe", "orders", "-o", "json"))
	if len(d) != 1 || d[0]["retention_ms"] != float64(3600000) || d[0]["name"] != "orders" {
		t.Fatalf("describe: %v", d)
	}
	if out := p.ok("", "topic", "describe", "orders"); !strings.Contains(out, "retention           1h0m0s") {
		t.Fatalf("describe table: %s", out)
	}
	p.exit(exitRefused, "UnknownTopic", "topic", "describe", "missing")

	// More topics than one ListTopics page (1000): the CLI follows the
	// cursor.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := client.New(ctx, client.Config{Addrs: []string{b.addr}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a := admin.New(c)
	for i := range client.MaxRequestTopics + 4 {
		if err := a.CreateTopic(ctx, fmt.Sprintf("t%04d", i), admin.TopicConfig{}); err != nil {
			t.Fatal(err)
		}
	}
	all := jsonLines(t, p.ok("", "topic", "list", "-o", "json"))
	if len(all) != client.MaxRequestTopics+5 || all[0]["name"] != "orders" || all[len(all)-1]["name"] != "t1003" {
		t.Fatalf("list: %d topics, first %v last %v", len(all), all[0], all[len(all)-1])
	}
	if out := p.ok("", "topic", "list", "--limit", "1002"); strings.Count(out, "\n") != 1003 || !strings.HasPrefix(out, "NAME ") {
		t.Fatalf("list --limit: %d lines", strings.Count(out, "\n"))
	}

	for _, args := range [][]string{{"stats", "-o", "json"}, {"broker", "stats", "-o", "json"}} {
		s := jsonLines(t, p.ok("", args...))
		if len(s) != 1 || s[0]["node_id"] != float64(1) || s[0]["open_connections"] == nil {
			t.Fatalf("%v: %v", args, s)
		}
	}
	if out := p.ok("", "stats"); !strings.Contains(out, "messages_accepted") {
		t.Fatalf("stats table: %s", out)
	}

	// A profile in the configuration file selects the broker.
	q := newProc(t, "")
	q.env = append(q.env, "STRAUM_CLI_BROKER=")
	q.ok("", "config", "set", "broker", b.addr, "--profile", "live")
	q.ok("", "config", "use", "live")
	if out := q.ok("", "topic", "list", "--limit", "1"); !strings.Contains(out, "orders") {
		t.Fatalf("profile: %s", out)
	}
	if out := q.ok("", "config", "show"); !strings.Contains(out, b.addr+"  file") {
		t.Fatalf("config show: %s", out)
	}
}

func TestLiveProduceConsume(t *testing.T) {
	b := startBroker(t, "live")
	p := newProc(t, b.addr)
	p.ok("", "topic", "create", "orders")
	dir := t.TempDir()
	f1, f2 := filepath.Join(dir, "f1"), filepath.Join(dir, "f2")
	os.WriteFile(f1, []byte("file one\nline two\n"), 0o600)
	os.WriteFile(f2, []byte{0xff, 0xfe}, 0o600)

	acks := jsonLines(t, p.ok("", "produce", "orders", "a1", "a2", "--key", "k", "--header", "h=v", "--header", "h=w", "-o", "json"))
	if len(acks) != 1 || acks[0]["base_seq"] != float64(0) || acks[0]["count"] != float64(2) {
		t.Fatalf("args: %v", acks)
	}
	p.ok("s1\ns2\n", "produce", "orders", "--stdin", "--key", "")                                                                             // seqs 2, 3 with an empty key
	p.ok("raw\nall\n", "produce", "orders", "--stdin", "--raw")                                                                               // 4
	p.ok("", "produce", "orders", "--file", f1, "--file", f2)                                                                                 // 5, 6
	p.ok("", "produce", "orders", "--file", f1, "--lines")                                                                                    // 7, 8
	p.ok(`{"key":{"base64":"/w=="},"headers":[{"name":"n","value":"v"}],"body":"j"}`+"\n", "produce", "orders", "--stdin", "--input", "json") // 9

	all := p.ok("", "consume", "orders", "-o", "json")
	want := []string{"a1", "a2", "s1", "s2", "raw\nall\n", "file one\nline two\n", "map[base64://4=]", "file one", "line two", "j"}
	if got := bodies(t, all); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("bodies %q, want %q", got, want)
	}
	rows := jsonLines(t, all)
	if rows[0]["key"] != "k" || fmt.Sprint(rows[0]["headers"]) != "[map[name:h value:v] map[name:h value:w]]" ||
		rows[2]["key"] != "" || rows[4]["key"] != nil || fmt.Sprint(rows[9]["key"]) != "map[base64:/w==]" {
		t.Fatalf("keys and headers: %v", rows)
	}
	// consume -o json feeds produce --input json unchanged.
	p.ok("", "topic", "create", "copy")
	p.ok(all, "produce", "copy", "--stdin", "--input", "json")
	if copied := p.ok("", "consume", "copy", "-o", "json"); strings.Join(bodies(t, copied), "|") != strings.Join(want, "|") {
		t.Fatalf("copy: %s", copied)
	}

	if out := p.ok("", "consume", "orders", "--from", "3", "-n", "2"); !strings.Contains(out, "\t3\t") || !strings.Contains(out, "\t4\t") || strings.Count(out, "\n") != 2 {
		// seq 4's body has newlines: quoted, on one line
		t.Fatalf("--from 3 -n 2: %q", out)
	}
	if got := bodies(t, p.ok("", "consume", "orders", "--until-seq", "2", "-o", "json")); len(got) != 2 {
		t.Fatalf("--until-seq: %v", got)
	}
	if out := p.ok("", "consume", "orders", "--from", "latest"); out != "" {
		t.Fatalf("--from latest without --follow: %q", out)
	}
	if out := p.ok("", "consume", "orders", "-n", "1", "--print", "headers"); !strings.HasSuffix(out, "\th=v,h=w\n") {
		t.Fatalf("--print headers: %q", out)
	}
	if got := bodies(t, p.ok("", "consume", "orders", "-n", "1", "--bytes", "base64", "-o", "json")); got[0] != "map[base64:YTE=]" {
		t.Fatalf("--bytes base64: %v", got)
	}
	// Small fetches: many batches, same result.
	if got := bodies(t, p.ok("", "consume", "orders", "--max-bytes", "1", "-o", "json")); len(got) != 10 {
		t.Fatalf("--max-bytes 1: %v", got)
	}

	// Batching: 2500 lines in batches of at most 1000.
	var lines strings.Builder
	for i := range 2500 {
		fmt.Fprintf(&lines, "m%d\n", i)
	}
	p.ok("", "topic", "create", "bulk")
	acks = jsonLines(t, p.ok(lines.String(), "produce", "bulk", "--stdin", "--batch-messages", "1000", "--linger", "1s", "-o", "json"))
	if len(acks) != 3 || acks[0]["count"] != float64(1000) || acks[2]["base_seq"] != float64(2000) {
		t.Fatalf("batches: %v", acks)
	}
	if n := len(bodies(t, p.ok("", "consume", "bulk", "-o", "json"))); n != 2500 {
		t.Fatalf("bulk: %d messages", n)
	}

	p.exit(exitRefused, "UnknownTopic", "produce", "missing", "x")
	p.exit(exitRefused, "UnknownTopic", "consume", "missing")
	p.exit(exitRefused, "UnknownPartition", "produce", "orders", "x", "--partition", "3")
	p.exit(exitRefused, "no partition 3", "consume", "orders", "--partition", "3")
	p.exit(exitRefused, "MessageTooLarge", "produce", "orders", "x", "--key", strings.Repeat("k", 2000))
	big := filepath.Join(dir, "big")
	os.WriteFile(big, make([]byte, 1<<20+1), 0o600)
	p.exit(exitUsage, "max_message_bytes", "produce", "orders", "--file", big)
}

func TestLiveFollowAndLimits(t *testing.T) {
	b := startBroker(t, "live")
	p := newProc(t, b.addr)
	p.ok("", "topic", "create", "events")

	// --count not reached within --duration: exit 2.
	start := time.Now()
	p.exit(exitLimit, "--duration ended after 0 of 5", "consume", "events", "--follow", "--duration", "500ms", "-n", "5", "--max-wait", "100ms")
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("--duration took %v", d)
	}
	// --duration without --count: exit 0.
	p.ok("", "consume", "events", "--follow", "--duration", "300ms")

	// --follow prints messages produced while it runs; Ctrl-C ends it with
	// exit 0.
	cmd := p.command("", "consume", "events", "--follow", "--max-wait", "200ms", "-o", "json")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 10)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			lines <- s.Text()
		}
		close(lines)
	}()
	time.Sleep(300 * time.Millisecond)
	p.ok("", "produce", "events", "e1")
	p.ok("", "produce", "events", "e2")
	for _, want := range []string{"e1", "e2"} {
		select {
		case l := <-lines:
			if !strings.Contains(l, `"body":"`+want+`"`) {
				t.Fatalf("follow: %q, want %s", l, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("follow: %s not printed", want)
		}
	}
	_ = cmd.Process.Signal(syscall.SIGINT)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("--follow after Ctrl-C: %v", err)
	}

	// --count with --follow stops once reached, exit 0.
	if got := bodies(t, p.ok("", "consume", "events", "--follow", "-n", "2", "-o", "json")); len(got) != 2 {
		t.Fatalf("--follow -n 2: %v", got)
	}
}

func TestLivePositions(t *testing.T) {
	b := startBroker(t, "live")
	p := newProc(t, b.addr)
	p.ok("", "topic", "create", "orders")
	p.ok("", "produce", "orders", "m0", "m1", "m2", "m3", "m4")

	consume := func(args ...string) []string {
		return bodies(t, p.ok("", append([]string{"consume", "orders", "-o", "json"}, args...)...))
	}
	if got := consume("--name", "g", "-n", "2"); strings.Join(got, ",") != "m0,m1" {
		t.Fatalf("first: %v", got)
	}
	if got := consume("--name", "g"); strings.Join(got, ",") != "m2,m3,m4" {
		t.Fatalf("continues: %v", got)
	}
	if got := consume("--name", "g"); len(got) != 0 {
		t.Fatalf("at the end: %v", got)
	}
	if got := consume("-n", "1"); strings.Join(got, ",") != "m0" {
		t.Fatalf("unnamed consumers save nothing: %v", got)
	}
	ps := jsonLines(t, p.ok("", "positions", "list", "-o", "json"))
	if len(ps) != 1 || ps[0]["name"] != "g" || ps[0]["next_seq"] != float64(5) || ps[0]["cluster_id"] != "live" {
		t.Fatalf("positions: %v", ps)
	}
	p.ok("", "positions", "set", "orders", "--name", "g", "--partition", "0", "--seq", "3")
	if got := consume("--name", "g"); strings.Join(got, ",") != "m3,m4" {
		t.Fatalf("after set: %v", got)
	}
	p.ok("", "positions", "reset", "orders", "--name", "g", "--to", "earliest")
	if got := consume("--name", "g", "-n", "1"); strings.Join(got, ",") != "m0" {
		t.Fatalf("after reset --to earliest: %v", got)
	}
	p.ok("", "positions", "reset", "orders", "--name", "g", "--to", "latest")
	p.ok("", "produce", "orders", "m5")
	if got := consume("--name", "g"); strings.Join(got, ",") != "m5" {
		t.Fatalf("after reset --to latest: %v", got)
	}
	p.exit(exitUsage, "past partition 0's next seq", "positions", "set", "orders", "--name", "g", "--partition", "0", "--seq", "100")
	p.exit(exitUsage, "no saved position", "consume", "orders", "--name", "other", "--from", "saved")

	// A broker re-created with the same cluster id: the saved position is
	// past its end, which the CLI refuses until reset.
	b.stop()
	b2 := startBroker(t, "live")
	q := &proc{t: t, env: append(append([]string(nil), p.env...), "STRAUM_CLI_BROKER="+b2.addr)}
	q.ok("", "topic", "create", "orders")
	q.exit(exitUsage, "positions reset", "consume", "orders", "--name", "g")
	q.ok("", "positions", "reset", "orders", "--name", "g")
	q.ok("", "consume", "orders", "--name", "g")
	if ps := jsonLines(t, q.ok("", "positions", "list", "-o", "json")); len(ps) != 0 {
		t.Fatalf("after reset: %v", ps)
	}
}

// Exit codes a real broker cannot be made to produce, against scripted
// fake-server scenarios in testdata/fake: each file names the CLI's
// arguments, the expected exit code and the server's script.
func TestLiveFakeServerExitCodes(t *testing.T) {
	fake := liveBin(t, "STRAUM_FAKE_SERVER_BIN")
	files, _ := filepath.Glob("testdata/fake/*.json")
	if len(files) == 0 {
		t.Fatal("no scenarios in testdata/fake")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var sc struct {
				CLI  []string `json:"cli"`
				Exit int      `json:"exit"`
			}
			if err := json.Unmarshal(data, &sc); err != nil {
				t.Fatal(err)
			}
			srv := exec.Command(fake, f)
			var srvErr bytes.Buffer
			srv.Stderr = &srvErr
			out, err := srv.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := srv.Start(); err != nil {
				t.Fatal(err)
			}
			line, err := bufio.NewReader(out).ReadString('\n')
			addr, ok := strings.CutPrefix(strings.TrimSpace(line), "straum-fake-server: listening on ")
			if err != nil || !ok {
				_ = srv.Process.Kill()
				t.Fatalf("fake server: %q %v", line, err)
			}
			code, _, errOut := newProc(t, addr).run("", sc.CLI...)
			if err := srv.Wait(); err != nil {
				t.Errorf("fake server: %v: %s", err, srvErr.String())
			}
			if code != sc.Exit {
				t.Fatalf("exit %d, want %d; stderr %s", code, sc.Exit, errOut)
			}
		})
	}
}
