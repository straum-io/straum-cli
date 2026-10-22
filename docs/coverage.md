# Broker coverage matrix

What the Straum core can do, and which CLI command covers each part. This
is the CLI's roadmap (D-141 A, amended to allow MQTT publish and
subscribe; the core's D-158 A and D-164 A supplied the credentials and TLS
that D-141 kept out until they existed).

Taken from the core at `0c5f007` (2026-10-16), credentials and TLS at `4e78fc2` (2026-10-21), node TLS at `3d018c0` (2026-10-22): `crates/gateway/src/lib.rs`,
`crates/journal` (`Kind`), `crates/server/src/config.rs`,
`crates/node/src/metrics.rs` and `docs/decisions`.

The **Core elements** column is checked by `make coverage-check`
(`scripts/check-coverage.sh`). The check reads the core's declared client
contract: the blocks of its `docs/configuration.md`, each pinned by a test
in the core. They hold the settings, the written and reserved journal
kinds, and the reason codes the gateway sends. The check fails in two cases:
- the matrix doesn't name one of those elements in backticks;
- a row's Status is anything other than done, verified, blocked or n/a.

Update this file with every change to the core that it describes.

Status values:
- **done**: implemented and tested.
- **verified**: also passes the three-node e2e test.
- **not started**: the core supports it and the CLI has no command yet.
  The check fails on it.
- **blocked**: waits on a core change, named in Notes.
- **n/a**: the core refuses it, or it is not a Straum concept.

## Core operations: MQTT 3.1.1 and 5 (the broker's only client interface)

| Capability | Core surface | CLI command | Status | Core elements | Notes |
|---|---|---|---|---|---|
| Connect and disconnect over MQTT 5 (clean session, keep-alive) | `STRAUM_CLUSTER_MQTT_LISTEN` | `pub`, `sub` | verified | `SessionGone`, `ServerUnavailable`, `ClientIdentifierNotValid`, `Success` | The CLI always connects with a clean session. A clean session writes no `Session`, `Connect` or `Disconnect` record: those kinds wait on 7c (see "Persistent sessions and wills") |
| Publish QoS 0 | PUBLISH | `pub` | verified | `Publish` | |
| Publish QoS 1, PUBACK after commit to a majority | PUBLISH/PUBACK (ADR-0005) | `pub` | verified | `Publish` | |
| Receive QoS 1, acknowledged by the subscriber | PUBLISH/PUBACK | `sub` | verified | `Ack`, `GrantedQos1` | |
| Subscribe, with `+` and `#` wildcards | SUBSCRIBE | `sub` | verified | `Subscribe` | |
| MQTT 5 subscription options: no local, retain as published, retain handling | SUBSCRIBE | `sub` flags | verified | `Subscribe` | No local is accepted by the core, but its effect is not observable: it filters a connection's own publishes, and a `sub` connection never publishes |
| Unsubscribe | UNSUBSCRIBE | `sub` sends it on exit | verified | `Unsubscribe` | |
| Retained messages, set and clear | PUBLISH with retain set | `pub --retain`; an empty payload clears | verified | `Publish` | |
| Disconnected by the node, with a reason (MQTT 5 DISCONNECT) | DISCONNECT; QuotaExceeded also in SUBACK | `pub`, `sub`: the reason on stderr, exit 4 | verified | `SessionTakenOver`, `KeepAliveTimeout`, `ServerBusy`, `QuotaExceeded` | Every reason takes the same path. The e2e test provokes `SessionTakenOver` (the same client id twice on one node) |
| Protocol violations by a client | DISCONNECT | none | n/a | `ProtocolError`, `TopicAliasInvalid`, `ReceiveMaximumExceeded` | paho keeps to the protocol, so the CLI never provokes these. If one came, it would be reported like any other disconnect |
| MQTT 3.1.1 | the same listener | none | n/a | | The CLI speaks MQTT 5 only (`eclipse/paho.golang`, its sole MQTT dependency), which covers every 3.1.1 operation. The core's 3.1.1 support stays available to other clients and is tested in the core (`rumqttc`) |
| Topic aliases | per connection | none | n/a | | A one-shot client sends a topic once; an alias saves nothing |
| Publish QoS 2 | refused | none | n/a | `QosNotSupported`, `QosTwoState` | Deferred past the first release (D-138 (4)) |
| Shared subscriptions | refused; CONNACK says they are unavailable | none | n/a | `SharedSubscriptionsNotSupported`, `SharedAssignment` | D-138 (4) |
| Subscription identifiers | refused | none | n/a | `SubscriptionIdentifiersNotSupported` | |
| Persistent sessions and wills | refused at CONNECT | `sub --session`, `--will-*` | blocked | `ImplementationSpecificError`, `WillPublished`, `Session`, `Connect`, `Disconnect` | Core step 7c (D-143). The core declares `Session`, `Connect` and `Disconnect` stable, but `apply` refuses them as not yet built; a pending core card moves them to the reserved kinds |
| Session expiry and message expiry | refused, or not carried through the log | flags | blocked | `SessionExpired`, `MessageExpired` | 7c, D-65 |
| Credentials and TLS | user name and password checked against a password file, topic rules (D-158 A); TLS on the MQTT listener (D-164 A) | `pub`, `sub`: `--username`, `--password-file` or `STRAUM_CLI_PASSWORD`, `--tls`, `--tls-ca`, `--tls-server-name`; a password never goes in clear text beyond loopback | verified | `BadUserNameOrPassword`, `NotAuthorized` | D-110, D-158, D-164 |
| Enhanced authentication, client certificates | refused | none | n/a | `BadAuthenticationMethod` | D-110 (client certificates later) |

## Observability

| Capability | Core surface | CLI command | Status | Core elements | Notes |
|---|---|---|---|---|---|
| Per-node role, slot, commit, applied, log, lag, outbox (the stable set) | `STRAUM_CLUSTER_METRICS_LISTEN` (D-112) | `status` | verified | `STRAUM_CLUSTER_METRICS_LISTEN` | Names pinned by the core (`docs/configuration.md`, "Stable metrics"); `make drift-check` |
| Every other metric: histograms, replicas, takeovers, MQTT load, catch-up | metrics endpoint, unstable | none | n/a | | Not a client contract (D-141 A). Prometheus and Grafana are the monitoring path (D-138 (7)) |
| Health verdict | none; the core draws no verdict | none | n/a | | By design (brief §7) |

## Admin operations

| Capability | Core surface | CLI command | Status | Core elements | Notes |
|---|---|---|---|---|---|
| Planned handoff of the slot | SIGUSR1, same host only | `handoff` | blocked | `SlotEnd` | Needs the admin API ADR (D-141 C); remotely it needs D-110 too |
| Users and ACLs | password and ACL files, read at start and on SIGHUP (D-158, D-166); no runtime API | `acl …` | blocked | | D-110 |
| Server configuration | env vars, read once at start | none | n/a | `STRAUM_EXPERIMENTAL_CLUSTER`, `STRAUM_CLUSTER_NODE_ID`, `STRAUM_CLUSTER_PEERS`, `STRAUM_CLUSTER_LISTEN`, `STRAUM_CLUSTER_UNAUTHENTICATED_NETWORK`, `STRAUM_CLUSTER_DATA_DIR`, `STRAUM_CLUSTER_HEARTBEAT_MS`, `STRAUM_CLUSTER_TAKEOVER_TICKS`, `STRAUM_CLUSTER_RETAIN_BYTES`, `STRAUM_READ_BYTES`, `STRAUM_LOG_LEVEL`, `STRAUM_WORKER_THREADS`, `STRAUM_AUTH_PASSWORD_FILE`, `STRAUM_AUTH_ACL_FILE`, `STRAUM_AUTH_ALLOW_ANONYMOUS`, `STRAUM_TLS_CERT_PATH`, `STRAUM_TLS_KEY_PATH`, `STRAUM_CLUSTER_TLS_CERT_PATH`, `STRAUM_CLUSTER_TLS_KEY_PATH`, `STRAUM_CLUSTER_TLS_CA_PATH`, `STRAUM_WAL_PREALLOCATE_BYTES` | Belongs to the server process; no runtime change exists to drive. The three `STRAUM_CLUSTER_TLS_*` secure the links between nodes (ADR-0010), which the CLI never opens |
| Membership change | none | none | n/a | | Not in the core |
| Create or delete topics | none; MQTT topics are not declared | none | n/a | | |

## Cluster internals

| Capability | Core surface | CLI command | Status | Core elements | Notes |
|---|---|---|---|---|---|
| Batches, node resets, slot changes | the log, between nodes | none | n/a | `BatchHeader`, `NodeReset`, `SlotStart`, `SlotEnd` | Not a client interface (committed-log boundary). Visible through `status` (`straum_slot`, `straum_leading`) |
| Snapshots and compaction | automatic, inside the node | none | n/a | | Never driven from outside |
| Backup, restore, migration, rebalance | none | none | n/a | | Not in the core |

## Not Straum concepts

Queues, partitions, offsets, consumer groups, exchanges, bindings,
routing keys, dead-letter queues, nack and purge. Straum is an MQTT broker
over one replicated log. Commands for these would invent semantics the
core doesn't have, which the brief forbids in §2 and §6.
