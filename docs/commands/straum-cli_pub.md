## straum-cli pub

Publish one MQTT message

### Synopsis

Publishes one message over MQTT 5 and disconnects.

QoS 1 succeeds only on a PUBACK, which straum sends once a majority of the
cluster has committed the message. QoS 0 succeeds once the message is
written to the connection: nothing confirms it.

With --retain the message becomes the topic's retained message; an empty
payload (-m '') clears it.

Exit codes: 0 published, 1 usage error, 4 refused or not sent (nothing
was published), 5 sent but the outcome is unknown (no PUBACK in time, or
the connection broke): the message may or may not be committed.

```
straum-cli pub [flags]
```

### Options

```
      --broker string         the node's MQTT address, host:port (STRAUM_CLUSTER_MQTT_LISTEN); or set STRAUM_CLI_BROKER
      --client-id string      the MQTT client id; empty lets the node assign one
  -h, --help                  help for pub
      --keep-alive duration   MQTT keep-alive, whole seconds, at most 65535s (default 30s)
  -m, --message string        the payload
  -q, --qos int               0 or 1 (default 1)
  -r, --retain                set the topic's retained message
      --stdin                 read the payload from stdin, up to 256 KiB
      --timeout duration      how long to wait for the node at each step: connect, PUBACK, SUBACK (default 10s)
  -t, --topic string          the topic
```

### SEE ALSO

* [straum-cli](straum-cli.md)	 - A terminal client for a Straum cluster

