## straum-cli version

Print the CLI version, commit, Go version and SDK version

```
straum-cli version [flags]
```

### Options

```
  -h, --help   help for version
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

