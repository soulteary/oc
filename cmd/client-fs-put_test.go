package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFSPutWithStalePartial(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		size       int64
	}{
		{"shorter", "new", 3},
		{"empty", "", 0},
		{"unknown-size", "new", -1},
		{"long-name", "new", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "object")
			if tc.name == "long-name" {
				path = filepath.Join(dir, strings.Repeat("o", 240))
			}
			stale := "LONG_OLD_CONTENT"
			if err := os.WriteFile(path+partSuffix, []byte(stale), 0600); err != nil {
				t.Fatal(err)
			}
			client, err := fsNew(path)
			if err != nil {
				t.Fatal(err)
			}
			n, err := client.Put(context.Background(), strings.NewReader(tc.data), tc.size, nil, PutOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if n != int64(len(tc.data)) {
				t.Fatalf("written = %d, want %d", n, len(tc.data))
			}
			assertFSFileContents(t, path, tc.data)
			assertFSFileContents(t, path+partSuffix, stale)
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 2 {
				t.Fatalf("unexpected files after commit: %v", entries)
			}
		})
	}
}

func TestFSPutFailedWriteKeepsDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "object")
	for _, name := range []string{path, path + partSuffix} {
		if err := os.WriteFile(name, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	client, err := fsNew(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Put(context.Background(), strings.NewReader("new"), 10, nil, PutOptions{}); err == nil {
		t.Fatal("short input unexpectedly succeeded")
	}
	assertFSFileContents(t, path, "original")
	assertFSFileContents(t, path+partSuffix, "original")
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 2 {
		t.Fatalf("temporary file leaked after failure: %v", entries)
	}
}

type fsPutGatedReader struct {
	io.Reader
	started chan struct{}
	release <-chan struct{}
	waited  bool
}

func (r *fsPutGatedReader) Read(p []byte) (int, error) {
	if !r.waited {
		r.waited = true
		close(r.started)
		<-r.release
	}
	return r.Reader.Read(p)
}

func TestFSPutConcurrentWritesUseSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "object")
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for _, data := range []string{"first", "SECOND_CONTENT"} {
		client, err := fsNew(path)
		if err != nil {
			t.Fatal(err)
		}
		started := make(chan struct{})
		reader := &fsPutGatedReader{Reader: strings.NewReader(data), started: started, release: release}
		go func() {
			_, err := client.Put(ctx, reader, int64(len(data)), nil, PutOptions{})
			if err != nil {
				results <- err.ToGoError()
			} else {
				results <- nil
			}
		}()
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("writer did not reach the temporary file")
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("writers shared a temporary file: %v", entries)
	}
	unblock()
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("writer did not finish")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first" && string(data) != "SECOND_CONTENT" {
		t.Fatalf("mixed output: %q", data)
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary files leaked: %v", entries)
	}
}

func assertFSFileContents(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s: contents = %q, want %q", path, data, want)
	}
}

type fsFailingReader struct{}

func (fsFailingReader) Read(p []byte) (int, error) {
	return copy(p, "partial"), errors.New("read failed")
}

func TestFSPutFailureCleanup(t *testing.T) {
	for _, kind := range []string{"read", "rename"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "object")
			var reader io.Reader = fsFailingReader{}
			if kind == "rename" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "keep"), []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
				reader = strings.NewReader("new")
			} else if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			client, err := fsNew(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Put(context.Background(), reader, -1, nil, PutOptions{}); err == nil {
				t.Fatal("expected failure")
			}
			entries, e := os.ReadDir(dir)
			if e != nil {
				t.Fatal(e)
			}
			if len(entries) != 1 {
				t.Fatalf("staging files leaked: %v", entries)
			}
			if kind == "read" {
				assertFSFileContents(t, path, "old")
			} else {
				assertFSFileContents(t, filepath.Join(path, "keep"), "old")
			}
		})
	}
}

func TestFSPutPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := t.TempDir()
	control := filepath.Join(dir, "control")
	fd, e := os.OpenFile(control, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if e != nil {
		t.Fatal(e)
	}
	fd.Close()
	want, e := os.Stat(control)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "object")
	client, err := fsNew(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Put(context.Background(), strings.NewReader("new"), 3, nil, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	got, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	if got.Mode().Perm() != want.Mode().Perm() {
		t.Fatalf("permissions %v, want %v", got.Mode(), want.Mode())
	}
	if _, err := client.Put(context.Background(), strings.NewReader("new"), 3, nil, PutOptions{isPreserve: true, metadata: map[string]string{metadataKey: "mode:416/uid:-1/gid:-1"}}); err != nil {
		t.Fatal(err)
	}
	got, e = os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	if got.Mode().Perm() != 0640 {
		t.Fatalf("preserved permissions = %v, want 0640", got.Mode())
	}
}

func TestFSPartialRemoveByTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "object")
	for i := 0; i < 2; i++ {
		file, _, e := createLocalPartial(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := file.WriteString("data"); e != nil {
			t.Fatal(e)
		}
		if e := file.Close(); e != nil {
			t.Fatal(e)
		}
	}
	client, err := fsNew(path)
	if err != nil {
		t.Fatal(err)
	}
	input := make(chan *ClientContent, 1)
	input <- &ClientContent{URL: *newClientURL(path)}
	close(input)
	for err := range client.Remove(context.Background(), true, false, false, input) {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("remaining partials: %v", entries)
	}
}

func TestFSPartialLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dir.part.minio-middle")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "object")
	for i := 0; i < 2; i++ {
		file, stage, e := createLocalPartial(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := file.WriteString("partial"); e != nil {
			t.Fatal(e)
		}
		if e := file.Close(); e != nil {
			t.Fatal(e)
		}
		if !isIgnoredFile(filepath.Join(stage, partialDataName)) {
			t.Fatal("watch would expose staged file")
		}
	}
	if e := os.WriteFile(path+partSuffix, []byte("legacy"), 0600); e != nil {
		t.Fatal(e)
	}
	client, err := fsNew(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Stat(context.Background(), StatOptions{incomplete: true}); err != nil {
		t.Fatal(err)
	}
	directory, err := fsNew(dir + string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	for content := range directory.List(context.Background(), ListOptions{Recursive: true}) {
		if content.Err != nil {
			t.Fatal(content.Err)
		}
		t.Fatalf("normal listing exposed staging: %s", content.URL.Path)
	}
	var found []*ClientContent
	for content := range directory.List(context.Background(), ListOptions{Incomplete: true}) {
		if content.Err != nil {
			t.Fatal(content.Err)
		}
		if content.URL.Path != path {
			t.Fatalf("lost target name: %s", content.URL.Path)
		}
		found = append(found, content)
	}
	if len(found) != 3 {
		t.Fatalf("found %d partials, want 3", len(found))
	}
	input := make(chan *ClientContent, len(found))
	for _, c := range found {
		input <- c
	}
	close(input)
	for err := range directory.Remove(context.Background(), true, false, false, input) {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("partials not removed: %v", entries)
	}
}

func makeTestLocalPartial(t *testing.T, target string) string {
	t.Helper()
	file, dir, err := createLocalPartial(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("partial"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFSPartialRejectsUnverifiedDirectories(t *testing.T) {
	for _, kind := range []string{"ordinary", "invalid-json", "wrong-format", "invalid-target", "null-target", "extra-file", "marker-symlink", "data-symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "object")
			stage := makeTestLocalPartial(t, target)
			switch kind {
			case "ordinary":
				if err := os.Remove(filepath.Join(stage, partialManifestName)); err != nil {
					t.Fatal(err)
				}
			case "invalid-json":
				if err := os.WriteFile(filepath.Join(stage, partialManifestName), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-format":
				if err := os.WriteFile(filepath.Join(stage, partialManifestName), []byte(`{"format":"other","target":"object"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid-target":
				if err := os.WriteFile(filepath.Join(stage, partialManifestName), []byte(`{"format":"oc-local-partial-v1","target":"../object"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "null-target":
				if err := os.WriteFile(filepath.Join(stage, partialManifestName), []byte(`{"format":"oc-local-partial-v1","target":"object\u0000"}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "extra-file":
				if err := os.WriteFile(filepath.Join(stage, "ordinary"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "marker-symlink", "data-symlink":
				name := partialManifestName
				if kind == "data-symlink" {
					name = partialDataName
				}
				outside := filepath.Join(dir, "outside")
				original, err := os.ReadFile(filepath.Join(stage, name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(outside, original, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(stage, name)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(stage, name)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			client, err := fsNew(dir + string(filepath.Separator))
			if err != nil {
				t.Fatal(err)
			}
			for c := range client.List(context.Background(), ListOptions{Incomplete: true, Recursive: true}) {
				if c.Err != nil {
					t.Fatal(c.Err)
				}
				t.Fatalf("unverified data listed as partial: %s", c.URL.Path)
			}
			input := make(chan *ClientContent, 1)
			input <- &ClientContent{URL: *newClientURL(target)}
			close(input)
			for range client.Remove(context.Background(), true, false, false, input) {
			}
			if _, err := os.Lstat(filepath.Join(stage, partialDataName)); err != nil {
				t.Fatalf("ordinary data removed: %v", err)
			}
		})
	}
}

func TestFSPartialLongTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, strings.Repeat("o", 255))
	makeTestLocalPartial(t, target)
	client, err := fsNew(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Stat(context.Background(), StatOptions{incomplete: true}); err != nil {
		t.Fatal(err)
	}
	var found []*ClientContent
	for c := range client.List(context.Background(), ListOptions{Incomplete: true}) {
		if c.Err != nil {
			t.Fatal(c.Err)
		}
		found = append(found, c)
	}
	if len(found) != 1 || found[0].URL.Path != target {
		t.Fatalf("long target lost: %v", found)
	}
	input := make(chan *ClientContent, 1)
	input <- &ClientContent{URL: *newClientURL(target)}
	close(input)
	for err := range client.Remove(context.Background(), true, false, false, input) {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("long target not cleaned: %v", entries)
	}
}

func TestFSPartialSymlinkRoot(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	makeTestLocalPartial(t, filepath.Join(real, "object"))
	if err := os.WriteFile(filepath.Join(real, "legacy")+partSuffix, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, recursive := range []bool{false, true} {
		client, err := fsNew(link + string(filepath.Separator))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for c := range client.List(context.Background(), ListOptions{Incomplete: true, Recursive: recursive}) {
			if c.Err != nil {
				t.Fatal(c.Err)
			}
			if filepath.Dir(c.URL.Path) != link {
				t.Fatalf("lost symlink alias: %s", c.URL.Path)
			}
			count++
		}
		if count != 2 {
			t.Fatalf("symlink root returned %d partials", count)
		}
	}
}

func TestFSPartialReplacementIsNotDeleted(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "object")
	stage := makeTestLocalPartial(t, target)
	listed, err := stagedPartial(stage)
	if err != nil {
		t.Fatal(err)
	}
	moved := stage + "-moved"
	if err := os.Rename(stage, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, partialDataName), []byte("ordinary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeLocalPartial(listed); err == nil {
		t.Fatal("replacement unexpectedly accepted")
	}
	assertFSFileContents(t, filepath.Join(stage, partialDataName), "ordinary")
	assertFSFileContents(t, filepath.Join(moved, partialDataName), "partial")
}

func TestFSPartialCancellationAndRecursion(t *testing.T) {
	dir := t.TempDir()
	makeTestLocalPartial(t, filepath.Join(dir, "first"))
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	makeTestLocalPartial(t, filepath.Join(nested, "second"))
	client, err := fsNew(dir + string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	for _, recursive := range []bool{false, true} {
		count := 0
		for c := range client.List(context.Background(), ListOptions{Incomplete: true, Recursive: recursive}) {
			if c.Err != nil {
				t.Fatal(c.Err)
			}
			count++
		}
		want := 1
		if recursive {
			want = 2
		}
		if count != want {
			t.Fatalf("recursive=%v: got %d, want %d", recursive, count, want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	out := client.List(ctx, ListOptions{Incomplete: true, Recursive: true})
	cancel()
	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("listing remained blocked after cancellation")
	}
}

func TestFSPartialManifestOnlyCleanup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "object")
	stage := makeTestLocalPartial(t, target)
	if err := os.Remove(filepath.Join(stage, partialDataName)); err != nil {
		t.Fatal(err)
	}
	client, err := fsNew(target)
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Stat(context.Background(), StatOptions{incomplete: true})
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 0 {
		t.Fatalf("orphan size=%d", info.Size)
	}
	input := make(chan *ClientContent, 1)
	input <- &ClientContent{URL: *newClientURL(target)}
	close(input)
	for err := range client.Remove(context.Background(), true, false, false, input) {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("manifest-only staging not cleaned: %v", entries)
	}
}

func TestFSPartialRemoveCancellation(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "object")
	stage := makeTestLocalPartial(t, target)
	client, err := fsNew(target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := make(chan *ClientContent, 1)
	input <- &ClientContent{URL: *newClientURL(target)}
	close(input)
	for range client.Remove(ctx, true, false, false, input) {
	}
	assertFSFileContents(t, filepath.Join(stage, partialDataName), "partial")
}

type fsCancelingReader struct{ cancel context.CancelFunc }

func (r fsCancelingReader) Read(p []byte) (int, error) { r.cancel(); return copy(p, "new"), io.EOF }

func TestFSPutValidationBeforeCommit(t *testing.T) {
	for _, kind := range []string{"canceled-before", "canceled-during", "zero-size", "excess-size", "invalid-time"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "object")
			if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			client, err := fsNew(target)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var reader io.Reader = strings.NewReader("new")
			size := int64(3)
			opts := PutOptions{}
			switch kind {
			case "canceled-before":
				cancel()
			case "canceled-during":
				reader = fsCancelingReader{cancel: cancel}
			case "zero-size":
				size = 0
			case "excess-size":
				size = 2
			case "invalid-time":
				if runtime.GOOS == "windows" {
					t.Skip("POSIX metadata")
				}
				opts = PutOptions{isPreserve: true, metadata: map[string]string{metadataKey: "mode:384/uid:-1/gid:-1/mtime:invalid"}}
			}
			if _, err := client.Put(ctx, reader, size, nil, opts); err == nil {
				t.Fatal("invalid or canceled copy succeeded")
			}
			assertFSFileContents(t, target, "old")
			entries, e := os.ReadDir(dir)
			if e != nil {
				t.Fatal(e)
			}
			if len(entries) != 1 {
				t.Fatalf("staging leaked: %v", entries)
			}
		})
	}
}

func TestFSPartialDanglingLegacySymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "object")
	if err := os.Symlink(filepath.Join(dir, "missing"), target+partSuffix); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	client, err := fsNew(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Stat(context.Background(), StatOptions{incomplete: true}); err != nil {
		t.Fatal(err)
	}
	input := make(chan *ClientContent, 1)
	input <- &ClientContent{URL: *newClientURL(target)}
	close(input)
	for err := range client.Remove(context.Background(), true, false, false, input) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(target + partSuffix); !os.IsNotExist(err) {
		t.Fatalf("dangling partial not removed: %v", err)
	}
}

func TestFSPartialNonUTF8Target(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows filenames are Unicode")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "object-"+string([]byte{0xff}))
	makeTestLocalPartial(t, target)
	partials, err := localPartials(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(partials) != 1 || partials[0].URL.Path != target {
		t.Fatal("raw filename bytes lost in manifest")
	}
	if err := removeLocalPartial(partials[0]); err != nil {
		t.Fatal(err)
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("raw filename staging leaked: %v", entries)
	}
}

func TestFSPartialValidReplacementIsNotDeleted(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "object")
	stage := makeTestLocalPartial(t, target)
	listed, err := stagedPartial(stage)
	if err != nil {
		t.Fatal(err)
	}
	moved := stage + "-moved"
	if err := os.Rename(stage, moved); err != nil {
		t.Fatal(err)
	}
	replacement := makeTestLocalPartial(t, target)
	if err := os.Rename(replacement, stage); err != nil {
		t.Fatal(err)
	}
	if err := removeLocalPartial(listed); err == nil {
		t.Fatal("new staging at an old path was deleted")
	}
	assertFSFileContents(t, filepath.Join(stage, partialDataName), "partial")
	assertFSFileContents(t, filepath.Join(moved, partialDataName), "partial")
}
