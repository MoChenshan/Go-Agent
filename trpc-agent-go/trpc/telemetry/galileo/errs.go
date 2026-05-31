package galileo

import (
	"errors"
	"strconv"

	trpcerrs "git.code.oa.com/trpc-go/trpc-go/errs"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	responseErrorTypeTRPCFramework       = "trpc_framework"
	responseErrorTypeTRPCBusiness        = "trpc_business"
	responseErrorTypeTRPCCalleeFramework = "trpc_callee_framework"
)

func toGalileoResponseError(err error) *model.ResponseError {
	if err == nil {
		return nil
	}

	respErr := model.ResponseErrorFromError(err, "")
	if respErr == nil {
		return fallbackGalileoResponseError(err)
	}

	hasType := respErr.Type != ""
	hasCode := hasResponseErrorCode(respErr)
	if !hasType && !hasCode {
		return fallbackGalileoResponseError(err)
	}

	fallback := fallbackGalileoResponseError(err)
	if fallback == nil || hasCode || !hasResponseErrorCode(fallback) {
		return respErr
	}

	clone := *respErr
	code := *fallback.Code
	clone.Code = &code
	return &clone
}

func fallbackGalileoResponseError(err error) *model.ResponseError {
	if err == nil {
		return nil
	}

	var trpcErr *trpcerrs.Error
	if errors.As(err, &trpcErr) && trpcErr != nil {
		code := strconv.Itoa(int(trpcErr.Code))
		respErr := &model.ResponseError{
			Message: trpcerrs.Msg(err),
			Code:    &code,
		}
		switch trpcErr.Type {
		case trpcerrs.ErrorTypeFramework:
			respErr.Type = responseErrorTypeTRPCFramework
		case trpcerrs.ErrorTypeBusiness:
			respErr.Type = responseErrorTypeTRPCBusiness
		case trpcerrs.ErrorTypeCalleeFramework:
			respErr.Type = responseErrorTypeTRPCCalleeFramework
		}
		return respErr
	}

	return model.ResponseErrorFromError(err, "")
}

func hasResponseErrorCode(respErr *model.ResponseError) bool {
	return respErr != nil && respErr.Code != nil && *respErr.Code != ""
}
