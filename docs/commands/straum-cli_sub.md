## straum-cli sub

Subscribe and print MQTT messages

### Synopsis

Subscribes over MQTT 5 and prints each message to stdout: the payload
and a newline, the topic first with -v, or one JSON object per line with
-o json (payload in base64). It stops on Ctrl-C, after --count messages,
or after --wait, then unsubscribes and disconnects. The session is clean
and ends with the connection: messages published while no sub runs are
not kept for it (persistent sessions wait on straum's step 7c).

Exit codes: 0 done, 1 usage error, 2 --wait passed before --count
messages arrived, 4 refused or the connection was lost.

```
straum-cli sub [flags]
```

### Options

```
      --broker string            the node's MQTT address, host:port (STRAUM_CLUSTER_MQTT_LISTEN); or set STRAUM_CLI_BROKER
      --client-id string         the MQTT client id; empty lets the node assign one
  -c, --count int                stop after this many messages; 0: no limit
  -h, --help                     help for sub
      --keep-alive duration      MQTT keep-alive, whole seconds, at most 65535s (default 30s)
      --no-local                 MQTT 5: skip messages this connection publishes
  -o, --output string            plain or json (default "plain")
      --password-file string     a file whose first line is the password; or set STRAUM_CLI_PASSWORD
  -q, --qos int                  the maximum QoS to receive: 0 or 1 (default 1)
      --retain-as-published      MQTT 5: keep the retain flag as the publisher set it
      --retain-handling int      MQTT 5: 0 send retained messages, 1 only for a new subscription, 2 never
      --timeout duration         how long to wait for the node at each step: connect, PUBACK, SUBACK (default 10s)
      --tls                      connect with TLS (STRAUM_TLS_CERT_PATH on the node)
      --tls-ca string            a PEM file of the CA certificates to trust instead of the system's; implies --tls
      --tls-server-name string   the name to check the node's certificate against; default the --broker host; implies --tls
  -t, --topic stringArray        a topic filter, + and # allowed (repeatable)
      --username string          the MQTT user name (STRAUM_AUTH_PASSWORD_FILE on the node); or set STRAUM_CLI_USERNAME
  -v, --verbose                  plain output: print the topic before the payload
      --wait duration            stop after this long; 0: no limit
```

### SEE ALSO

* [straum-cli](straum-cli.md)	 - A terminal client for a Straum cluster

