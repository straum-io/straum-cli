package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/straum-io/straum-go-sdk/client"
	"github.com/straum-io/straum-go-sdk/consumer"
)

type consumeOpts struct {
	name      string
	from      string
	partition int64
	follow    bool
	maxWait   time.Duration
	count     uint64
	untilSeq  uint64
	duration  time.Duration
	maxBytes  uint32
	print     []string
	bytes     string
}

func consumeCmd(g *globals) *cobra.Command {
	o := &consumeOpts{}
	cmd := &cobra.Command{
		Use:   "consume <topic>",
		Short: "Read messages from a topic",
		Long: `Read messages from a topic, from every partition or from --partition.

Start (--from): earliest, latest (the partition's next seq at start), a seq,
or saved (the position saved for --name). Default: the saved position when
--name has one, else earliest.

Stop: at the partition's end as seen at start; with --follow, never (Ctrl-C
ends it with exit 0), waiting up to --max-wait per Fetch for new messages.
--count, --until-seq and --duration stop earlier. Exit 2 when --duration
ends before --count messages were read.

--name N saves the next seq per partition after each batch is written to
stdout, so a later run continues there (see straum-cli positions). Delivery
to stdout is at least once: a crash between writing and saving prints those
messages again. A run that reads nothing saves nothing: after
--from latest with no new message, the next run starts at the default.

Table output is tab-separated: partition, seq, timestamp, key ("-" for no
key), body, and with --print headers the headers as name=value,... Bytes are
text when printable UTF-8, quoted when UTF-8 with control characters, else
base64:<data>. JSON output: one object per message (docs/output.md).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runConsume(cmd, g, o, args[0]) },
	}
	f := cmd.Flags()
	f.StringVar(&o.name, "name", "", "consumer name: start from and save its position")
	f.StringVar(&o.from, "from", "", "earliest, latest, saved or a seq")
	f.Int64Var(&o.partition, "partition", -1, "read only this partition")
	f.BoolVarP(&o.follow, "follow", "f", false, "keep reading new messages until interrupted")
	f.DurationVar(&o.maxWait, "max-wait", 30*time.Second, "with --follow: how long one Fetch waits for new messages (capped by the broker)")
	f.Uint64VarP(&o.count, "count", "n", 0, "stop after N messages (0: no limit)")
	f.Uint64Var(&o.untilSeq, "until-seq", 0, "read only messages with a seq below S (0: no limit)")
	f.DurationVar(&o.duration, "duration", 0, "stop after this long (0: no limit)")
	f.Uint32Var(&o.maxBytes, "max-bytes", 1<<20, "encoded bytes per Fetch (capped by the broker; the first message always comes)")
	f.StringSliceVar(&o.print, "print", nil, "extra table columns: headers")
	f.StringVar(&o.bytes, "bytes", "text", "how bytes are written: text (UTF-8 where valid) or base64")
	return cmd
}

var errDuration = errors.New("--duration elapsed")

// fetched is one batch of one partition, or the end or failure of its
// reader.
type fetched struct {
	partition uint32
	from      uint64 // the seq requested
	batch     *consumer.Batch
	err       error
	done      bool
}

// startSeq resolves --from for a partition.
func (o *consumeOpts) startSeq(p client.PartitionMetadata, saved map[uint32]uint64) (uint64, error) {
	s, ok := saved[p.Partition]
	from := o.from
	if from == "" {
		from = "earliest"
		if ok {
			from = "saved"
		}
	}
	var seq uint64
	switch from {
	case "earliest":
		seq = p.StartSeq
	case "latest":
		seq = p.NextSeq
	case "saved":
		if o.name == "" {
			return 0, usageErr("--from saved needs --name")
		}
		if !ok {
			return 0, usageErr("no saved position for %q on partition %d; use --from earliest|latest|<seq>", o.name, p.Partition)
		}
		seq = s
	default:
		n, err := strconv.ParseUint(from, 10, 64)
		if err != nil {
			return 0, usageErr("--from %q: want earliest, latest, saved or a seq", from)
		}
		seq = n
	}
	if from == "saved" && seq > p.NextSeq {
		return 0, usageErr("saved position %d of %q on partition %d is past the partition's next seq %d: the broker was re-created with the same cluster id? Run: straum-cli positions reset <topic> --name %s",
			seq, o.name, p.Partition, p.NextSeq, o.name)
	}
	return seq, nil
}

func runConsume(cmd *cobra.Command, g *globals, o *consumeOpts, topic string) error {
	switch {
	case o.name != "" && !validName(o.name):
		return usageErr("--name: 1 to 64 of A-Z a-z 0-9 . _ -")
	case o.bytes != "text" && o.bytes != "base64":
		return usageErr("--bytes: want text or base64")
	case o.maxWait < 0 || o.duration < 0:
		return usageErr("--max-wait and --duration must not be negative")
	case o.maxBytes == 0:
		return usageErr("--max-bytes must be positive")
	}
	f := messageFormat{forceBase64: o.bytes == "base64"}
	for _, p := range o.print {
		if p != "headers" {
			return usageErr("--print %q: want headers", p)
		}
		f.headers = true
	}

	parent := cmd.Context()
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	var store positionStore
	if o.name != "" {
		var err error
		if store, err = g.positions(); err != nil {
			return err
		}
		unlock, err := store.lockName(o.name)
		if err != nil {
			return err
		}
		defer unlock()
	}
	c, err := g.connect(ctx, 0)
	if err != nil {
		return err
	}
	defer c.Close()
	f.asJSON = g.json()
	info, md, err := topicMetadata(ctx, g, c, topic)
	if err != nil {
		return err
	}
	saved := map[uint32]uint64{}
	if o.name != "" {
		if saved, err = store.saved(info.ClusterID, topic, o.name); err != nil {
			return err
		}
	}

	// Each partition's reader fetches from its start to its end and hands
	// batches to this goroutine, which writes them and saves positions.
	var parts []client.PartitionMetadata
	for _, p := range md.Partitions {
		if o.partition < 0 || int64(p.Partition) == o.partition {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return &exitError{exitRefused, fmt.Errorf("topic %s has no partition %d", topic, o.partition)}
	}
	// Without --follow every Fetch reads messages that exist, so none waits.
	wait := time.Duration(-1)
	if o.follow {
		wait = o.maxWait
	}
	cons := consumer.New(c, consumer.Options{MaxBytes: o.maxBytes, MaxWait: wait})
	wait = max(wait, 0)
	batches := make(chan fetched)
	for _, p := range parts {
		start, err := o.startSeq(p, saved)
		if err != nil {
			return err
		}
		end := uint64(math.MaxUint64)
		if !o.follow {
			end = p.NextSeq
		}
		if o.untilSeq > 0 {
			end = min(end, o.untilSeq)
		}
		go func(part uint32, pos, end uint64) {
			for pos < end {
				// A Fetch in --follow may wait --max-wait before its answer.
				rctx, rcancel := context.WithTimeout(ctx, g.timeout+wait)
				b, err := cons.Fetch(rctx, topic, part, pos)
				rcancel()
				select {
				case batches <- fetched{partition: part, from: pos, batch: b, err: err}:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
				if n := len(b.Messages); n > 0 {
					pos = b.Messages[n-1].Seq + 1
				}
			}
			select {
			case batches <- fetched{partition: part, done: true}:
			case <-ctx.Done():
			}
		}(p.Partition, start, end)
	}
	if o.duration > 0 {
		t := time.AfterFunc(o.duration, func() { cancel(errDuration) })
		defer t.Stop()
	}

	out := bufio.NewWriterSize(cmd.OutOrStdout(), 64<<10)
	var written uint64
	running := len(parts)
	for running > 0 {
		var fb fetched
		select {
		case fb = <-batches:
		case <-ctx.Done():
			return consumeEnd(parent, ctx, o, written)
		}
		switch {
		case fb.done:
			running--
			continue
		case fb.err != nil:
			if ctx.Err() != nil {
				return consumeEnd(parent, ctx, o, written)
			}
			return requestErr(fb.err, false)
		}
		b := fb.batch
		if b.StartSeq > fb.from {
			fmt.Fprintf(cmd.ErrOrStderr(), "straum-cli: partition %d: %d messages from seq %d were released before they were read\n",
				fb.partition, b.StartSeq-fb.from, fb.from)
		}
		msgs := b.Messages
		if o.untilSeq > 0 {
			for i, m := range msgs {
				if m.Seq >= o.untilSeq {
					msgs = msgs[:i]
					break
				}
			}
		}
		if o.count > 0 && uint64(len(msgs)) > o.count-written {
			msgs = msgs[:o.count-written]
		}
		if len(msgs) == 0 {
			continue
		}
		if err := renderMessages(out, f, topic, fb.partition, msgs); err != nil {
			return localErr(err)
		}
		if err := out.Flush(); err != nil {
			return localErr(err)
		}
		written += uint64(len(msgs))
		if o.name != "" {
			next := map[uint32]uint64{fb.partition: msgs[len(msgs)-1].Seq + 1}
			if err := store.save(info.ClusterID, topic, o.name, next); err != nil {
				return err
			}
		}
		if o.count > 0 && written >= o.count {
			return nil
		}
	}
	return nil
}

// consumeEnd is the result of a consume stopped by its context: the
// interrupt (exit 0 with --follow, else 130) or --duration (exit 2 when
// --count was not reached).
func consumeEnd(parent, ctx context.Context, o *consumeOpts, written uint64) error {
	if parent.Err() != nil {
		if o.follow {
			return nil
		}
		return parent.Err()
	}
	if errors.Is(context.Cause(ctx), errDuration) && o.count > 0 && written < o.count {
		return limitErr("--duration ended after %d of %d messages", written, o.count)
	}
	return nil
}
