## straum-cli ping

Connect, print the broker's Hello answer, and measure Ping round trips

### Synopsis

Connect, print the broker's Hello answer (protocol version, cluster id, node
id, frame and message limits), and measure Ping round trips.

Exit 2 when some pings of --count failed; exit 3 when all failed.

```
straum-cli ping [flags]
```

### Options

```
  -c, --count int           number of pings (default 1)
  -h, --help                help for ping
      --interval duration   wait between pings (default 1s)
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

