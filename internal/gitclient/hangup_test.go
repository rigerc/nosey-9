package gitclient

import (
	"context"
	"errors"
	"testing"
)

func TestIsRequestHangup(t *testing.T) {
	hangups := []string{
		"sending upload-req message: encoding depth 1: write |1: broken pipe",
		"sending done message: write |1: broken pipe",
		"sending haves message: EOF",
		"closing input: EOF",
	}
	for _, message := range hangups {
		if !isRequestHangup(errors.New(message)) {
			t.Fatal(message)
		}
	}
	for _, err := range []error{nil, errors.New("connection refused"), errors.New("upload-pack: not our ref abc"), errors.New("sending upload-req message: permission denied")} {
		if isRequestHangup(err) {
			t.Fatal(err)
		}
	}
}

func TestRetryOnHangupIsBounded(t *testing.T) {
	hangup := errors.New("sending done message: write |1: broken pipe")
	calls := 0
	err := retryOnHangup(context.Background(), func() error { calls++; return hangup })
	if !errors.Is(err, hangup) || calls != hangupAttempts {
		t.Fatal(err, calls)
	}
}

func TestRetryOnHangupReturnsTheServersAnswer(t *testing.T) {
	calls := 0
	want := errors.New("upload-pack: not our ref abc")
	err := retryOnHangup(context.Background(), func() error {
		calls++
		if calls == 1 {
			return errors.New("sending upload-req message: write |1: broken pipe")
		}
		return want
	})
	if !errors.Is(err, want) || calls != 2 {
		t.Fatal(err, calls)
	}
}

func TestRetryOnHangupStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	hangup := errors.New("closing input: broken pipe")
	if err := retryOnHangup(ctx, func() error { calls++; return hangup }); !errors.Is(err, hangup) || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestRetryOnHangupLeavesOtherFailuresAlone(t *testing.T) {
	calls := 0
	failure := errors.New("connection refused")
	if err := retryOnHangup(context.Background(), func() error { calls++; return failure }); !errors.Is(err, failure) || calls != 1 {
		t.Fatal(err, calls)
	}
}
