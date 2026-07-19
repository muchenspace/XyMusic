package apperror_test

import (
	"errors"
	"fmt"
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestApplicationErrorSupportsErrorChains(t *testing.T) {
	cause := errors.New("database unavailable")
	err := apperror.New(
		apperror.CodeDependencyUnavailable,
		"服务依赖暂不可用",
		apperror.WithCause(cause),
	)
	wrapper := fmt.Errorf("readiness failed: %w", err)

	if !errors.Is(wrapper, cause) {
		t.Fatal("expected diagnostic cause to remain in the error chain")
	}
	if !apperror.IsCode(wrapper, apperror.CodeDependencyUnavailable) {
		t.Fatal("expected application error code to be discoverable")
	}
	resolved, ok := apperror.As(wrapper)
	if !ok || resolved.Detail != "服务依赖暂不可用" {
		t.Fatalf("unexpected resolved error: %#v", resolved)
	}
}

func TestValidationCopiesFieldErrors(t *testing.T) {
	input := map[string][]string{"username": {"用户名不能为空"}}
	err := apperror.Validation("请求参数有误", input)
	input["username"][0] = "mutated"
	input["password"] = []string{"mutated"}

	fieldErrors, ok := err.Metadata["fieldErrors"].(map[string][]string)
	if !ok {
		t.Fatalf("unexpected fieldErrors metadata: %#v", err.Metadata["fieldErrors"])
	}
	if got := fieldErrors["username"][0]; got != "用户名不能为空" {
		t.Fatalf("field errors were not copied: %q", got)
	}
	if _, exists := fieldErrors["password"]; exists {
		t.Fatal("metadata changed after construction")
	}
}

func TestRateLimitedCarriesRetryMetadata(t *testing.T) {
	err := apperror.RateLimited(15)
	if err.Code != apperror.CodeRateLimited {
		t.Fatalf("unexpected code: %s", err.Code)
	}
	if got := err.Metadata["retryAfterSeconds"]; got != 15 {
		t.Fatalf("unexpected retry metadata: %#v", got)
	}
}
