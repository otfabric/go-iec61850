// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-mms"
)

type memFile struct {
	name    string
	data    []byte
	modTime time.Time
}

type memFileHandle struct {
	data   []byte
	offset int
}

type memFileProvider struct {
	mu    sync.Mutex
	files map[string]*memFile
}

func newMemFileProvider() *memFileProvider {
	return &memFileProvider{files: make(map[string]*memFile)}
}

func (p *memFileProvider) addFile(name string, data []byte, modTime time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files[name] = &memFile{name: name, data: data, modTime: modTime}
}

func (p *memFileProvider) List(_ context.Context, req mms.FileListRequest) (*mms.FileListResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var entries []mms.FileEntry
	pastContinue := req.ContinueAfter == ""
	for _, f := range p.files {
		if !pastContinue {
			if f.name == req.ContinueAfter {
				pastContinue = true
			}
			continue
		}
		entries = append(entries, mms.FileEntry{
			Name:         f.name,
			Size:         int64(len(f.data)),
			LastModified: f.modTime,
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	return &mms.FileListResult{Entries: entries, MoreFollows: false}, nil
}

func (p *memFileProvider) Open(_ context.Context, path string) (mms.FileHandle, mms.FileAttributes, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	f, ok := p.files[path]
	if !ok {
		return nil, mms.FileAttributes{}, fs.ErrNotExist
	}

	dataCopy := make([]byte, len(f.data))
	copy(dataCopy, f.data)

	return &memFileHandle{data: dataCopy}, mms.FileAttributes{
		Size:         int64(len(f.data)),
		LastModified: f.modTime,
	}, nil
}

func (p *memFileProvider) Read(_ context.Context, handle mms.FileHandle, maxBytes int) ([]byte, bool, error) {
	h := handle.(*memFileHandle)
	remaining := len(h.data) - h.offset
	if remaining <= 0 {
		return nil, false, nil
	}

	n := remaining
	if n > maxBytes {
		n = maxBytes
	}

	chunk := h.data[h.offset : h.offset+n]
	h.offset += n

	moreFollows := h.offset < len(h.data)
	return chunk, moreFollows, nil
}

func (p *memFileProvider) Close(_ context.Context, _ mms.FileHandle) error {
	return nil
}

func (p *memFileProvider) Delete(_ context.Context, path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.files[path]; !ok {
		return fs.ErrNotExist
	}
	delete(p.files, path)
	return nil
}

func (p *memFileProvider) Rename(_ context.Context, currentName, newName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	f, ok := p.files[currentName]
	if !ok {
		return fs.ErrNotExist
	}
	delete(p.files, currentName)
	f.name = newName
	p.files[newName] = f
	return nil
}

func (p *memFileProvider) ObtainFile(_ context.Context, _, _ string) error {
	return nil
}

func setupFileLoopback(t *testing.T) (*Client, *memFileProvider) {
	t.Helper()
	ctx := context.Background()

	fp := newMemFileProvider()
	fp.addFile("/test/hello.txt", []byte("hello world"), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	fp.addFile("/test/data.bin", bytes.Repeat([]byte{0xAB}, 1024), time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC))

	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
		FileProvider: fp,
	})

	clientT, serverT := loopbackPair()
	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client, err := NewClient(mmsClient, ClientOptions{})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close(ctx)
	})

	return client, fp
}

func TestListFiles(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	entries, err := client.ListFiles(ctx, "")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	found := map[string]bool{}
	for _, e := range entries {
		found[e.Name] = true
		if e.Size <= 0 {
			t.Errorf("entry %q: size = %d, want > 0", e.Name, e.Size)
		}
	}
	if !found["/test/hello.txt"] {
		t.Error("missing /test/hello.txt")
	}
	if !found["/test/data.bin"] {
		t.Error("missing /test/data.bin")
	}
}

func TestReadFile(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	data, entry, err := client.ReadFile(ctx, "/test/hello.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if string(data) != "hello world" {
		t.Errorf("data = %q, want %q", data, "hello world")
	}
	if entry.Name != "/test/hello.txt" {
		t.Errorf("entry.Name = %q", entry.Name)
	}
	if entry.Size != 11 {
		t.Errorf("entry.Size = %d, want 11", entry.Size)
	}
}

func TestReadFile_Empty(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	_, _, err := client.ReadFile(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty filename")
	}
}

func TestDownloadFile(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	var buf bytes.Buffer
	entry, err := client.DownloadFile(ctx, "/test/data.bin", &buf)
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}

	if buf.Len() != 1024 {
		t.Errorf("downloaded %d bytes, want 1024", buf.Len())
	}
	if entry.Size != 1024 {
		t.Errorf("entry.Size = %d, want 1024", entry.Size)
	}
	expected := bytes.Repeat([]byte{0xAB}, 1024)
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Error("downloaded content mismatch")
	}
}

func TestDownloadFile_NilWriter(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	_, err := client.DownloadFile(ctx, "/test/hello.txt", nil)
	if err == nil {
		t.Fatal("expected error for nil writer")
	}
}

func TestDeleteFile(t *testing.T) {
	client, fp := setupFileLoopback(t)
	ctx := context.Background()

	if err := client.DeleteFile(ctx, "/test/hello.txt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	fp.mu.Lock()
	_, exists := fp.files["/test/hello.txt"]
	fp.mu.Unlock()

	if exists {
		t.Error("file should have been deleted")
	}
}

func TestDeleteFile_Empty(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	err := client.DeleteFile(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty filename")
	}
}

func TestListFiles_ClosedClient(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.ListFiles(ctx, "")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestReadFile_ClosedClient(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, _, err := client.ReadFile(ctx, "/test/hello.txt")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

type shortWriter struct{ n int }

func (sw *shortWriter) Write(p []byte) (int, error) {
	if len(p) > sw.n {
		return sw.n, nil
	}
	return len(p), nil
}

func TestDownloadFile_ShortWrite(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	_, err := client.DownloadFile(ctx, "/test/hello.txt", &shortWriter{n: 3})
	if err == nil {
		t.Fatal("expected error for short write")
	}
	if !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("got %v, want io.ErrShortWrite", err)
	}
}

type errWriter struct{ err error }

func (ew *errWriter) Write(_ []byte) (int, error) { return 0, ew.err }

func TestDownloadFile_WriterError(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	writeErr := errors.New("disk full")
	_, err := client.DownloadFile(ctx, "/test/hello.txt", &errWriter{err: writeErr})
	if err == nil {
		t.Fatal("expected error from writer")
	}
	if !errors.Is(err, writeErr) {
		t.Errorf("got %v, want wrapped %v", err, writeErr)
	}
}

func TestRenameFile(t *testing.T) {
	client, fp := setupFileLoopback(t)
	ctx := context.Background()

	if err := client.RenameFile(ctx, "/test/hello.txt", "/test/renamed.txt"); err != nil {
		t.Fatalf("RenameFile: %v", err)
	}

	fp.mu.Lock()
	_, oldExists := fp.files["/test/hello.txt"]
	_, newExists := fp.files["/test/renamed.txt"]
	fp.mu.Unlock()

	if oldExists {
		t.Error("old file should not exist after rename")
	}
	if !newExists {
		t.Error("new file should exist after rename")
	}
}

func TestRenameFile_EmptyCurrent(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	err := client.RenameFile(ctx, "", "/test/new.txt")
	if err == nil {
		t.Fatal("expected error for empty current name")
	}
}

func TestRenameFile_EmptyNew(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	err := client.RenameFile(ctx, "/test/hello.txt", "")
	if err == nil {
		t.Fatal("expected error for empty new name")
	}
}

func TestObtainFile(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	err := client.ObtainFile(ctx, "/test/hello.txt", "/test/copy.txt")
	if err != nil {
		t.Fatalf("ObtainFile: %v", err)
	}
}

func TestObtainFile_EmptySource(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	err := client.ObtainFile(ctx, "", "/test/dest.txt")
	if err == nil {
		t.Fatal("expected error for empty source")
	}
}

func TestObtainFile_EmptyDest(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	err := client.ObtainFile(ctx, "/test/hello.txt", "")
	if err == nil {
		t.Fatal("expected error for empty destination")
	}
}

func TestGetFileAttributes(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	entry, err := client.GetFileAttributes(ctx, "/test/hello.txt")
	if err != nil {
		t.Fatalf("GetFileAttributes: %v", err)
	}

	if entry.Name != "/test/hello.txt" {
		t.Errorf("Name = %q, want %q", entry.Name, "/test/hello.txt")
	}
	if entry.Size != 11 {
		t.Errorf("Size = %d, want 11", entry.Size)
	}
	if entry.LastModified.IsZero() {
		t.Error("LastModified is zero")
	}
}

func TestGetFileAttributes_Empty(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	_, err := client.GetFileAttributes(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty filename")
	}
}

func TestRenameFile_ClosedClient(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	err := client.RenameFile(ctx, "/test/hello.txt", "/test/renamed.txt")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestObtainFile_ClosedClient(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	err := client.ObtainFile(ctx, "/test/hello.txt", "/test/copy.txt")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}

func TestGetFileAttributes_ClosedClient(t *testing.T) {
	client, _ := setupFileLoopback(t)
	ctx := context.Background()

	_ = client.Close(ctx)

	_, err := client.GetFileAttributes(ctx, "/test/hello.txt")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want ErrClosed", err)
	}
}
