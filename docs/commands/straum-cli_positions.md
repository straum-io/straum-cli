## straum-cli positions

List, reset and set the saved positions of named consumers

### Synopsis

List, reset and set the saved positions of named consumers (consume --name).

The broker keeps no consumer positions in protocol version 0; straum-cli keeps
them per profile in <state dir>/straum-cli/<profile>/positions.json, where the
state directory is $XDG_STATE_HOME, else ~/.local/state (Linux), else the user
configuration directory (macOS, Windows). A position is the next seq to read,
per broker cluster id, topic, partition and name.

### Options

```
  -h, --help   help for positions
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

* [straum-cli](straum-cli.md)	 - A terminal client for a Straum broker (protocol version 0)
* [straum-cli positions list](straum-cli_positions_list.md)	 - List saved positions
* [straum-cli positions reset](straum-cli_positions_reset.md)	 - Delete a name's saved positions for a topic, or move them with --to
* [straum-cli positions set](straum-cli_positions_set.md)	 - Set a name's saved position for one partition on the connected broker's cluster

