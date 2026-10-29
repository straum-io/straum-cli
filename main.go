// Command straum-cli is a terminal client for a Straum broker, protocol
// version 0, built on straum-go-sdk.
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
	"syscall"

	"github.com/spf13/cobra"
)

// Set at release: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const sdkModule = "github.com/straum-io/straum-go-sdk"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// env is the process environment the CLI reads; tests pass their own.
type env struct {
	getenv func(string) string
	stdin  io.Reader
}

// newRoot builds the command tree; docs/commands is generated from it.
func newRoot(e *env) *cobra.Command {
	g := &globals{env: e}
	root := &cobra.Command{
		Use:   "straum-cli",
		Short: "A terminal client for a Straum broker (protocol version 0)",
		Long: `A terminal client for a Straum broker (protocol version 0).

Settings come from flags, then STRAUM_CLI_* environment variables, then the
selected profile of the configuration file, then built-in defaults; see
"straum-cli config". Results go to stdout as a table or as JSON lines (-o json);
diagnostics go to stderr. The exit codes are listed in README.md.`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		DisableAutoGenTag: true,
	}
	g.register(root)
	root.AddCommand(
		versionCmd(),
		pingCmd(g),
		topicCmd(g),
		produceCmd(g),
		consumeCmd(g),
		brokerCmd(g),
		statsCmd(g),
		configCmd(g),
		positionsCmd(g),
	)
	return root
}

func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRoot(&env{getenv: getenv, stdin: stdin})
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return exitOK
	}
	var x *exitError
	switch {
	case errors.As(err, &x):
		if x.err != nil {
			fmt.Fprintln(stderr, "straum-cli:", x.err)
		}
		return x.code
	case ctx.Err() != nil:
		fmt.Fprintln(stderr, "straum-cli: interrupted")
		return exitInterrupted
	default:
		// cobra's own errors: unknown command or flag, bad arguments.
		fmt.Fprintln(stderr, "straum-cli:", err)
		return exitUsage
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version, commit, Go version and SDK version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			commit, sdk := "unknown", "unknown"
			if info, ok := debug.ReadBuildInfo(); ok {
				for _, s := range info.Settings {
					if s.Key == "vcs.revision" {
						commit = s.Value
					}
				}
				for _, d := range info.Deps {
					if d.Path == sdkModule {
						sdk = d.Version
					}
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "straum-cli %s\ncommit %s\n%s\nstraum-go-sdk %s\n", version, commit, runtime.Version(), sdk)
		},
	}
}
