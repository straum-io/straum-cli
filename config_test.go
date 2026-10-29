package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestParseConfigErrors(t *testing.T) {
	cases := []struct{ text, msg string }{
		{"broker = 127.0.0.1:1\n", ":1: only current"},
		{"[profile a]\ncolour = red\n", `:2: unknown key "colour"`},
		{"[profile a]\ntls = true\n", ":2: tls is reserved"},
		{"[profile a]\nusername = u\n", ":2: username is reserved"},
		{"[profile a]\nbroker\n", ":2: want key = value"},
		{"[profile a]\ntimeout = soon\n", ":2: timeout"},
		{"[profile a]\noutput = yaml\n", ":2: output"},
		{"[profile a]\nbroker = host\n", ":2: broker"},
		{"[profile a]\nmax_in_flight_bytes = 0\n", ":2: max_in_flight_bytes"},
		{"[profile a]\ntimeout = 1s\ntimeout = 2s\n", ":3: timeout set twice"},
		{"[profile a]\n[profile a]\n", ":2: profile a appears twice"},
		{"[prof a]\n", ":1: want [profile <name>]"},
		{"[profile a/b]\n", ":1: want [profile <name>]"},
	}
	for _, tc := range cases {
		_, err := parseConfig("cfg", []byte(tc.text))
		if err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%q: %v, want %q", tc.text, err, tc.msg)
		}
	}
	cf, err := parseConfig("cfg", []byte("# c\ncurrent = b\n\n[profile a]\n  broker = 127.0.0.1:1, 127.0.0.1:2 # not a comment\n"))
	if err == nil {
		t.Fatalf("a trailing # is part of the value, which is then not an address: %+v", cf)
	}
}

// Precedence: flag, then environment, then the selected profile, then the
// default; the profile from --profile, then STRAUM_CLI_PROFILE, then
// current.
func TestSettingsPrecedence(t *testing.T) {
	const file = `current = a
[profile a]
broker = 127.0.0.1:1001
timeout = 3s
output = json
[profile b]
broker = 127.0.0.1:1002
`
	cases := []struct {
		name string
		env  map[string]string
		args []string
		want map[string]string // key -> value/source
	}{
		{"file current", nil, nil, map[string]string{
			"profile": "a/file", "broker": "127.0.0.1:1001/file", "timeout": "3s/file", "output": "json/file", "connect_timeout": "10s/default"}},
		{"env profile", map[string]string{"STRAUM_CLI_PROFILE": "b"}, nil, map[string]string{
			"profile": "b/env", "broker": "127.0.0.1:1002/file", "timeout": "10s/default", "output": "table/default"}},
		{"flag profile beats env", map[string]string{"STRAUM_CLI_PROFILE": "b"}, []string{"--profile", "a"}, map[string]string{
			"profile": "a/flag", "broker": "127.0.0.1:1001/file"}},
		{"env beats file", map[string]string{"STRAUM_CLI_BROKER": "127.0.0.1:2000", "STRAUM_CLI_TIMEOUT": "4s", "STRAUM_CLI_OUTPUT": "table"}, nil, map[string]string{
			"broker": "127.0.0.1:2000/env", "timeout": "4s/env", "output": "table/env"}},
		{"flag beats env", map[string]string{"STRAUM_CLI_BROKER": "127.0.0.1:2000"}, []string{"--broker", "127.0.0.1:3000", "--timeout", "5s", "--connect-timeout", "6s"}, map[string]string{
			"broker": "127.0.0.1:3000/flag", "timeout": "5s/flag", "connect_timeout": "6s/flag"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCLI(t)
			c.writeConfig(file)
			for k, v := range tc.env {
				c.env[k] = v
			}
			code, out, errOut := c.run("", append([]string{"config", "show"}, tc.args...)...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			got := map[string]string{}
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				if strings.HasPrefix(line, "{") { // the profile chose -o json
					var r struct{ Key, Value, Source string }
					if err := json.Unmarshal([]byte(line), &r); err != nil {
						t.Fatal(err)
					}
					got[r.Key] = r.Value + "/" + r.Source
					continue
				}
				f := strings.Fields(line)
				got[f[0]] = f[1] + "/" + f[2]
			}
			for k, want := range tc.want {
				if got[k] != want {
					t.Errorf("%s = %s, want %s", k, got[k], want)
				}
			}
		})
	}
}

func TestConfigNoFileDefaults(t *testing.T) {
	c := newCLI(t)
	code, out, errOut := c.run("", "config", "show")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	golden(t, "config-show.golden.txt", out)
	code, out, _ = c.run("", "config", "show", "-o", "json")
	if code != 0 {
		t.Fatal(code)
	}
	golden(t, "config-show.golden.json", out)
}

// config set and config use change single lines and keep comments; the
// file stays valid.
func TestConfigSetUse(t *testing.T) {
	c := newCLI(t)
	c.writeConfig("# team settings\n[profile a]\n# the dev broker\nbroker = 127.0.0.1:1001\n\n[profile b]\ntimeout = 2s\n")
	steps := [][]string{
		{"config", "set", "timeout", "7s", "--profile", "a"},
		{"config", "set", "broker", "127.0.0.1:1009", "--profile", "a"},
		{"config", "set", "output", "json", "--profile", "c"},
		{"config", "use", "b"},
		{"config", "set", "broker", "127.0.0.1:1002"}, // the current profile, b
		{"config", "use", "a"},
	}
	for _, s := range steps {
		if code, _, errOut := c.run("", s...); code != 0 {
			t.Fatalf("%v: exit %d: %s", s, code, errOut)
		}
	}
	data, err := os.ReadFile(c.env["STRAUM_CLI_CONFIG"])
	if err != nil {
		t.Fatal(err)
	}
	want := `current = a
# team settings
[profile a]
# the dev broker
broker = 127.0.0.1:1009
timeout = 7s

[profile b]
timeout = 2s
broker = 127.0.0.1:1002

[profile c]
output = json
`
	if string(data) != want {
		t.Fatalf("file:\n%s\nwant:\n%s", data, want)
	}
	code, out, _ := c.run("", "config", "profiles")
	if code != 0 || out != "* a\n  b\n  c\n" {
		t.Fatalf("profiles: %d %q", code, out)
	}
	code, out, _ = c.run("", "config", "path")
	if code != 0 || out != c.env["STRAUM_CLI_CONFIG"]+"\n" {
		t.Fatalf("path: %d %q", code, out)
	}
}
