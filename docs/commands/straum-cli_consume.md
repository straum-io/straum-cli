## straum-cli consume

Read messages from a topic

### Synopsis

Read messages from a topic, from every partition or from --partition.

Start (--from): earliest, latest (the partition's next seq at start), a seq,
or saved (the position saved for --name). Default: the saved position when
--name has one, else earliest.

Stop: at the partition's end as seen at start; with --follow, never (Ctrl-C
ends it with exit 0), waiting up to --max-wait per Fetch for new messages.
--count, --until-seq and --duration stop earlier. Exit 2 when --duration
ends before --count messages were read.

--name N saves the next seq per partition after each batch is written to
stdout, so a later run continues there (see straum-cli positions). Delivery
to stdout is at least once: a crash between writing and saving prints those
messages again. A run that reads nothing saves nothing: after
--from latest with no new message, the next run starts at the default.

Table output is tab-separated: partition, seq, timestamp, key ("-" for no
key), body, and with --print headers the headers as name=value,... Bytes are
text when printable UTF-8, quoted when UTF-8 with control characters, else
base64:<data>. JSON output: one object per message (docs/output.md).

```
straum-cli consume <topic> [flags]
```

### Options

```
      --bytes string        how bytes are written: text (UTF-8 where valid) or base64 (default "text")
  -n, --count uint          stop after N messages (0: no limit)
      --duration duration   stop after this long (0: no limit)
  -f, --follow              keep reading new messages until interrupted
      --from string         earliest, latest, saved or a seq
  -h, --help                help for consume
      --max-bytes uint32    encoded bytes per Fetch (capped by the broker; the first message always comes) (default 1048576)
      --max-wait duration   with --follow: how long one Fetch waits for new messages (capped by the broker) (default 30s)
      --name string         consumer name: start from and save its position
      --partition int       read only this partition (default -1)
      --print strings       extra table columns: headers
      --until-seq uint      read only messages with a seq below S (0: no limit)
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

