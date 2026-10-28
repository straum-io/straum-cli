package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/straum-io/straum-go-sdk/admin"
	"github.com/straum-io/straum-go-sdk/client"
	"github.com/straum-io/straum-go-sdk/consumer"
	"github.com/straum-io/straum-go-sdk/producer"
)

// The JSON output's schema is in docs/output.md; fields are only added
// before 1.0.

func newTable(w io.Writer) *tabwriter.Writer { return tabwriter.NewWriter(w, 0, 8, 2, ' ', 0) }

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func formatTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// jsonBytes is a byte field in JSON: a string when the bytes are valid
// UTF-8 (unless forced to base64), else {"base64": "..."}; nil stays null.
func jsonBytes(b []byte, forceBase64 bool) any {
	if b == nil {
		return nil
	}
	if !forceBase64 && utf8.Valid(b) {
		return string(b)
	}
	return map[string]string{"base64": base64.StdEncoding.EncodeToString(b)}
}

// textBytes is a byte field in a table: the text when it is printable
// UTF-8, quoted when it is UTF-8 with control characters, else base64
// with the prefix "base64:".
func textBytes(b []byte, forceBase64 bool) string {
	if !forceBase64 && utf8.Valid(b) {
		s := string(b)
		if strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) && r != ' ' }) < 0 {
			return s
		}
		return strconv.Quote(s)
	}
	return "base64:" + base64.StdEncoding.EncodeToString(b)
}

func renderSettings(w io.Writer, asJSON bool, rows [][2]string, srcs []string) error {
	if asJSON {
		for i, r := range rows {
			if err := writeJSON(w, map[string]string{"key": r[0], "value": r[1], "source": srcs[i]}); err != nil {
				return err
			}
		}
		return nil
	}
	tw := newTable(w)
	fmt.Fprintln(tw, "KEY\tVALUE\tSOURCE")
	for i, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r[0], r[1], srcs[i])
	}
	return tw.Flush()
}

func renderProfiles(w io.Writer, asJSON bool, names []string, current string) error {
	for _, n := range names {
		var err error
		if asJSON {
			err = writeJSON(w, map[string]any{"profile": n, "current": n == current})
		} else {
			mark := " "
			if n == current {
				mark = "*"
			}
			_, err = fmt.Fprintf(w, "%s %s\n", mark, n)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func renderHello(w io.Writer, asJSON bool, addr string, h client.BrokerInfo) error {
	if asJSON {
		return writeJSON(w, map[string]any{
			"type": "hello", "broker": addr, "version": h.Version, "cluster_id": h.ClusterID,
			"node_id": h.NodeID, "max_frame_bytes": h.MaxFrameBytes, "max_message_bytes": h.MaxMessageBytes,
		})
	}
	tw := newTable(w)
	fmt.Fprintf(tw, "broker\t%s\nprotocol version\t%d\ncluster id\t%s\nnode id\t%d\nmax frame bytes\t%d\nmax message bytes\t%d\n",
		addr, h.Version, h.ClusterID, h.NodeID, h.MaxFrameBytes, h.MaxMessageBytes)
	return tw.Flush()
}

func renderPong(w io.Writer, asJSON bool, seq int, rtt time.Duration) error {
	ms := float64(rtt.Microseconds()) / 1000
	if asJSON {
		return writeJSON(w, map[string]any{"type": "pong", "seq": seq, "rtt_ms": ms})
	}
	_, err := fmt.Fprintf(w, "pong seq=%d rtt=%.3f ms\n", seq, ms)
	return err
}

// topicLister writes topic list pages: JSON lines as each page arrives, a
// table at the end (end), aligned over every page.
type topicLister struct {
	w      io.Writer
	asJSON bool
	rows   []admin.TopicSummary // table: held until end
}

func (l *topicLister) page(topics []admin.TopicSummary) error {
	if !l.asJSON {
		// ponytail: the table holds every name (at most 80 bytes each) in
		// memory; -o json streams.
		l.rows = append(l.rows, topics...)
		return nil
	}
	for _, t := range topics {
		if err := writeJSON(l.w, map[string]any{"name": t.Name, "partitions": t.Partitions}); err != nil {
			return err
		}
	}
	return nil
}

func (l *topicLister) end() error {
	if l.asJSON {
		return nil
	}
	tw := newTable(l.w)
	fmt.Fprintln(tw, "NAME\tPARTITIONS")
	for _, t := range l.rows {
		fmt.Fprintf(tw, "%s\t%d\n", t.Name, t.Partitions)
	}
	return tw.Flush()
}

func renderDescribe(w io.Writer, asJSON bool, d *admin.TopicDescription) error {
	if asJSON {
		parts := make([]map[string]any, 0, len(d.Partitions))
		for _, p := range d.Partitions {
			parts = append(parts, map[string]any{
				"partition": p.Partition, "start_seq": p.StartSeq, "next_seq": p.NextSeq,
				"message_count": p.MessageCount, "index_bytes": p.IndexBytes,
			})
		}
		return writeJSON(w, map[string]any{
			"name": d.Name, "replication_factor": d.ReplicationFactor,
			"retention_ms": d.Retention.Milliseconds(), "partitions": parts,
		})
	}
	retention := "none (keep everything)"
	if d.Retention > 0 {
		retention = d.Retention.String()
	}
	tw := newTable(w)
	fmt.Fprintf(tw, "topic\t%s\nreplication factor\t%d\nretention\t%s\n\n", d.Name, d.ReplicationFactor, retention)
	fmt.Fprintln(tw, "PARTITION\tSTART_SEQ\tNEXT_SEQ\tMESSAGES\tINDEX_BYTES")
	for _, p := range d.Partitions {
		fmt.Fprintf(tw, "%d\t%d\t%d\t%d\t%d\n", p.Partition, p.StartSeq, p.NextSeq, p.MessageCount, p.IndexBytes)
	}
	return tw.Flush()
}

func statsFields(s *admin.BrokerStats) []struct {
	name  string
	value uint64
} {
	return []struct {
		name  string
		value uint64
	}{
		{"node_id", uint64(s.NodeID)},
		{"open_connections", uint64(s.OpenConnections)},
		{"pending_bytes", s.PendingBytes},
		{"pending_bytes_peak", s.PendingBytesPeak},
		{"max_pending_bytes", s.MaxPendingBytes},
		{"fetch_waiters", uint64(s.FetchWaiters)},
		{"produce_requests", s.ProduceRequests},
		{"groups_committed", s.GroupsCommitted},
		{"fsyncs", s.Fsyncs},
		{"messages_accepted", s.MessagesAccepted},
		{"messages_deduplicated", s.MessagesDeduplicated},
		{"body_bytes_received", s.BodyBytesReceived},
		{"body_bytes_new", s.BodyBytesNew},
		{"wal_bytes", s.WalBytes},
		{"segment_bytes", s.SegmentBytes},
		{"index_bytes", s.IndexBytes},
	}
}

func renderStats(w io.Writer, asJSON bool, s *admin.BrokerStats) error {
	fields := statsFields(s)
	if asJSON {
		// Hand-built to keep the protocol's field order.
		var b strings.Builder
		b.WriteByte('{')
		for i, f := range fields {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%q:%d", f.name, f.value)
		}
		b.WriteString("}\n")
		_, err := io.WriteString(w, b.String())
		return err
	}
	tw := newTable(w)
	fmt.Fprintln(tw, "COUNTER\tVALUE")
	for _, f := range fields {
		fmt.Fprintf(tw, "%s\t%d\n", f.name, f.value)
	}
	return tw.Flush()
}

func renderCreated(w io.Writer, asJSON bool, topic string, created bool) error {
	if asJSON {
		return writeJSON(w, map[string]any{"topic": topic, "created": created})
	}
	verb := "created"
	if !created {
		verb = "exists"
	}
	_, err := fmt.Fprintf(w, "%s %s\n", verb, topic)
	return err
}

func renderAck(w io.Writer, asJSON bool, topic string, a producer.Ack) error {
	if asJSON {
		return writeJSON(w, map[string]any{
			"topic": topic, "partition": a.Partition, "base_seq": a.BaseSeq,
			"count": a.Count, "timestamp_ms": a.Timestamp.UnixMilli(),
		})
	}
	_, err := fmt.Fprintf(w, "acked topic=%s partition=%d base_seq=%d count=%d timestamp=%s\n",
		topic, a.Partition, a.BaseSeq, a.Count, formatTime(a.Timestamp))
	return err
}

type messageFormat struct {
	asJSON      bool
	forceBase64 bool
	headers     bool // table: print headers
}

type jsonHeader struct {
	Name  any `json:"name"`
	Value any `json:"value"`
}

type jsonMessage struct {
	Topic       string       `json:"topic"`
	Partition   uint32       `json:"partition"`
	Seq         uint64       `json:"seq"`
	TimestampMs int64        `json:"timestamp_ms"`
	Key         any          `json:"key"`
	Headers     []jsonHeader `json:"headers"`
	Body        any          `json:"body"`
}

// renderMessages writes one fetched batch.
func renderMessages(w io.Writer, f messageFormat, topic string, partition uint32, msgs []consumer.Message) error {
	if f.asJSON {
		for _, m := range msgs {
			hs := make([]jsonHeader, 0, len(m.Headers))
			for _, h := range m.Headers {
				hs = append(hs, jsonHeader{jsonBytes(h.Name, f.forceBase64), jsonBytes(h.Value, f.forceBase64)})
			}
			jm := jsonMessage{topic, partition, m.Seq, m.Timestamp.UnixMilli(),
				jsonBytes(m.Key, f.forceBase64), hs, jsonBytes(m.Body, f.forceBase64)}
			if jm.Body == nil {
				jm.Body = "" // a body is always present; an empty one is ""
			}
			if err := writeJSON(w, jm); err != nil {
				return err
			}
		}
		return nil
	}
	for _, m := range msgs {
		key := "-"
		if m.Key != nil {
			key = textBytes(m.Key, f.forceBase64)
			if key == "" {
				key = `""`
			}
		}
		line := fmt.Sprintf("%d\t%d\t%s\t%s\t%s", partition, m.Seq, formatTime(m.Timestamp), key, textBytes(m.Body, f.forceBase64))
		if f.headers {
			hs := make([]string, 0, len(m.Headers))
			for _, h := range m.Headers {
				hs = append(hs, textBytes(h.Name, f.forceBase64)+"="+textBytes(h.Value, f.forceBase64))
			}
			line += "\t" + strings.Join(hs, ",")
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}
