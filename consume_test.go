package main

import (
	"strings"
	"testing"

	"github.com/straum-io/straum-go-sdk/client"
)

func TestStartSeq(t *testing.T) {
	p := client.PartitionMetadata{Partition: 0, StartSeq: 5, NextSeq: 20}
	saved := map[uint32]uint64{0: 12}
	cases := []struct {
		from, name string
		saved      map[uint32]uint64
		want       uint64
		err        string
	}{
		{"", "", nil, 5, ""},
		{"", "g", nil, 5, ""},           // named, nothing saved: earliest
		{"", "g", saved, 12, ""},        // named: the saved position
		{"earliest", "g", saved, 5, ""}, // --from wins over the saved position
		{"latest", "", nil, 20, ""},
		{"7", "", nil, 7, ""},
		{"30", "", nil, 30, ""}, // past the end: waits there, like the broker
		{"saved", "", saved, 0, "needs --name"},
		{"saved", "g", nil, 0, "no saved position"},
		{"soon", "", nil, 0, "want earliest"},
		{"", "g", map[uint32]uint64{0: 21}, 0, "positions reset"},
	}
	for _, tc := range cases {
		o := &consumeOpts{from: tc.from, name: tc.name}
		got, err := o.startSeq(p, tc.saved)
		switch {
		case tc.err != "":
			if err == nil || exitOf(err) != exitUsage || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("from %q name %q: %d %v, want error %q", tc.from, tc.name, got, err, tc.err)
			}
		case err != nil || got != tc.want:
			t.Errorf("from %q name %q: %d %v, want %d", tc.from, tc.name, got, err, tc.want)
		}
	}
}
