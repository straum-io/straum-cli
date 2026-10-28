package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/straum-io/straum-go-sdk/client"
)

// Saved consumer positions (D-237 A): the broker keeps none, so a named
// consumer's next seq per (cluster id, topic, partition) lives in
// <state dir>/straum-cli/<profile>/positions.json. Unnamed consumers save
// nothing.

type position struct {
	ClusterID string    `json:"cluster_id"`
	Topic     string    `json:"topic"`
	Partition uint32    `json:"partition"`
	Name      string    `json:"name"`
	NextSeq   uint64    `json:"next_seq"`
	SavedAt   time.Time `json:"saved_at"`
}

type positionsFile struct {
	Version   int        `json:"version"`
	Positions []position `json:"positions"`
}

const positionsVersion = 1

var errLocked = errors.New("locked")

type positionStore struct{ dir string }

func (s positionStore) path() string { return filepath.Join(s.dir, "positions.json") }

// stateDir is $XDG_STATE_HOME, else ~/.local/state on Linux and other
// Unix systems, else the user configuration directory (macOS, Windows).
func stateDir(getenv func(string) string) (string, error) {
	if d := getenv("XDG_STATE_HOME"); d != "" {
		return d, nil
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state"), nil
}

func (g *globals) positions() (positionStore, error) {
	if err := g.resolve(false); err != nil {
		return positionStore{}, err
	}
	dir, err := stateDir(g.env.getenv)
	if err != nil {
		return positionStore{}, localErr(fmt.Errorf("no state directory: %v", err))
	}
	return positionStore{filepath.Join(dir, "straum-cli", g.profileName)}, nil
}

// lockName holds a consumer name for the duration of a consume, so that two
// processes never save positions for one name at once.
func (s positionStore) lockName(name string) (func(), error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, localErr(err)
	}
	unlock, err := lockFile(filepath.Join(s.dir, name+".lock"), false)
	if errors.Is(err, errLocked) {
		return nil, usageErr("consumer name %q is in use by another straum-cli process", name)
	}
	return unlock, localErr(err)
}

func (s positionStore) load() (*positionsFile, error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return &positionsFile{Version: positionsVersion}, nil
	}
	if err != nil {
		return nil, localErr(err)
	}
	var f positionsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, usageErr("%s: %v", s.path(), err)
	}
	if f.Version != positionsVersion {
		return nil, usageErr("%s: version %d, this straum-cli reads version %d", s.path(), f.Version, positionsVersion)
	}
	return &f, nil
}

// update changes the file under an exclusive lock and replaces it
// atomically.
func (s positionStore) update(change func(*positionsFile) error) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return localErr(err)
	}
	unlock, err := lockFile(filepath.Join(s.dir, "positions.lock"), true)
	if err != nil {
		return localErr(err)
	}
	defer unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	if err := change(f); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return localErr(err)
	}
	return localErr(writeFileAtomic(s.path(), append(data, '\n')))
}

func (f *positionsFile) find(k position) int {
	return slices.IndexFunc(f.Positions, func(p position) bool {
		return p.ClusterID == k.ClusterID && p.Topic == k.Topic && p.Partition == k.Partition && p.Name == k.Name
	})
}

func (f *positionsFile) put(p position) {
	if i := f.find(p); i >= 0 {
		f.Positions[i] = p
	} else {
		f.Positions = append(f.Positions, p)
	}
}

// saved returns the saved positions of a name on a cluster's topic, by
// partition.
func (s positionStore) saved(cluster, topic, name string) (map[uint32]uint64, error) {
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	out := map[uint32]uint64{}
	for _, p := range f.Positions {
		if p.ClusterID == cluster && p.Topic == topic && p.Name == name {
			out[p.Partition] = p.NextSeq
		}
	}
	return out, nil
}

func (s positionStore) save(cluster, topic, name string, next map[uint32]uint64) error {
	now := time.Now().UTC()
	return s.update(func(f *positionsFile) error {
		for part, seq := range next {
			f.put(position{cluster, topic, part, name, seq, now})
		}
		return nil
	})
}

func renderPositions(w io.Writer, asJSON bool, ps []position) error {
	if asJSON {
		for _, p := range ps {
			if err := writeJSON(w, p); err != nil {
				return err
			}
		}
		return nil
	}
	tw := newTable(w)
	fmt.Fprintln(tw, "NAME\tTOPIC\tPARTITION\tNEXT_SEQ\tCLUSTER\tSAVED_AT")
	for _, p := range ps {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\t%s\n", p.Name, p.Topic, p.Partition, p.NextSeq, p.ClusterID, formatTime(p.SavedAt))
	}
	return tw.Flush()
}

func positionsCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "positions",
		Short: "List, reset and set the saved positions of named consumers",
		Long: `List, reset and set the saved positions of named consumers (consume --name).

The broker keeps no consumer positions in protocol version 0; straum-cli keeps
them per profile in <state dir>/straum-cli/<profile>/positions.json, where the
state directory is $XDG_STATE_HOME, else ~/.local/state (Linux), else the user
configuration directory (macOS, Windows). A position is the next seq to read,
per broker cluster id, topic, partition and name.`,
	}
	var topic string
	list := &cobra.Command{
		Use:   "list",
		Short: "List saved positions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := g.positions()
			if err != nil {
				return err
			}
			f, err := s.load()
			if err != nil {
				return err
			}
			ps := slices.DeleteFunc(f.Positions, func(p position) bool { return topic != "" && p.Topic != topic })
			slices.SortFunc(ps, func(a, b position) int {
				return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Topic, b.Topic),
					cmp.Compare(a.ClusterID, b.ClusterID), cmp.Compare(a.Partition, b.Partition))
			})
			return localErr(renderPositions(cmd.OutOrStdout(), g.json(), ps))
		},
	}
	list.Flags().StringVar(&topic, "topic", "", "only this topic")

	var name, to string
	var partition int64
	reset := &cobra.Command{
		Use:   "reset <topic>",
		Short: "Delete a name's saved positions for a topic, or move them with --to",
		Long: `Delete a name's saved positions for a topic (every cluster id; the next
consume --name starts at --from's default), or with --to earliest|latest|<seq>
set them for the connected broker's cluster.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !validName(name) {
				return usageErr("--name: 1 to 64 of A-Z a-z 0-9 . _ -")
			}
			s, err := g.positions()
			if err != nil {
				return err
			}
			unlock, err := s.lockName(name)
			if err != nil {
				return err
			}
			defer unlock()
			if to == "" {
				return s.update(func(f *positionsFile) error {
					f.Positions = slices.DeleteFunc(f.Positions, func(p position) bool {
						return p.Topic == args[0] && p.Name == name && (partition < 0 || int64(p.Partition) == partition)
					})
					return nil
				})
			}
			return setPositions(cmd.Context(), g, s, args[0], name, partition, to)
		},
	}
	reset.Flags().StringVar(&name, "name", "", "consumer name (required)")
	reset.Flags().StringVar(&to, "to", "", "earliest, latest or a seq, instead of deleting")
	reset.Flags().Int64Var(&partition, "partition", -1, "only this partition")
	_ = reset.MarkFlagRequired("name")

	var setName string
	var setPartition int64
	var seq uint64
	set := &cobra.Command{
		Use:   "set <topic>",
		Short: "Set a name's saved position for one partition on the connected broker's cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !validName(setName) {
				return usageErr("--name: 1 to 64 of A-Z a-z 0-9 . _ -")
			}
			if setPartition < 0 {
				return usageErr("--partition is required")
			}
			s, err := g.positions()
			if err != nil {
				return err
			}
			unlock, err := s.lockName(setName)
			if err != nil {
				return err
			}
			defer unlock()
			return setPositions(cmd.Context(), g, s, args[0], setName, setPartition, strconv.FormatUint(seq, 10))
		},
	}
	set.Flags().StringVar(&setName, "name", "", "consumer name (required)")
	set.Flags().Int64Var(&setPartition, "partition", -1, "partition (required)")
	set.Flags().Uint64Var(&seq, "seq", 0, "the next seq to read (required)")
	_ = set.MarkFlagRequired("name")
	_ = set.MarkFlagRequired("seq")

	cmd.AddCommand(list, reset, set)
	return cmd
}

// setPositions connects for the cluster id and the topic's partitions and
// saves to (earliest, latest or a seq) for each partition, or only for
// partition if it is not negative.
func setPositions(ctx context.Context, g *globals, s positionStore, topic, name string, partition int64, to string) error {
	c, err := g.connect(ctx, 0)
	if err != nil {
		return err
	}
	defer c.Close()
	info, md, err := topicMetadata(ctx, g, c, topic)
	if err != nil {
		return err
	}
	next := map[uint32]uint64{}
	found := false
	for _, p := range md.Partitions {
		if partition >= 0 && int64(p.Partition) != partition {
			continue
		}
		found = true
		switch to {
		case "earliest":
			next[p.Partition] = p.StartSeq
		case "latest":
			next[p.Partition] = p.NextSeq
		default:
			n, err := strconv.ParseUint(to, 10, 64)
			if err != nil {
				return usageErr("--to %q: want earliest, latest or a seq", to)
			}
			if n > p.NextSeq {
				return usageErr("seq %d is past partition %d's next seq %d", n, p.Partition, p.NextSeq)
			}
			next[p.Partition] = n
		}
	}
	if !found {
		return usageErr("topic %s has no partition %d", topic, partition)
	}
	return s.save(info.ClusterID, topic, name, next)
}

// topicMetadata returns the broker's Hello answer and a topic's partitions.
func topicMetadata(ctx context.Context, g *globals, c *client.Client, topic string) (client.BrokerInfo, client.TopicMetadata, error) {
	var info client.BrokerInfo
	var md *client.Metadata
	err := g.request(ctx, func(ctx context.Context) (err error) {
		if info, err = c.Hello(ctx); err != nil {
			return err
		}
		md, err = c.Metadata(ctx, topic)
		return err
	})
	if err != nil {
		return info, client.TopicMetadata{}, requestErr(err, false)
	}
	for _, t := range md.Topics {
		if t.Name == topic {
			if t.Error != 0 {
				return info, t, &exitError{exitRefused, fmt.Errorf("%s: %w", topic, t.Error)}
			}
			return info, t, nil
		}
	}
	return info, client.TopicMetadata{}, requestErr(fmt.Errorf("%w: Metadata did not list %q", client.ErrProtocol, topic), false)
}
