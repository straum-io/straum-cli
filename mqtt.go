package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/paho"
	"github.com/spf13/cobra"
	"github.com/straum-io/straum-cli/metrics"
)

// The core's largest MQTT packet (node::experimental, 256 KiB); a bigger
// payload cannot be published, so it is not read into memory either.
const maxPayload = 256 << 10

// failure is an error with its own exit code; main prints it.
type failure struct {
	code int
	err  error
}

func (f failure) Error() string { return f.err.Error() }

func refused(format string, a ...any) error {
	return failure{exitRefused, fmt.Errorf(format, a...)}
}

// mqttFlags are the connection settings `pub` and `sub` share. The CLI
// always asks for a clean session with no expiry and no will: the core
// refuses the others until its step 7c.
type mqttFlags struct {
	broker    string
	clientID  string
	keepAlive time.Duration
	timeout   time.Duration
	// Credentials (core D-158 A) and TLS (core D-164 A). The password
	// never comes from a flag, so it is in no process list or history.
	username      string
	passwordFile  string
	password      []byte
	useTLS        bool
	tlsCA         string
	tlsServerName string
	tlsConfig     *tls.Config
}

func (m *mqttFlags) register(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&m.broker, "broker", "", "the node's MQTT address, host:port (STRAUM_CLUSTER_MQTT_LISTEN); or set STRAUM_CLI_BROKER")
	f.StringVar(&m.clientID, "client-id", "", "the MQTT client id; empty lets the node assign one")
	f.DurationVar(&m.keepAlive, "keep-alive", 30*time.Second, "MQTT keep-alive, whole seconds, at most 65535s")
	f.DurationVar(&m.timeout, "timeout", 10*time.Second, "how long to wait for the node at each step: connect, PUBACK, SUBACK")
	f.StringVar(&m.username, "username", "", "the MQTT user name (STRAUM_AUTH_PASSWORD_FILE on the node); or set STRAUM_CLI_USERNAME")
	f.StringVar(&m.passwordFile, "password-file", "", "a file whose first line is the password; or set STRAUM_CLI_PASSWORD")
	f.BoolVar(&m.useTLS, "tls", false, "connect with TLS (STRAUM_TLS_CERT_PATH on the node)")
	f.StringVar(&m.tlsCA, "tls-ca", "", "a PEM file of the CA certificates to trust instead of the system's; implies --tls")
	f.StringVar(&m.tlsServerName, "tls-server-name", "", "the name to check the node's certificate against; default the --broker host; implies --tls")
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (m *mqttFlags) check(getenv func(string) string) error {
	if m.broker == "" {
		m.broker = getenv("STRAUM_CLI_BROKER")
	}
	if m.broker == "" {
		return errors.New("no broker: pass --broker host:port or set STRAUM_CLI_BROKER")
	}
	if err := metrics.CheckAddress(m.broker); err != nil {
		return err
	}
	if m.timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	if m.keepAlive < 0 || m.keepAlive > 65535*time.Second || m.keepAlive%time.Second != 0 {
		return errors.New("--keep-alive must be whole seconds, 0s to 65535s")
	}
	if m.username == "" {
		m.username = getenv("STRAUM_CLI_USERNAME")
	}
	switch {
	case m.passwordFile != "":
		b, err := os.ReadFile(m.passwordFile)
		if err != nil {
			return fmt.Errorf("--password-file: %w", err)
		}
		line, _, _ := strings.Cut(string(b), "\n")
		m.password = []byte(strings.TrimSuffix(line, "\r"))
		if len(m.password) == 0 {
			return fmt.Errorf("--password-file %s: the first line is empty", m.passwordFile)
		}
	case getenv("STRAUM_CLI_PASSWORD") != "":
		m.password = []byte(getenv("STRAUM_CLI_PASSWORD"))
	}
	if m.password != nil && m.username == "" {
		return errors.New("a password needs a user name: --username or STRAUM_CLI_USERNAME")
	}
	host, _, _ := net.SplitHostPort(m.broker)
	if m.tlsCA != "" || m.tlsServerName != "" {
		m.useTLS = true
	}
	// The core lets its MQTT listener leave loopback only with TLS when it
	// checks credentials (D-164 A): a password never crosses a network in
	// clear text from here either.
	if m.password != nil && !m.useTLS && !isLoopback(host) {
		return fmt.Errorf("a password to %s would cross the network in clear text: add --tls", m.broker)
	}
	if m.useTLS {
		m.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
		if m.tlsServerName != "" {
			m.tlsConfig.ServerName = m.tlsServerName
		}
		if m.tlsCA != "" {
			pem, err := os.ReadFile(m.tlsCA)
			if err != nil {
				return fmt.Errorf("--tls-ca: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return fmt.Errorf("--tls-ca %s: no PEM certificate in it", m.tlsCA)
			}
			m.tlsConfig.RootCAs = pool
		}
	}
	return nil
}

// connect dials and sends CONNECT. Every error is exitRefused: nothing
// was published or subscribed yet.
func (m *mqttFlags) connect(ctx context.Context, conf paho.ClientConfig) (*paho.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	var conn net.Conn
	var err error
	if m.tlsConfig != nil {
		conn, err = (&tls.Dialer{Config: m.tlsConfig}).DialContext(ctx, "tcp", m.broker)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", m.broker)
	}
	if err != nil {
		return nil, refused("%w", err)
	}
	conf.Conn = conn
	conf.ClientID = m.clientID
	c := paho.NewClient(conf)
	cp := &paho.Connect{
		ClientID:   m.clientID,
		KeepAlive:  uint16(m.keepAlive / time.Second),
		CleanStart: true,
	}
	if m.username != "" {
		cp.Username, cp.UsernameFlag = m.username, true
	}
	if m.password != nil {
		cp.Password, cp.PasswordFlag = m.password, true
	}
	ack, err := c.Connect(ctx, cp)
	if err != nil {
		conn.Close()
		if ack != nil && ack.ReasonCode >= 0x80 {
			var text string
			if ack.Properties != nil {
				text = ack.Properties.ReasonString
			}
			return nil, refused("connect refused: %s", reason(ack.ReasonCode, text))
		}
		return nil, refused("connect: %w", err)
	}
	return c, nil
}

// reasonNames are the refusals a person meets most, named as the core's
// stable reason codes are (docs/coverage.md).
var reasonNames = map[byte]string{
	0x86: "bad user name or password", // BadUserNameOrPassword
	0x87: "not authorized",            // NotAuthorized
	0x8c: "bad authentication method", // BadAuthenticationMethod
}

func reason(code byte, text string) string {
	name := fmt.Sprintf("reason 0x%02x", code)
	if n, ok := reasonNames[code]; ok {
		name += " (" + n + ")"
	}
	if text != "" {
		return name + ": " + text
	}
	return name
}

func qosFlag(qos int) error {
	if qos != 0 && qos != 1 {
		return errors.New("--qos must be 0 or 1 (straum refuses QoS 2, D-138)")
	}
	return nil
}

func pubCmd(getenv func(string) string) *cobra.Command {
	var (
		m      mqttFlags
		topic  string
		msg    string
		stdin  bool
		qos    int
		retain bool
	)
	cmd := &cobra.Command{
		Use:   "pub",
		Short: "Publish one MQTT message",
		Long: `Publishes one message over MQTT 5 and disconnects.

QoS 1 succeeds only on a PUBACK, which straum sends once a majority of the
cluster has committed the message. QoS 0 succeeds once the message is
written to the connection: nothing confirms it.

With --retain the message becomes the topic's retained message; an empty
payload (-m '') clears it.

Exit codes: 0 published, 1 usage error, 4 refused or not sent (nothing
was published), 5 sent but the outcome is unknown (no PUBACK in time, or
the connection broke): the message may or may not be committed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := m.check(getenv); err != nil {
				return err
			}
			if topic == "" {
				return errors.New("--topic is required")
			}
			if err := qosFlag(qos); err != nil {
				return err
			}
			if cmd.Flags().Changed("message") == stdin {
				return errors.New("give exactly one of -m and --stdin")
			}
			payload := []byte(msg)
			if stdin {
				b, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxPayload+1))
				if err != nil {
					return err
				}
				if len(b) > maxPayload {
					return fmt.Errorf("payload larger than %d bytes", maxPayload)
				}
				payload = b
			}

			c, err := m.connect(cmd.Context(), paho.ClientConfig{})
			if err != nil {
				return err
			}
			defer c.Disconnect(&paho.Disconnect{ReasonCode: 0})
			ctx, cancel := context.WithTimeout(cmd.Context(), m.timeout)
			defer cancel()
			resp, err := c.Publish(ctx, &paho.Publish{Topic: topic, QoS: byte(qos), Retain: retain, Payload: payload})
			switch {
			case resp != nil && resp.ReasonCode >= 0x80:
				var text string
				if resp.Properties != nil {
					text = resp.Properties.ReasonString
				}
				return refused("publish refused: %s", reason(resp.ReasonCode, text))
			case err != nil:
				return failure{exitUnknown, fmt.Errorf("publish outcome unknown: %w", err)}
			}
			return nil
		},
	}
	m.register(cmd)
	f := cmd.Flags()
	f.StringVarP(&topic, "topic", "t", "", "the topic")
	f.StringVarP(&msg, "message", "m", "", "the payload")
	f.BoolVar(&stdin, "stdin", false, "read the payload from stdin, up to 256 KiB")
	f.IntVarP(&qos, "qos", "q", 1, "0 or 1")
	f.BoolVarP(&retain, "retain", "r", false, "set the topic's retained message")
	return cmd
}

// message is one line of `sub -o json`. Stable: fields are only added.
type message struct {
	Topic   string `json:"topic"`
	QoS     byte   `json:"qos"`
	Retain  bool   `json:"retain"`
	Payload []byte `json:"payload_base64"`
}

func subCmd(getenv func(string) string) *cobra.Command {
	var (
		m                 mqttFlags
		topics            []string
		qos               int
		noLocal           bool
		retainAsPublished bool
		retainHandling    int
		count             int
		wait              time.Duration
		output            string
		verbose           bool
	)
	cmd := &cobra.Command{
		Use:   "sub",
		Short: "Subscribe and print MQTT messages",
		Long: `Subscribes over MQTT 5 and prints each message to stdout: the payload
and a newline, the topic first with -v, or one JSON object per line with
-o json (payload in base64). It stops on Ctrl-C, after --count messages,
or after --wait, then unsubscribes and disconnects. The session is clean
and ends with the connection: messages published while no sub runs are
not kept for it (persistent sessions wait on straum's step 7c).

Exit codes: 0 done, 1 usage error, 2 --wait passed before --count
messages arrived, 4 refused or the connection was lost.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := m.check(getenv); err != nil {
				return err
			}
			if len(topics) == 0 {
				return errors.New("--topic is required")
			}
			if err := qosFlag(qos); err != nil {
				return err
			}
			if retainHandling < 0 || retainHandling > 2 {
				return errors.New("--retain-handling must be 0, 1 or 2")
			}
			if count < 0 || wait < 0 {
				return errors.New("--count and --wait must not be negative")
			}
			if output != "plain" && output != "json" {
				return fmt.Errorf("--output %q: want plain or json", output)
			}

			out := cmd.OutOrStdout()
			var (
				mu       sync.Mutex
				received int
				writeErr error
				enough   = make(chan struct{})
			)
			// Printed before paho acknowledges a QoS 1 message: at least once.
			onMessage := func(p paho.PublishReceived) (bool, error) {
				mu.Lock()
				defer mu.Unlock()
				if writeErr != nil || count > 0 && received >= count {
					return true, nil
				}
				pb := p.Packet
				switch {
				case output == "json":
					writeErr = json.NewEncoder(out).Encode(message{pb.Topic, pb.QoS, pb.Retain, pb.Payload})
				case verbose:
					_, writeErr = fmt.Fprintf(out, "%s %s\n", pb.Topic, pb.Payload)
				default:
					_, writeErr = fmt.Fprintf(out, "%s\n", pb.Payload)
				}
				received++
				if writeErr != nil || count > 0 && received == count {
					close(enough)
				}
				return true, nil
			}
			serverGone := make(chan *paho.Disconnect, 1)
			c, err := m.connect(cmd.Context(), paho.ClientConfig{
				OnPublishReceived:  []func(paho.PublishReceived) (bool, error){onMessage},
				OnServerDisconnect: func(d *paho.Disconnect) { serverGone <- d },
			})
			if err != nil {
				return err
			}
			defer c.Disconnect(&paho.Disconnect{ReasonCode: 0})

			sub := &paho.Subscribe{}
			for _, t := range topics {
				sub.Subscriptions = append(sub.Subscriptions, paho.SubscribeOptions{
					Topic: t, QoS: byte(qos), NoLocal: noLocal,
					RetainAsPublished: retainAsPublished, RetainHandling: byte(retainHandling),
				})
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), m.timeout)
			ack, err := c.Subscribe(ctx, sub)
			cancel()
			if ack != nil {
				for i, code := range ack.Reasons {
					if code >= 0x80 && i < len(topics) {
						return refused("subscribe to %q refused: %s", topics[i], reason(code, ""))
					}
				}
			}
			if err != nil {
				return refused("subscribe: %w", err)
			}

			var deadline <-chan time.Time
			if wait > 0 {
				deadline = time.After(wait)
			}
			var result error
			select {
			case <-cmd.Context().Done():
			case <-enough:
			case <-deadline:
				mu.Lock()
				if count > 0 && received < count {
					fmt.Fprintf(cmd.ErrOrStderr(), "straum-cli: %d of %d messages before --wait passed\n", received, count)
					result = exitCode(exitPartial)
				}
				mu.Unlock()
			case d := <-serverGone:
				return disconnected(d)
			case <-c.Done():
				// paho hands a DISCONNECT to OnServerDisconnect on its own
				// goroutine, so the connection can read as closed first.
				// A fixed grace for the reason; the exit code is the
				// same without it, only the message differs.
				select {
				case d := <-serverGone:
					return disconnected(d)
				case <-time.After(200 * time.Millisecond):
					return refused("connection lost")
				}
			}
			mu.Lock()
			err = writeErr
			mu.Unlock()
			if err != nil {
				return err
			}
			// A fresh context: Ctrl-C must not cancel the clean-up it asks for.
			ctx, cancel = context.WithTimeout(context.WithoutCancel(cmd.Context()), m.timeout)
			defer cancel()
			if _, err := c.Unsubscribe(ctx, &paho.Unsubscribe{Topics: topics}); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "straum-cli: unsubscribe: %v\n", err)
			}
			return result
		},
	}
	m.register(cmd)
	f := cmd.Flags()
	f.StringArrayVarP(&topics, "topic", "t", nil, "a topic filter, + and # allowed (repeatable)")
	f.IntVarP(&qos, "qos", "q", 1, "the maximum QoS to receive: 0 or 1")
	f.BoolVar(&noLocal, "no-local", false, "MQTT 5: skip messages this connection publishes")
	f.BoolVar(&retainAsPublished, "retain-as-published", false, "MQTT 5: keep the retain flag as the publisher set it")
	f.IntVar(&retainHandling, "retain-handling", 0, "MQTT 5: 0 send retained messages, 1 only for a new subscription, 2 never")
	f.IntVarP(&count, "count", "c", 0, "stop after this many messages; 0: no limit")
	f.DurationVar(&wait, "wait", 0, "stop after this long; 0: no limit")
	f.StringVarP(&output, "output", "o", "plain", "plain or json")
	f.BoolVarP(&verbose, "verbose", "v", false, "plain output: print the topic before the payload")
	return cmd
}

func disconnected(d *paho.Disconnect) error {
	var text string
	if d.Properties != nil {
		text = d.Properties.ReasonString
	}
	return refused("the node disconnected: %s", reason(d.ReasonCode, text))
}
