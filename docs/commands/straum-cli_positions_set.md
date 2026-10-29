## straum-cli positions set

Set a name's saved position for one partition on the connected broker's cluster

```
straum-cli positions set <topic> [flags]
```

### Options

```
  -h, --help            help for set
      --name string     consumer name (required)
      --partition int   partition (required) (default -1)
      --seq uint        the next seq to read (required)
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

