package envelope

import (
	"encoding/json"
	"fmt"
	"strings"
)

type PrintRequest struct {
	RequestID    string `json:"request_id"`
	Message      string `json:"message"`
	RespondWith  string `json:"respond_with"`
	OutputFormat string `json:"output_format"`
}

func BuildPrintRequest(req PrintRequest) (string, error) {
	format := strings.TrimSpace(req.OutputFormat)
	if format == "" {
		format = "text"
	}
	payload := map[string]string{
		"kind":          "maude_print_request",
		"request_id":    req.RequestID,
		"message":       strings.TrimSpace(req.Message),
		"respond_with":  req.RespondWith,
		"output_format": format,
		"instruction":   printInstruction(format, req.RespondWith),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func printInstruction(format string, respondWith string) string {
	switch format {
	case "json":
		return fmt.Sprintf("Answer the message. For the final print-mode response only, pipe only the final answer text to `%s`. Do not wrap it in JSON; Maude will emit the `--output-format=json` result object. Do not rely on visible tmux output as the response path.", respondWith)
	case "stream-json":
		return fmt.Sprintf("Answer the message. For the final print-mode response only, stream only the final answer text to `%s`. Do not write newline-delimited JSON yourself; Maude will convert bytes received by `maude agent print` into the `--output-format=stream-json` event stream. Do not rely on visible tmux output as the response path.", respondWith)
	default:
		return fmt.Sprintf("Answer the message. For the final print-mode response only, pipe raw markdown/stdout to `%s`. Do not rely on visible tmux output as the response path.", respondWith)
	}
}
