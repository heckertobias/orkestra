package api

import (
	"net/http"
	"testing"
)

func TestRequestBaseURL(t *testing.T) {
	tests := []struct {
		name          string
		dbPublicURL   string
		envPublicURL  string
		secureCookies bool
		headers       map[string]string
		want          string
	}{
		{
			name:          "admin-set DB public URL wins over env and headers",
			dbPublicURL:   "https://ui.example.com/",
			envPublicURL:  "https://env.example.com",
			secureCookies: true,
			headers:       map[string]string{"X-Forwarded-Proto": "http", "Host": "internal:8080"},
			want:          "https://ui.example.com",
		},
		{
			name:          "env public URL wins over headers when no DB value",
			envPublicURL:  "https://orkestra.example.com",
			secureCookies: false,
			headers:       map[string]string{"X-Forwarded-Proto": "http", "Host": "internal:8080"},
			want:          "https://orkestra.example.com",
		},
		{
			name:    "X-Forwarded-Proto sets the scheme",
			headers: map[string]string{"X-Forwarded-Proto": "https", "Host": "orkestra.example.com"},
			want:    "https://orkestra.example.com",
		},
		{
			name:    "X-Forwarded-Host preferred over Host",
			headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "public.example.com", "Host": "internal:8080"},
			want:    "https://public.example.com",
		},
		{
			name:          "scheme defaults to https from secure cookies when no forwarded proto",
			secureCookies: true,
			headers:       map[string]string{"Host": "orkestra.example.com"},
			want:          "https://orkestra.example.com",
		},
		{
			name:          "scheme defaults to http when secure cookies disabled",
			secureCookies: false,
			headers:       map[string]string{"Host": "orkestra.example.com"},
			want:          "http://orkestra.example.com",
		},
		{
			name:          "local default when nothing is set (secure cookies)",
			secureCookies: true,
			headers:       map[string]string{},
			want:          "https://localhost:8080",
		},
		{
			name:          "local default when nothing is set (plain http)",
			secureCookies: false,
			headers:       map[string]string{},
			want:          "http://localhost:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			for k, v := range tt.headers {
				header.Set(k, v)
			}
			if got := requestBaseURL(tt.dbPublicURL, tt.envPublicURL, header, tt.secureCookies); got != tt.want {
				t.Errorf("requestBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStartupBaseURL(t *testing.T) {
	tests := []struct {
		name          string
		dbPublicURL   string
		envPublicURL  string
		bindAddr      string
		secureCookies bool
		want          string
	}{
		{
			name:         "admin-set DB public URL wins",
			dbPublicURL:  "https://ui.example.com/",
			envPublicURL: "https://env.example.com",
			bindAddr:     "0.0.0.0:8080",
			want:         "https://ui.example.com",
		},
		{
			name:         "env public URL used when no DB value",
			envPublicURL: "https://orkestra.example.com/",
			bindAddr:     "0.0.0.0:8080",
			want:         "https://orkestra.example.com",
		},
		{
			name:          "0.0.0.0 bind normalised to localhost with https from secure cookies",
			bindAddr:      "0.0.0.0:8080",
			secureCookies: true,
			want:          "https://localhost:8080",
		},
		{
			name:          "unspecified IPv6 bind normalised to localhost with http",
			bindAddr:      "[::]:8080",
			secureCookies: false,
			want:          "http://localhost:8080",
		},
		{
			name:          "explicit host preserved",
			bindAddr:      "10.0.0.5:8080",
			secureCookies: true,
			want:          "https://10.0.0.5:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StartupBaseURL(tt.dbPublicURL, tt.envPublicURL, tt.bindAddr, tt.secureCookies); got != tt.want {
				t.Errorf("StartupBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSchemeForSecureCookies(t *testing.T) {
	if got := SchemeForSecureCookies(true); got != "https" {
		t.Errorf("SchemeForSecureCookies(true) = %q, want %q", got, "https")
	}
	if got := SchemeForSecureCookies(false); got != "http" {
		t.Errorf("SchemeForSecureCookies(false) = %q, want %q", got, "http")
	}
}
