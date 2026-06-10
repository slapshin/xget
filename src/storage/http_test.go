package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDownloadRange(t *testing.T) {
	content := []byte("0123456789abcdefghij")

	tests := []struct {
		name    string
		handler http.HandlerFunc
		start   int64
		end     int64
		want    string
		wantErr bool
	}{
		{
			name: "server honors range with 206",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.ServeContent(w, r, "file.bin", time.Time{}, bytes.NewReader(content))
			},
			start: 5,
			end:   9,
			want:  "56789",
		},
		{
			name: "server ignores range with 200 full body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(content)
			},
			start: 5,
			end:   9,
			want:  "56789",
		},
		{
			name: "server ignores range with 200 starting at zero",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(content)
			},
			start: 0,
			end:   3,
			want:  "0123",
		},
		{
			name: "200 body shorter than range start",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(content[:3])
			},
			start:   5,
			end:     9,
			wantErr: true,
		},
		{
			name: "unexpected status code",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			start:   5,
			end:     9,
			wantErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(testCase.handler)
			defer server.Close()

			source := NewHTTPSource(server.URL, 5*time.Second)

			reader, err := source.DownloadRange(context.Background(), testCase.start, testCase.end)
			if testCase.wantErr {
				if err == nil {
					reader.Close()
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			defer reader.Close()

			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("reading range body: %v", err)
			}

			if string(got) != testCase.want {
				t.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
	}
}
