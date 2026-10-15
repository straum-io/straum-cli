// Command straum-cli is a terminal client for a Straum cluster.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Set at release: -ldflags "-X main.version=v0.1.0".
var version = "dev"

// Exit codes; documented in README.md.
const (
	exitOK      = 0
	exitUsage   = 1 // bad flags, bad configuration, or an internal error
	exitPartial = 2 // some nodes are unknown
	exitNone    = 3 // every node is unknown
	exitRefused = 4 // MQTT: refused, or failed before anything took effect
	exitUnknown = 5 // MQTT: sent, but whether it took effect is unknown
)

// exitCode carries a non-zero exit that is not an error message:
// the output already says what happened.
type exitCode int

func (c exitCode) Error() string { return fmt.Sprintf("exit %d", int(c)) }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := &cobra.Command{
		Use:           "straum-cli",
		Short:         "A terminal client for a Straum cluster",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(versionCmd(), statusCmd(getenv), pubCmd(getenv), subCmd(getenv))

	err := root.ExecuteContext(ctx)
	var c exitCode
	var f failure
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &c):
		return int(c)
	case errors.As(err, &f):
		fmt.Fprintln(stderr, "straum-cli:", f.err)
		return f.code
	default:
		fmt.Fprintln(stderr, "straum-cli:", err)
		return exitUsage
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI's version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			commit := "unknown"
			if info, ok := debug.ReadBuildInfo(); ok {
				for _, s := range info.Settings {
					if s.Key == "vcs.revision" {
						commit = s.Value
					}
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "straum-cli %s (commit %s, %s)\n", version, commit, runtime.Version())
		},
	}
}

func statusCmd(getenv func(string) string) *cobra.Command {
	var (
		nodes   []string
		timeout time.Duration
		output  string
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show what each configured node reports about itself",
		Long: `Reads the metrics endpoint (STRAUM_CLUSTER_METRICS_LISTEN) of every
configured node once and shows the stable metrics each one reports, with
the address it came from and how old the observation is. A node that does
not answer is shown as unknown. No health verdict is drawn.

Nodes come from --node, or else from STRAUM_CLI_NODES (comma-separated
host:port). There is no default cluster.

Exit codes: 0 every node observed, 1 usage or configuration error,
2 some nodes unknown, 3 every node unknown.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("node") {
				if env := getenv("STRAUM_CLI_NODES"); env != "" {
					nodes = strings.Split(env, ",")
				}
			}
			if len(nodes) == 0 {
				return errors.New("no nodes: pass --node host:port or set STRAUM_CLI_NODES")
			}
			seen := make(map[string]bool, len(nodes))
			for i, n := range nodes {
				n = strings.TrimSpace(n)
				if err := checkAddress(n); err != nil {
					return err
				}
				if seen[n] {
					return fmt.Errorf("node %s given twice", n)
				}
				seen[n] = true
				nodes[i] = n
			}
			if timeout <= 0 {
				return errors.New("--timeout must be positive")
			}
			write := writeTable
			switch output {
			case "table":
			case "json":
				write = writeJSON
			default:
				return fmt.Errorf("--output %q: want table or json", output)
			}

			r := collect(cmd.Context(), newHTTPClient(), nodes, timeout, time.Now)
			if err := write(cmd.OutOrStdout(), r); err != nil {
				return err
			}
			unknown := 0
			for _, n := range r.Nodes {
				if n.State == stateUnknown {
					unknown++
					fmt.Fprintf(cmd.ErrOrStderr(), "straum-cli: %s: %s\n", n.Address, n.Error)
				}
			}
			switch unknown {
			case 0:
				return nil
			case len(r.Nodes):
				return exitCode(exitNone)
			default:
				return exitCode(exitPartial)
			}
		},
	}
	cmd.Flags().StringSliceVar(&nodes, "node", nil, "a node's metrics address, host:port (repeatable or comma-separated)")
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Second, "how long to wait for each node")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "table or json")
	return cmd
}
