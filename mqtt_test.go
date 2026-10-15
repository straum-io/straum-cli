package main

import (
	"strings"
	"testing"
)

// Behaviour against a broker is in e2e_test.go, against a real cluster.

func TestPubSubUsage(t *testing.T) {
	b := []string{"--broker", "127.0.0.1:1"}
	tests := []struct {
		name    string
		env     map[string]string
		args    []string
		wantErr string
	}{
		{"pub no broker", nil, []string{"pub", "-t", "a", "-m", "x"}, "no broker"},
		{"sub no broker", nil, []string{"sub", "-t", "a"}, "no broker"},
		{"bad broker", nil, []string{"pub", "--broker", "mqtt://h:1883", "-t", "a", "-m", "x"}, "want host:port"},
		{"bad env broker", map[string]string{"STRAUM_CLI_BROKER": "h"}, []string{"pub", "-t", "a", "-m", "x"}, "want host:port"},
		{"pub no topic", nil, append([]string{"pub", "-m", "x"}, b...), "--topic"},
		{"sub no topic", nil, append([]string{"sub"}, b...), "--topic"},
		{"pub qos 2", nil, append([]string{"pub", "-t", "a", "-m", "x", "-q", "2"}, b...), "QoS 2"},
		{"sub qos 2", nil, append([]string{"sub", "-t", "a", "-q", "2"}, b...), "QoS 2"},
		{"no payload", nil, append([]string{"pub", "-t", "a"}, b...), "exactly one of -m and --stdin"},
		{"two payloads", nil, append([]string{"pub", "-t", "a", "-m", "x", "--stdin"}, b...), "exactly one of -m and --stdin"},
		{"retain handling", nil, append([]string{"sub", "-t", "a", "--retain-handling", "3"}, b...), "--retain-handling"},
		{"negative count", nil, append([]string{"sub", "-t", "a", "-c", "-1"}, b...), "must not be negative"},
		{"output", nil, append([]string{"sub", "-t", "a", "-o", "table"}, b...), "want plain or json"},
		{"keep-alive fraction", nil, append([]string{"sub", "-t", "a", "--keep-alive", "1500ms"}, b...), "--keep-alive"},
		{"timeout", nil, append([]string{"pub", "-t", "a", "-m", "x", "--timeout", "0s"}, b...), "--timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.env, tt.args...)
			if code != exitUsage || stdout != "" || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit 1 and %q", code, stdout, stderr, tt.wantErr)
			}
		})
	}
}

func TestPubPayloadLimit(t *testing.T) {
	var out, errOut strings.Builder
	big := strings.NewReader(strings.Repeat("x", maxPayload+1))
	code := run(t.Context(), []string{"pub", "--broker", deadAddr(t), "-t", "a", "--stdin"}, func(string) string { return "" }, big, &out, &errOut)
	if code != exitUsage || !strings.Contains(errOut.String(), "larger than") {
		t.Errorf("exit %d, %q", code, errOut.String())
	}
}

// Nothing listening: nothing was sent, so the outcome is known.
func TestPubSubNoBroker(t *testing.T) {
	dead := deadAddr(t)
	for _, args := range [][]string{
		{"pub", "--broker", dead, "-t", "a", "-m", "x"},
		{"sub", "--broker", dead, "-t", "a"},
	} {
		code, stdout, stderr := runCLI(t, nil, args...)
		if code != exitRefused || stdout != "" || !strings.Contains(stderr, "connection refused") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", args[0], code, stdout, stderr)
		}
	}
	env := map[string]string{"STRAUM_CLI_BROKER": dead}
	if code, _, _ := runCLI(t, env, "pub", "-t", "a", "-m", "x"); code != exitRefused {
		t.Errorf("broker from env: exit %d", code)
	}
}
