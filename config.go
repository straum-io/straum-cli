package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// The configuration file (D-238 B): `key = value` lines with `#` comments,
// a top-level `current = <profile>`, and sections `[profile <name>]`.

const defaultProfile = "default"

// A setting the CLI resolves: flag, then environment variable, then the
// profile, then the default.
type setting struct {
	key, flag, env, def, help string
	check                     func(string) error
}

var settingsTable = []setting{
	{"broker", "broker", "STRAUM_CLI_BROKER", "127.0.0.1:7171",
		"broker addresses, host:port, comma-separated (the bootstrap list)", checkBrokers},
	{"timeout", "timeout", "STRAUM_CLI_TIMEOUT", "10s", "per-request timeout", checkDuration},
	{"connect_timeout", "connect-timeout", "", "10s", "bound on dialing and Hello", checkDuration},
	{"output", "output", "STRAUM_CLI_OUTPUT", "table", "output format: table or json", checkOutput},
	{"client_name", "", "", "straum-cli", "the name sent in Hello", checkNonEmpty},
	{"max_in_flight_bytes", "", "", "8388608", "bytes of requests in flight per connection", checkPositive},
}

// Keys and flags reserved for broker versions that serve TLS and
// authentication (D-203 A); they are refused until then.
var reservedKeys = []string{"tls", "tls_ca", "tls_server_name", "auth_mechanism", "username", "password_file"}

func lookupSetting(key string) (setting, error) {
	for _, s := range settingsTable {
		if s.key == key {
			return s, nil
		}
	}
	if slices.Contains(reservedKeys, key) {
		return setting{}, fmt.Errorf("%s is reserved for a broker version with TLS and authentication; protocol version 0 has neither", key)
	}
	return setting{}, fmt.Errorf("unknown key %q", key)
}

func checkBrokers(v string) error {
	_, err := splitBrokers(v)
	return err
}

func splitBrokers(v string) ([]string, error) {
	var out []string
	for a := range strings.SplitSeq(v, ",") {
		a = strings.TrimSpace(a)
		host, port, err := net.SplitHostPort(a)
		if _, perr := strconv.ParseUint(port, 10, 16); err != nil || perr != nil || host == "" {
			return nil, fmt.Errorf("broker address %q: want host:port", a)
		}
		out = append(out, a)
	}
	return out, nil
}

func checkDuration(v string) error {
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fmt.Errorf("%q: want a positive duration such as 10s", v)
	}
	return nil
}

func checkOutput(v string) error {
	if v != "table" && v != "json" {
		return fmt.Errorf("%q: want table or json", v)
	}
	return nil
}

func checkNonEmpty(v string) error {
	if v == "" {
		return errors.New("empty value")
	}
	return nil
}

func checkPositive(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fmt.Errorf("%q: want a positive integer", v)
	}
	return nil
}

// configFile is a parsed configuration file; lines keeps the text so that
// `config set` and `config use` change one line and keep the comments.
type configFile struct {
	path     string
	exists   bool
	current  string
	profiles map[string]map[string]string
	names    []string // profiles in file order
	lines    []string
}

func parseConfig(path string, data []byte) (*configFile, error) {
	cf := &configFile{path: path, profiles: map[string]map[string]string{}}
	var section map[string]string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		cf.lines = append(cf.lines, line)
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		bad := func(format string, a ...any) error {
			return fmt.Errorf("%s:%d: %s", path, n, fmt.Sprintf(format, a...))
		}
		if strings.HasPrefix(t, "[") {
			name, ok := profileHeader(t)
			if !ok {
				return nil, bad("want [profile <name>], got %s", t)
			}
			if _, dup := cf.profiles[name]; dup {
				return nil, bad("profile %s appears twice", name)
			}
			section = map[string]string{}
			cf.profiles[name] = section
			cf.names = append(cf.names, name)
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			return nil, bad("want key = value")
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if section == nil {
			if k != "current" {
				return nil, bad("only current = <profile> may precede the first [profile] section")
			}
			cf.current = v
			continue
		}
		s, err := lookupSetting(k)
		if err != nil {
			return nil, bad("%v", err)
		}
		if err := s.check(v); err != nil {
			return nil, bad("%s: %v", k, err)
		}
		if _, dup := section[k]; dup {
			return nil, bad("%s set twice in this profile", k)
		}
		section[k] = v
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cf, nil
}

func profileHeader(t string) (string, bool) {
	inner, ok := strings.CutPrefix(t, "[profile ")
	if !ok || !strings.HasSuffix(inner, "]") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimSuffix(inner, "]"))
	return name, validName(name)
}

// validName: profile and consumer names become path elements.
func validName(s string) bool {
	if s == "" || len(s) > 64 || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func loadConfig(path string) (*configFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &configFile{path: path, profiles: map[string]map[string]string{}}, nil
	}
	if err != nil {
		return nil, err
	}
	cf, err := parseConfig(path, data)
	if err != nil {
		return nil, err
	}
	cf.exists = true
	return cf, nil
}

// set writes key = value into the profile's section, adding the line or
// the section if missing.
func (cf *configFile) set(profile, key, value string) {
	start := -1
	for i, l := range cf.lines {
		if name, ok := profileHeader(strings.TrimSpace(l)); ok && name == profile {
			start = i
			break
		}
	}
	line := key + " = " + value
	if start < 0 {
		if n := len(cf.lines); n > 0 && strings.TrimSpace(cf.lines[n-1]) != "" {
			cf.lines = append(cf.lines, "")
		}
		cf.lines = append(cf.lines, "[profile "+profile+"]", line)
		return
	}
	last := start // last non-blank line of the section
	for i := start + 1; i < len(cf.lines); i++ {
		t := strings.TrimSpace(cf.lines[i])
		if strings.HasPrefix(t, "[") {
			break
		}
		if k, _, ok := strings.Cut(t, "="); ok && !strings.HasPrefix(t, "#") && strings.TrimSpace(k) == key {
			cf.lines[i] = line
			return
		}
		if t != "" {
			last = i
		}
	}
	cf.lines = slices.Insert(cf.lines, last+1, line)
}

// setCurrent writes current = profile at the top level.
func (cf *configFile) setCurrent(profile string) {
	for i, l := range cf.lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			break
		}
		if k, _, ok := strings.Cut(t, "="); ok && !strings.HasPrefix(t, "#") && strings.TrimSpace(k) == "current" {
			cf.lines[i] = "current = " + profile
			return
		}
	}
	cf.lines = slices.Insert(cf.lines, 0, "current = "+profile)
}

// save re-parses the edited text (an edit must leave a valid file) and
// replaces the file atomically.
func (cf *configFile) save() error {
	data := []byte(strings.Join(cf.lines, "\n") + "\n")
	if _, err := parseConfig(cf.path, data); err != nil {
		return err
	}
	return writeFileAtomic(cf.path, data)
}

// writeFileAtomic writes a temporary file next to path, syncs it and
// renames it over path, so a reader sees the old or the new content.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// globals holds the global flags and the settings resolved from them.
type globals struct {
	env        *env
	cmd        *cobra.Command
	configFlag string
	profile    string
	flags      map[string]*string // by setting key

	// Resolved by resolve().
	file           *configFile
	profileName    string
	profileSource  string
	values         map[string]string
	sources        map[string]string
	brokers        []string
	timeout        time.Duration
	connectTimeout time.Duration
	output         string
	clientName     string
	maxInFlight    int
}

func (g *globals) register(root *cobra.Command) {
	pf := root.PersistentFlags()
	pf.StringVar(&g.configFlag, "config", "", "configuration file (default: $STRAUM_CLI_CONFIG, else <user config dir>/straum-cli/config)")
	pf.StringVar(&g.profile, "profile", "", "profile of the configuration file (default: $STRAUM_CLI_PROFILE, else the file's current, else default)")
	g.flags = map[string]*string{}
	for _, s := range settingsTable {
		if s.flag == "" {
			continue
		}
		v := new(string)
		g.flags[s.key] = v
		help := s.help + " (default " + s.def + ")"
		if s.flag == "output" {
			pf.StringVarP(v, s.flag, "o", "", help)
		} else {
			pf.StringVar(v, s.flag, "", help)
		}
	}
	for _, k := range reservedKeys {
		f := strings.ReplaceAll(k, "_", "-")
		if k == "tls" {
			pf.Bool(f, false, "reserved for a broker version with TLS")
		} else {
			pf.String(f, "", "reserved for a broker version with TLS and authentication")
		}
		_ = pf.MarkHidden(f)
	}
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		g.cmd = cmd
		for _, k := range reservedKeys {
			if cmd.Flags().Changed(strings.ReplaceAll(k, "_", "-")) {
				_, err := lookupSetting(k)
				return localErr(err)
			}
		}
		return nil
	}
}

func (g *globals) configPath() (string, error) {
	if g.configFlag != "" {
		return g.configFlag, nil
	}
	if p := g.env.getenv("STRAUM_CLI_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no configuration directory (%v); pass --config", err)
	}
	return filepath.Join(dir, "straum-cli", "config"), nil
}

// resolve reads the configuration file and computes every setting. With
// allowMissing, a profile absent from the file is not an error (config set
// creates it).
func (g *globals) resolve(allowMissing bool) error {
	path, err := g.configPath()
	if err != nil {
		return localErr(err)
	}
	if g.file, err = loadConfig(path); err != nil {
		return localErr(err)
	}
	switch {
	case g.profile != "":
		g.profileName, g.profileSource = g.profile, "flag"
	case g.env.getenv("STRAUM_CLI_PROFILE") != "":
		g.profileName, g.profileSource = g.env.getenv("STRAUM_CLI_PROFILE"), "env"
	case g.file.current != "":
		g.profileName, g.profileSource = g.file.current, "file"
	default:
		g.profileName, g.profileSource = defaultProfile, "default"
	}
	if !validName(g.profileName) {
		return usageErr("profile %q: use 1 to 64 of A-Z a-z 0-9 . _ -", g.profileName)
	}
	prof, ok := g.file.profiles[g.profileName]
	if !ok && !allowMissing && g.profileSource != "default" {
		return usageErr("profile %q is not in %s", g.profileName, path)
	}
	g.values, g.sources = map[string]string{}, map[string]string{}
	for _, s := range settingsTable {
		v, src := s.def, "default"
		if fv, ok := prof[s.key]; ok {
			v, src = fv, "file"
		}
		if s.env != "" && g.env.getenv(s.env) != "" {
			v, src = g.env.getenv(s.env), "env"
		}
		if s.flag != "" && g.cmd != nil && g.cmd.Flags().Changed(s.flag) {
			v, src = *g.flags[s.key], "flag"
		}
		if err := s.check(v); err != nil {
			return usageErr("%s (from %s): %v", s.key, src, err)
		}
		g.values[s.key], g.sources[s.key] = v, src
	}
	g.brokers, _ = splitBrokers(g.values["broker"])
	g.timeout, _ = time.ParseDuration(g.values["timeout"])
	g.connectTimeout, _ = time.ParseDuration(g.values["connect_timeout"])
	g.output = g.values["output"]
	g.clientName = g.values["client_name"]
	g.maxInFlight, _ = strconv.Atoi(g.values["max_in_flight_bytes"])
	return nil
}

func (g *globals) json() bool { return g.output == "json" }

func configCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show and change the configuration file and its profiles",
		Long: `Show and change the configuration file and its profiles.

The file holds key = value lines with # comments, an optional top-level
"current = <profile>", and sections "[profile <name>]":

  current = local

  [profile local]
  broker = 127.0.0.1:7171
  timeout = 10s

Keys: broker, timeout, connect_timeout, output, client_name,
max_in_flight_bytes. An unknown key is an error naming its line.`,
	}
	show := &cobra.Command{
		Use:     "show",
		Aliases: []string{"view"},
		Short:   "Print the effective settings and where each comes from (flag, env, file, default)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := g.resolve(false); err != nil {
				return err
			}
			rows := [][2]string{{"profile", g.profileName}}
			srcs := []string{g.profileSource}
			for _, s := range settingsTable {
				rows = append(rows, [2]string{s.key, g.values[s.key]})
				srcs = append(srcs, g.sources[s.key])
			}
			return renderSettings(cmd.OutOrStdout(), g.json(), rows, srcs)
		},
	}
	path := &cobra.Command{
		Use:   "path",
		Short: "Print the configuration file's path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := g.configPath()
			if err != nil {
				return localErr(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), p)
			return nil
		},
	}
	profiles := &cobra.Command{
		Use:   "profiles",
		Short: "List the profiles of the configuration file; * marks the current one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := g.resolve(true); err != nil {
				return err
			}
			return renderProfiles(cmd.OutOrStdout(), g.json(), g.file.names, g.file.current)
		},
	}
	use := &cobra.Command{
		Use:   "use <profile>",
		Short: "Make a profile the current one (current = <profile>)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := g.resolve(true); err != nil {
				return err
			}
			if _, ok := g.file.profiles[args[0]]; !ok {
				return usageErr("profile %q is not in %s; add it with config set --profile %s", args[0], g.file.path, args[0])
			}
			g.file.setCurrent(args[0])
			return localErr(g.file.save())
		},
	}
	set := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a key in the selected profile, creating the file or the profile if needed",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := g.resolve(true); err != nil {
				return err
			}
			s, err := lookupSetting(args[0])
			if err != nil {
				return localErr(err)
			}
			if err := s.check(args[1]); err != nil {
				return usageErr("%s: %v", s.key, err)
			}
			g.file.set(g.profileName, s.key, args[1])
			return localErr(g.file.save())
		},
	}
	cmd.AddCommand(show, path, profiles, use, set)
	return cmd
}
