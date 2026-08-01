package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/mmdash/mmdash/clients/cli/internal/apperror"
)

type Printer struct {
	JSON   bool
	Stderr io.Writer
	Stdout io.Writer
}

func (printer Printer) Result(value interface{}) error {
	if printer.JSON {
		return json.NewEncoder(printer.Stdout).Encode(value)
	}
	switch typed := value.(type) {
	case string:
		_, err := fmt.Fprintln(printer.Stdout, typed)
		return err
	case fmt.Stringer:
		_, err := fmt.Fprintln(printer.Stdout, typed.String())
		return err
	default:
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(printer.Stdout, string(encoded))
		return err
	}
}

func (printer Printer) Error(err error) int {
	typed := apperror.Normalize(err)
	if printer.JSON {
		_ = json.NewEncoder(printer.Stderr).Encode(typed)
	} else {
		_, _ = fmt.Fprintf(printer.Stderr, "%s: %s\n", typed.Code, typed.Message)
		if typed.RequestID != "" {
			_, _ = fmt.Fprintf(printer.Stderr, "request_id: %s\n", typed.RequestID)
		}
	}
	return typed.ExitCode
}
