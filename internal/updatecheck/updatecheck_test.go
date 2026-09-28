package updatecheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckVersionOrdering(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "newer", current: "v1.2.3", latest: "v1.2.4", want: true},
		{name: "equal", current: "1.2.3", latest: "v1.2.3", want: false},
		{name: "older", current: "v1.2.4", latest: "1.2.3", want: false},
		{name: "numeric multi digit", current: "v1.9.0", latest: "v1.10.0", want: true},
		{name: "current prerelease", current: "v1.2.3-rc.1", latest: "v1.2.3", want: true},
		{name: "current prerelease newer than latest stable", current: "v1.2.3-rc.1", latest: "v1.2.2", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := releaseServer(t, releaseResponse{TagName: tt.latest})
			defer server.Close()

			got := check(context.Background(), tt.current, server.Client(), server.URL)
			if (got != "") != tt.want {
				t.Fatalf("check(%q, %q) = %q, want notice=%v", tt.current, tt.latest, got, tt.want)
			}
			if tt.want && !strings.Contains(got, "https://github.com/roeyazroel/linear-tui/releases/tag/"+tt.latest) {
				t.Fatalf("notice %q does not contain the trusted release URL", got)
			}
		})
	}
}

func TestCheckSkipsLatestPrereleaseTag(t *testing.T) {
	server := releaseServer(t, releaseResponse{TagName: "v9.9.9-rc.1"})
	defer server.Close()

	if got := check(context.Background(), "v1.0.0", server.Client(), server.URL); got != "" {
		t.Fatalf("check() = %q, want empty notice", got)
	}
}

func TestCheckUsesActualValidatedTagInURL(t *testing.T) {
	server := releaseServer(t, releaseResponse{TagName: "1.2.4"})
	defer server.Close()

	got := check(context.Background(), "v1.0.0", server.Client(), server.URL)
	if !strings.Contains(got, "https://github.com/roeyazroel/linear-tui/releases/tag/1.2.4") {
		t.Fatalf("notice %q does not contain the actual release tag URL", got)
	}
	if strings.Contains(got, "/releases/tag/v1.2.4") {
		t.Fatalf("notice %q rewrote the actual release tag", got)
	}
}

func TestCheckSkipsUnknownCurrentVersion(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	if got := check(context.Background(), "dev", server.Client(), server.URL); got != "" {
		t.Fatalf("check(dev) = %q, want empty notice", got)
	}
	if called {
		t.Fatal("check(dev) made a release request")
	}
}

func TestCheckSkipsDraftAndPrerelease(t *testing.T) {
	for _, field := range []string{"draft", "prerelease"} {
		t.Run(field, func(t *testing.T) {
			response := releaseResponse{TagName: "v9.9.9"}
			if field == "draft" {
				response.Draft = true
			} else {
				response.Prerelease = true
			}
			server := releaseServer(t, response)
			defer server.Close()

			if got := check(context.Background(), "v1.0.0", server.Client(), server.URL); got != "" {
				t.Fatalf("check() = %q, want empty notice", got)
			}
		})
	}
}

func TestCheckErrorsAreQuiet(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "" {
					t.Errorf("Authorization header = %q, want absent", got)
				}
				w.WriteHeader(status)
			}))
			defer server.Close()

			if got := check(context.Background(), "v1.0.0", server.Client(), server.URL); got != "" {
				t.Fatalf("check() = %q, want empty notice", got)
			}
		})
	}
}

func TestCheckTimeoutIsQuiet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if got := check(ctx, "v1.0.0", server.Client(), server.URL); got != "" {
		t.Fatalf("check() = %q, want empty notice", got)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout check took %s", elapsed)
	}
}

func TestCheckMalformedResponsesAreQuiet(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: "{"},
		{name: "missing tag", body: `{"draft":false,"prerelease":false}`},
		{name: "invalid tag", body: `{"tag_name":"v1.2.x","draft":false,"prerelease":false}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			if got := check(context.Background(), "v1.0.0", server.Client(), server.URL); got != "" {
				t.Fatalf("check() = %q, want empty notice", got)
			}
		})
	}
}

type releaseResponse struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func releaseServer(t *testing.T, response releaseResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header = %q, want absent", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
}
