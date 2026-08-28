package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestConfirmWritesPromptToErrorOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := Context{
		Context:     context.Background(),
		Input:       strings.NewReader("yes\n"),
		Output:      &stdout,
		ErrorOutput: &stderr,
		JSON:        true,
	}

	if err := app.Confirm(Change{Action: "move", Resource: "pipeline test"}); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "move pipeline test?") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestConfirmStopsWhenContextExpires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()

	app := Context{
		Context:     ctx,
		Input:       reader,
		ErrorOutput: &bytes.Buffer{},
	}
	done := make(chan error, 1)
	go func() {
		done <- app.Confirm(Change{Action: "move", Resource: "pipeline test"})
	}()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("confirmation did not stop after context cancellation")
	}
}
