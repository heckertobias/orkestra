package auth_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/heckertobias/orkestra/internal/master/auth"
)

// setCookieHeader returns the single Set-Cookie header value emitted by fn.
func setCookieHeader(t *testing.T, fn func(http.Header)) string {
	t.Helper()
	h := http.Header{}
	fn(h)
	got := h.Values("Set-Cookie")
	if len(got) != 1 {
		t.Fatalf("expected exactly one Set-Cookie header, got %d: %v", len(got), got)
	}
	return got[0]
}

func hasSecureAttr(setCookie string) bool {
	for _, attr := range strings.Split(setCookie, ";") {
		if strings.EqualFold(strings.TrimSpace(attr), "Secure") {
			return true
		}
	}
	return false
}

func TestSetSessionCookieSecureFlag(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	for _, secure := range []bool{true, false} {
		got := setCookieHeader(t, func(h http.Header) {
			auth.SetSessionCookie(h, "raw-token", expires, secure)
		})
		if hasSecureAttr(got) != secure {
			t.Errorf("SetSessionCookie(secure=%v): Secure attribute = %v, want %v (header: %q)",
				secure, hasSecureAttr(got), secure, got)
		}
	}
}

func TestClearSessionCookieSecureFlag(t *testing.T) {
	for _, secure := range []bool{true, false} {
		got := setCookieHeader(t, func(h http.Header) {
			auth.ClearSessionCookie(h, secure)
		})
		if hasSecureAttr(got) != secure {
			t.Errorf("ClearSessionCookie(secure=%v): Secure attribute = %v, want %v (header: %q)",
				secure, hasSecureAttr(got), secure, got)
		}
	}
}
