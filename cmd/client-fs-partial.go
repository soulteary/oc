package cmd

import (
	"context"
	"crypto/rand"
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

// localPartial holds directory handles for the entire write lifecycle. Path
// names are only used for display and to detect replacement, never for data I/O.
type localPartial struct {
	parent, root                 *os.Root
	name, dir                    string
	info                         os.FileInfo
	createdManifest, createdData bool
}

func createLocalPartial(target string) (_ *os.File, _ *localPartial, err error) {
	parent, err := os.OpenRoot(filepath.Dir(target))
	if err != nil {
		return nil, nil, err
	}
	stage := &localPartial{parent: parent}
	defer func() {
		if err != nil {
			err = errors.Join(err, stage.cleanup())
		}
	}()
	for {
		stage.name = partialDirPrefix + rand.Text() + partSuffix
		err = parent.Mkdir(stage.name, 0700)
		if !os.IsExist(err) {
			break
		}
	}
	if err != nil {
		stage.name = ""
		return nil, nil, err
	}
	stage.dir = filepath.Join(filepath.Dir(target), stage.name)
	info, err := parent.Lstat(stage.name)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() || !privatePartialDirectory(info) {
		return nil, nil, fmt.Errorf("staging directory is not private")
	}
	stage.root, err = parent.OpenRoot(stage.name)
	if err != nil {
		return nil, nil, err
	}
	opened, err := stage.root.Stat(".")
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, nil, fmt.Errorf("staging directory changed")
	}
	if err = securePartialDirectory(stage.root); err != nil {
		return nil, nil, err
	}
	stage.info = opened
	// Exclusive creation in the opened private directory cannot follow a link.
	marker, err := stage.root.OpenFile(partialManifestName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, err
	}
	stage.createdManifest = true
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
	if err != nil {
		return nil, nil, err
	}
	file, err := stage.root.OpenFile(partialDataName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		return nil, nil, err
	}
	stage.createdData = true
	return file, stage, nil
}

func (s *localPartial) unchanged() error {
	info, err := s.parent.Lstat(s.name)
	if err != nil {
		return err
	}
	if s.info == nil || !info.IsDir() || !os.SameFile(info, s.info) {
		return fmt.Errorf("staging directory changed during write")
	}
	return nil
}

func (s *localPartial) commit(target string) error {
	if err := s.unchanged(); err != nil {
		return err
	}
	return renameLocalPartial(s.root, s.parent, filepath.Base(target))
}

func (s *localPartial) close() {
	if s.root != nil {
		_ = s.root.Close()
	}
	if s.parent != nil {
		_ = s.parent.Close()
	}
}

func (s *localPartial) cleanup() error {
	defer s.close()
	var errs []error
	if s.root != nil && s.info != nil {
		for _, entry := range []struct {
			name    string
			created bool
		}{
			{partialDataName, s.createdData}, {partialManifestName, s.createdManifest},
		} {
			if !entry.created {
				continue
			}
			if err := s.root.Remove(entry.name); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
		// Never traverse a replacement. Remove only the empty directory entry.
		if err := s.unchanged(); err != nil {
			errs = append(errs, err)
		} else {
			_ = s.root.Close()
			if err := s.parent.Remove(s.name); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
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
