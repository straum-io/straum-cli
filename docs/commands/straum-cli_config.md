## straum-cli config

Show and change the configuration file and its profiles

### Synopsis

Show and change the configuration file and its profiles.

The file holds key = value lines with # comments, an optional top-level
"current = <profile>", and sections "[profile <name>]":

  current = local

  [profile local]
  broker = 127.0.0.1:7171
  timeout = 10s

Keys: broker, timeout, connect_timeout, output, client_name,
max_in_flight_bytes. An unknown key is an error naming its line.

### Options

```
  -h, --help   help for config
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
* [straum-cli config path](straum-cli_config_path.md)	 - Print the configuration file's path
* [straum-cli config profiles](straum-cli_config_profiles.md)	 - List the profiles of the configuration file; * marks the current one
* [straum-cli config set](straum-cli_config_set.md)	 - Set a key in the selected profile, creating the file or the profile if needed
* [straum-cli config show](straum-cli_config_show.md)	 - Print the effective settings and where each comes from (flag, env, file, default)
* [straum-cli config use](straum-cli_config_use.md)	 - Make a profile the current one (current = <profile>)

