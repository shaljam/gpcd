package cmd

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

func TestDownloadMediaNoMatchingURL(t *testing.T) {
	for _, tc := range []struct {
		name  string
		media Media
		body  string
	}{
		{"empty variations", Media{ID: "photo", Type: MediaTypePhoto}, `{"_embedded":{"variations":[]}}`},
		{"resolution mismatch", Media{ID: "video", Type: MediaTypeVideo, Resolution: "4k"}, `{"_embedded":{"variations":[{"url":"https://example.invalid/video","quality":"1080p"}]}}`},
		{"unsupported type", Media{ID: "unknown", Type: "Unknown"}, `{"_embedded":{"variations":[{"url":"https://example.invalid/photo"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := NewClient(server.URL+"/", "", "", 1)
			if err := client.DownloadMedia(&tc.media, t.TempDir()); !errors.Is(err, errNoDownloadURL) {
				t.Fatalf("expected missing URL error, got %v", err)
			}
		})
	}
}

func TestDownloadMediaHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := NewClient(server.URL+"/", "", "", 1)
	err := client.DownloadMedia(&Media{ID: "photo", Type: MediaTypePhoto}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "401") || errors.Is(err, errNoDownloadURL) {
		t.Fatalf("expected HTTP authentication error, got %v", err)
	}
}

func TestDownloadContinuesAfterMissingURL(t *testing.T) {
	requestedSecond := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{"_pages":{"total_pages":1},"_embedded":{"media":[{"id":"first","filename":"first.jpg","type":"Photo"},{"id":"second","filename":"second.jpg","type":"Photo"}]}}`))
		case "/first/download":
			_, _ = w.Write([]byte(`{"_embedded":{"variations":[]}}`))
		case "/second/download":
			requestedSecond = true
			_, _ = w.Write([]byte(`{"_embedded":{"variations":[]}}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.String("api-endpoint", server.URL+"/", "")
	flags.String("bearer-token", "", "")
	flags.String("user-agent", "", "")
	flags.Int("max-concurrent-downloads", 1, "")
	flags.String("local-path", t.TempDir(), "")
	err := Download(cli.NewContext(cli.NewApp(), flags, nil))
	if !requestedSecond {
		t.Fatal("batch stopped before requesting the second media")
	}
	if err == nil || !strings.Contains(err.Error(), "2 of 2 media skipped") {
		t.Fatalf("expected skipped media summary, got %v", err)
	}
}

func TestDownloadMediaInvalidEndpoint(t *testing.T) {
	client := NewClient("://invalid/", "", "", 1)
	err := client.DownloadMedia(&Media{ID: "photo"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "creating request") {
		t.Fatalf("expected request creation error, got %v", err)
	}
}

func TestDownloadTimeLapseVideo(t *testing.T) {
	const contents = "test time lapse video contents"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/timelapse/download":
			fmt.Fprintf(w, `{"_embedded":{"variations":[{"url":%q,"quality":"1080p"},{"url":%q,"quality":"2160p"}]}}`, server.URL+"/wrong-resolution", server.URL+"/original")
		case "/original":
			http.ServeContent(w, r, "GX010309.MP4", time.Time{}, strings.NewReader(contents))
		default:
			t.Errorf("requested unexpected URL: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	client := NewClient(server.URL+"/", "", "", 1)
	media := &Media{ID: "timelapse", Filename: "GX010309.MP4", Type: MediaTypeTimeLapseVideo, Resolution: "2160p"}
	if err := client.DownloadMedia(media, dir); err != nil {
		t.Fatalf("downloading time lapse video: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, media.Filename))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != contents {
		t.Fatalf("downloaded content = %q, want %q", got, contents)
	}
}

func TestDownloadBurstPhoto(t *testing.T) {
	const contents = "test burst photo contents"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/burst/download":
			fmt.Fprintf(w, `{"_embedded":{"variations":[{"url":%q,"quality":"source"}]}}`, server.URL+"/original")
		case "/original":
			http.ServeContent(w, r, "GPAA0067.JPG", time.Time{}, strings.NewReader(contents))
		default:
			t.Errorf("requested unexpected URL: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	client := NewClient(server.URL+"/", "", "", 1)
	media := &Media{ID: "burst", Filename: "GPAA0067.JPG", Type: MediaTypeBurst, Resolution: "27127296"}
	if err := client.DownloadMedia(media, dir); err != nil {
		t.Fatalf("downloading burst photo: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, media.Filename))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != contents {
		t.Fatalf("downloaded content = %q, want %q", got, contents)
	}
}

func TestShouldSkipExisting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		local    string
		missing  bool
		partial  bool
		length   string
		status   int
		wantSkip bool
		wantHead bool
	}{
		{name: "matching", local: "photo", length: "5", status: 200, wantSkip: true, wantHead: true},
		{name: "shorter", local: "ph", length: "5", status: 200, wantHead: true},
		{name: "larger", local: "photo-extra", length: "5", status: 200, wantHead: true},
		{name: "missing", missing: true, length: "5", status: 200},
		{name: "partial only", missing: true, partial: true, length: "5", status: 200},
		{name: "unknown remote size", local: "photo", status: 200, wantHead: true},
		{name: "head unsupported", local: "photo", length: "5", status: 405, wantHead: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headSeen := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodHead {
					t.Errorf("unexpected request method: %s", r.Method)
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("GoPro authorization leaked to download URL")
				}
				headSeen = true
				if tc.length != "" {
					w.Header().Set("Content-Length", tc.length)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			output := filepath.Join(t.TempDir(), "photo.jpg")
			if !tc.missing {
				if err := os.WriteFile(output, []byte(tc.local), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.partial {
				if err := os.WriteFile(output+".part1", []byte("ph"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			client := NewClient(server.URL+"/", "secret", "", 1)
			skip, _, err := client.shouldSkipExisting(output, server.URL)
			if err != nil {
				t.Fatal(err)
			}
			if skip != tc.wantSkip || headSeen != tc.wantHead {
				t.Fatalf("skip=%v HEAD=%v, want skip=%v HEAD=%v", skip, headSeen, tc.wantSkip, tc.wantHead)
			}
		})
	}
}

func TestDownloadSkipExisting(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		local   string
		wantGet bool
	}{
		{"enabled matching size", true, "photo", false},
		{"disabled", false, "photo", true},
		{"shorter existing file", true, "ph", true},
		{"larger existing file", true, "photo-extra", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const contents = "photo"
			gotGet := false
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/photo/download":
					fmt.Fprintf(w, `{"_embedded":{"variations":[{"url":%q,"head":%q}]}}`, server.URL+"/original", server.URL+"/metadata")
				case "/metadata":
					if r.Method != http.MethodHead {
						t.Errorf("unexpected metadata method: %s", r.Method)
					}
					http.ServeContent(w, r, "photo.jpg", time.Time{}, strings.NewReader(contents))
				case "/original":
					if r.Method == http.MethodGet {
						gotGet = true
					}
					http.ServeContent(w, r, "photo.jpg", time.Time{}, strings.NewReader(contents))
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			output := filepath.Join(dir, "photo.jpg")
			if err := os.WriteFile(output, []byte(tc.local), 0600); err != nil {
				t.Fatal(err)
			}
			client := NewClient(server.URL+"/", "", "", 1)
			client.SkipExisting = tc.enabled
			if err := client.DownloadMedia(&Media{ID: "photo", Filename: "photo.jpg", Type: MediaTypePhoto}, dir); err != nil {
				t.Fatal(err)
			}
			if gotGet != tc.wantGet {
				t.Fatalf("GET=%v, want %v", gotGet, tc.wantGet)
			}
			data, err := os.ReadFile(output)
			if err != nil || string(data) != contents {
				t.Fatalf("file content=%q error=%v, want %q", data, err, contents)
			}
		})
	}
}
