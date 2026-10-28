package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files and docs/commands")

// cli runs the command in-process with a private configuration file and
// state directory, plus the given environment.
type cli struct {
	t   *testing.T
	env map[string]string
	dir string
}

func newCLI(t *testing.T) *cli {
	dir := t.TempDir()
	return &cli{t: t, dir: dir, env: map[string]string{
		"STRAUM_CLI_CONFIG": filepath.Join(dir, "config"),
		"XDG_STATE_HOME":    filepath.Join(dir, "state"),
	}}
}

func (c *cli) run(stdin string, args ...string) (int, string, string) {
	c.t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, func(k string) string { return c.env[k] }, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func (c *cli) writeConfig(text string) {
	c.t.Helper()
	if err := os.WriteFile(c.env["STRAUM_CLI_CONFIG"], []byte(text), 0o600); err != nil {
		c.t.Fatal(err)
	}
}

// golden compares got with testdata/name, or rewrites it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run make golden", err)
	}
	if got != string(want) {
		t.Errorf("%s differs; run make golden after an intended change\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// Errors found before anything is sent exit 1 for every command; an
// unreachable broker exits 3. The broker address is a closed port.
func TestCommandsUsageAndConnect(t *testing.T) {
	cases := []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"nosuch"}, exitUsage, "unknown command"},
		{[]string{"ping", "--count", "0"}, exitUsage, "--count"},
		{[]string{"ping", "--tls"}, exitUsage, "reserved"},
		{[]string{"ping", "--username", "u"}, exitUsage, "reserved"},
		{[]string{"ping", "--broker", "nohost"}, exitUsage, "host:port"},
		{[]string{"ping", "-o", "yaml"}, exitUsage, "table or json"},
		{[]string{"ping", "--profile", "missing"}, exitUsage, `profile "missing" is not in`},
		{[]string{"ping", "--profile", "../x"}, exitUsage, "profile"},
		{[]string{"topic", "create"}, exitUsage, "accepts 1 arg"},
		{[]string{"topic", "list", "--limit", "-1"}, exitUsage, "--limit"},
		{[]string{"topic", "describe"}, exitUsage, "accepts 1 arg"},
		{[]string{"produce", "t"}, exitUsage, "no messages"},
		{[]string{"produce", "t", "a", "--stdin"}, exitUsage, "one source"},
		{[]string{"produce", "t", "--stdin", "--lines"}, exitUsage, "--lines goes with --file"},
		{[]string{"produce", "t", "a", "--raw"}, exitUsage, "--raw goes with --stdin"},
		{[]string{"produce", "t", "a", "--input", "json"}, exitUsage, "--input json reads lines"},
		{[]string{"produce", "t", "--stdin", "--input", "json", "--key", "k"}, exitUsage, "drop --key"},
		{[]string{"produce", "t", "a", "--acks", "written"}, exitUsage, "only durable"},
		{[]string{"produce", "t", "a", "--header", "novalue"}, exitUsage, "name=value"},
		{[]string{"produce", "t", "a", "--batch-messages", "0"}, exitUsage, "positive"},
		{[]string{"consume", "t", "--bytes", "hex"}, exitUsage, "--bytes"},
		{[]string{"consume", "t", "--print", "keys"}, exitUsage, "--print"},
		{[]string{"consume", "t", "--name", "a/b"}, exitUsage, "--name"},
		{[]string{"consume", "t", "--max-bytes", "0"}, exitUsage, "--max-bytes"},
		{[]string{"positions", "reset", "t"}, exitUsage, "required flag"},
		{[]string{"positions", "set", "t", "--name", "n", "--seq", "1"}, exitUsage, "--partition is required"},
		{[]string{"config", "set", "colour", "red"}, exitUsage, "unknown key"},
		{[]string{"config", "set", "tls", "true"}, exitUsage, "reserved"},
		{[]string{"config", "set", "timeout", "0s"}, exitUsage, "positive duration"},
		{[]string{"config", "use", "nope"}, exitUsage, "not in"},
		// 127.0.0.1:1 refuses connections.
		{[]string{"ping"}, exitConnect, "connect"},
		{[]string{"topic", "create", "t"}, exitConnect, "connect"},
		{[]string{"topic", "list"}, exitConnect, "connect"},
		{[]string{"topic", "describe", "t"}, exitConnect, "connect"},
		{[]string{"produce", "t", "a"}, exitConnect, "connect"},
		{[]string{"consume", "t"}, exitConnect, "connect"},
		{[]string{"broker", "stats"}, exitConnect, "connect"},
		{[]string{"stats"}, exitConnect, "connect"},
		{[]string{"positions", "set", "t", "--name", "n", "--partition", "0", "--seq", "1"}, exitConnect, "connect"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			c := newCLI(t)
			c.env["STRAUM_CLI_BROKER"] = "127.0.0.1:1"
			code, out, errOut := c.run("", tc.args...)
			if code != tc.code || !strings.Contains(errOut, tc.msg) {
				t.Fatalf("exit %d, stderr %q; want exit %d with %q", code, errOut, tc.code, tc.msg)
			}
			if out != "" {
				t.Errorf("stdout %q, want nothing", out)
			}
			if !strings.HasPrefix(errOut, "straum-cli: ") {
				t.Errorf("stderr %q lacks the straum-cli: prefix", errOut)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := newCLI(t).run("", "version")
	if code != 0 || !strings.HasPrefix(out, "straum-cli dev\n") || !strings.Contains(out, "straum-go-sdk ") {
		t.Fatalf("exit %d: %q", code, out)
	}
}
