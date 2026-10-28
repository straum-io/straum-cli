## straum-cli positions reset

Delete a name's saved positions for a topic, or move them with --to

### Synopsis

Delete a name's saved positions for a topic (every cluster id; the next
consume --name starts at --from's default), or with --to earliest|latest|<seq>
set them for the connected broker's cluster.

```
straum-cli positions reset <topic> [flags]
```

### Options

```
  -h, --help            help for reset
      --name string     consumer name (required)
      --partition int   only this partition (default -1)
      --to string       earliest, latest or a seq, instead of deleting
```

### Options inherited from parent commands

```
      --broker string            broker addresses, host:port, comma-separated (the bootstrap list) (default 127.0.0.1:7171)
      --config string            configuration file (default: $STRAUM_CLI_CONFIG, else <user config dir>/straum-cli/config)
      --connect-timeout string   bound on dialing and Hello (default 10s)
  -o, --output string            output format: table or json (default table)
      --profile string           profile of the configuration file (default: $STRAUM_CLI_PROFILE, else the file's current, else default)
      --timeout string           per-request timeout (default 10s)
```

### SEE ALSO

* [straum-cli positions](straum-cli_positions.md)	 - List, reset and set the saved positions of named consumers

