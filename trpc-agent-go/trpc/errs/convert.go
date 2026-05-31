// Package errs provides helpers to convert between the agent ResponseError
// and trpc-go errs.Error.
package errs

import (
	"strconv"

	"git.code.oa.com/trpc-go/trpc-go/errs"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	// typeTRPC is the ResponseError.Type value used for tRPC errors.
	typeTRPC = "TRPC"
)

// FromResponseError converts a model.ResponseError to a tRPC error.
//
// If the ResponseError.Code cannot be parsed to an integer, the
// returned error uses errs.RetUnknown as its code.
func FromResponseError(re *model.ResponseError) error {
	if re == nil {
		return nil
	}

	code := errs.RetUnknown
	if re.Code != nil {
		if v, err := strconv.Atoi(*re.Code); err == nil {
			code = v
		}
	}

	// Use ResponseError.Message as error message.
	// Default to empty string when not provided.
	return errs.New(code, re.Message)
}

// ToResponseError converts a tRPC error to a model.ResponseError.
//
// The resulting ResponseError carries:
// - Type: a constant "TRPC" marker
// - Message: extracted with errs.Msg
// - Code: extracted with errs.Code and encoded as decimal string
func ToResponseError(err error) *model.ResponseError {
	if err == nil {
		return nil
	}

	code := errs.Code(err)
	s := strconv.Itoa(code)
	return &model.ResponseError{
		Type:    typeTRPC,
		Message: errs.Msg(err),
		Code:    &s,
	}
}

// CodeFromResponseError returns the integer code parsed from
// model.ResponseError. The second return value indicates whether the
// code field existed and was a valid integer.
func CodeFromResponseError(re *model.ResponseError) (int, bool) {
	if re == nil || re.Code == nil {
		return 0, false
	}
	v, err := strconv.Atoi(*re.Code)
	if err != nil {
		return 0, false
	}
	return v, true
}
