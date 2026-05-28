package stack

import (
	"errors"
	"strings"
)

type blockedRunError struct {
	class   string
	message string
	cause   error
}

func newBlockedRunError(class string, message string, cause error) error {
	class = strings.TrimSpace(class)
	if class == "" {
		class = "BLOCKED"
	}
	message = strings.TrimSpace(message)
	if message == "" && cause != nil {
		message = cause.Error()
	}
	if message == "" {
		message = "operation blocked"
	}
	return &blockedRunError{class: class, message: message, cause: cause}
}

func (e *blockedRunError) Error() string {
	return e.message
}

func (e *blockedRunError) Unwrap() error {
	return e.cause
}

func (e *blockedRunError) Class() string {
	if e == nil || strings.TrimSpace(e.class) == "" {
		return "BLOCKED"
	}
	return strings.TrimSpace(e.class)
}

func (e *blockedRunError) Message() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.message)
}

func isBlockedRunError(err error) bool {
	var blocked *blockedRunError
	return errors.As(err, &blocked)
}

func blockedRunErrorDetails(err error) (string, string) {
	var blocked *blockedRunError
	if errors.As(err, &blocked) {
		return blocked.Class(), blocked.Message()
	}
	return "BLOCKED", strings.TrimSpace(err.Error())
}
