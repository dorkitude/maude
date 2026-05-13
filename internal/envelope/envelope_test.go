package envelope

import (
	"strings"
	"testing"
)

func TestBuildPrintRequest(t *testing.T) {
	t.Parallel()

	got, err := BuildPrintRequest(PrintRequest{
		RequestID:   "abc",
		Message:     " hello ",
		RespondWith: "maude agent print --request abc",
	})
	if err != nil {
		t.Fatalf("BuildPrintRequest() error = %v", err)
	}
	for _, want := range []string{`"kind": "maude_print_request"`, `"request_id": "abc"`, `"message": "hello"`, `"respond_with": "maude agent print --request abc"`, `"output_format": "text"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("envelope missing %q:\n%s", want, got)
		}
	}
}

func TestBuildPrintRequestIncludesOutputFormatInstruction(t *testing.T) {
	t.Parallel()

	got, err := BuildPrintRequest(PrintRequest{
		RequestID:    "abc",
		Message:      "hello",
		RespondWith:  "maude agent print --request abc",
		OutputFormat: "stream-json",
	})
	if err != nil {
		t.Fatalf("BuildPrintRequest() error = %v", err)
	}
	for _, want := range []string{`"output_format": "stream-json"`, "Do not write newline-delimited JSON yourself", "convert bytes received by `maude agent print`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("envelope missing %q:\n%s", want, got)
		}
	}
}
