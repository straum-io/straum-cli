package metrics

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// parseSamples reads the Prometheus text exposition format (0.0.4) and
// returns the value of every sample without labels, by metric name.
// Labelled samples, comments and blank lines are skipped; a line that is
// none of these, or a name seen twice, is an error.
func parseSamples(r io.Reader) (map[string]string, error) {
	samples := make(map[string]string)
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		end := 0
		for end < len(line) && isNameByte(line[end], end == 0) {
			end++
		}
		if end == 0 {
			return nil, fmt.Errorf("line %d: no metric name", n)
		}
		name, rest := line[:end], line[end:]
		if rest != "" && rest[0] == '{' {
			continue
		}
		// value, then an optional timestamp
		fields := strings.Fields(rest)
		if len(fields) == 0 || len(fields) > 2 || rest[0] != ' ' && rest[0] != '\t' {
			return nil, fmt.Errorf("line %d: malformed sample %q", n, line)
		}
		if _, dup := samples[name]; dup {
			return nil, fmt.Errorf("line %d: %s appears twice", n, name)
		}
		samples[name] = fields[0]
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return samples, nil
}

func isNameByte(c byte, first bool) bool {
	return c == '_' || c == ':' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || !first && '0' <= c && c <= '9'
}
