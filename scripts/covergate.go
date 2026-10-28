// SPDX-License-Identifier: MIT

//go:build ignore

// covergate enforces a minimum statement coverage per package from a
// Go cover profile:
//
//	go test -coverprofile=coverage.out ./...
//	go run scripts/covergate.go -min 90 coverage.out
//
// Packages matching -exclude (examples, the sclgen generator and generated raw SCL
// bindings by default) are reported but not enforced.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type counts struct{ covered, total int }

func main() {
	minPct := flag.Float64("min", 90, "minimum coverage percentage per package")
	exclude := flag.String("exclude", `/examples/|/scl/cmd/sclgen|/scl/internal/raw/|/interop`, "regexp of packages not enforced")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: covergate [-min N] [-exclude RE] coverage.out")
		os.Exit(2)
	}
	excl := regexp.MustCompile(*exclude)
	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer f.Close()

	// Profiles from several packages may repeat blocks; keep the max
	// count per block.
	blocks := map[string][2]int{} // block -> {statements, hit}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		// file.go:l.c,l.c numStmts count
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		n, _ := strconv.Atoi(fields[1])
		c, _ := strconv.Atoi(fields[2])
		b := blocks[fields[0]]
		b[0] = n
		if c > 0 {
			b[1] = 1
		}
		blocks[fields[0]] = b
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	pkgs := map[string]*counts{}
	for k, b := range blocks {
		file := k[:strings.LastIndex(k, ":")]
		pkg := path.Dir(file)
		p := pkgs[pkg]
		if p == nil {
			p = &counts{}
			pkgs[pkg] = p
		}
		p.total += b[0]
		if b[1] > 0 {
			p.covered += b[0]
		}
	}
	names := make([]string, 0, len(pkgs))
	for k := range pkgs {
		names = append(names, k)
	}
	sort.Strings(names)
	fail := false
	for _, name := range names {
		p := pkgs[name]
		pct := 100.0
		if p.total > 0 {
			pct = 100 * float64(p.covered) / float64(p.total)
		}
		status := "ok"
		switch {
		case excl.MatchString(name + "/"):
			status = "skip"
		case pct < *minPct:
			status = "FAIL"
			fail = true
		}
		fmt.Printf("%-4s %6.1f%%  %s\n", status, pct, name)
	}
	if fail {
		fmt.Printf("coverage below %.1f%% in at least one package\n", *minPct)
		os.Exit(1)
	}
}
