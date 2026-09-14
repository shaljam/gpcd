package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTransferResume(t *testing.T) {
	const contents = "0123456789abcdefghijklmn"
	var mutex sync.Mutex
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			mutex.Lock()
			requested = append(requested, r.Header.Get("Range"))
			mutex.Unlock()
		}
		http.ServeContent(w, r, "video.mp4", time.Time{}, strings.NewReader(contents))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "video.mp4")
	for name, data := range map[string]string{
		output:            "oversized previous file that must be replaced",
		output + ".part1": contents[:3],
		output + ".part2": contents[9:18],
		output + ".part3": contents[18:20],
	} {
		if err := os.WriteFile(name, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	client := NewClient(server.URL, "", "", 3)
	if err := client.downloadFile(context.Background(), server.URL, "", output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != contents {
		t.Fatalf("contents=%q error=%v, want %q", data, err, contents)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(requested) != 2 {
		t.Fatalf("requests=%v, want only incomplete ranges", requested)
	}
	for _, part := range []string{".part1", ".part2", ".part3", ".download"} {
		if _, err := os.Stat(output + part); !os.IsNotExist(err) {
			t.Fatalf("temporary file %s remains: %v", part, err)
		}
	}
}

func TestTransferSmallFiles(t *testing.T) {
	for _, size := range []int{1, 2, 5, 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			contents := strings.Repeat("x", size)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.ServeContent(w, r, "photo.jpg", time.Time{}, strings.NewReader(contents))
			}))
			defer server.Close()
			output := filepath.Join(t.TempDir(), "photo.jpg")
			client := NewClient(server.URL, "", "", 8)
			if err := client.downloadFile(context.Background(), server.URL, "", output); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(output)
			if err != nil || string(got) != contents {
				t.Fatalf("contents=%q error=%v, want %q", got, err, contents)
			}
		})
	}
}

func TestTransferFailuresPreserveDestination(t *testing.T) {
	for _, kind := range []string{"http error", "wrong range", "short body", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.Header().Set("Content-Length", "5")
					w.Header().Set("Accept-Ranges", "bytes")
					return
				}
				switch kind {
				case "http error":
					http.Error(w, "unavailable", http.StatusForbidden)
				case "wrong range":
					w.Header().Set("Content-Range", "bytes 1-4/5")
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write([]byte("photo"))
				case "short body":
					w.Header().Set("Content-Range", "bytes 0-4/5")
					w.Header().Set("Content-Length", "5")
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write([]byte("ph"))
				}
			}))
			defer server.Close()
			output := filepath.Join(t.TempDir(), "photo.jpg")
			if err := os.WriteFile(output, []byte("previous"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			client := NewClient(server.URL, "", "", 1)
			if err := client.downloadFile(ctx, server.URL, "", output); err == nil {
				t.Fatal("expected transfer failure")
			}
			got, err := os.ReadFile(output)
			if err != nil || string(got) != "previous" {
				t.Fatalf("previous file changed: %q, %v", got, err)
			}
		})
	}
}

func TestTransferWithoutRanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte("photo"))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(output, []byte("previous oversized file"), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(server.URL, "", "", 1)
	if err := client.downloadFile(context.Background(), server.URL, "", output); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output)
	if err != nil || string(got) != "photo" {
		t.Fatalf("contents=%q error=%v, want photo", got, err)
	}
}
