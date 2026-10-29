package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/straum-io/straum-go-sdk/client"
	"github.com/straum-io/straum-go-sdk/producer"
)

type produceOpts struct {
	stdin, raw, lines bool
	files             []string
	input             string
	key, keyFile      string
	headers           []string
	headerFiles       []string
	partition         int64
	batchMessages     int
	batchBytes        int
	linger            time.Duration
	acks              string
}

func produceCmd(g *globals) *cobra.Command {
	o := &produceOpts{}
	cmd := &cobra.Command{
		Use:   "produce <topic> [<body>...]",
		Short: "Publish messages from arguments, stdin or files",
		Long: `Publish messages to a topic. One source per invocation:

  arguments    each argument is one message body
  --stdin      one message per line (--raw: the whole input is one message)
  --file F     each file is one message (--lines: one per line); repeatable

--key and --header apply to every message. --input json reads JSON lines
{"key": ..., "headers": [{"name": ..., "value": ...}], "body": ...} where each
byte field is a string or {"base64": "..."}, the format consume -o json
writes; key may be null or absent (no key).

Messages go to --partition, or keyed ones by the protocol's default
partitioner and unkeyed ones round robin. They are sent in batches per
partition, one Produce in flight at a time, so input order is seq order.
A batch is sent when it reaches --batch-messages or --batch-bytes, or when
the input stalls for --linger (0: at once). Each acknowledgement is printed
when it arrives; acknowledgements are durable (on the broker's disk).

The broker also refuses a Produce larger than one store commit
(max_group_messages, default 4096, and max_group_bytes, default 4 MiB, which
HelloOk does not report) with MessageTooLarge, exit 4; the defaults stay
below both.

On an input error (a bad JSON line, a line or file over the broker's
message limit, an unreadable file) the messages read before it are still
sent, the count of acknowledged messages goes to stderr and, with -o json,
a final {"type": "summary", "acknowledged": N, "error": ...} line to stdout;
the exit code is 1.

Without --retries, a lost connection or an interrupt during a Produce is exit 5: the request in flight may or
may not be stored. With --retries, a resent request may be stored twice.`,
		Args: cobra.MinimumNArgs(1),
	}
	attempts := retriesFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runProduce(cmd, g, o, attempts(), args[0], args[1:])
	}
	f := cmd.Flags()
	f.BoolVar(&o.stdin, "stdin", false, "read messages from stdin, one per line")
	f.BoolVar(&o.raw, "raw", false, "with --stdin: the whole input is one message")
	f.StringArrayVar(&o.files, "file", nil, "read a message from file `F` (repeatable)")
	f.BoolVar(&o.lines, "lines", false, "with --file: one message per line")
	f.StringVar(&o.input, "input", "text", "input format of --stdin and --file --lines: text or json")
	f.StringVar(&o.key, "key", "", "key of every message (an empty --key \"\" is a present, empty key)")
	f.StringVar(&o.keyFile, "key-file", "", "read the key of every message from a file")
	f.StringArrayVar(&o.headers, "header", nil, "header `name=value` of every message (repeatable)")
	f.StringArrayVar(&o.headerFiles, "header-file", nil, "header `name=path`, the value read from a file (repeatable)")
	f.Int64Var(&o.partition, "partition", -1, "send every message to this partition")
	f.IntVar(&o.batchMessages, "batch-messages", 1000, "most messages per Produce")
	f.IntVar(&o.batchBytes, "batch-bytes", 1<<20, "most encoded message bytes per Produce (capped by the broker's frame limit)")
	f.DurationVar(&o.linger, "linger", 0, "how long a batch waits for more input when the input stalls")
	f.StringVar(&o.acks, "acks", "durable", "acknowledgement level; protocol version 0 has only durable")
	return cmd
}

// item is one message read from the input, or the error that ended it.
type item struct {
	m   client.Message
	err error
}

func (o *produceOpts) validate(bodies []string) error {
	sources := 0
	for _, on := range []bool{len(bodies) > 0, o.stdin, len(o.files) > 0} {
		if on {
			sources++
		}
	}
	switch {
	case sources == 0:
		return usageErr("no messages: pass bodies as arguments, --stdin or --file")
	case sources > 1:
		return usageErr("use one source: arguments, --stdin or --file")
	case o.raw && !o.stdin:
		return usageErr("--raw goes with --stdin")
	case o.lines && len(o.files) == 0:
		return usageErr("--lines goes with --file")
	case o.input != "text" && o.input != "json":
		return usageErr("--input: want text or json")
	case o.input == "json" && (len(bodies) > 0 || o.raw || (len(o.files) > 0 && !o.lines)):
		return usageErr("--input json reads lines: use it with --stdin or --file --lines")
	case o.input == "json" && (o.key != "" || o.keyFile != "" || len(o.headers) > 0 || len(o.headerFiles) > 0):
		return usageErr("--input json carries keys and headers per message; drop --key and --header")
	case o.key != "" && o.keyFile != "":
		return usageErr("use --key or --key-file, not both")
	case o.batchMessages < 1 || o.batchBytes < 1:
		return usageErr("--batch-messages and --batch-bytes must be positive")
	case o.linger < 0:
		return usageErr("--linger must not be negative")
	case o.acks != "durable":
		return usageErr("--acks %q: protocol version 0 supports only durable", o.acks)
	case o.partition < -1 || o.partition > 1<<32-1:
		return usageErr("--partition out of range")
	}
	return nil
}

// template is the key and headers every message gets from the flags.
func (o *produceOpts) template(keySet bool) (client.Message, error) {
	var m client.Message
	switch {
	case o.keyFile != "":
		k, err := os.ReadFile(o.keyFile)
		if err != nil {
			return m, localErr(err)
		}
		m.Key = k
	case keySet:
		m.Key = []byte(o.key)
	}
	for _, h := range o.headers {
		name, value, ok := strings.Cut(h, "=")
		if !ok || name == "" {
			return m, usageErr("--header %q: want name=value", h)
		}
		m.Headers = append(m.Headers, client.Header{Name: []byte(name), Value: []byte(value)})
	}
	for _, h := range o.headerFiles {
		name, path, ok := strings.Cut(h, "=")
		if !ok || name == "" {
			return m, usageErr("--header-file %q: want name=path", h)
		}
		v, err := os.ReadFile(path)
		if err != nil {
			return m, localErr(err)
		}
		m.Headers = append(m.Headers, client.Header{Name: []byte(name), Value: v})
	}
	return m, nil
}

// read sends the input's messages to out and closes it; it stops when ctx
// ends. maxBody bounds a message read whole (the broker's body limit).
func (o *produceOpts) read(ctx context.Context, stdin io.Reader, bodies []string, tmpl client.Message, maxBody int, out chan<- item) {
	defer close(out)
	send := func(it item) bool {
		select {
		case out <- it:
			return true
		case <-ctx.Done():
			return false
		}
	}
	msg := func(body []byte) client.Message {
		m := tmpl
		m.Body = body
		return m
	}
	readWhole := func(name string, r io.Reader) bool {
		b, err := io.ReadAll(io.LimitReader(r, int64(maxBody)+1))
		if err == nil && len(b) > maxBody {
			err = fmt.Errorf("%s: more than the broker's max_message_bytes (%d)", name, maxBody)
		}
		if err != nil {
			return send(item{err: localErr(err)})
		}
		return send(item{m: msg(b)})
	}
	readLines := func(name string, r io.Reader) bool {
		sc := bufio.NewScanner(r)
		// A JSON line holds base64 bodies (4/3) and headers; a text line
		// is one body.
		limit := maxBody + 1
		if o.input == "json" {
			limit = 2*maxBody + 64<<10
		}
		sc.Buffer(make([]byte, 0, min(64<<10, limit)), limit)
		for n := 1; sc.Scan(); n++ {
			line := sc.Bytes()
			line = line[:len(line)-len(trimCR(line))]
			if o.input == "json" {
				if len(strings.TrimSpace(string(line))) == 0 {
					continue
				}
				m, err := parseJSONMessage(line)
				if err != nil {
					return send(item{err: usageErr("%s:%d: %v", name, n, err)})
				}
				if !send(item{m: m}) {
					return false
				}
				continue
			}
			if !send(item{m: msg(append([]byte(nil), line...))}) {
				return false
			}
		}
		if err := sc.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				err = fmt.Errorf("a line longer than %d bytes", limit-1)
			}
			return send(item{err: localErr(fmt.Errorf("%s: %w", name, err))})
		}
		return true
	}
	switch {
	case len(bodies) > 0:
		for _, b := range bodies {
			if !send(item{m: msg([]byte(b))}) {
				return
			}
		}
	case o.stdin && o.raw:
		readWhole("stdin", stdin)
	case o.stdin:
		readLines("stdin", stdin)
	default:
		for _, path := range o.files {
			f, err := os.Open(path)
			if err != nil {
				send(item{err: localErr(err)})
				return
			}
			ok := false
			if o.lines {
				ok = readLines(path, f)
			} else {
				ok = readWhole(path, f)
			}
			f.Close()
			if !ok {
				return
			}
		}
	}
}

// trimCR returns the trailing "\r" of a line, if any.
func trimCR(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == '\r' {
		return line[len(line)-1:]
	}
	return nil
}

type jsonInput struct {
	Key     json.RawMessage `json:"key"`
	Headers []struct {
		Name  json.RawMessage `json:"name"`
		Value json.RawMessage `json:"value"`
	} `json:"headers"`
	Body json.RawMessage `json:"body"`
}

// decodeBytes reads a byte field: a string, {"base64": "..."}, or null
// (nil: absent).
func decodeBytes(raw json.RawMessage, field string) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []byte(s), nil
	}
	var o struct {
		Base64 *string `json:"base64"`
	}
	if err := json.Unmarshal(raw, &o); err != nil || o.Base64 == nil {
		return nil, fmt.Errorf("%s: want a string or {\"base64\": \"...\"}", field)
	}
	b, err := base64.StdEncoding.DecodeString(*o.Base64)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", field, err)
	}
	return b, nil
}

func parseJSONMessage(line []byte) (client.Message, error) {
	var in jsonInput
	var m client.Message
	// Other fields, such as those consume -o json adds, are ignored.
	if err := json.Unmarshal(line, &in); err != nil {
		return m, err
	}
	if len(in.Body) == 0 || string(in.Body) == "null" {
		return m, errors.New("body: missing")
	}
	var err error
	if m.Key, err = decodeBytes(in.Key, "key"); err != nil {
		return m, err
	}
	if m.Body, err = decodeBytes(in.Body, "body"); err != nil {
		return m, err
	}
	for i, h := range in.Headers {
		var hd client.Header
		if hd.Name, err = decodeBytes(h.Name, fmt.Sprintf("headers[%d].name", i)); err != nil {
			return m, err
		}
		if hd.Value, err = decodeBytes(h.Value, fmt.Sprintf("headers[%d].value", i)); err != nil {
			return m, err
		}
		if hd.Name == nil {
			hd.Name = []byte{}
		}
		if hd.Value == nil {
			hd.Value = []byte{}
		}
		m.Headers = append(m.Headers, hd)
	}
	return m, nil
}

// messageSize is a message's encoded size in a Produce (protocol 4.6).
func messageSize(m client.Message) int {
	n := 1 + 4 + len(m.Body)
	if m.Key != nil {
		n += 2 + len(m.Key)
	}
	if len(m.Headers) > 0 {
		n += 2
		for _, h := range m.Headers {
			n += 2 + len(h.Name) + 4 + len(h.Value)
		}
	}
	return n
}

// produceOverhead is a Produce frame's size without its messages: the
// frame header (14), topic (2 + name), partition, producer id, producer
// seq, ack level, expected epoch, timeout and the message count.
func produceOverhead(topic string) int { return 14 + 2 + len(topic) + 4 + 8 + 8 + 1 + 4 + 4 + 4 }

// batcher groups messages per partition and sends a batch when it is full
// or when flushAll is called.
type batcher struct {
	maxMessages, maxBytes int
	send                  func(partition uint32, msgs []client.Message) error
	pending               map[uint32][]client.Message
	sizes                 map[uint32]int
	count                 int // messages pending in all partitions
}

func newBatcher(maxMessages, maxBytes int, send func(uint32, []client.Message) error) *batcher {
	return &batcher{maxMessages: maxMessages, maxBytes: maxBytes, send: send,
		pending: map[uint32][]client.Message{}, sizes: map[uint32]int{}}
}

func (b *batcher) add(p uint32, m client.Message) error {
	size := messageSize(m)
	if len(b.pending[p]) > 0 && b.sizes[p]+size > b.maxBytes {
		if err := b.flush(p); err != nil {
			return err
		}
	}
	b.pending[p] = append(b.pending[p], m)
	b.sizes[p] += size
	b.count++
	if len(b.pending[p]) >= b.maxMessages || b.sizes[p] >= b.maxBytes {
		return b.flush(p)
	}
	return nil
}

func (b *batcher) flush(p uint32) error {
	msgs := b.pending[p]
	if len(msgs) == 0 {
		return nil
	}
	if err := b.send(p, msgs); err != nil {
		return err
	}
	b.count -= len(msgs)
	delete(b.pending, p)
	delete(b.sizes, p)
	return nil
}

func (b *batcher) flushAll() error {
	for p := range b.pending {
		if err := b.flush(p); err != nil {
			return err
		}
	}
	return nil
}

func runProduce(cmd *cobra.Command, g *globals, o *produceOpts, attempts int, topic string, bodies []string) error {
	if err := o.validate(bodies); err != nil {
		return err
	}
	tmpl, err := o.template(cmd.Flags().Changed("key"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	c, err := g.connect(ctx, attempts)
	if err != nil {
		return err
	}
	defer c.Close()
	var info client.BrokerInfo
	if err := g.request(ctx, func(ctx context.Context) (err error) { info, err = c.Hello(ctx); return }); err != nil {
		return requestErr(err, false)
	}
	var partitions uint32
	if o.partition < 0 {
		err := g.request(ctx, func(ctx context.Context) (err error) { partitions, err = c.PartitionCount(ctx, topic); return })
		if err != nil {
			return requestErr(err, false)
		}
		if partitions == 0 {
			return &exitError{exitRefused, fmt.Errorf("topic %s has no partitions", topic)}
		}
	}

	p := producer.New(c, producer.Options{})
	out := cmd.OutOrStdout()
	read, acked := 0, 0
	b := newBatcher(o.batchMessages, max(1, min(o.batchBytes, int(info.MaxFrameBytes)-produceOverhead(topic))),
		func(part uint32, msgs []client.Message) error {
			var ack producer.Ack
			err := g.request(ctx, func(ctx context.Context) (err error) {
				ack, err = p.ProduceTo(ctx, topic, part, msgs...)
				return
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "straum-cli: %d messages read were not acknowledged\n", read-acked)
				return requestErr(err, true)
			}
			acked += len(msgs)
			return localErr(renderAck(out, g.json(), topic, ack))
		})

	items := make(chan item, 1024)
	go o.read(ctx, g.env.stdin, bodies, tmpl, int(info.MaxMessageBytes), items)
	var rr uint32
	var lingerTimer <-chan time.Time
	for {
		var it item
		var ok bool
		if b.count == 0 {
			select {
			case it, ok = <-items:
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			select {
			case it, ok = <-items:
			default:
				// The input stalls: send now, or wait up to --linger.
				if o.linger == 0 {
					if err := b.flushAll(); err != nil {
						return err
					}
					continue
				}
				if lingerTimer == nil {
					lingerTimer = time.After(o.linger)
				}
				select {
				case it, ok = <-items:
				case <-lingerTimer:
					lingerTimer = nil
					if err := b.flushAll(); err != nil {
						return err
					}
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		if !ok {
			return b.flushAll()
		}
		if it.err != nil {
			// What was read before the bad input is still sent.
			if err := b.flushAll(); err != nil {
				return err
			}
			// Exit 1 then says what was stored: the acknowledged count.
			fmt.Fprintf(cmd.ErrOrStderr(), "straum-cli: %d messages were acknowledged before the input error\n", acked)
			if g.json() {
				if err := renderInputStop(out, acked, it.err); err != nil {
					return localErr(err)
				}
			}
			return it.err
		}
		read++
		var part uint32
		switch {
		case o.partition >= 0:
			part = uint32(o.partition)
		case it.m.Key != nil:
			part = producer.Partition(it.m.Key, partitions)
		default:
			part = rr % partitions
			rr++
		}
		if err := b.add(part, it.m); err != nil {
			return err
		}
		if b.count == 0 {
			lingerTimer = nil
		}
	}
}
