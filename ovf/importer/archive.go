/*
Copyright (c) 2024-2024 VMware, Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package importer

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/vmware/govmomi/ovf"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"
)

func ReadOvf(fpath string, a Archive) ([]byte, error) {
	r, _, err := a.Open(fpath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return io.ReadAll(r)
}

func ReadEnvelope(data []byte) (*ovf.Envelope, error) {
	e, err := ovf.Unmarshal(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ovf: %s", err)
	}

	return e, nil
}

type Archive interface {
	Open(string) (io.ReadCloser, int64, error)
}

type TapeArchive struct {
	Path string
	Opener
}

type TapeArchiveEntry struct {
	io.Reader
	f io.Closer

	Name string
}

func (t *TapeArchiveEntry) Close() error {
	return t.f.Close()
}

func (t *TapeArchive) Open(name string) (io.ReadCloser, int64, error) {
	f, _, err := t.OpenFile(t.Path)
	if err != nil {
		return nil, 0, err
	}

	r := tar.NewReader(f)

	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}

		matched, err := path.Match(name, path.Base(h.Name))
		if err != nil {
			return nil, 0, err
		}

		if matched {
			return &TapeArchiveEntry{r, f, h.Name}, h.Size, nil
		}
	}

	_ = f.Close()

	return nil, 0, os.ErrNotExist
}

type FileArchive struct {
	Path string
	Opener
}

func (t *FileArchive) Open(name string) (io.ReadCloser, int64, error) {
	fpath := name
	if name != t.Path && !filepath.IsAbs(name) {
		fpath = filepath.Join(filepath.Dir(t.Path), name)
	}

	return t.OpenFile(fpath)
}

type Opener struct {
	*vim25.Client
}

func IsRemotePath(path string) bool {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return true
	}
	return false
}

func (o Opener) OpenLocal(path string) (io.ReadCloser, int64, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, 0, err
	}

	s, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}

	return f, s.Size(), nil
}

func (o Opener) OpenFile(path string) (io.ReadCloser, int64, error) {
	if IsRemotePath(path) {
		return o.OpenRemote(path)
	}
	return o.OpenLocal(path)
}

func (o Opener) OpenRemote(link string) (io.ReadCloser, int64, error) {
	if o.Client == nil {
		return nil, 0, errors.New("remote path not supported")
	}

	u, err := url.Parse(link)
	if err != nil {
		return nil, 0, err
	}

	return o.Download(context.Background(), u, &soap.DefaultDownload)
}
