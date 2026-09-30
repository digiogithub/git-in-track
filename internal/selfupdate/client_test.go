package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeGitHub serves a release list, tags and asset downloads.
type fakeGitHub struct {
	srv      *httptest.Server
	releases []Release
	files    map[string][]byte // asset name -> served bytes
	auth     string
	ua       string
	limited  bool
}

func newFake(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{files: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		f.auth, f.ua = r.Header.Get("Authorization"), r.Header.Get("User-Agent")
		if f.limited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1900000000")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		lo, hi := (page-1)*per, page*per
		if lo > len(f.releases) {
			lo = len(f.releases)
		}
		if hi > len(f.releases) {
			hi = len(f.releases)
		}
		_ = json.NewEncoder(w).Encode(f.releases[lo:hi])
	})
	mux.HandleFunc("/repos/o/r/releases/tags/", func(w http.ResponseWriter, r *http.Request) {
		tag := strings.TrimPrefix(r.URL.Path, "/repos/o/r/releases/tags/")
		for _, rel := range f.releases {
			if rel.TagName == tag {
				_ = json.NewEncoder(w).Encode(rel)
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		b, ok := f.files[strings.TrimPrefix(r.URL.Path, "/dl/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) client() *Client {
	return New(Options{BaseURL: f.srv.URL, Repo: "o/r", Token: "tok", Version: "9.9.9"})
}

func (f *fakeGitHub) add(tag string, draft, pre bool) {
	f.releases = append(f.releases, Release{TagName: tag, Draft: draft, Prerelease: pre})
}

func tarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, n := range sortedKeys(entries) {
		_ = tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(entries[n])), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(entries[n]))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range sortedKeys(entries) {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte(entries[n]))
	}
	zw.Close()
	return buf.Bytes()
}

func sortedKeys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	// deterministic, tiny maps
	for i := range ks {
		for j := i + 1; j < len(ks); j++ {
			if ks[j] < ks[i] {
				ks[i], ks[j] = ks[j], ks[i]
			}
		}
	}
	return ks
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// publish registers a release carrying one archive plus checksums.txt.
func (f *fakeGitHub) publish(name string, archive []byte, checksums string) *Release {
	tag := "v" + strings.Split(name, "_")[1]
	f.files[name] = archive
	f.files[ChecksumsName] = []byte(checksums)
	rel := Release{TagName: tag, Assets: []Asset{
		{Name: name, Size: int64(len(archive)), BrowserDownloadURL: f.srv.URL + "/dl/" + name},
		{Name: ChecksumsName, Size: int64(len(checksums)), BrowserDownloadURL: f.srv.URL + "/dl/" + ChecksumsName},
	}}
	return &rel
}

func TestLatest(t *testing.T) {
	ctx := context.Background()
	t.Run("highest semver not API order, drafts and prereleases skipped", func(t *testing.T) {
		f := newFake(t)
		f.add("v1.0.0", false, false)
		f.add("v2.1.0", true, false)
		f.add("v2.0.0", false, false)
		f.add("v3.0.0-rc.1", false, true)
		f.add("nightly", false, false)
		f.add("v1.9.0", false, false)
		r, err := f.client().Latest(ctx, false)
		if err != nil || r.TagName != "v2.0.0" {
			t.Fatalf("got %v %v", r, err)
		}
		if f.auth != "Bearer tok" || f.ua != "gintrack/9.9.9" {
			t.Fatalf("headers auth=%q ua=%q", f.auth, f.ua)
		}
	})
	t.Run("prerelease included on request", func(t *testing.T) {
		f := newFake(t)
		f.add("v2.0.0", false, false)
		f.add("v3.0.0-rc.1", false, true)
		r, err := f.client().Latest(ctx, true)
		if err != nil || r.TagName != "v3.0.0-rc.1" {
			t.Fatalf("got %v %v", r, err)
		}
	})
	t.Run("prerelease flag honored even for plain tag", func(t *testing.T) {
		f := newFake(t)
		f.add("v2.0.0", false, false)
		f.add("v2.1.0", false, true)
		r, _ := f.client().Latest(ctx, false)
		if r.TagName != "v2.0.0" {
			t.Fatalf("got %s", r.TagName)
		}
	})
	t.Run("pagination", func(t *testing.T) {
		f := newFake(t)
		for i := 0; i < 100; i++ {
			f.add(fmt.Sprintf("v1.0.%d", i), false, false)
		}
		f.add("v1.5.0", false, false) // on page 2
		r, err := f.client().Latest(ctx, false)
		if err != nil || r.TagName != "v1.5.0" {
			t.Fatalf("got %v %v", r, err)
		}
	})
	t.Run("none", func(t *testing.T) {
		f := newFake(t)
		f.add("v1.0.0", true, false)
		if _, err := f.client().Latest(ctx, false); !errors.Is(err, ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("rate limit", func(t *testing.T) {
		f := newFake(t)
		f.limited = true
		_, err := f.client().Latest(ctx, false)
		var rl *RateLimitError
		if !errors.As(err, &rl) || !strings.Contains(err.Error(), "GITHUB_TOKEN") || rl.Reset.IsZero() {
			t.Fatalf("got %v", err)
		}
	})
}

func TestByVersion(t *testing.T) {
	f := newFake(t)
	f.add("v1.2.3", false, false)
	f.add("0.9.0", false, false) // tag without v
	for _, c := range []struct {
		in, tag string
		ok      bool
	}{
		{"1.2.3", "v1.2.3", true}, {"v1.2.3", "v1.2.3", true}, {"0.9.0", "0.9.0", true},
		{"9.9.9", "", false}, {"", "", false}, {"a/b", "", false},
	} {
		t.Run(c.in, func(t *testing.T) {
			r, err := f.client().ByVersion(context.Background(), c.in)
			if c.ok && (err != nil || r.TagName != c.tag) {
				t.Fatalf("got %v %v", r, err)
			}
			if !c.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSelectAsset(t *testing.T) {
	r := &Release{TagName: "v2.2.0"}
	for _, n := range []string{"gintrack_2.2.0_linux_amd64.tar.gz", "gintrack_2.2.0_darwin_arm64.zip", "gintrack_2.2.0_windows_amd64.zip", "checksums.txt"} {
		r.Assets = append(r.Assets, Asset{Name: n})
	}
	for _, c := range []struct{ os, arch, want string }{
		{"linux", "amd64", "gintrack_2.2.0_linux_amd64.tar.gz"},
		{"darwin", "arm64", "gintrack_2.2.0_darwin_arm64.zip"},
		{"windows", "amd64", "gintrack_2.2.0_windows_amd64.zip"},
	} {
		t.Run(c.os+"/"+c.arch, func(t *testing.T) {
			a, err := SelectAsset(r, c.os, c.arch)
			if err != nil || a.Name != c.want {
				t.Fatalf("got %v %v", a, err)
			}
		})
	}
	t.Run("missing lists available", func(t *testing.T) {
		_, err := SelectAsset(r, "linux", "arm64")
		if err == nil || !strings.Contains(err.Error(), "gintrack_2.2.0_darwin_arm64.zip") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestDownload(t *testing.T) {
	ctx := context.Background()
	const payload = "#!fake-binary"
	tgz := tarGz(t, map[string]string{"gintrack": payload, "LICENSE": "l", "README.md": "r"})
	zp := zipOf(t, map[string]string{"gintrack.exe": payload, "LICENSE": "l"})

	good := func(name string, b []byte) string { return sum(b) + "  " + name + "\n" }

	t.Run("tar.gz ok", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_linux_amd64.tar.gz"
		rel := f.publish(name, tgz, good(name, tgz))
		dst := t.TempDir()
		p, err := f.client().Download(ctx, rel, rel.Assets[0], dst)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		st, _ := os.Stat(p)
		if string(b) != payload || filepath.Base(p) != "gintrack" || st.Mode().Perm()&0o100 == 0 {
			t.Fatalf("bad result %q %v", b, st.Mode())
		}
		left, _ := filepath.Glob(filepath.Join(dst, ".gintrack-*"))
		if len(left) != 0 {
			t.Fatalf("temp files left: %v", left)
		}
	})
	t.Run("zip ok", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_windows_amd64.zip"
		rel := f.publish(name, zp, good(name, zp))
		p, err := f.client().Download(ctx, rel, rel.Assets[0], t.TempDir())
		if err != nil || filepath.Base(p) != "gintrack.exe" {
			t.Fatalf("got %q %v", p, err)
		}
	})
	t.Run("bad checksum", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_linux_amd64.tar.gz"
		rel := f.publish(name, tgz, strings.Repeat("a", 64)+"  "+name+"\n")
		_, err := f.client().Download(ctx, rel, rel.Assets[0], t.TempDir())
		var cm *ChecksumMismatchError
		if !errors.As(err, &cm) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("checksum entry missing", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_linux_amd64.tar.gz"
		rel := f.publish(name, tgz, good("other.tar.gz", tgz))
		if _, err := f.client().Download(ctx, rel, rel.Assets[0], t.TempDir()); !errors.Is(err, ErrChecksumMissing) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("checksums.txt asset missing", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_linux_amd64.tar.gz"
		rel := f.publish(name, tgz, good(name, tgz))
		rel.Assets = rel.Assets[:1]
		if _, err := f.client().Download(ctx, rel, rel.Assets[0], t.TempDir()); !errors.Is(err, ErrChecksumMissing) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("truncated download", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_linux_amd64.tar.gz"
		rel := f.publish(name, tgz, good(name, tgz))
		f.files[name] = tgz[:len(tgz)/2]
		_, err := f.client().Download(ctx, rel, rel.Assets[0], t.TempDir())
		var se *SizeError
		if !errors.As(err, &se) || se.Got != int64(len(tgz)/2) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("oversized download", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_linux_amd64.tar.gz"
		rel := f.publish(name, tgz, good(name, tgz))
		f.files[name] = append(append([]byte{}, tgz...), make([]byte, 2<<20)...)
		_, err := f.client().Download(ctx, rel, rel.Assets[0], t.TempDir())
		var se *SizeError
		if !errors.As(err, &se) || se.Got != -1 {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("declared size above hard max", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_linux_amd64.tar.gz"
		rel := f.publish(name, tgz, good(name, tgz))
		rel.Assets[0].Size = MaxArchiveBytes + 1
		if _, err := f.client().Download(ctx, rel, rel.Assets[0], t.TempDir()); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("traversal and nested entries ignored", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_linux_amd64.tar.gz"
		evil := tarGz(t, map[string]string{"../gintrack": "x", "sub/gintrack": "x", "/gintrack": "x"})
		rel := f.publish(name, evil, good(name, evil))
		dst := t.TempDir()
		_, err := f.client().Download(ctx, rel, rel.Assets[0], dst)
		if err == nil || !strings.Contains(err.Error(), "not found in archive") {
			t.Fatalf("got %v", err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "gintrack")); err == nil {
			t.Fatal("escaped dstDir")
		}
	})
	t.Run("zip traversal ignored", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_windows_amd64.zip"
		evil := zipOf(t, map[string]string{"../gintrack.exe": "x", "a/gintrack.exe": "x"})
		rel := f.publish(name, evil, good(name, evil))
		if _, err := f.client().Download(ctx, rel, rel.Assets[0], t.TempDir()); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("binary absent from archive", func(t *testing.T) {
		f := newFake(t)
		name := "gintrack_1.0.0_linux_amd64.tar.gz"
		b := tarGz(t, map[string]string{"LICENSE": "l"})
		rel := f.publish(name, b, good(name, b))
		if _, err := f.client().Download(ctx, rel, rel.Assets[0], t.TempDir()); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestLookupChecksum(t *testing.T) {
	h := strings.Repeat("ab", 32)
	txt := h + "  gintrack_1_linux_amd64.tar.gz\nbad line\n" + strings.ToUpper(h) + " *x.zip\n"
	if got, ok := lookupChecksum(txt, "gintrack_1_linux_amd64.tar.gz"); !ok || got != h {
		t.Fatal(got, ok)
	}
	if got, ok := lookupChecksum(txt, "x.zip"); !ok || got != h {
		t.Fatal(got, ok)
	}
	if _, ok := lookupChecksum(txt, "nope"); ok {
		t.Fatal("unexpected")
	}
}
