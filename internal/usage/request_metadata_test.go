package usage

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
)

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return false }

func TestStatusFromErrorMapsContextDeadlineExceededToRequestTimeout(t *testing.T) {
	t.Parallel()

	if status := statusFromError(context.DeadlineExceeded); status != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", status, http.StatusRequestTimeout)
	}
}

func TestStatusFromErrorMapsNetTimeoutToRequestTimeout(t *testing.T) {
	t.Parallel()

	var err net.Error = timeoutNetError{}
	if status := statusFromError(err); status != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", status, http.StatusRequestTimeout)
	}
}

func TestStatusFromErrorPrefersExplicitStatusCode(t *testing.T) {
	t.Parallel()

	if status := statusFromError(statusOnlyError{status: http.StatusBadGateway}); status != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", status, http.StatusBadGateway)
	}
}

type statusOnlyError struct {
	status int
}

func (e statusOnlyError) Error() string {
	return http.StatusText(e.status)
}

func (e statusOnlyError) StatusCode() int {
	return e.status
}

func TestStatusFromErrorIgnoresNonTimeoutNetErrors(t *testing.T) {
	t.Parallel()

	var err net.Error = &net.DNSError{IsTimeout: false}
	if status := statusFromError(err); status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}
}

func TestStatusFromErrorHandlesWrappedDeadlineExceeded(t *testing.T) {
	t.Parallel()

	err := &net.OpError{Err: context.DeadlineExceeded}
	if status := statusFromError(err); status != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", status, http.StatusRequestTimeout)
	}
}

func TestStatusFromErrorWithTimeoutSyscallError(t *testing.T) {
	t.Parallel()

	err := &net.OpError{Err: os.ErrDeadlineExceeded}
	if status := statusFromError(err); status != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", status, http.StatusRequestTimeout)
	}
}

func TestStatusFromErrorWithZeroValue(t *testing.T) {
	t.Parallel()

	if status := statusFromError(nil); status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}
}
