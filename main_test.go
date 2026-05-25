package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testExecutor(t *testing.T) *executor {
	t.Helper()
	return newExecutor(t.TempDir(), limits{
		DefaultTimeout:     time.Second,
		MaxTimeout:         5 * time.Second,
		DefaultOutputLimit: 1024,
		MaxOutputLimit:     4096,
		MaxStdin:           4096,
	}, 1)
}

func TestExecutorSuccess(t *testing.T) {
	e := testExecutor(t)
	res, err := e.execute(context.Background(), "sh", []string{"-c", "printf hello"}, "", 0, 0)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("exit code = %v", res.ExitCode)
	}
	if res.Stdout != "hello" || res.Stderr != "" || res.TimedOut {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestExecutorNonZeroExitIsResult(t *testing.T) {
	e := testExecutor(t)
	res, err := e.execute(context.Background(), "sh", []string{"-c", "printf nope >&2; exit 7"}, "", 0, 0)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.ExitCode == nil || *res.ExitCode != 7 {
		t.Fatalf("exit code = %v", res.ExitCode)
	}
	if res.Stderr != "nope" {
		t.Fatalf("stderr = %q", res.Stderr)
	}
}

func TestExecutorTimeout(t *testing.T) {
	e := testExecutor(t)
	res, err := e.execute(context.Background(), "sh", []string{"-c", "sleep 5"}, "", 50, 0)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("expected timeout: %+v", res)
	}
	if res.ExitCode != nil {
		t.Fatalf("timeout exit code = %v", *res.ExitCode)
	}
}

func TestExecutorOutputTruncation(t *testing.T) {
	e := testExecutor(t)
	res, err := e.execute(context.Background(), "sh", []string{"-c", "printf abcdef"}, "", 0, 3)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Stdout != "abc" || !res.StdoutTruncated {
		t.Fatalf("unexpected truncation: %+v", res)
	}
}

func TestAuthMiddlewareRequiresToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := authMiddleware("token", "secret", next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d", res.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret")
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("valid token status = %d body=%q", res.Code, strings.TrimSpace(res.Body.String()))
	}
}

func TestAuthMiddlewareCanBeDisabled(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := authMiddleware("none", "", next)

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d", res.Code)
	}
}
