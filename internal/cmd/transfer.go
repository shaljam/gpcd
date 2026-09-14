package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
)

// downloadFile retains the existing .partN layout for interrupted downloads.
func (c *Client) downloadFile(ctx context.Context, getURL, headURL, outputPath string) error {
	if headURL == "" {
		headURL = getURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, headURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.Do(req)
	if err != nil {
		return errors.Wrap(err, "requesting download metadata")
	}
	size := resp.ContentLength
	ranges := resp.StatusCode == http.StatusOK && resp.Header.Get("Accept-Ranges") == "bytes" && size > 0
	resp.Body.Close()
	if !ranges {
		return c.downloadWholeFile(ctx, getURL, outputPath)
	}

	concurrency := c.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	bar := progressbar.DefaultBytes(size, "downloading")
	defer bar.Close()
	partSize := size / int64(concurrency)
	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, concurrency)
	var workers sync.WaitGroup
	for i := 1; i <= concurrency; i++ {
		start := int64(i-1) * (partSize + 1)
		end := start + partSize
		if i == concurrency || end >= size {
			end = size - 1
		}
		if start >= size {
			break
		}
		workers.Add(1)
		go func(part int, start, end int64) {
			defer workers.Done()
			if err := c.downloadPart(workContext, getURL, outputPath, part, start, end, size, bar); err != nil {
				errs <- err
				cancel()
			}
		}(i, start, end)
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Merge into a temporary file so old destination bytes cannot survive.
	tmpPath := outputPath + ".download"
	destination, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	var mergeErr error
	for i := 1; i <= concurrency; i++ {
		if int64(i-1)*(partSize+1) >= size {
			break
		}
		part, err := os.Open(fmt.Sprintf("%s.part%d", outputPath, i))
		if err != nil {
			mergeErr = err
			break
		}
		_, mergeErr = io.Copy(destination, part)
		part.Close()
		if mergeErr != nil {
			break
		}
	}
	closeErr := destination.Close()
	if mergeErr != nil {
		return mergeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return err
	}
	for i := 1; i <= concurrency; i++ {
		if int64(i-1)*(partSize+1) >= size {
			break
		}
		if err := os.Remove(fmt.Sprintf("%s.part%d", outputPath, i)); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) downloadPart(ctx context.Context, getURL, outputPath string, part int, start, end, total int64, bar *progressbar.ProgressBar) error {
	partPath := fmt.Sprintf("%s.part%d", outputPath, part)
	expected := end - start + 1
	var downloaded int64
	info, err := os.Stat(partPath)
	if err == nil {
		downloaded = info.Size()
	} else if !os.IsNotExist(err) {
		return err
	}
	if downloaded > expected {
		return errors.Errorf("partial file %s is larger than its expected range; move it aside before retrying", partPath)
	}
	_ = bar.Add64(downloaded)
	if downloaded == expected {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start+downloaded, end))
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	wantRange := fmt.Sprintf("bytes %d-%d/%d", start+downloaded, end, total)
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != wantRange {
		return errors.Errorf("downloading part %d: unexpected HTTP %s or Content-Range %q (expected %q)", part, resp.Status, resp.Header.Get("Content-Range"), wantRange)
	}
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	count, copyErr := io.Copy(io.MultiWriter(file, bar), io.LimitReader(resp.Body, expected-downloaded+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if count != expected-downloaded {
		return errors.Errorf("part %d received %d bytes, expected %d", part, count, expected-downloaded)
	}
	return nil
}

func (c *Client) downloadWholeFile(ctx context.Context, getURL, outputPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("downloading file: HTTP %s", resp.Status)
	}
	file, err := os.OpenFile(outputPath+".download", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	bar := progressbar.DefaultBytes(resp.ContentLength, "downloading")
	count, copyErr := io.Copy(io.MultiWriter(file, bar), resp.Body)
	bar.Close()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if resp.ContentLength >= 0 && count != resp.ContentLength {
		return errors.New("downloaded size does not match Content-Length " + strconv.FormatInt(resp.ContentLength, 10))
	}
	return os.Rename(outputPath+".download", outputPath)
}
