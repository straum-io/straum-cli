package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/straum-io/straum-go-sdk/client"
)

func TestParseJSONMessage(t *testing.T) {
	m, err := parseJSONMessage([]byte(`{"body":"b"}`))
	if err != nil || m.Key != nil || string(m.Body) != "b" || m.Headers != nil {
		t.Fatalf("%+v %v", m, err)
	}
	m, err = parseJSONMessage([]byte(`{"key":null,"body":{"base64":"AAE="},"headers":[{"name":"a","value":{"base64":""}}]}`))
	if err != nil || m.Key != nil || string(m.Body) != "\x00\x01" || len(m.Headers) != 1 || m.Headers[0].Value == nil {
		t.Fatalf("%+v %v", m, err)
	}
	for _, bad := range []string{`{"body":1}`, `{"body":{"base64":"!"}}`, `{"key":"k"}`, `{"key":{"hex":"00"}}`, `[1]`} {
		if _, err := parseJSONMessage([]byte(bad)); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

// messageSize matches protocol 4.6: flags, key and headers when present,
// body with its length.
func TestMessageSize(t *testing.T) {
	cases := []struct {
		m    client.Message
		want int
	}{
		{client.Message{}, 5},
		{client.Message{Key: []byte{}}, 7},
		{client.Message{Key: []byte("ab"), Body: []byte("xyz")}, 1 + 4 + 2 + 2 + 3},
		{client.Message{Headers: []client.Header{{Name: []byte("n"), Value: []byte("vv")}}}, 1 + 4 + 2 + 2 + 1 + 4 + 2},
	}
	for _, tc := range cases {
		if got := messageSize(tc.m); got != tc.want {
			t.Errorf("%+v: %d, want %d", tc.m, got, tc.want)
		}
	}
}

func TestBatcher(t *testing.T) {
	var sent [][]string
	b := newBatcher(3, 20, func(p uint32, msgs []client.Message) error {
		var bodies []string
		for _, m := range msgs {
			bodies = append(bodies, string(m.Body))
		}
		sent = append(sent, append([]string{string(rune('0' + p))}, bodies...))
		return nil
	})
	add := func(p uint32, body string) {
		if err := b.add(p, client.Message{Body: []byte(body)}); err != nil {
			t.Fatal(err)
		}
	}
	add(0, "a")            // 6 bytes
	add(0, "b")            // 12
	add(1, "x")            // other partition
	add(0, "c")            // 3 messages: sent
	add(0, "dddd")         // 9
	add(0, "eeeeeeeeeeee") // 9+17 > 20: d goes alone first
	if err := b.flushAll(); err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(sent))
	for i, s := range sent {
		got[i] = strings.Join(s, ",")
	}
	want := "0,a,b,c|0,dddd"
	if g := strings.Join(got[:2], "|"); g != want {
		t.Fatalf("batches %v, want %s then the rest", got, want)
	}
	if len(got) != 4 || b.count != 0 {
		t.Fatalf("batches %v, pending %d", got, b.count)
	}

	fail := errors.New("down")
	b = newBatcher(2, 100, func(uint32, []client.Message) error { return fail })
	_ = b.add(0, client.Message{})
	if err := b.add(0, client.Message{}); !errors.Is(err, fail) || b.count != 2 {
		t.Fatalf("%v, pending %d", err, b.count)
	}
}

func readAll(t *testing.T, o *produceOpts, stdin string, bodies []string, maxBody int) ([]string, error) {
	t.Helper()
	ch := make(chan item)
	tmpl := client.Message{Key: []byte("k")}
	go o.read(context.Background(), strings.NewReader(stdin), bodies, tmpl, maxBody, ch)
	var got []string
	for it := range ch {
		if it.err != nil {
			return got, it.err
		}
		if string(it.m.Key) != "k" && o.input == "text" {
			t.Errorf("template key lost: %+v", it.m)
		}
		got = append(got, string(it.m.Body))
	}
	return got, nil
}

func TestReadSources(t *testing.T) {
	dir := t.TempDir()
	f1, f2 := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	os.WriteFile(f1, []byte("one\r\ntwo\n"), 0o600)
	os.WriteFile(f2, []byte("three"), 0o600)
	cases := []struct {
		name   string
		o      produceOpts
		stdin  string
		bodies []string
		want   string
	}{
		{"args", produceOpts{input: "text"}, "", []string{"x", "", "y"}, "x||y"},
		{"stdin lines", produceOpts{input: "text", stdin: true}, "a\n\nb\r\nc", nil, "a||b|c"},
		{"stdin raw", produceOpts{input: "text", stdin: true, raw: true}, "a\nb\n", nil, "a\nb\n"},
		{"files whole", produceOpts{input: "text", files: []string{f1, f2}}, "", nil, "one\r\ntwo\n|three"},
		{"files lines", produceOpts{input: "text", files: []string{f1, f2}, lines: true}, "", nil, "one|two|three"},
		{"stdin json", produceOpts{input: "json", stdin: true}, "{\"body\":\"j1\"}\n\n{\"key\":\"k\",\"body\":\"j2\"}\n", nil, "j1|j2"},
	}
	for _, tc := range cases {
		got, err := readAll(t, &tc.o, tc.stdin, tc.bodies, 100)
		if err != nil || strings.Join(got, "|") != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.name, got, err, tc.want)
		}
	}
	// Limits and bad input end the stream with an error after what came
	// before.
	o := produceOpts{input: "text", stdin: true}
	got, err := readAll(t, &o, "ok\n"+strings.Repeat("x", 11)+"\n", nil, 10)
	if strings.Join(got, "|") != "ok" || exitOf(err) != exitUsage || !strings.Contains(err.Error(), "longer than 10") {
		t.Errorf("long line: %q %v", got, err)
	}
	o = produceOpts{input: "text", stdin: true, raw: true}
	if _, err := readAll(t, &o, strings.Repeat("x", 11), nil, 10); err == nil || !strings.Contains(err.Error(), "max_message_bytes") {
		t.Errorf("large raw: %v", err)
	}
	o = produceOpts{input: "json", stdin: true}
	if got, err := readAll(t, &o, "{\"body\":\"a\"}\nnot json\n", nil, 100); len(got) != 1 || err == nil || !strings.Contains(err.Error(), "stdin:2") {
		t.Errorf("bad json: %q %v", got, err)
	}
	o = produceOpts{input: "text", files: []string{filepath.Join(dir, "missing")}}
	if _, err := readAll(t, &o, "", nil, 100); exitOf(err) != exitUsage {
		t.Errorf("missing file: %v", err)
	}
}

func TestTemplate(t *testing.T) {
	dir := t.TempDir()
	kf, hf := filepath.Join(dir, "k"), filepath.Join(dir, "h")
	os.WriteFile(kf, []byte{0, 1}, 0o600)
	os.WriteFile(hf, []byte("hv"), 0o600)
	m, err := (&produceOpts{}).template(false)
	if err != nil || m.Key != nil {
		t.Fatalf("no key: %+v %v", m, err)
	}
	m, err = (&produceOpts{}).template(true)
	if err != nil || m.Key == nil || len(m.Key) != 0 {
		t.Fatalf(`--key "" is a present, empty key: %+v %v`, m, err)
	}
	m, err = (&produceOpts{keyFile: kf, headers: []string{"a=b=c", "e="}, headerFiles: []string{"f=" + hf}}).template(false)
	if err != nil || string(m.Key) != "\x00\x01" || len(m.Headers) != 3 || string(m.Headers[0].Value) != "b=c" ||
		len(m.Headers[1].Value) != 0 || string(m.Headers[2].Value) != "hv" {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := (&produceOpts{headerFiles: []string{"f"}}).template(false); exitOf(err) != exitUsage {
		t.Fatalf("header-file without =: %v", err)
	}
}
