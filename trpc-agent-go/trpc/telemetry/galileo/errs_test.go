package galileo

import (
	"errors"
	"testing"

	trpcerrs "git.code.oa.com/trpc-go/trpc-go/errs"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type structuredError struct {
	msg     string
	errType string
	errCode string
	cause   error
}

func (e structuredError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return "structured error"
}

func (e structuredError) ErrorType() string {
	return e.errType
}

func (e structuredError) ErrorCode() string {
	return e.errCode
}

func (e structuredError) Unwrap() error {
	return e.cause
}

func strPtr(s string) *string {
	return &s
}

func TestToGalileoResponseError_PreservesStructuredError(t *testing.T) {
	err := structuredError{
		msg:     "boom",
		errType: "preprocess_failure",
		errCode: "70002",
	}

	got := toGalileoResponseError(err)
	if got == nil {
		t.Fatal("toGalileoResponseError() returned nil")
	}
	if got.Type != "preprocess_failure" {
		t.Fatalf("Type = %q, want %q", got.Type, "preprocess_failure")
	}
	if got.Code == nil || *got.Code != "70002" {
		t.Fatalf("Code = %v, want %q", got.Code, "70002")
	}
	if got.Message != "boom" {
		t.Fatalf("Message = %q, want %q", got.Message, "boom")
	}
}

func TestToGalileoResponseError_PreservesModelResponseError(t *testing.T) {
	err := &model.ResponseError{
		Type:    "biz_error",
		Code:    strPtr("E42"),
		Message: "bad",
	}

	got := toGalileoResponseError(err)
	if got == nil {
		t.Fatal("toGalileoResponseError() returned nil")
	}
	if got.Type != "biz_error" {
		t.Fatalf("Type = %q, want %q", got.Type, "biz_error")
	}
	if got.Code == nil || *got.Code != "E42" {
		t.Fatalf("Code = %v, want %q", got.Code, "E42")
	}
	if got.Message != "bad" {
		t.Fatalf("Message = %q, want %q", got.Message, "bad")
	}
}

func TestToGalileoResponseError_MessageOnlyResponseErrorDoesNotGainLabels(t *testing.T) {
	err := &model.ResponseError{Message: "message only"}

	got := toGalileoResponseError(err)
	if got == nil {
		t.Fatal("toGalileoResponseError() returned nil")
	}
	if got.Type != "" {
		t.Fatalf("Type = %q, want empty", got.Type)
	}
	if got.Code != nil {
		t.Fatalf("Code = %v, want nil", got.Code)
	}
	if got.Message != "message only" {
		t.Fatalf("Message = %q, want %q", got.Message, "message only")
	}
}

func TestToGalileoResponseError_MergesFallbackCodeForTypedError(t *testing.T) {
	err := structuredError{
		msg:     "typed wrapper",
		errType: "preprocess_failure",
		cause:   trpcerrs.New(70001, "biz failed"),
	}

	got := toGalileoResponseError(err)
	if got == nil {
		t.Fatal("toGalileoResponseError() returned nil")
	}
	if got.Type != "preprocess_failure" {
		t.Fatalf("Type = %q, want %q", got.Type, "preprocess_failure")
	}
	if got.Code == nil || *got.Code != "70001" {
		t.Fatalf("Code = %v, want %q", got.Code, "70001")
	}
	if got.Message != "typed wrapper" {
		t.Fatalf("Message = %q, want %q", got.Message, "typed wrapper")
	}
}

func TestToGalileoResponseError_PrefersOuterStructuredFieldsOverTRPCFallback(t *testing.T) {
	err := structuredError{
		msg:     "outer business error",
		errType: "preprocess_failure",
		errCode: "70002",
		cause:   trpcerrs.New(70001, "biz failed"),
	}

	got := toGalileoResponseError(err)
	if got == nil {
		t.Fatal("toGalileoResponseError() returned nil")
	}
	if got.Type != "preprocess_failure" {
		t.Fatalf("Type = %q, want %q", got.Type, "preprocess_failure")
	}
	if got.Code == nil || *got.Code != "70002" {
		t.Fatalf("Code = %v, want %q", got.Code, "70002")
	}
}

func TestToGalileoResponseError_PlainError(t *testing.T) {
	got := toGalileoResponseError(errors.New("plain"))
	if got == nil {
		t.Fatal("toGalileoResponseError() returned nil")
	}
	if got.Type != "" {
		t.Fatalf("Type = %q, want empty", got.Type)
	}
	if got.Code != nil {
		t.Fatalf("Code = %v, want nil", got.Code)
	}
	if got.Message != "plain" {
		t.Fatalf("Message = %q, want %q", got.Message, "plain")
	}
}

func TestFallbackGalileoResponseError_BusinessTRPCError(t *testing.T) {
	got := fallbackGalileoResponseError(trpcerrs.New(70001, "biz"))
	if got == nil {
		t.Fatal("fallbackGalileoResponseError() returned nil")
	}
	if got.Type != responseErrorTypeTRPCBusiness {
		t.Fatalf("Type = %q, want %q", got.Type, responseErrorTypeTRPCBusiness)
	}
	if got.Code == nil || *got.Code != "70001" {
		t.Fatalf("Code = %v, want %q", got.Code, "70001")
	}
	if got.Message != "biz" {
		t.Fatalf("Message = %q, want %q", got.Message, "biz")
	}
}

func TestFallbackGalileoResponseError_FrameworkTRPCError(t *testing.T) {
	got := fallbackGalileoResponseError(trpcerrs.NewFrameError(31, "fw"))
	if got == nil {
		t.Fatal("fallbackGalileoResponseError() returned nil")
	}
	if got.Type != responseErrorTypeTRPCFramework {
		t.Fatalf("Type = %q, want %q", got.Type, responseErrorTypeTRPCFramework)
	}
	if got.Code == nil || *got.Code != "31" {
		t.Fatalf("Code = %v, want %q", got.Code, "31")
	}
	if got.Message != "fw" {
		t.Fatalf("Message = %q, want %q", got.Message, "fw")
	}
}

func TestFallbackGalileoResponseError_CalleeFrameworkTRPCError(t *testing.T) {
	got := fallbackGalileoResponseError(trpcerrs.NewCalleeFrameError(111, "callee"))
	if got == nil {
		t.Fatal("fallbackGalileoResponseError() returned nil")
	}
	if got.Type != responseErrorTypeTRPCCalleeFramework {
		t.Fatalf("Type = %q, want %q", got.Type, responseErrorTypeTRPCCalleeFramework)
	}
	if got.Code == nil || *got.Code != "111" {
		t.Fatalf("Code = %v, want %q", got.Code, "111")
	}
	if got.Message != "callee" {
		t.Fatalf("Message = %q, want %q", got.Message, "callee")
	}
}
