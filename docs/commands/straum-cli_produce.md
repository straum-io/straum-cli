## straum-cli produce

Publish messages from arguments, stdin or files

### Synopsis

Publish messages to a topic. One source per invocation:

  arguments    each argument is one message body
  --stdin      one message per line (--raw: the whole input is one message)
  --file F     each file is one message (--lines: one per line); repeatable

--key and --header apply to every message. --input json reads JSON lines
{"key": ..., "headers": [{"name": ..., "value": ...}], "body": ...} where each
byte field is a string or {"base64": "..."}, the format consume -o json
writes; key may be null or absent (no key).

Messages go to --partition, or keyed ones by the protocol's default
partitioner and unkeyed ones round robin. They are sent in batches per
partition, one Produce in flight at a time, so input order is seq order.
A batch is sent when it reaches --batch-messages or --batch-bytes, or when
the input stalls for --linger (0: at once). Each acknowledgement is printed
when it arrives; acknowledgements are durable (on the broker's disk).

The broker also refuses a Produce larger than one store commit
(max_group_messages, default 4096, and max_group_bytes, default 4 MiB, which
HelloOk does not report) with MessageTooLarge, exit 4; the defaults stay
below both.

Without --retries, a lost connection or an interrupt during a Produce is exit 5: the request in flight may or
may not be stored. With --retries, a resent request may be stored twice.

```
straum-cli produce <topic> [<body>...] [flags]
```

### Options

```
      --acks string             acknowledgement level; protocol version 0 has only durable (default "durable")
      --batch-bytes int         most encoded message bytes per Produce (capped by the broker's frame limit) (default 1048576)
      --batch-messages int      most messages per Produce (default 1000)
      --file F                  read a message from file F (repeatable)
      --header name=value       header name=value of every message (repeatable)
      --header-file name=path   header name=path, the value read from a file (repeatable)
  -h, --help                    help for produce
      --input string            input format of --stdin and --file --lines: text or json (default "text")
      --key string              key of every message (an empty --key "" is a present, empty key)
      --key-file string         read the key of every message from a file
      --lines                   with --file: one message per line
      --linger duration         how long a batch waits for more input when the input stalls
      --partition int           send every message to this partition (default -1)
      --raw                     with --stdin: the whole input is one message
      --retries int             resend the request up to N times after a lost connection or Overloaded; a resent request may take effect twice (protocol version 0 has no idempotent produce)
      --stdin                   read messages from stdin, one per line
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

