## straum-cli status

Show what each configured node reports about itself

### Synopsis

Reads the metrics endpoint (STRAUM_CLUSTER_METRICS_LISTEN) of every
configured node once and shows the stable metrics each one reports, with
the address it came from and how old the observation is. A node that does
not answer is shown as unknown. No health verdict is drawn.

Nodes come from --node, or else from STRAUM_CLI_NODES (comma-separated
host:port). There is no default cluster.

Exit codes: 0 every node observed, 1 usage or configuration error,
2 some nodes unknown, 3 every node unknown.

```
straum-cli status [flags]
```

### Options

```
  -h, --help               help for status
      --node strings       a node's metrics address, host:port (repeatable or comma-separated)
  -o, --output string      table or json (default "table")
      --timeout duration   how long to wait for each node (default 2s)
```

### SEE ALSO

* [straum-cli](straum-cli.md)	 - A terminal client for a Straum cluster

