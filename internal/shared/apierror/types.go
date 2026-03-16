package apierror

import "fmt"

type CustomError struct {
	Message  string `json:"message"`
	Code     string `json:"code"`
	HTTPCode int    `json:"httpCode"`
	Details  any    `json:"details,omitempty"`
}

func (err *CustomError) Error() string {
	if err.Code != "" {
		return fmt.Sprintf("[%s] %s", err.Code, err.Message)
	}
	return err.Message
}

func (err *CustomError) Is(target error) bool {
	if t, ok := target.(*CustomError); ok {
		return err.Code == t.Code &&
			err.Message == t.Message &&
			err.HTTPCode == t.HTTPCode
	}
	return false
}

func NewCustomError(httpCode int, code, message string, details ...any) *CustomError {
	e := &CustomError{
		HTTPCode: httpCode,
		Code:     code,
		Message:  message,
	}
	if len(details) > 0 {
		e.Details = details[0]
	}
	return e
}

func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if ce, ok := err.(*CustomError); ok {
		return ce.HTTPCode == 404
	}
	return false
}
