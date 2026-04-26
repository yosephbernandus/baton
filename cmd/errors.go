package cmd

import "fmt"

type ExitErr struct {
	Code    int
	Message string
}

func (e *ExitErr) Error() string {
	return e.Message
}

func exitError(code int, format string, args ...interface{}) *ExitErr {
	msg := fmt.Sprintf(format, args...)
	return &ExitErr{Code: code, Message: msg}
}
