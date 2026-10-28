## straum-cli topic

Create, list and describe topics

### Options

```
  -h, --help   help for topic
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
* [straum-cli topic create](straum-cli_topic_create.md)	 - Create a topic
* [straum-cli topic describe](straum-cli_topic_describe.md)	 - Print a topic's configuration and each partition's seq range
* [straum-cli topic list](straum-cli_topic_list.md)	 - List topics by name, following the broker's page cursor to the end

