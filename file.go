package iec61850

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
)

// FileEntry describes a single file on an IEC 61850 / MMS server.
type FileEntry struct {
	// Name is the file path as returned by the server.
	Name string

	// Size is the file size in bytes.
	Size int64

	// LastModified is the file's last modification timestamp.
	LastModified time.Time
}

// ListFiles returns the directory listing for the given file path
// pattern on the server. An empty pattern lists the root directory.
//
// Pagination is handled internally; all matching entries are returned.
// Results are sorted by name for stable, deterministic output.
func (c *Client) ListFiles(ctx context.Context, pattern string) ([]FileEntry, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}

	mmsEntries, err := c.mmsClient.FileDirectoryAll(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("iec61850: list files %q: %w", pattern, err)
	}

	entries := make([]FileEntry, len(mmsEntries))
	for i, e := range mmsEntries {
		entries[i] = FileEntry{
			Name:         e.Name,
			Size:         e.Size,
			LastModified: e.LastModified,
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	c.logger.Debug("iec61850: list files", "pattern", pattern, "count", len(entries))
	return entries, nil
}

// ReadFile reads an entire file from the server and returns its contents.
//
// For large files, prefer [Client.DownloadFile] which streams to an
// [io.Writer] without buffering the entire file in memory.
func (c *Client) ReadFile(ctx context.Context, fileName string) ([]byte, *FileEntry, error) {
	if err := c.checkOpen(); err != nil {
		return nil, nil, err
	}
	if fileName == "" {
		return nil, nil, fmt.Errorf("iec61850: read file: %w: empty file name", ErrInvalidArgument)
	}

	data, openResult, err := c.mmsClient.DownloadFile(ctx, fileName)
	if err != nil {
		return nil, nil, fmt.Errorf("iec61850: read file %q: %w", fileName, err)
	}

	entry := &FileEntry{
		Name:         fileName,
		Size:         openResult.Size,
		LastModified: openResult.LastModified,
	}

	c.logger.Debug("iec61850: read file", "name", fileName, "size", len(data))
	return data, entry, nil
}

// DownloadFile streams a file from the server to the provided writer.
// This is the preferred method for large files as it does not buffer
// the entire file in memory.
//
// The returned [FileEntry] contains the file metadata (size, timestamp).
// The actual number of bytes written may differ from FileEntry.Size if
// the server reports an approximate size.
func (c *Client) DownloadFile(ctx context.Context, fileName string, w io.Writer) (*FileEntry, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if fileName == "" {
		return nil, fmt.Errorf("iec61850: download file: %w: empty file name", ErrInvalidArgument)
	}
	if w == nil {
		return nil, fmt.Errorf("iec61850: download file %q: %w: nil writer", fileName, ErrInvalidArgument)
	}

	openResult, err := c.mmsClient.FileOpen(ctx, fileName)
	if err != nil {
		return nil, fmt.Errorf("iec61850: download file %q: open: %w", fileName, err)
	}

	closeAndWrap := func(primary error) error {
		closeErr := c.mmsClient.FileClose(ctx, openResult.FrsmID)
		if closeErr != nil {
			return errors.Join(primary, fmt.Errorf("iec61850: download file %q: close: %w", fileName, closeErr))
		}
		return primary
	}

	var totalWritten int64
	for {
		chunk, readErr := c.mmsClient.FileRead(ctx, openResult.FrsmID)
		if readErr != nil {
			return nil, closeAndWrap(fmt.Errorf("iec61850: download file %q: read: %w", fileName, readErr))
		}

		if len(chunk.Data) > 0 {
			n, writeErr := w.Write(chunk.Data)
			totalWritten += int64(n)
			if writeErr != nil {
				return nil, closeAndWrap(fmt.Errorf("iec61850: download file %q: write: %w", fileName, writeErr))
			}
			if n != len(chunk.Data) {
				return nil, closeAndWrap(fmt.Errorf("iec61850: download file %q: %w", fileName, io.ErrShortWrite))
			}
		}

		if !chunk.MoreFollows {
			break
		}
	}

	if err := c.mmsClient.FileClose(ctx, openResult.FrsmID); err != nil {
		return &FileEntry{
			Name:         fileName,
			Size:         openResult.Size,
			LastModified: openResult.LastModified,
		}, fmt.Errorf("iec61850: download file %q: close: %w", fileName, err)
	}

	entry := &FileEntry{
		Name:         fileName,
		Size:         openResult.Size,
		LastModified: openResult.LastModified,
	}

	c.logger.Debug("iec61850: download file", "name", fileName, "bytes", totalWritten)
	return entry, nil
}

// DeleteFile deletes a file on the server.
func (c *Client) DeleteFile(ctx context.Context, fileName string) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if fileName == "" {
		return fmt.Errorf("iec61850: delete file: %w: empty file name", ErrInvalidArgument)
	}

	if err := c.mmsClient.FileDelete(ctx, fileName); err != nil {
		return fmt.Errorf("iec61850: delete file %q: %w", fileName, err)
	}

	c.logger.Debug("iec61850: delete file", "name", fileName)
	return nil
}

// RenameFile renames a file on the server.
func (c *Client) RenameFile(ctx context.Context, currentName, newName string) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if currentName == "" {
		return fmt.Errorf("iec61850: rename file: %w: empty current name", ErrInvalidArgument)
	}
	if newName == "" {
		return fmt.Errorf("iec61850: rename file: %w: empty new name", ErrInvalidArgument)
	}

	if err := c.mmsClient.FileRename(ctx, currentName, newName); err != nil {
		return fmt.Errorf("iec61850: rename file %q → %q: %w", currentName, newName, err)
	}

	c.logger.Debug("iec61850: rename file", "from", currentName, "to", newName)
	return nil
}

// ObtainFile instructs the server to copy a file from sourceFile to
// destinationFile. This is the standard MMS file transfer mechanism
// for "uploading" data — the server fetches the source file.
//
// In IEC 61850 deployments, ObtainFile is typically used for
// configuration file transfer (e.g. SCL/CID files) where the server
// pulls a file from an engineering workstation or another IED.
//
// Note: MMS does not define a direct client-to-server file write
// (push) operation. ObtainFile is the closest equivalent, but it
// requires the server to be able to reach the source file path.
func (c *Client) ObtainFile(ctx context.Context, sourceFile, destinationFile string) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	if sourceFile == "" {
		return fmt.Errorf("iec61850: obtain file: %w: empty source file name", ErrInvalidArgument)
	}
	if destinationFile == "" {
		return fmt.Errorf("iec61850: obtain file: %w: empty destination file name", ErrInvalidArgument)
	}

	if err := c.mmsClient.ObtainFile(ctx, sourceFile, destinationFile); err != nil {
		return fmt.Errorf("iec61850: obtain file %q → %q: %w", sourceFile, destinationFile, err)
	}

	c.logger.Debug("iec61850: obtain file", "source", sourceFile, "dest", destinationFile)
	return nil
}

// GetFileAttributes retrieves the metadata (size, last modified) for
// a single file without reading its contents.
//
// Implementation note: MMS does not define a dedicated metadata-only
// call. This method opens and immediately closes the file to obtain
// the metadata from the FileOpen response. If go-mms gains a pure
// metadata accessor (e.g., via FileDirectory with an exact path),
// this should switch to that to avoid the open/close overhead and
// potential server-side side effects.
func (c *Client) GetFileAttributes(ctx context.Context, fileName string) (*FileEntry, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	if fileName == "" {
		return nil, fmt.Errorf("iec61850: get file attributes: %w: empty file name", ErrInvalidArgument)
	}

	openResult, err := c.mmsClient.FileOpen(ctx, fileName)
	if err != nil {
		return nil, fmt.Errorf("iec61850: get file attributes %q: open: %w", fileName, err)
	}

	if err := c.mmsClient.FileClose(ctx, openResult.FrsmID); err != nil {
		return &FileEntry{
			Name:         fileName,
			Size:         openResult.Size,
			LastModified: openResult.LastModified,
		}, fmt.Errorf("iec61850: get file attributes %q: close: %w", fileName, err)
	}

	return &FileEntry{
		Name:         fileName,
		Size:         openResult.Size,
		LastModified: openResult.LastModified,
	}, nil
}
