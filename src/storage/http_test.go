package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

// startHTTP2OnlyServer starts a TCP server that answers every request with a
// raw HTTP/2 SETTINGS frame, like an HTTP/2-only server that ignores the
// client protocol.
func startHTTP2OnlyServer(t *testing.T) net.Listener {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting listener: %v", err)
	}

	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go func(conn net.Conn) {
				defer conn.Close()

				// Read the HTTP/1.1 request before answering so the client
				// write does not race with the connection close.
				buf := make([]byte, 4096)
				_, _ = conn.Read(buf)

				// Empty HTTP/2 SETTINGS frame.
				_, _ = conn.Write([]byte("\x00\x00\x00\x04\x00\x00\x00\x00\x00"))
			}(conn)
		}
	}()

	return listener
}

func TestHTTP2OnlyServerFallsBackToSingleStream(t *testing.T) {
	listener := startHTTP2OnlyServer(t)

	source := NewHTTPSource("http://"+listener.Addr().String(), 5*time.Second)

	// Stub the fallback client to act as a working HTTP/2 server.
	source.fallbackClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Length": []string{"42"},
					"Accept-Ranges":  []string{"bytes"},
				},
				ContentLength: 42,
				Body:          io.NopCloser(strings.NewReader("")),
				Request:       req,
			}, nil
		}),
	}

	size, err := source.GetSize(context.Background())
	if err != nil {
		t.Fatalf("GetSize via fallback client: %v", err)
	}

	if size != 42 {
		t.Fatalf("got size %d, want 42", size)
	}

	if !source.http1Unsupported.Load() {
		t.Fatal("expected http1Unsupported flag to be set")
	}

	acceptsRanges, err := source.AcceptsRanges(context.Background())
	if err != nil {
		t.Fatalf("AcceptsRanges: %v", err)
	}

	if acceptsRanges {
		t.Fatal("expected AcceptsRanges to report false on HTTP/2-only server")
	}
}

func TestIsHTTP2PrefaceError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "http2 frames in http1 response",
			err: fmt.Errorf("net/http: HTTP/1.x transport connection broken: " +
				"malformed HTTP response \"\\x00\\x00\\x12\\x04\""),
			want: true,
		},
		{
			name: "ALPN negotiation failure",
			err:  fmt.Errorf("remote error: tls: no application protocol"),
			want: true,
		},
		{
			name: "unrelated error",
			err:  fmt.Errorf("connection refused"),
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := isHTTP2PrefaceError(testCase.err)
			if got != testCase.want {
				t.Fatalf("got %v, want %v", got, testCase.want)
			}
		})
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
