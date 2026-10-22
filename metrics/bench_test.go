package metrics

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"
)

// BenchmarkParse is parsing one node's render, the core's 16 KiB.
func BenchmarkParse(b *testing.B) {
	body := []byte(fixture(b, "node0.prom"))
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for b.Loop() {
		if _, err := parseSamples(bytes.NewReader(body)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCollect is one round against 5 nodes on loopback.
func BenchmarkCollect(b *testing.B) {
	body := fixture(b, "node0.prom")
	var addrs []string
	for range 5 {
		addr, _ := serve(b, http.StatusOK, body)
		addrs = append(addrs, addr)
	}
	c := Client{Timeout: time.Second}
	b.ReportAllocs()
	for b.Loop() {
		r, err := c.Collect(context.Background(), addrs)
		if err != nil || r.Nodes[0].State != Observed {
			b.Fatal(err, r.Nodes[0].Error)
		}
	}
}
