package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var errNoDownloadURL = errors.New("unable to find a downloadable URL for this media")

func Download(ctx *cli.Context) error {
	c := NewClientFromCLIContext(ctx)
	c.SkipExisting = ctx.Bool("skip-existing")

	medias, err := c.ListMedias(ctx.Timestamp("from"), ctx.Timestamp("to"))
	if err != nil {
		return err
	}

	skipped := 0
	for _, m := range medias {
		fmt.Println(m)
		if err = c.DownloadMedia(m, ctx.String("local-path")); err != nil {
			if errors.Is(err, errNoDownloadURL) {
				log.Printf("skipping %s (ID %s, type %s, resolution %s): %v", m.Filename, m.ID, m.Type, m.Resolution, err)
				skipped++
				continue
			}
			return errors.Wrapf(err, "downloading %s (ID %s)", m.Filename, m.ID)
		}
	}

	if skipped > 0 {
		return errors.Errorf("download batch finished with %d of %d media skipped because no matching downloadable URL was available", skipped, len(medias))
	}
	return nil
}

func (c *Client) DownloadMedia(m *Media, localPath string) (err error) {
	type listMediaURLsResponse struct {
		Embedded struct {
			Variations []struct {
				GetURL  string `json:"url"`
				HeadURL string `json:"head"`
				Type    string `json:"type"`
				Quality string `json:"quality"`
			} `json:"variations"`
		} `json:"_embedded"`
	}

	var (
		req            *http.Request
		resp           *http.Response
		parsedResponse listMediaURLsResponse
	)

	req, err = http.NewRequest("GET", fmt.Sprintf("%s%s/download", c.APIEndpoint, m.ID), nil)
	if err != nil {
		return errors.Wrap(err, "creating request")
	}
	c.addAuthorizationHeaders(req)

	resp, err = c.Do(req)
	if err != nil {
		err = errors.Wrap(err, "processing response")
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.Errorf("requesting download URLs: HTTP %s", resp.Status)
	}

	if err = json.NewDecoder(resp.Body).Decode(&parsedResponse); err != nil {
		err = errors.Wrap(err, "unmarshalling response")
		return
	}

	var (
		getURL  string
		headURL string
	)

Variations:
	for _, v := range parsedResponse.Embedded.Variations {
		switch m.Type {
		case MediaTypePhoto, MediaTypeBurst:
			getURL = v.GetURL
			headURL = v.HeadURL
			break Variations
		case MediaTypeVideo, MediaTypeTimeLapseVideo:
			if v.Quality == m.Resolution {
				getURL = v.GetURL
				headURL = v.HeadURL
				break Variations
			}
		}
	}

	if len(getURL) == 0 {
		err = errNoDownloadURL
		return
	}

	outputPath := filepath.Join(localPath, m.Filename)
	remoteSize := int64(-1)
	if c.SkipExisting {
		checkURL := headURL
		if checkURL == "" {
			checkURL = getURL
		}
		var skip bool
		skip, remoteSize, err = c.shouldSkipExisting(outputPath, checkURL)
		if err != nil {
			return err
		}
		if skip {
			log.Printf("skipping existing %s (ID %s): file size matches (%d bytes)", m.Filename, m.ID, remoteSize)
			return nil
		}
	}
	if err = os.MkdirAll(localPath, os.ModePerm); err != nil {
		return
	}

	downloadContext, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return c.downloadFile(downloadContext, getURL, headURL, outputPath)
}

// shouldSkipExisting only checks the final file, leaving partial downloads resumable.
// A negative remote size means that no reliable remote size was available.
func (c *Client) shouldSkipExisting(outputPath, headURL string) (bool, int64, error) {
	info, err := os.Stat(outputPath)
	if os.IsNotExist(err) {
		return false, -1, nil
	}
	if err != nil {
		return false, -1, errors.Wrap(err, "checking existing file")
	}
	if !info.Mode().IsRegular() {
		return false, -1, errors.Errorf("download destination is not a regular file: %s", outputPath)
	}
	req, err := http.NewRequest(http.MethodHead, headURL, nil)
	if err != nil {
		return false, -1, errors.Wrap(err, "creating file size request")
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.Do(req)
	if err != nil {
		log.Printf("unable to check remote file size for %s: %v; downloading normally", outputPath, err)
		return false, -1, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength < 0 || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
		log.Printf("remote file size unavailable for %s; downloading normally", outputPath)
		return false, -1, nil
	}
	return info.Size() == resp.ContentLength, resp.ContentLength, nil
}
