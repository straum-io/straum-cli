package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/straum-io/straum-go-sdk/client"
	"github.com/straum-io/straum-go-sdk/straumerr"
)

func exitOf(err error) int {
	var x *exitError
	if errors.As(err, &x) {
		return x.code
	}
	return -1
}

// Every protocol code, for a request that changes nothing and for Produce
// or CreateTopic: a broker error means the request had no effect, so the
// two agree; connection failures differ.
func TestRequestErrMapping(t *testing.T) {
	want := map[straumerr.Code]int{
		straumerr.UnknownTopic:        exitRefused,
		straumerr.UnknownPartition:    exitRefused,
		straumerr.TopicExists:         exitRefused,
		straumerr.MessageTooLarge:     exitRefused,
		straumerr.FrameTooLarge:       exitRefused,
		straumerr.Overloaded:          exitExhausted,
		straumerr.UnsupportedAckLevel: exitRefused,
		straumerr.UnsupportedVersion:  exitConnect,
		straumerr.Unsupported:         exitRefused,
		straumerr.Unauthenticated:     exitConnect,
		straumerr.Forbidden:           exitRefused,
		straumerr.MalformedRequest:    exitRefused,
		straumerr.NotLeader:           exitExhausted,
		straumerr.FencedLeaderEpoch:   exitExhausted,
		straumerr.StorageError:        exitStorage,
		straumerr.Code(999):           exitRefused, // unknown codes are not retryable
	}
	for code := straumerr.Code(1); code <= 15; code++ {
		if _, ok := want[code]; !ok {
			t.Fatalf("code %v has no expected exit", code)
		}
	}
	for code, exit := range want {
		err := fmt.Errorf("produce: %w", &straumerr.Error{Code: code})
		for _, mutating := range []bool{false, true} {
			if got := exitOf(requestErr(err, mutating)); got != exit {
				t.Errorf("%v (mutating %v): exit %d, want %d", code, mutating, got, exit)
			}
		}
	}
	unknownOutcome := []error{
		fmt.Errorf("x: %w", client.ErrConnection),
		context.DeadlineExceeded,
		fmt.Errorf("x: %w", client.ErrProtocol),
	}
	for _, err := range unknownOutcome {
		if got := exitOf(requestErr(err, true)); got != exitUnknown {
			t.Errorf("%v mutating: exit %d, want 5", err, got)
		}
		if got := exitOf(requestErr(err, false)); got != exitConnect {
			t.Errorf("%v read-only: exit %d, want 3", err, got)
		}
	}
	if err := requestErr(context.Canceled, false); !errors.Is(err, context.Canceled) || exitOf(err) != -1 {
		t.Errorf("an interrupt is left to run(): %v", err)
	}
	if got := exitOf(requestErr(context.Canceled, true)); got != exitUnknown {
		t.Errorf("an interrupt during Produce or CreateTopic: exit %d, want 5", got)
	}
	if exitOf(connectErr(errors.New("dial"))) != exitConnect {
		t.Error("connect failure is exit 3")
	}
	if requestErr(nil, true) != nil || localErr(nil) != nil {
		t.Error("nil stays nil")
	}
}
