package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/straum-io/straum-go-sdk/admin"
	"github.com/straum-io/straum-go-sdk/client"
	"github.com/straum-io/straum-go-sdk/straumerr"
)

// connect resolves the settings and connects (Hello). attempts is the
// SDK's RetryPolicy.MaxAttempts, 0 for its default.
func (g *globals) connect(ctx context.Context, attempts int) (*client.Client, error) {
	if err := g.resolve(false); err != nil {
		return nil, err
	}
	cfg := client.Config{
		Addrs:            g.brokers,
		ClientName:       g.clientName,
		ConnectTimeout:   g.connectTimeout,
		MaxInFlightBytes: g.maxInFlight,
	}
	cfg.Retry.MaxAttempts = attempts
	c, err := client.New(ctx, cfg)
	if err != nil {
		return nil, connectErr(err)
	}
	return c, nil
}

// request runs f with the per-request timeout.
func (g *globals) request(ctx context.Context, f func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	return f(ctx)
}

// retriesFlag adds --retries to a command whose request is not idempotent
// in protocol version 0 and returns the SDK's MaxAttempts for it.
func retriesFlag(cmd *cobra.Command) func() int {
	n := cmd.Flags().Int("retries", 0, "resend the request up to N times after a lost connection or Overloaded; a resent request may take effect twice (protocol version 0 has no idempotent produce)")
	return func() int { return max(*n, 0) + 1 }
}

func pingCmd(g *globals) *cobra.Command {
	var count int
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "ping",
		Short: "Connect, print the broker's Hello answer, and measure Ping round trips",
		Long: `Connect, print the broker's Hello answer (protocol version, cluster id, node
id, frame and message limits), and measure Ping round trips.

Exit 2 when some pings of --count failed; exit 3 when all failed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if count < 1 {
				return usageErr("--count must be at least 1")
			}
			ctx := cmd.Context()
			c, err := g.connect(ctx, 0)
			if err != nil {
				return err
			}
			defer c.Close()
			out := cmd.OutOrStdout()
			var info client.BrokerInfo
			if err := g.request(ctx, func(ctx context.Context) (err error) { info, err = c.Hello(ctx); return }); err != nil {
				return requestErr(err, false)
			}
			if err := renderHello(out, g.json(), g.brokers[0], info); err != nil {
				return localErr(err)
			}
			a := admin.New(c)
			var lost int
			var last error
			for i := 1; i <= count; i++ {
				if i > 1 {
					select {
					case <-time.After(interval):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				var rtt time.Duration
				err := g.request(ctx, func(ctx context.Context) (err error) { rtt, err = a.Ping(ctx); return })
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err != nil {
					lost++
					last = err
					fmt.Fprintf(cmd.ErrOrStderr(), "straum-cli: ping %d: %v\n", i, err)
					continue
				}
				if err := renderPong(out, g.json(), i, rtt); err != nil {
					return localErr(err)
				}
			}
			switch {
			case lost == count:
				return requestErr(last, false)
			case lost > 0:
				return limitErr("%d of %d pings failed", lost, count)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&count, "count", "c", 1, "number of pings")
	cmd.Flags().DurationVar(&interval, "interval", time.Second, "wait between pings")
	return cmd
}

func topicCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topic",
		Short: "Create, list and describe topics",
	}
	cmd.AddCommand(topicCreateCmd(g), topicListCmd(g), topicDescribeCmd(g))
	return cmd
}

func topicCreateCmd(g *globals) *cobra.Command {
	var partitions uint32
	var replication uint16
	var retention time.Duration
	var ifNotExists bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a topic",
		Long: `Create a topic. Names are 1 to 80 bytes of A-Z a-z 0-9 . _ -; names starting
with __ are reserved. Version 0 brokers accept one partition and replication
factor 1, and record the retention without releasing messages by age.

Without --retries, a lost connection is exit 5 (the topic may or may not
exist); with --if-not-exists, a resend that finds the topic is success.`,
		Args: cobra.ExactArgs(1),
		RunE: nil, // set below, after --retries
	}
	attempts := retriesFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c, err := g.connect(ctx, attempts())
		if err != nil {
			return err
		}
		defer c.Close()
		created := true
		err = g.request(ctx, func(ctx context.Context) error {
			return admin.New(c).CreateTopic(ctx, args[0], admin.TopicConfig{
				Partitions: partitions, ReplicationFactor: replication, Retention: retention,
			})
		})
		if ifNotExists && errors.Is(err, straumerr.TopicExists) {
			created, err = false, nil
		}
		if err != nil {
			return requestErr(err, true)
		}
		return localErr(renderCreated(cmd.OutOrStdout(), g.json(), args[0], created))
	}
	f := cmd.Flags()
	f.Uint32Var(&partitions, "partitions", 1, "number of partitions")
	f.Uint16Var(&replication, "replication-factor", 1, "replicas per partition")
	f.DurationVar(&retention, "retention", 0, "how long messages are kept (0: everything is kept)")
	f.BoolVar(&ifNotExists, "if-not-exists", false, "succeed when the topic already exists")
	return cmd
}

func topicListCmd(g *globals) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List topics by name, following the broker's page cursor to the end",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 0 {
				return usageErr("--limit must not be negative")
			}
			ctx := cmd.Context()
			c, err := g.connect(ctx, 0)
			if err != nil {
				return err
			}
			defer c.Close()
			a := admin.New(c)
			l := &topicLister{w: cmd.OutOrStdout(), asJSON: g.json()}
			cursor, seen := "", 0
			for {
				page := uint32(client.MaxRequestTopics)
				if limit > 0 {
					page = uint32(min(limit-seen, client.MaxRequestTopics))
				}
				var topics []admin.TopicSummary
				var next string
				err := g.request(ctx, func(ctx context.Context) (err error) {
					topics, next, err = a.ListTopics(ctx, cursor, page)
					return
				})
				if err != nil {
					return requestErr(err, false)
				}
				if err := l.page(topics); err != nil {
					return localErr(err)
				}
				seen += len(topics)
				if next == "" || (limit > 0 && seen >= limit) {
					return localErr(l.end())
				}
				cursor = next
			}
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "list at most N topics (0: all)")
	return cmd
}

func topicDescribeCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <name>",
		Short: "Print a topic's configuration and each partition's seq range",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := g.connect(ctx, 0)
			if err != nil {
				return err
			}
			defer c.Close()
			var d *admin.TopicDescription
			err = g.request(ctx, func(ctx context.Context) (err error) {
				d, err = admin.New(c).DescribeTopic(ctx, args[0])
				return
			})
			if err != nil {
				return requestErr(err, false)
			}
			return localErr(renderDescribe(cmd.OutOrStdout(), g.json(), d))
		},
	}
}

func brokerStatsRun(g *globals) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		c, err := g.connect(ctx, 0)
		if err != nil {
			return err
		}
		defer c.Close()
		var s *admin.BrokerStats
		err = g.request(ctx, func(ctx context.Context) (err error) {
			s, err = admin.New(c).BrokerStats(ctx)
			return
		})
		if err != nil {
			return requestErr(err, false)
		}
		return localErr(renderStats(cmd.OutOrStdout(), g.json(), s))
	}
}

const statsShort = "Print the broker's counters since it started (BrokerStats)"

func brokerCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "broker", Short: "Broker information"}
	cmd.AddCommand(&cobra.Command{Use: "stats", Short: statsShort, Args: cobra.NoArgs, RunE: brokerStatsRun(g)})
	return cmd
}

// statsCmd is `broker stats` under a shorter name.
func statsCmd(g *globals) *cobra.Command {
	return &cobra.Command{Use: "stats", Short: statsShort + "; the same as broker stats", Args: cobra.NoArgs, RunE: brokerStatsRun(g)}
}
