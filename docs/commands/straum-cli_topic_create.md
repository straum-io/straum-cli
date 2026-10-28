## straum-cli topic create

Create a topic

### Synopsis

Create a topic. Names are 1 to 80 bytes of A-Z a-z 0-9 . _ -; names starting
with __ are reserved. Version 0 brokers accept one partition and replication
factor 1, and record the retention without releasing messages by age.

Without --retries, a lost connection is exit 5 (the topic may or may not
exist); with --if-not-exists, a resend that finds the topic is success.

```
straum-cli topic create <name> [flags]
```

### Options

```
  -h, --help                        help for create
      --if-not-exists               succeed when the topic already exists
      --partitions uint32           number of partitions (default 1)
      --replication-factor uint16   replicas per partition (default 1)
      --retention duration          how long messages are kept (0: everything is kept)
      --retries int                 resend the request up to N times after a lost connection or Overloaded; a resent request may take effect twice (protocol version 0 has no idempotent produce)
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

