// SPDX-License-Identifier: MIT

package iec61850

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"runtime"
	"sync"
	"testing"

	"github.com/otfabric/go-mms"
)

// scriptedFileProvider serves one file, "/f", whose size announced at
// open is announced and whose FileRead answers come from chunks: chunk i
// is the answer to the i-th read, MoreFollows on every answer but the
// last. When endless is set, every read beyond the script answers the
// last chunk again with MoreFollows. It records the reads and closes.
type scriptedFileProvider struct {
	memFileProvider
	announced int64
	chunks    [][]byte
	endless   bool

	mu     sync.Mutex
	reads  int
	closes int
}

type scriptedHandle struct{ next int }

func (p *scriptedFileProvider) Open(context.Context, string) (mms.FileHandle, mms.FileAttributes, error) {
	return &scriptedHandle{}, mms.FileAttributes{Size: p.announced}, nil
}

func (p *scriptedFileProvider) Read(_ context.Context, handle mms.FileHandle, _ int) ([]byte, bool, error) {
	p.mu.Lock()
	p.reads++
	p.mu.Unlock()
	h := handle.(*scriptedHandle)
	if len(p.chunks) == 0 {
		return nil, false, nil
	}
	i := h.next
	h.next++
	if i >= len(p.chunks)-1 {
		if p.endless {
			return p.chunks[len(p.chunks)-1], true, nil
		}
		if i > len(p.chunks)-1 {
			return nil, false, fs.ErrClosed
		}
		return p.chunks[i], false, nil
	}
	return p.chunks[i], true, nil
}

func (p *scriptedFileProvider) Close(context.Context, mms.FileHandle) error {
	p.mu.Lock()
	p.closes++
	p.mu.Unlock()
	return nil
}

func (p *scriptedFileProvider) counts() (reads, closes int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads, p.closes
}

// scriptedFileClient connects a Client with limits to an MMS server
// that serves fp.
func scriptedFileClient(t *testing.T, fp *scriptedFileProvider, limits FileLimits) *Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	srv := mms.NewServer(mms.ServerOptions{
		MMS: mms.ServerMMSOptions{
			MaxPDUSize:                65000,
			MaxOutstandingCalling:     5,
			MaxOutstandingCalled:      5,
			DataStructureNestingLevel: 10,
		},
		FileProvider: fp,
	})
	clientT, serverT := loopbackPair(t)
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ctx, serverT)
	}()
	mc, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("mms.NewClient: %v", err)
	}
	c, err := NewClient(mc, ClientOptions{FileLimits: limits})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Close(context.Background())
		_ = mc.Abort(context.Background())
		cancel()
		<-served
	})
	return c
}

// chunksOf returns n chunks of size bytes each.
func chunksOf(n, size int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = bytes.Repeat([]byte{byte('a' + i%26)}, size)
	}
	return out
}

// ReadFile reads at most FileLimits.MaxSize bytes: a file announced
// larger is refused before it is read, a server that sends more than the
// limit is stopped, and in both cases the file is closed on the server.
func TestReadFile_MaxFileSize(t *testing.T) {
	const limit = 1000
	cases := []struct {
		name      string
		announced int64
		chunks    [][]byte
		endless   bool
		wantErr   bool
		wantReads int // -1: any
	}{
		{"announced and sent limit-1", limit - 1, chunksOf(1, limit-1), false, false, 1},
		{"announced and sent limit", limit, chunksOf(4, limit/4), false, false, 4},
		{"announced limit+1", limit + 1, chunksOf(1, limit+1), false, true, 0},
		{"announced 0, sent limit", 0, chunksOf(2, limit/2), false, false, 2},
		{"announced 0, sent limit+1", 0, append(chunksOf(1, limit), []byte{1}), false, true, 2},
		{"announced 0, endless", 0, chunksOf(1, 100), true, true, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fp := &scriptedFileProvider{announced: tc.announced, chunks: tc.chunks, endless: tc.endless}
			c := scriptedFileClient(t, fp, FileLimits{MaxSize: limit})
			data, entry, err := c.ReadFile(waitCtx(t), "/f")
			reads, closes := fp.counts()
			if tc.wantErr {
				if !errors.Is(err, ErrLimitExceeded) {
					t.Fatalf("ReadFile = %d bytes, %v; want ErrLimitExceeded", len(data), err)
				}
				if data != nil || entry != nil {
					t.Errorf("ReadFile returned data (%d bytes) or an entry with the error", len(data))
				}
			} else {
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				want := bytes.Join(tc.chunks, nil)
				if !bytes.Equal(data, want) {
					t.Errorf("ReadFile = %d bytes, want %d", len(data), len(want))
				}
			}
			if tc.wantReads >= 0 && reads != tc.wantReads {
				t.Errorf("%d FileRead requests, want %d", reads, tc.wantReads)
			}
			if closes != 1 {
				t.Errorf("%d FileClose requests, want 1", closes)
			}
		})
	}
}

// A server that keeps sending data makes ReadFile allocate about the
// limit, not more.
func TestReadFile_EndlessFileAllocation(t *testing.T) {
	const limit = 1 << 20
	fp := &scriptedFileProvider{chunks: chunksOf(1, 32<<10), endless: true}
	c := scriptedFileClient(t, fp, FileLimits{MaxSize: limit, SizeTolerance: 1 << 30})
	ctx := waitCtx(t)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, _, err := c.ReadFile(ctx, "/f")
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("ReadFile = %v, want ErrLimitExceeded", err)
	}
	// The buffer grows by doubling to the limit; every chunk is encoded,
	// carried and decoded by both MMS ends of the in-memory transport,
	// about 20 times its size in this test. The budget is proportional to
	// the limit, whatever the server goes on sending.
	if got, budget := after.TotalAlloc-before.TotalAlloc, uint64(32*limit); got > budget {
		t.Errorf("ReadFile of an endless file allocated %d bytes, budget %d", got, budget)
	}
}

// DownloadFile accepts up to FileLimits.SizeTolerance bytes beyond the
// size announced at open, and stops (closing the file) when the server
// sends more.
func TestDownloadFile_StopsBeyondAnnouncedSize(t *testing.T) {
	const announced, tolerance = 100, 50
	cases := []struct {
		name    string
		sent    int
		endless bool
		wantErr bool
	}{
		{"announced", announced, false, false},
		{"tolerance-1", announced + tolerance - 1, false, false},
		{"tolerance", announced + tolerance, false, false},
		{"tolerance+1", announced + tolerance + 1, false, true},
		{"endless", 10, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunks := [][]byte{bytes.Repeat([]byte{'x'}, tc.sent)}
			if !tc.endless {
				chunks = append(chunksOf(1, 1), bytes.Repeat([]byte{'x'}, tc.sent-1))
			}
			fp := &scriptedFileProvider{announced: announced, chunks: chunks, endless: tc.endless}
			c := scriptedFileClient(t, fp, FileLimits{SizeTolerance: tolerance})
			var buf bytes.Buffer
			entry, err := c.DownloadFile(waitCtx(t), "/f", &buf)
			_, closes := fp.counts()
			if tc.wantErr {
				if !errors.Is(err, ErrLimitExceeded) {
					t.Fatalf("DownloadFile = %d bytes, %v; want ErrLimitExceeded", buf.Len(), err)
				}
				if entry != nil {
					t.Error("DownloadFile returned an entry with the error")
				}
				if buf.Len() > announced+tolerance {
					t.Errorf("%d bytes written, at most %d", buf.Len(), announced+tolerance)
				}
			} else {
				if err != nil {
					t.Fatalf("DownloadFile: %v", err)
				}
				if buf.Len() != tc.sent || entry.Size != announced {
					t.Errorf("DownloadFile wrote %d bytes, entry size %d; want %d, %d", buf.Len(), entry.Size, tc.sent, announced)
				}
			}
			if closes != 1 {
				t.Errorf("%d FileClose requests, want 1", closes)
			}
		})
	}
}

// ReadFile and DownloadFile accept up to FileLimits.MaxEmptyChunks
// consecutive answers without data that announce more, and stop at the
// next one, closing the file. A chunk with data starts the count again.
func TestDownloadFile_StopsAfterEmptyChunks(t *testing.T) {
	empties := func(n int) [][]byte { return make([][]byte, n) }
	data := []byte("data")
	cases := []struct {
		name    string
		limit   int
		chunks  [][]byte
		endless bool
		wantErr bool
	}{
		{"limit-1 empty", 3, append(empties(2), data), false, false},
		{"limit empty", 3, append(empties(3), data), false, false},
		{"limit+1 empty", 3, append(empties(4), data), false, true},
		{"limit empty twice", 3, append(append(append(empties(3), data), empties(3)...), data), false, false},
		{"endless empty", 3, append([][]byte{data}, nil), true, true},
		{"default limit", 0, append(empties(DefaultMaxEmptyFileChunks), data), false, false},
		{"default limit+1", 0, append(empties(DefaultMaxEmptyFileChunks+1), data), false, true},
	}
	for _, tc := range cases {
		for _, op := range []string{"ReadFile", "DownloadFile"} {
			t.Run(tc.name+"/"+op, func(t *testing.T) {
				fp := &scriptedFileProvider{announced: 100, chunks: tc.chunks, endless: tc.endless}
				c := scriptedFileClient(t, fp, FileLimits{MaxEmptyChunks: tc.limit})
				var err error
				if op == "ReadFile" {
					_, _, err = c.ReadFile(waitCtx(t), "/f")
				} else {
					_, err = c.DownloadFile(waitCtx(t), "/f", io.Discard)
				}
				if tc.wantErr != errors.Is(err, ErrLimitExceeded) || !tc.wantErr && err != nil {
					t.Fatalf("%s = %v, want limit error %v", op, err, tc.wantErr)
				}
				if _, closes := fp.counts(); closes != 1 {
					t.Errorf("%d FileClose requests, want 1", closes)
				}
			})
		}
	}
}

func TestFileLimits_Resolved(t *testing.T) {
	def := FileLimits{MaxSize: DefaultMaxFileSize, SizeTolerance: DefaultFileSizeTolerance, MaxEmptyChunks: DefaultMaxEmptyFileChunks}
	for _, in := range []FileLimits{{}, {MaxSize: -1, SizeTolerance: -1, MaxEmptyChunks: -1}} {
		if got := in.resolved(); got != def {
			t.Errorf("%+v.resolved() = %+v, want %+v", in, got, def)
		}
	}
	set := FileLimits{MaxSize: 1, SizeTolerance: 2, MaxEmptyChunks: 3}
	if got := set.resolved(); got != set {
		t.Errorf("%+v.resolved() = %+v", set, got)
	}
}
