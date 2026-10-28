// SPDX-License-Identifier: MIT

// Command files lists the files of an IEC 61850 server (MMS file
// services) and downloads one.
//
// Usage:
//
//	files [-addr 127.0.0.1:102] [-dir ""] [-get demo.icd] [-out .]
//
// Without -get it only lists. The download is written to -out under
// the file's base name and never overwrites an existing file. Run it
// against examples/ied-server, which serves its SCL as demo.icd.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	iec61850 "github.com/otfabric/go-iec61850"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:102", "server address")
	dir := flag.String("dir", "", "directory (file name prefix) to list")
	get := flag.String("get", "", "file to download")
	outDir := flag.String("out", ".", "download directory")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := run(ctx, *addr, *dir, *get, *outDir, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, addr, dir, get, outDir string, out io.Writer) error {
	client, err := iec61850.Dial(ctx, addr, iec61850.DialOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.Background()) }()

	files, err := client.ListFiles(ctx, dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		fmt.Fprintf(out, "%-40s %10d  %s\n", f.Name, f.Size, f.LastModified.Format(time.DateTime))
	}
	if get == "" {
		return nil
	}

	path := filepath.Join(outDir, safeName(get))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s exists; not overwriting", path)
	} else if err != nil {
		return err
	}
	entry, err := client.DownloadFile(ctx, get, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("download %s: %w", get, err)
	}
	fmt.Fprintf(out, "downloaded %s (%d bytes) to %s\n", get, entry.Size, path)
	return nil
}

// safeName reduces a server file name to a local base name.
func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" {
		return "download"
	}
	return name
}
