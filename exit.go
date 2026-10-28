package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/straum-io/straum-go-sdk/client"
	"github.com/straum-io/straum-go-sdk/straumerr"
)

// Exit codes, a public contract documented in README.md. Before 1.0 a
// minor version may change them, noted in CHANGELOG.md.
const (
	exitOK          = 0
	exitUsage       = 1 // usage, configuration or local state; nothing was sent
	exitLimit       = 2 // stopped by a limit before the requested result
	exitConnect     = 3 // could not connect, or a read-only request failed on the connection
	exitRefused     = 4 // the broker refused; the request had no effect
	exitUnknown     = 5 // Produce or CreateTopic sent, outcome unknown
	exitStorage     = 6 // the broker's storage failed
	exitExhausted   = 7 // retries exhausted; the request had no effect
	exitInterrupted = 130
)

// exitError ends the command with code; err, if not nil, goes to stderr.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

func usageErr(format string, a ...any) error {
	return &exitError{exitUsage, fmt.Errorf(format, a...)}
}

func localErr(err error) error {
	if err == nil {
		return nil
	}
	return &exitError{exitUsage, err}
}

func limitErr(format string, a ...any) error {
	return &exitError{exitLimit, fmt.Errorf(format, a...)}
}

// connectErr is a failure of client.New: nothing was sent.
func connectErr(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	return &exitError{exitConnect, fmt.Errorf("connect: %w", err)}
}

// requestErr maps an SDK error to an exit code. mutating marks Produce and
// CreateTopic: the SDK does not tell "not sent" from "sent, no answer"
// after a lost connection, so any connection failure, deadline or
// protocol violation is "outcome unknown" (exit 5) for them, and exit 3
// for requests that change nothing.
func requestErr(err error, mutating bool) error {
	if err == nil {
		return nil
	}
	var se *straumerr.Error
	switch {
	case errors.As(err, &se):
		switch {
		case se.Code == straumerr.StorageError:
			return &exitError{exitStorage, err}
		case se.Code == straumerr.UnsupportedVersion || se.Code == straumerr.Unauthenticated:
			return &exitError{exitConnect, err}
		case se.Code.Retry() != straumerr.NoRetry:
			return &exitError{exitExhausted, fmt.Errorf("%w (retries exhausted)", err)}
		default:
			return &exitError{exitRefused, err}
		}
	case errors.Is(err, context.Canceled):
		return err // interrupted: run() reports it
	case errors.Is(err, client.ErrConnection), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, client.ErrProtocol), errors.Is(err, client.ErrClosed):
		if mutating {
			return &exitError{exitUnknown, fmt.Errorf("%w; the outcome is unknown: the request may have taken effect", err)}
		}
		return &exitError{exitConnect, err}
	default:
		return &exitError{exitConnect, err}
	}
}
