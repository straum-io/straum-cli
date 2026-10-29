## straum-cli

A terminal client for a Straum broker (protocol version 0)

### Synopsis

A terminal client for a Straum broker (protocol version 0).

Settings come from flags, then STRAUM_CLI_* environment variables, then the
selected profile of the configuration file, then built-in defaults; see
"straum-cli config". Results go to stdout as a table or as JSON lines (-o json);
diagnostics go to stderr. The exit codes are listed in README.md.

### Options

```
      --broker string            broker addresses, host:port, comma-separated (the bootstrap list) (default 127.0.0.1:7171)
      --config string            configuration file (default: $STRAUM_CLI_CONFIG, else <user config dir>/straum-cli/config)
      --connect-timeout string   bound on dialing and Hello (default 10s)
  -h, --help                     help for straum-cli
  -o, --output string            output format: table or json (default table)
      --profile string           profile of the configuration file (default: $STRAUM_CLI_PROFILE, else the file's current, else default)
      --timeout string           per-request timeout (default 10s)
```

### SEE ALSO

* [straum-cli broker](straum-cli_broker.md)	 - Broker information
* [straum-cli config](straum-cli_config.md)	 - Show and change the configuration file and its profiles
* [straum-cli consume](straum-cli_consume.md)	 - Read messages from a topic
* [straum-cli ping](straum-cli_ping.md)	 - Connect, print the broker's Hello answer, and measure Ping round trips
* [straum-cli positions](straum-cli_positions.md)	 - List, reset and set the saved positions of named consumers
* [straum-cli produce](straum-cli_produce.md)	 - Publish messages from arguments, stdin or files
* [straum-cli stats](straum-cli_stats.md)	 - Print the broker's counters since it started (BrokerStats); the same as broker stats
* [straum-cli topic](straum-cli_topic.md)	 - Create, list and describe topics
* [straum-cli version](straum-cli_version.md)	 - Print the CLI version, commit, Go version and SDK version

