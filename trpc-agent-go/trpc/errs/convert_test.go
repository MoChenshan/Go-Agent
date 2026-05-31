package errs

import (
	"errors"
	"testing"

	trpcerrs "git.code.oa.com/trpc-go/trpc-go/errs"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func strPtr(s string) *string { return &s }

func TestFromResponseError_NumericCode(t *testing.T) {
	re := &model.ResponseError{
		Type:    "business",
		Message: "bad request",
		Code:    strPtr("400"),
	}

	err := FromResponseError(re)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if c := trpcerrs.Code(err); c != 400 {
		t.Fatalf("code = %d, want 400", c)
	}
	if m := trpcerrs.Msg(err); m != "bad request" {
		t.Fatalf("msg = %q, want %q", m, "bad request")
	}
}

func TestFromResponseError_InvalidCode_Unknown(t *testing.T) {
	re := &model.ResponseError{
		Type:    "business",
		Message: "oops",
		Code:    strPtr("NOT_NUMERIC"),
	}

	err := FromResponseError(re)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if c := trpcerrs.Code(err); c != trpcerrs.RetUnknown {
		t.Fatalf("code = %d, want %d", c, trpcerrs.RetUnknown)
	}
}

func TestToResponseError(t *testing.T) {
	base := trpcerrs.New(404, "not found")
	// Wrap once to ensure we still extract correctly.
	err := trpcerrs.Wrap(base, 404, "resource missing")

	re := ToResponseError(err)
	if re == nil {
		t.Fatalf("expected ResponseError, got nil")
	}
	if re.Type != "TRPC" {
		t.Fatalf("type = %q, want %q", re.Type, "TRPC")
	}
	if re.Message == "" {
		t.Fatalf("message should not be empty")
	}
	if re.Code == nil || *re.Code != "404" {
		t.Fatalf("code = %v, want %q", re.Code, "404")
	}
}

func TestCodeFromResponseError(t *testing.T) {
	if _, ok := CodeFromResponseError(nil); ok {
		t.Fatalf("nil input should return ok=false")
	}
	if _, ok := CodeFromResponseError(&model.ResponseError{}); ok {
		t.Fatalf("missing code should return ok=false")
	}
	reBad := &model.ResponseError{Code: strPtr("x")}
	if _, ok := CodeFromResponseError(reBad); ok {
		t.Fatalf("non-numeric code should return ok=false")
	}
	re := &model.ResponseError{Code: strPtr("200")}
	if v, ok := CodeFromResponseError(re); !ok || v != 200 {
		t.Fatalf("(v,ok)=(%d,%v), want (200,true)", v, ok)
	}
}

func TestNilConversions(t *testing.T) {
	if FromResponseError(nil) != nil {
		t.Fatalf("FromResponseError(nil) should be nil")
	}
	if ToResponseError(nil) != nil {
		t.Fatalf("ToResponseError(nil) should be nil")
	}

	// Confirm trpcerrs.Msg on non-errs error falls back to the string.
	e := errors.New("plain")
	re := ToResponseError(e)
	if re == nil || re.Message == "" {
		t.Fatalf("ToResponseError should still produce message")
	}
}
