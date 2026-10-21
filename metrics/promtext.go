package metrics

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

// parseSamples reads the Prometheus text exposition format (0.0.4) and
// returns the value of every sample without labels, by metric name.
// Labelled samples, comments and blank lines are skipped; a line that is
// none of these, or a name seen twice, is an error.
func parseSamples(r io.Reader) (map[string]string, error) {
	samples := make(map[string]string)
	sc := bufio.NewScanner(r)
	// Bytes, not Text: comments and labelled samples, most of the render,
	// are skipped without a string allocated for them.
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] == '#' {
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
		if len(rest) > 0 && rest[0] == '{' {
			continue
		}
		// value, then an optional timestamp
		fields := bytes.Fields(rest)
		if len(fields) == 0 || len(fields) > 2 || rest[0] != ' ' && rest[0] != '\t' {
			return nil, fmt.Errorf("line %d: malformed sample %q", n, line)
		}
		if _, dup := samples[string(name)]; dup { // a lookup, no allocation
			return nil, fmt.Errorf("line %d: %s appears twice", n, name)
		}
		samples[string(name)] = string(fields[0])
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return samples, nil
}

func isNameByte(c byte, first bool) bool {
	return c == '_' || c == ':' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || !first && '0' <= c && c <= '9'
}
