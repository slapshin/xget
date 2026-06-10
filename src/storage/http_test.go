package storage

import (
	"bytes"
	"context"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewHTTPSourceDisablesHTTP2(t *testing.T) {
	source := NewHTTPSource("http://example.com/file", 5*time.Second)

	transport, ok := source.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("got transport type %T, want *http.Transport", source.client.Transport)
	}

	if transport.ForceAttemptHTTP2 {
		t.Fatal("expected ForceAttemptHTTP2 to be false")
	}

	if transport.TLSNextProto == nil {
		t.Fatal("TLSNextProto must be an initialized empty map, not nil, to disable HTTP/2 upgrade")
	}

	if len(transport.TLSNextProto) != 0 {
		t.Fatalf("TLSNextProto must be empty, got %d entries", len(transport.TLSNextProto))
	}

	nextProtos := transport.TLSClientConfig.NextProtos
	if len(nextProtos) != 1 || nextProtos[0] != "http/1.1" {
		t.Fatalf("TLS ALPN must offer only http/1.1, got %v", nextProtos)
	}
}

// TestHTTPSourceUsesHTTP1AgainstHTTP2Server reproduces the Cloudflare R2
// failure: against a server that supports HTTP/2 the TLS handshake must not
// negotiate it, otherwise the server answers with HTTP/2 frames that the
// HTTP/1.1 connection cannot parse ("malformed HTTP response").
func TestHTTPSourceUsesHTTP1AgainstHTTP2Server(t *testing.T) {
	content := []byte("0123456789abcdefghij")

	var proto atomic.Value

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto.Store(r.Proto)
		http.ServeContent(w, r, "file.bin", time.Time{}, bytes.NewReader(content))
	}))
	server.EnableHTTP2 = true
	server.StartTLS()

	defer server.Close()

	source := NewHTTPSource(server.URL, 5*time.Second)

	transport, ok := source.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("got transport type %T, want *http.Transport", source.client.Transport)
	}

	// Trust the test server certificate.
	certPool := x509.NewCertPool()
	certPool.AddCert(server.Certificate())
	transport.TLSClientConfig.RootCAs = certPool

	reader, err := source.DownloadRange(context.Background(), 5, 9)
	if err != nil {
		t.Fatalf("DownloadRange against HTTP/2-capable server: %v", err)
	}

	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading range body: %v", err)
	}

	if string(got) != "56789" {
		t.Fatalf("got %q, want %q", got, "56789")
	}

	gotProto, _ := proto.Load().(string)
	if gotProto != "HTTP/1.1" {
		t.Fatalf("got protocol %q, want HTTP/1.1", gotProto)
	}
}

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
