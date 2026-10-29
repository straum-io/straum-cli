## straum-cli topic list

List topics by name, following the broker's page cursor to the end

```
straum-cli topic list [flags]
```

### Options

```
  -h, --help        help for list
      --limit int   list at most N topics (0: all)
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

* [straum-cli topic](straum-cli_topic.md)	 - Create, list and describe topics

