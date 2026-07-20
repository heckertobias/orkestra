package api

import (
	"net/http"
	"testing"
)

func TestPublicURLFallback(t *testing.T) {
	tests := []struct {
		name          string
		publicBaseURL string
		headers       map[string]string
		want          string
	}{
		{
			name:          "configured base URL wins over headers",
			publicBaseURL: "https://orkestra.example.com",
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
			name:    "defaults to http when no forwarded proto",
			headers: map[string]string{"Host": "orkestra.example.com"},
			want:    "http://orkestra.example.com",
		},
		{
			name:    "local default when nothing is set",
			headers: map[string]string{},
			want:    "http://localhost:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			for k, v := range tt.headers {
				header.Set(k, v)
			}
			if got := publicURLFallback(tt.publicBaseURL, header); got != tt.want {
				t.Errorf("publicURLFallback() = %q, want %q", got, tt.want)
			}
		})
	}
}
