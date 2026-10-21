package main

import (
	"crypto/tls"
	"os"
	"strings"
	"testing"
	"time"
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
		{"password, no user", map[string]string{"STRAUM_CLI_PASSWORD": "p"}, append([]string{"pub", "-t", "a", "-m", "x"}, b...), "needs a user name"},
		{"password in clear text", map[string]string{"STRAUM_CLI_PASSWORD": "p"}, []string{"pub", "--broker", "10.0.0.5:1883", "--username", "u", "-t", "a", "-m", "x"}, "in clear text"},
		{"no password file", nil, append([]string{"pub", "-t", "a", "-m", "x", "--username", "u", "--password-file", "/nonexistent/p"}, b...), "--password-file"},
		{"empty password file", nil, append([]string{"pub", "-t", "a", "-m", "x", "--username", "u", "--password-file", os.DevNull}, b...), "the first line is empty"},
		{"no CA file", nil, append([]string{"pub", "-t", "a", "-m", "x", "--tls-ca", "/nonexistent/ca.pem"}, b...), "--tls-ca"},
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

func TestTLSAndCredentialFlags(t *testing.T) {
	dir := t.TempDir()
	notPEM := dir + "/ca.pem"
	if err := os.WriteFile(notPEM, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	pw := dir + "/password"
	if err := os.WriteFile(pw, []byte("s3cret\r\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var m mqttFlags
	m.broker, m.timeout, m.tlsCA = "10.0.0.5:8883", time.Second, notPEM
	if err := m.check(func(string) string { return "" }); err == nil || !strings.Contains(err.Error(), "no PEM certificate") {
		t.Errorf("a CA file without a certificate: %v", err)
	}

	// The first line of the file, its line ending dropped; a CA implies TLS,
	// and TLS lets a password leave loopback.
	m = mqttFlags{broker: "10.0.0.5:8883", timeout: time.Second, username: "u", passwordFile: pw, tlsServerName: "node-a"}
	if err := m.check(func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if string(m.password) != "s3cret" || m.tlsConfig == nil || m.tlsConfig.ServerName != "node-a" || m.tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("password %q, tls %+v", m.password, m.tlsConfig)
	}

	// On loopback a password may go without TLS, as the core allows.
	m = mqttFlags{broker: "127.0.0.1:1883", timeout: time.Second}
	if err := m.check(func(k string) string {
		return map[string]string{"STRAUM_CLI_USERNAME": "u", "STRAUM_CLI_PASSWORD": "p"}[k]
	}); err != nil || m.username != "u" || m.tlsConfig != nil {
		t.Errorf("loopback: %v, %+v", err, m)
	}
}
