// SPDX-License-Identifier: MIT

// Command files demonstrates listing and reading files from an
// IEC 61850 server.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/otfabric/go-iec61850"
)

func main() {
	addr := "127.0.0.1:102"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
	if err != nil {
		cancel()
		log.Fatalf("dial: %v", err)
	}
	defer cancel()
	defer func() { _ = client.Close(context.Background()) }()

	files, err := client.ListFiles(ctx, "")
	if err != nil {
		log.Fatalf("list files: %v", err)
	}

	fmt.Printf("Found %d file(s):\n", len(files))
	for _, f := range files {
		fmt.Printf("  %-40s  %8d bytes  %s\n", f.Name, f.Size, f.LastModified.Format("2006-01-02 15:04:05"))
	}

	if len(files) > 0 {
		name := files[0].Name
		localPath := "downloaded_" + sanitizeFilename(name)
		fmt.Printf("\nDownloading %q to %q...\n", name, localPath)

		f, err := os.OpenFile(localPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("create local file: %v", err)
		}

		entry, err := client.DownloadFile(ctx, name, f)
		if err != nil {
			_ = f.Close()
			_ = os.Remove(localPath)
			log.Fatalf("download: %v", err)
		}
		_ = f.Close()
		fmt.Printf("Downloaded %d bytes to %s\n", entry.Size, localPath)
	}
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)

	safe := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '/' || c == '\\' || c == ':' || c == '*' || c == '?' || c == '"' || c == '<' || c == '>' || c == '|' {
			safe = append(safe, '_')
		} else {
			safe = append(safe, c)
		}
	}

	result := strings.TrimLeft(string(safe), ". ")
	result = strings.TrimRight(result, " ")
	if result == "" {
		return "downloaded_file"
	}
	return result
}
