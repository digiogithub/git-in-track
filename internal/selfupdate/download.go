package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	// MaxArchiveBytes is the hard ceiling for a downloaded archive.
	MaxArchiveBytes = 200 << 20
	// MaxBinaryBytes is the ceiling for the extracted binary.
	MaxBinaryBytes = 500 << 20
	sizeMargin     = 1 << 20
	maxChecksums   = 1 << 20
)

// ChecksumMismatchError reports that the archive hash differs from checksums.txt.
type ChecksumMismatchError struct{ Asset, Want, Got string }

func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf("selfupdate: sha256 mismatch for %s: want %s, got %s", e.Asset, e.Want, e.Got)
}

// ErrChecksumMissing is returned when checksums.txt has no entry for the asset
// (or the release has no checksums.txt). Verification cannot be skipped.
var ErrChecksumMissing = errors.New("selfupdate: no checksum entry for asset")

// SizeError reports a download whose size is wrong or exceeds the cap.
type SizeError struct {
	Asset     string
	Want, Got int64 // Got is -1 when the cap was hit before the stream ended
}

func (e *SizeError) Error() string {
	if e.Got < 0 {
		return fmt.Sprintf("selfupdate: %s exceeds the expected size of %d bytes", e.Asset, e.Want)
	}
	return fmt.Sprintf("selfupdate: %s is %d bytes, expected %d (truncated download?)", e.Asset, e.Got, e.Want)
}

// BinaryName is the executable name inside an archive for goos.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "gintrack.exe"
	}
	return "gintrack"
}

// Download fetches asset, verifies its size and sha256 against checksums.txt
// of release, extracts the gintrack binary into dstDir (mode 0755) and returns
// its path. The archive is deleted; the installed binary is never touched. The
// target platform is inferred from the asset name.
func (c *Client) Download(ctx context.Context, release *Release, asset Asset, dstDir string) (string, error) {
	if asset.Size <= 0 || asset.Size > MaxArchiveBytes {
		return "", fmt.Errorf("selfupdate: asset %s has unacceptable size %d", asset.Name, asset.Size)
	}
	if asset.BrowserDownloadURL == "" {
		return "", fmt.Errorf("selfupdate: asset %s has no download URL", asset.Name)
	}
	var sumAsset *Asset
	for i := range release.Assets {
		if release.Assets[i].Name == ChecksumsName {
			sumAsset = &release.Assets[i]
		}
	}
	if sumAsset == nil {
		return "", fmt.Errorf("%w: release %s has no %s", ErrChecksumMissing, release.TagName, ChecksumsName)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", fmt.Errorf("selfupdate: %w", err)
	}

	sums, err := c.fetchSmall(ctx, sumAsset.BrowserDownloadURL)
	if err != nil {
		return "", fmt.Errorf("selfupdate: fetch %s: %w", ChecksumsName, err)
	}
	want, ok := lookupChecksum(sums, asset.Name)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrChecksumMissing, asset.Name)
	}

	tmp, err := os.CreateTemp(dstDir, ".gintrack-download-*")
	if err != nil {
		return "", fmt.Errorf("selfupdate: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	got, err := c.fetchTo(ctx, asset, tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if got != want {
		return "", &ChecksumMismatchError{Asset: asset.Name, Want: want, Got: got}
	}

	bin := "gintrack"
	if strings.Contains(asset.Name, "_windows_") {
		bin = BinaryName("windows")
	}
	switch {
	case strings.HasSuffix(asset.Name, ".tar.gz"):
		return extractTarGz(tmp.Name(), bin, dstDir)
	case strings.HasSuffix(asset.Name, ".zip"):
		return extractZip(tmp.Name(), bin, dstDir)
	}
	return "", fmt.Errorf("selfupdate: unsupported archive %q", asset.Name)
}

func (c *Client) get(ctx context.Context, u string) (*http.Response, error) {
	req, err := c.newRequest(ctx, u, false)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return resp, nil
}

func (c *Client) fetchSmall(ctx context.Context, u string) (string, error) {
	resp, err := c.get(ctx, u)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksums+1))
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if len(b) > maxChecksums {
		return "", errors.New("checksum file too large")
	}
	return string(b), nil
}

// fetchTo streams the asset into w, enforcing the size cap and exact size,
// and returns the hex sha256 of the bytes.
func (c *Client) fetchTo(ctx context.Context, a Asset, w io.Writer) (string, error) {
	resp, err := c.get(ctx, a.BrowserDownloadURL)
	if err != nil {
		return "", fmt.Errorf("selfupdate: download %s: %w", a.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	limit := a.Size + sizeMargin
	if limit > MaxArchiveBytes {
		limit = MaxArchiveBytes
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", fmt.Errorf("selfupdate: download %s: %w", a.Name, err)
	}
	if n > limit {
		return "", &SizeError{Asset: a.Name, Want: a.Size, Got: -1}
	}
	if n != a.Size {
		return "", &SizeError{Asset: a.Name, Want: a.Size, Got: n}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// lookupChecksum finds name in sha256sum-format text.
func lookupChecksum(text, name string) (string, bool) {
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		if strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// isExact reports whether an archive entry name is exactly the binary: a
// single path element with no traversal, no directories and no absolute path.
func isExact(entry, bin string) bool {
	if entry != path.Clean(entry) || strings.ContainsAny(entry, `\`) {
		return false
	}
	return entry == bin
}

func writeBinary(dstDir, bin string, r io.Reader) (string, error) {
	f, err := os.CreateTemp(dstDir, ".gintrack-extract-*")
	if err != nil {
		return "", fmt.Errorf("selfupdate: %w", err)
	}
	name := f.Name()
	n, err := io.Copy(f, io.LimitReader(r, MaxBinaryBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > MaxBinaryBytes {
		err = errors.New("selfupdate: extracted binary exceeds size limit")
	}
	if err == nil {
		err = os.Chmod(name, 0o755)
	}
	dst := filepath.Join(dstDir, bin)
	if err == nil {
		_ = os.Remove(dst)
		err = os.Rename(name, dst)
	}
	if err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("selfupdate: write binary: %w", err)
	}
	return dst, nil
}

func extractTarGz(archive, bin, dstDir string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", fmt.Errorf("selfupdate: %w", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("selfupdate: open archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("selfupdate: read archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || !isExact(strings.TrimPrefix(h.Name, "./"), bin) {
			continue
		}
		return writeBinary(dstDir, bin, tr)
	}
	return "", fmt.Errorf("selfupdate: %s not found in archive", bin)
}

func extractZip(archive, bin, dstDir string) (string, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return "", fmt.Errorf("selfupdate: open archive: %w", err)
	}
	defer func() { _ = zr.Close() }()
	for _, zf := range zr.File {
		if !zf.Mode().IsRegular() || !isExact(zf.Name, bin) {
			continue
		}
		return extractZipEntry(zf, bin, dstDir)
	}
	return "", fmt.Errorf("selfupdate: %s not found in archive", bin)
}

func extractZipEntry(zf *zip.File, bin, dstDir string) (string, error) {
	rc, err := zf.Open()
	if err != nil {
		return "", fmt.Errorf("selfupdate: read archive: %w", err)
	}
	defer func() { _ = rc.Close() }()
	return writeBinary(dstDir, bin, rc)
}
