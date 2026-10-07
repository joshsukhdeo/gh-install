package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joshsukhdeo/gh-pt/selector"
	"github.com/ulikunitz/xz"
)

// HTTPRangeReaderAt implements io.ReaderAt using HTTP Range requests.
// It allows reading metadata (e.g. ZIP central directory) from remote archives
// without downloading or buffering the entire archive to disk.
type HTTPRangeReaderAt struct {
	ctx        context.Context
	url        string
	size       int64
	client     *http.Client
	authHeader string
}

func NewHTTPRangeReaderAt(ctx context.Context, url string, size int64, client *http.Client, authHeader string) *HTTPRangeReaderAt {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &HTTPRangeReaderAt{
		ctx:        ctx,
		url:        url,
		size:       size,
		client:     client,
		authHeader: authHeader,
	}
}

func (r *HTTPRangeReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= r.size {
		return 0, io.EOF
	}
	end := off + int64(len(p)) - 1
	if end >= r.size {
		end = r.size - 1
	}

	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))
	if r.authHeader != "" {
		req.Header.Set("Authorization", r.authHeader)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected HTTP status %d for range %d-%d", resp.StatusCode, off, end)
	}

	expectedLen := end - off + 1
	n, err := io.ReadFull(resp.Body, p[:expectedLen])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return n, err
	}
	return n, nil
}

// FetchRemoteAssetSize queries the total size of a remote asset via Range or HEAD request.
func FetchRemoteAssetSize(ctx context.Context, url string, client *http.Client, authHeader string) (int64, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 1. Try 1-byte range request which reliably returns Content-Range on S3/Azure/GitHub
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err == nil {
		req.Header.Set("Range", "bytes=0-0")
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			cr := resp.Header.Get("Content-Range")
			if cr != "" {
				if slashIdx := strings.LastIndex(cr, "/"); slashIdx >= 0 {
					totalStr := strings.TrimSpace(cr[slashIdx+1:])
					if total, err := strconv.ParseInt(totalStr, 10, 64); err == nil && total > 0 {
						return total, nil
					}
				}
			}
			if resp.ContentLength > 0 && resp.StatusCode == http.StatusOK {
				return resp.ContentLength, nil
			}
		}
	}

	// 2. Fall back to standard HEAD request
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	if authHeader != "" {
		headReq.Header.Set("Authorization", authHeader)
	}
	headResp, err := client.Do(headReq)
	if err != nil {
		return 0, err
	}
	_ = headResp.Body.Close()
	if headResp.ContentLength > 0 {
		return headResp.ContentLength, nil
	}

	return 0, fmt.Errorf("could not determine remote asset size for %s", url)
}

// PeekRemoteZip peeks into a remote ZIP archive via HTTP range requests on the Central Directory,
// discovering all files inside without downloading the full archive or writing to disk.
func PeekRemoteZip(ctx context.Context, downloadURL string, size int64, client *http.Client, authHeader string) ([]*selector.SelectorItem, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if size <= 0 {
		var err error
		size, err = FetchRemoteAssetSize(ctx, downloadURL, client, authHeader)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve zip size for remote peek: %w", err)
		}
	}

	readerAt := NewHTTPRangeReaderAt(ctx, downloadURL, size, client, authHeader)
	zipReader, err := zip.NewReader(readerAt, size)
	if err != nil {
		return nil, fmt.Errorf("failed to read remote zip central directory: %w", err)
	}

	var items []*selector.SelectorItem
	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		cleanPath := filepath.Clean(f.Name)
		if !selector.IsAllowedBinaryDir(cleanPath) {
			continue
		}
		baseName := filepath.Base(cleanPath)
		items = append(items, &selector.SelectorItem{
			Name:         baseName,
			DownloadPath: cleanPath,
			FsPath:       cleanPath,
			Compressed:   true,
			BinaryType:   selector.BinaryTypeFromPath(baseName),
		})
	}

	return items, nil
}

// StreamTarReader reads a tar archive stream in memory (decompressed as needed),
// extracting header names and discovering binary candidates without writing any files to disk.
func StreamTarReader(stream io.Reader, filename string) ([]*selector.SelectorItem, error) {
	lower := strings.ToLower(filename)
	var r io.Reader = stream
	var closeFn func()

	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		gz, err := gzip.NewReader(stream)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize gzip reader: %w", err)
		}
		r = gz
		closeFn = func() { _ = gz.Close() }
	} else if strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz") || strings.HasSuffix(lower, ".tbz2") {
		r = bzip2.NewReader(stream)
	} else if strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz") {
		xzr, err := xz.NewReader(stream)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize xz reader: %w", err)
		}
		r = xzr
	}
	if closeFn != nil {
		defer closeFn()
	}

	tr := tar.NewReader(r)
	var items []*selector.SelectorItem
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if len(items) > 0 {
				break
			}
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		cleanPath := filepath.Clean(hdr.Name)
		if !selector.IsAllowedBinaryDir(cleanPath) {
			continue
		}
		baseName := filepath.Base(cleanPath)
		items = append(items, &selector.SelectorItem{
			Name:         baseName,
			DownloadPath: cleanPath,
			FsPath:       cleanPath,
			Compressed:   true,
			BinaryType:   selector.BinaryTypeFromPath(baseName),
		})
	}

	return items, nil
}

// StreamRemoteTar downloads and streams a tar archive from GitHub releases directly into memory,
// reading its entry headers without writing any files to disk.
func (r *GithubRelease) StreamRemoteTar(tag, assetName string) ([]*selector.SelectorItem, error) {
	// First attempt via gh release download streaming to stdout
	cmd := execCommand("gh", "release", "download", tag, "--repo", r.CliParams.Repository, "--pattern", assetName, "-O", "-")
	stdout, err := cmd.StdoutPipe()
	if err == nil {
		if startErr := cmd.Start(); startErr == nil {
			items, streamErr := StreamTarReader(stdout, assetName)
			_ = cmd.Wait()
			if streamErr == nil && len(items) > 0 {
				return items, nil
			}
		}
	}

	// Fallback to ghExec buffer
	var stdOut bytes.Buffer
	stdOut, _, execErr := ghExec("release", "download", tag, "--repo", r.CliParams.Repository, "--pattern", assetName, "-O", "-")
	if execErr == nil && stdOut.Len() > 0 {
		return StreamTarReader(&stdOut, assetName)
	}

	return nil, fmt.Errorf("failed to stream remote tar archive %s: %w", assetName, execErr)
}

// PeekRemoteZip peeks into a remote zip asset using HTTP range requests without downloading the entire file.
func (r *GithubRelease) PeekRemoteZip(asset *selector.SelectorItem, tag string) ([]*selector.SelectorItem, error) {
	downloadURL := asset.URL
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", r.CliParams.Repository, tag, asset.Name)
	}

	var authHeader string
	if tokenOut, _, err := ghExec("auth", "token"); err == nil {
		t := strings.TrimSpace(tokenOut.String())
		if t != "" {
			authHeader = "Bearer " + t
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return PeekRemoteZip(ctx, downloadURL, asset.Size, client, authHeader)
}

// PeekRemoteTarConfig configures how many bytes to read for tar header inspection.
type PeekRemoteTarConfig struct {
	// MaxBytesToRead limits how many bytes to fetch from the remote archive.
	// For uncompressed .tar: 64KB covers ~100 headers.
	// For compressed (.tar.gz/.tar.bz2/.tar.xz): needs more to get complete compression blocks.
	// Default: 256KB (balances bandwidth vs. header coverage for compressed archives).
	MaxBytesToRead int64
	// MaxEntries limits the number of tar entries to parse.
	// Default: 100
	MaxEntries int
}

// defaultMaxBytesToRead returns appropriate default based on URL suffix
func defaultMaxBytesToRead(url string) int64 {
	lower := strings.ToLower(url)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz") || strings.HasSuffix(lower, ".tbz2") ||
		strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz") {
		return 512 * 1024 // 512KB for compressed - ensures complete compression blocks
	}
	return 64 * 1024 // 64KB for uncompressed
}

// PeekRemoteTar peeks into a remote tar archive (optionally compressed) via HTTP range requests,
// reading only the first N bytes to discover file entries without downloading the full archive.
func PeekRemoteTar(ctx context.Context, downloadURL string, size int64, client *http.Client, authHeader string, cfg *PeekRemoteTarConfig) ([]*selector.SelectorItem, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg == nil {
		cfg = &PeekRemoteTarConfig{}
	}
	if cfg.MaxBytesToRead <= 0 {
		cfg.MaxBytesToRead = defaultMaxBytesToRead(downloadURL)
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 100
	}

	// Determine how many bytes to actually read (bounded by file size)
	readSize := cfg.MaxBytesToRead
	if size > 0 && readSize > size {
		readSize = size
	}

	readerAt := NewHTTPRangeReaderAt(ctx, downloadURL, size, client, authHeader)

	// Read the first chunk of the archive
	buf := make([]byte, readSize)
	n, err := readerAt.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read remote tar prefix: %w", err)
	}
	buf = buf[:n]

	// Decompress if needed based on URL suffix
	lower := strings.ToLower(downloadURL)
	var tarStream io.Reader = bytes.NewReader(buf)
	var closeFn func()

	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		gz, err := gzip.NewReader(tarStream)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize gzip reader: %w", err)
		}
		tarStream = gz
		closeFn = func() { _ = gz.Close() }
	} else if strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz") || strings.HasSuffix(lower, ".tbz2") {
		tarStream = bzip2.NewReader(tarStream)
	} else if strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz") {
		xzr, err := xz.NewReader(tarStream)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize xz reader: %w", err)
		}
		tarStream = xzr
	}

	if closeFn != nil {
		defer closeFn()
	}

	tr := tar.NewReader(tarStream)
	var items []*selector.SelectorItem
	entriesRead := 0

	for entriesRead < cfg.MaxEntries {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			// If we hit an error mid-stream, we may have truncated in the middle of a file
			// Return what we have so far
			if len(items) > 0 {
				break
			}
			return nil, fmt.Errorf("tar parsing error after %d entries: %w", entriesRead, err)
		}

		// Only regular files (not dirs, symlinks, etc.)
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}

		cleanPath := filepath.Clean(hdr.Name)
		if !selector.IsAllowedBinaryDir(cleanPath) {
			continue
		}

		baseName := filepath.Base(cleanPath)
		items = append(items, &selector.SelectorItem{
			Name:         baseName,
			DownloadPath: cleanPath,
			FsPath:       cleanPath,
			Compressed:   true,
			BinaryType:   selector.BinaryTypeFromPath(baseName),
		})
		entriesRead++
	}

	return items, nil
}

// PeekRemoteTar peeks into a remote tar asset using HTTP range requests without downloading the entire file.
func (r *GithubRelease) PeekRemoteTar(asset *selector.SelectorItem, tag string, cfg *PeekRemoteTarConfig) ([]*selector.SelectorItem, error) {
	downloadURL := asset.URL
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", r.CliParams.Repository, tag, asset.Name)
	}

	var authHeader string
	if tokenOut, _, err := ghExec("auth", "token"); err == nil {
		t := strings.TrimSpace(tokenOut.String())
		if t != "" {
			authHeader = "Bearer " + t
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return PeekRemoteTar(ctx, downloadURL, asset.Size, client, authHeader, cfg)
}
