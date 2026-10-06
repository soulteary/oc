package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf8"
)

const (
	partialManifestName = "manifest.json"
	partialDataName     = "data"
	partialFormat       = "oc-local-partial-v1"
)

type localPartialManifest struct {
	Format      string `json:"format"`
	Target      string `json:"target"`
	TargetBytes string `json:"targetBytes,omitempty"`
}

type emptyPartialInfo struct {
	os.FileInfo
	target string
}

func (i emptyPartialInfo) Name() string { return i.target }
func (i emptyPartialInfo) Size() int64  { return 0 }

type fsContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r fsContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func partialDirCandidate(path string) bool {
	name := filepath.Base(path)
	return strings.HasPrefix(name, partialDirPrefix) && strings.HasSuffix(name, partSuffix)
}

func createLocalPartial(target string) (*os.File, string, error) {
	dir, err := os.MkdirTemp(filepath.Dir(target), partialDirPrefix+"*"+partSuffix)
	if err != nil {
		return nil, "", err
	}
	marker, err := os.CreateTemp(dir, ".manifest-*"+partSuffix)
	if err != nil {
		cleanupLocalPartial(dir)
		return nil, "", err
	}
	defer os.Remove(marker.Name())
	manifest := localPartialManifest{Format: partialFormat, Target: filepath.Base(target)}
	if !utf8.ValidString(manifest.Target) {
		manifest.TargetBytes = base64.StdEncoding.EncodeToString([]byte(manifest.Target))
		manifest.Target = ""
	}
	err = json.NewEncoder(marker).Encode(manifest)
	if err == nil {
		err = marker.Sync()
	}
	closeErr := marker.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(marker.Name(), filepath.Join(dir, partialManifestName))
	}
	if err != nil {
		_ = os.Remove(marker.Name())
		cleanupLocalPartial(dir)
		return nil, "", err
	}
	file, err := os.OpenFile(filepath.Join(dir, partialDataName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		cleanupLocalPartial(dir)
		return nil, "", err
	}
	return file, dir, nil
}

func cleanupLocalPartial(dir string) {
	_ = os.Remove(filepath.Join(dir, partialDataName))
	_ = os.Remove(filepath.Join(dir, partialManifestName))
	_ = os.Remove(dir)
}

// The name alone never authorizes deletion. OpenRoot confines operations to
// the same directory even if its path is replaced while cleanup is running.
func openLocalPartial(dir string) (*os.Root, localPartialManifest, error) {
	var manifest localPartialManifest
	if !partialDirCandidate(dir) {
		return nil, manifest, fmt.Errorf("not an OC staging directory")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, manifest, err
	}
	if !info.IsDir() {
		return nil, manifest, fmt.Errorf("staging directory must not be a symlink")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, manifest, err
	}
	fail := func(err error) (*os.Root, localPartialManifest, error) { _ = root.Close(); return nil, manifest, err }
	opened, err := root.Stat(".")
	if err != nil {
		return fail(err)
	}
	if !os.SameFile(info, opened) {
		return fail(fmt.Errorf("staging directory changed"))
	}
	marker, err := root.Lstat(partialManifestName)
	if err != nil {
		return fail(err)
	}
	if !marker.Mode().IsRegular() || marker.Size() > 8192 {
		return fail(fmt.Errorf("invalid staging manifest"))
	}
	file, err := root.Open(partialManifestName)
	if err != nil {
		return fail(err)
	}
	data, err := io.ReadAll(io.LimitReader(file, 8193))
	_ = file.Close()
	if err != nil {
		return fail(err)
	}
	if len(data) > 8192 || json.Unmarshal(data, &manifest) != nil {
		return fail(fmt.Errorf("invalid staging manifest"))
	}
	if manifest.TargetBytes != "" {
		decoded, err := base64.StdEncoding.DecodeString(manifest.TargetBytes)
		if err != nil || manifest.Target != "" {
			return fail(fmt.Errorf("invalid staging manifest"))
		}
		manifest.Target = string(decoded)
	}
	if manifest.Format != partialFormat || strings.ContainsRune(manifest.Target, 0) ||
		manifest.Target == "." || !filepath.IsLocal(manifest.Target) || filepath.Base(manifest.Target) != manifest.Target {
		return fail(fmt.Errorf("invalid staging manifest"))
	}
	return root, manifest, nil
}

func isPartialDir(path string) bool {
	root, _, err := openLocalPartial(path)
	if err != nil {
		return false
	}
	_ = root.Close()
	return true
}

func stagedPartial(dir string) (*ClientContent, error) {
	root, manifest, err := openLocalPartial(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := file.ReadDir(3)
	_ = file.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) != 1 && len(entries) != 2 {
		return nil, fmt.Errorf("unexpected files in staging directory")
	}
	info, err := root.Lstat(partialDataName)
	if os.IsNotExist(err) && len(entries) == 1 && entries[0].Name() == partialManifestName {
		marker, err := root.Stat(partialManifestName)
		if err != nil {
			return nil, err
		}
		return &ClientContent{URL: *newClientURL(filepath.Join(filepath.Dir(dir), manifest.Target)),
			Time: marker.ModTime(), Type: marker.Mode(), fsPartialPath: filepath.Join(dir, partialDataName)}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("staged data must be a regular file")
	}
	return &ClientContent{URL: *newClientURL(filepath.Join(filepath.Dir(dir), manifest.Target)),
		Size: info.Size(), Time: info.ModTime(), Type: info.Mode(), fsPartialPath: filepath.Join(dir, partialDataName), fsPartialInfo: info}, nil
}

func nameTooLong(err error) bool {
	return errors.Is(err, syscall.ENAMETOOLONG) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(206))
}

func localPartials(target string) ([]*ClientContent, error) {
	var found []*ClientContent
	legacy := target + partSuffix
	if info, err := os.Lstat(legacy); err == nil {
		if !info.IsDir() {
			found = append(found, &ClientContent{URL: *newClientURL(target), Size: info.Size(), Time: info.ModTime(), Type: info.Mode(), fsPartialPath: legacy, fsPartialInfo: info})
		}
	} else if !os.IsNotExist(err) && !nameTooLong(err) {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !partialDirCandidate(entry.Name()) {
			continue
		}
		partial, err := stagedPartial(filepath.Join(filepath.Dir(target), entry.Name()))
		if err == nil && filepath.Base(partial.URL.Path) == filepath.Base(target) {
			found = append(found, partial)
		}
	}
	return found, nil
}

func removeLocalPartial(content *ClientContent) error {
	path := content.fsPartialPath
	if path == content.URL.Path+partSuffix {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.IsDir() || content.fsPartialInfo != nil && !os.SameFile(info, content.fsPartialInfo) {
			return fmt.Errorf("partial file changed since listing")
		}
		return os.Remove(path)
	}
	dir := filepath.Dir(path)
	root, manifest, err := openLocalPartial(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if filepath.Base(path) != partialDataName || filepath.Join(filepath.Dir(dir), manifest.Target) != content.URL.Path {
		return fmt.Errorf("partial file does not match target")
	}
	current, err := stagedPartial(dir)
	if err != nil {
		return err
	}
	info, err := root.Lstat(partialDataName)
	if os.IsNotExist(err) && current.fsPartialInfo == nil {
		// A crash before opening data, or after removing it, can leave only the manifest.
	} else if err != nil {
		return err
	} else if current.fsPartialInfo == nil || !os.SameFile(info, current.fsPartialInfo) || content.fsPartialInfo != nil && !os.SameFile(info, content.fsPartialInfo) {
		return fmt.Errorf("partial file changed since listing")
	}
	if err := root.Remove(partialDataName); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := root.Remove(partialManifestName); err != nil {
		return err
	}
	_ = root.Close()
	return os.Remove(dir)
}

func (f *fsClient) listLocalPartials(ctx context.Context, recursive bool) <-chan *ClientContent {
	out := make(chan *ClientContent)
	go func() {
		defer close(out)
		root := filepath.Clean(f.PathURL.Path)
		send := func(c *ClientContent) error {
			select {
			case out <- c:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		info, err := os.Stat(root)
		if err != nil && !os.IsNotExist(err) {
			_ = send(&ClientContent{Err: f.toClientError(err, root)})
			return
		}
		if err != nil || !info.IsDir() {
			partials, err := localPartials(root)
			if err != nil {
				_ = send(&ClientContent{Err: f.toClientError(err, root)})
				return
			}
			for _, partial := range partials {
				if send(partial) != nil {
					return
				}
			}
			return
		}
		// ReadDir follows the explicitly requested root symlink. Descendant
		// symlinks are not followed, so recursive scans cannot leave this tree.
		var scan func(string) error
		scan = func(dir string) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				path := filepath.Join(dir, entry.Name())
				if entry.IsDir() {
					if partial, err := stagedPartial(path); err == nil {
						if send(partial) != nil {
							return ctx.Err()
						}
						continue
					}
					if isPartialDir(path) {
						continue
					}
					if recursive {
						if err := scan(path); err != nil {
							return err
						}
					}
					continue
				}
				if !strings.HasSuffix(entry.Name(), partSuffix) {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				partial := &ClientContent{URL: *newClientURL(strings.TrimSuffix(path, partSuffix)), Size: info.Size(), Time: info.ModTime(), Type: info.Mode(), fsPartialPath: path, fsPartialInfo: info}
				if send(partial) != nil {
					return ctx.Err()
				}
			}
			return nil
		}
		if err := scan(root); err != nil && ctx.Err() == nil {
			_ = send(&ClientContent{Err: f.toClientError(err, root)})
		}
	}()
	return out
}
