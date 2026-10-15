//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestE2EThreeNodes runs `status` against a real three-node cluster on
// loopback. STRAUM_SERVER_BIN is the core's straum-server binary
// (make e2e builds it from ../straum).
func TestE2EThreeNodes(t *testing.T) {
	bin := os.Getenv("STRAUM_SERVER_BIN")
	if bin == "" {
		t.Fatal("STRAUM_SERVER_BIN is not set")
	}
	data := t.TempDir()
	var peers, metrics []string
	for i := range 3 {
		peers = append(peers, fmt.Sprintf("%d=%s", i, deadAddr(t)))
		metrics = append(metrics, deadAddr(t))
	}
	procs := make([]*exec.Cmd, 3)
	for i := range 3 {
		cmd := exec.Command(bin)
		cmd.Env = []string{
			"STRAUM_EXPERIMENTAL_CLUSTER=1",
			fmt.Sprintf("STRAUM_CLUSTER_NODE_ID=%d", i),
			"STRAUM_CLUSTER_PEERS=" + strings.Join(peers, ","),
			"STRAUM_CLUSTER_MQTT_LISTEN=" + deadAddr(t),
			"STRAUM_CLUSTER_METRICS_LISTEN=" + metrics[i],
			fmt.Sprintf("STRAUM_CLUSTER_DATA_DIR=%s/n%d", data, i),
			"STRAUM_LOG_LEVEL=warn",
		}
		if testing.Verbose() {
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		procs[i] = cmd
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	}
	nodes := strings.Join(metrics, ",")
	status := func() (int, report) {
		code, stdout, _ := runCLI(t, nil, "status", "--node", nodes, "-o", "json")
		var r report
		if err := json.Unmarshal([]byte(stdout), &r); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, stdout)
		}
		return code, r
	}

	// Up: every node observed and serving, one of them sequencing.
	deadline := time.Now().Add(20 * time.Second)
	for {
		code, r := status()
		leading, serving := 0, 0
		for _, n := range r.Nodes {
			leading += int(n.Metrics["straum_leading"])
			serving += int(n.Metrics["straum_serving"])
		}
		if code == exitOK && leading == 1 && serving == 3 {
			for i, n := range r.Nodes {
				if n.Address != metrics[i] || n.Metrics["straum_node"] != uint64(i) {
					t.Errorf("node %d answered for another: %+v", i, n)
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cluster not up: exit %d, %+v", code, r)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// One node down: it is unknown, the others still observed.
	procs[2].Process.Kill()
	procs[2].Wait()
	code, r := status()
	if code != exitPartial || r.Nodes[2].State != stateUnknown || r.Nodes[0].State != stateObserved || r.Nodes[1].State != stateObserved {
		t.Fatalf("one down: exit %d, %+v", code, r)
	}

	// All down.
	for _, p := range procs[:2] {
		p.Process.Kill()
		p.Wait()
	}
	if code, r := status(); code != exitNone {
		t.Fatalf("all down: exit %d, %+v", code, r)
	}
}
