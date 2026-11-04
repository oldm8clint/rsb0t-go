package sessionipc

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDispatcherRunsRegisteredHandler(t *testing.T) {
	d := NewDispatcher()
	var gotAccount, gotArgs string
	d.Register("Ping", func(account string, args json.RawMessage) error {
		gotAccount = account
		gotArgs = string(args)
		return nil
	})

	res := d.Dispatch(ActionCall{Account: "bob", Name: "Ping", Args: json.RawMessage(`{"n":1}`)})
	if !res.OK() {
		t.Fatalf("expected success, got err %q", res.Err)
	}
	if gotAccount != "bob" || gotArgs != `{"n":1}` {
		t.Fatalf("handler saw account=%q args=%q", gotAccount, gotArgs)
	}
}

func TestDispatcherPropagatesHandlerError(t *testing.T) {
	d := NewDispatcher()
	d.Register("Fail", func(string, json.RawMessage) error { return errors.New("boom") })

	res := d.Dispatch(ActionCall{Name: "Fail"})
	if res.OK() || res.Err != "boom" {
		t.Fatalf("expected err %q, got %+v", "boom", res)
	}
}

func TestDispatcherUnknownAction(t *testing.T) {
	d := NewDispatcher()
	res := d.Dispatch(ActionCall{Name: "Nope"})
	if res.OK() {
		t.Fatalf("expected an error for an unregistered action")
	}
}

// TestDispatcherRegisterReplaces confirms Register can swap a handler out, deliberately
// unlike internal/bot.Register's panic-on-duplicate — see action.go's doc comment.
func TestDispatcherRegisterReplaces(t *testing.T) {
	d := NewDispatcher()
	d.Register("X", func(string, json.RawMessage) error { return errors.New("old") })
	d.Register("X", func(string, json.RawMessage) error { return nil })

	if res := d.Dispatch(ActionCall{Name: "X"}); !res.OK() {
		t.Fatalf("expected the replaced handler to run, got err %q", res.Err)
	}
}
