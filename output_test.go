package main

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/straum-io/straum-go-sdk/admin"
	"github.com/straum-io/straum-go-sdk/client"
	"github.com/straum-io/straum-go-sdk/consumer"
	"github.com/straum-io/straum-go-sdk/producer"
)

// Golden files fix every command's table and JSON output; make golden
// rewrites them after an intended change.
func TestGoldenOutput(t *testing.T) {
	ts := time.Date(2026, 10, 28, 12, 0, 0, 123e6, time.UTC)
	hello := client.BrokerInfo{Version: 0, ClusterID: "prod", NodeID: 1, MaxFrameBytes: 8 << 20, MaxMessageBytes: 1 << 20}
	desc := &admin.TopicDescription{Name: "orders", ReplicationFactor: 1, Retention: 24 * time.Hour,
		Partitions: []admin.PartitionDescription{{Partition: 0, StartSeq: 0, NextSeq: 42, MessageCount: 42, IndexBytes: 1344}}}
	stats := &admin.BrokerStats{NodeID: 1, OpenConnections: 2, PendingBytes: 3, PendingBytesPeak: 4, MaxPendingBytes: 5,
		FetchWaiters: 6, ProduceRequests: 7, GroupsCommitted: 8, Fsyncs: 9, MessagesAccepted: 10, MessagesDeduplicated: 11,
		BodyBytesReceived: 12, BodyBytesNew: 13, WalBytes: 14, SegmentBytes: 15, IndexBytes: 16}
	msgs := []consumer.Message{
		{Seq: 7, Timestamp: ts, Message: client.Message{Body: []byte("plain text")}},
		{Seq: 8, Timestamp: ts, Message: client.Message{Key: []byte{}, Body: []byte("tab\there")}},
		{Seq: 9, Timestamp: ts, Message: client.Message{Key: []byte("user-1"), Body: []byte{0xff, 0x00, 0x01},
			Headers: []client.Header{{Name: []byte("trace"), Value: []byte("abc")}, {Name: []byte("bin"), Value: []byte{0xfe}}}}},
		{Seq: 10, Timestamp: ts, Message: client.Message{Body: []byte{}}},
	}
	ps := []position{{ClusterID: "prod", Topic: "orders", Partition: 0, Name: "billing", NextSeq: 42, SavedAt: ts}}

	for _, format := range []string{"txt", "json"} {
		asJSON := format == "json"
		var b bytes.Buffer
		section := func(name string, err error) {
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		b.WriteString("# ping\n")
		section("hello", renderHello(&b, asJSON, "127.0.0.1:7171", hello))
		section("pong", renderPong(&b, asJSON, 1, 182*time.Microsecond))
		b.WriteString("# topic create\n")
		section("created", renderCreated(&b, asJSON, "orders", true))
		section("exists", renderCreated(&b, asJSON, "orders", false))
		b.WriteString("# topic list (two pages)\n")
		l := &topicLister{w: &b, asJSON: asJSON}
		section("page1", l.page([]admin.TopicSummary{{Name: "a", Partitions: 1}, {Name: "orders", Partitions: 1}}))
		section("page2", l.page([]admin.TopicSummary{{Name: "payments-eu", Partitions: 1}}))
		section("end", l.end())
		b.WriteString("# topic describe\n")
		section("describe", renderDescribe(&b, asJSON, desc))
		b.WriteString("# broker stats\n")
		section("stats", renderStats(&b, asJSON, stats))
		b.WriteString("# produce\n")
		section("ack", renderAck(&b, asJSON, "orders", producer.Ack{Partition: 0, BaseSeq: 40, Count: 2, LeaderEpoch: 1, Timestamp: ts}))
		if asJSON {
			b.WriteString("# produce, stopped by an input error\n")
			section("input stop", renderInputStop(&b, 1000, errors.New("stdin:1001: body: missing")))
		}
		b.WriteString("# consume\n")
		section("consume", renderMessages(&b, messageFormat{asJSON: asJSON}, "orders", 0, msgs))
		b.WriteString("# consume --print headers\n")
		section("consume headers", renderMessages(&b, messageFormat{asJSON: asJSON, headers: true}, "orders", 0, msgs[2:3]))
		b.WriteString("# consume --bytes base64\n")
		section("consume base64", renderMessages(&b, messageFormat{asJSON: asJSON, forceBase64: true}, "orders", 0, msgs[:1]))
		b.WriteString("# positions list\n")
		section("positions", renderPositions(&b, asJSON, ps))
		b.WriteString("# config profiles\n")
		section("profiles", renderProfiles(&b, asJSON, []string{"local", "staging"}, "local"))
		golden(t, "output.golden."+format, b.String())
	}
}

// consume -o json output is produce --input json input.
func TestJSONRoundTrip(t *testing.T) {
	m := consumer.Message{Seq: 1, Message: client.Message{Key: []byte{}, Body: []byte{0xff, 'a'},
		Headers: []client.Header{{Name: []byte("n"), Value: []byte{}}, {Name: []byte{0x80}, Value: []byte("v")}}}}
	for _, b64 := range []bool{false, true} {
		var b bytes.Buffer
		if err := renderMessages(&b, messageFormat{asJSON: true, forceBase64: b64}, "t", 0, []consumer.Message{m}); err != nil {
			t.Fatal(err)
		}
		got, err := parseJSONMessage(bytes.TrimSpace(b.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if got.Key == nil || len(got.Key) != 0 || !bytes.Equal(got.Body, m.Body) || len(got.Headers) != 2 ||
			!bytes.Equal(got.Headers[1].Name, []byte{0x80}) || got.Headers[0].Value == nil {
			t.Fatalf("base64 %v: %+v from %s", b64, got, b.String())
		}
	}
}
