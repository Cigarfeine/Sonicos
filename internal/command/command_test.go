package command

import (
	"context"
	"reflect"
	"testing"
)

func TestDispatchParsesCommandAndArguments(t *testing.T) {
	registry := NewRegistry()
	var gotName string
	var gotArgs []string
	err := registry.Register(Command{
		Name:    "greet",
		Aliases: []string{"hello"},
		Handler: func(_ context.Context, _ Message, args []string, _ ReplyFunc) error {
			gotName = "greet"
			gotArgs = args
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	dispatched, err := registry.Dispatch(context.Background(), "!", Message{Text: "!HELLO Ada Lovelace"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !dispatched {
		t.Fatal("expected command to be dispatched")
	}
	if gotName != "greet" || !reflect.DeepEqual(gotArgs, []string{"Ada", "Lovelace"}) {
		t.Fatalf("unexpected dispatch: name=%q args=%v", gotName, gotArgs)
	}
}

func TestDispatchIgnoresTextWithoutRegisteredCommand(t *testing.T) {
	registry := NewRegistry()
	dispatched, err := registry.Dispatch(context.Background(), "!", Message{Text: "!missing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dispatched {
		t.Fatal("unexpected command dispatch")
	}
}

func TestRegisterRejectsDuplicateName(t *testing.T) {
	registry := NewRegistry()
	cmd := Command{Name: "status", Handler: func(context.Context, Message, []string, ReplyFunc) error { return nil }}
	if err := registry.Register(cmd); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(cmd); err == nil {
		t.Fatal("expected duplicate command registration to fail")
	}
}
