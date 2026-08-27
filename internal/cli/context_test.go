package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmWritesPromptToErrorOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	context := Context{
		Input:       strings.NewReader("yes\n"),
		Output:      &stdout,
		ErrorOutput: &stderr,
		JSON:        true,
	}

	if err := context.Confirm(Change{Action: "move", Resource: "pipeline test"}); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "move pipeline test?") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
