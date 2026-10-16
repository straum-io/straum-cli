package metrics

import (
	"os"
	"strings"
	"testing"
)

// The fixtures are /metrics of a real three-node cluster (straum
// a70ce06, debug build, idle after election).
func TestParseRealNodeOutput(t *testing.T) {
	for i, want := range []string{"0", "1", "2"} {
		f, err := os.Open("../testdata/node" + want + ".prom")
		if err != nil {
			t.Fatal(err)
		}
		samples, err := parseSamples(f)
		f.Close()
		if err != nil {
			t.Fatalf("node%d: %v", i, err)
		}
		if samples["straum_node"] != want {
			t.Errorf("node%d: straum_node = %q", i, samples["straum_node"])
		}
		for _, name := range stable {
			if _, ok := samples[name]; !ok {
				t.Errorf("node%d: %s missing", i, name)
			}
		}
	}
}

func TestParseSamples(t *testing.T) {
	in := `# HELP a_metric x
# TYPE a_metric gauge
a_metric 1

labelled{replica="1"} 7
with_ts 3 1700000000000
	indented 4
`
	got, err := parseSamples(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a_metric": "1", "with_ts": "3", "indented": "4"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseSamplesRejects(t *testing.T) {
	for name, in := range map[string]string{
		"no value":      "a_metric\n",
		"no name":       "1 2\n",
		"extra fields":  "a 1 2 3\n",
		"glued value":   "a-1\n",
		"duplicate":     "a 1\na 2\n",
		"html error":    "<html>oops</html>\n",
		"line too long": "a " + strings.Repeat("9", 70_000) + "\n",
	} {
		if _, err := parseSamples(strings.NewReader(in)); err == nil {
			t.Errorf("%s: accepted %q", name, in[:min(len(in), 40)])
		}
	}
}
