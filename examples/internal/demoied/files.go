// SPDX-License-Identifier: MIT

package demoied

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	mms "github.com/otfabric/go-mms"
)

// Files is a read-only mms.FileProvider serving files from memory. The
// demo server serves its own SCL as "demo.icd".
type Files map[string][]byte

type fileHandle struct {
	data []byte
	off  int
}

var started = time.Now().Truncate(time.Second)

// List implements mms.FileProvider: the files whose name starts with
// the FileSpec, in name order.
func (f Files) List(_ context.Context, req mms.FileListRequest) (*mms.FileListResult, error) {
	names := make([]string, 0, len(f))
	for name := range f {
		if strings.HasPrefix(name, req.FileSpec) && name > req.ContinueAfter {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	res := &mms.FileListResult{}
	for _, name := range names {
		if req.MaxEntries > 0 && len(res.Entries) == req.MaxEntries {
			res.MoreFollows = true
			break
		}
		res.Entries = append(res.Entries, mms.FileEntry{Name: name, Size: int64(len(f[name])), LastModified: started})
	}
	return res, nil
}

// Open implements mms.FileProvider.
func (f Files) Open(_ context.Context, path string) (mms.FileHandle, mms.FileAttributes, error) {
	data, ok := f[path]
	if !ok {
		return nil, mms.FileAttributes{}, fs.ErrNotExist
	}
	return &fileHandle{data: data}, mms.FileAttributes{Size: int64(len(data)), LastModified: started}, nil
}

// Read implements mms.FileProvider.
func (f Files) Read(_ context.Context, h mms.FileHandle, maxBytes int) ([]byte, bool, error) {
	fh, ok := h.(*fileHandle)
	if !ok {
		return nil, false, fmt.Errorf("demoied: bad file handle %T", h)
	}
	n := min(maxBytes, len(fh.data)-fh.off)
	chunk := fh.data[fh.off : fh.off+n]
	fh.off += n
	return chunk, fh.off < len(fh.data), nil
}

// Close implements mms.FileProvider.
func (f Files) Close(context.Context, mms.FileHandle) error { return nil }

// Delete, Rename and ObtainFile are refused: the files are read-only.
func (f Files) Delete(context.Context, string) error             { return fs.ErrPermission }
func (f Files) Rename(context.Context, string, string) error     { return fs.ErrPermission }
func (f Files) ObtainFile(context.Context, string, string) error { return fs.ErrPermission }
