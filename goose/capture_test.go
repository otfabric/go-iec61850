// SPDX-License-Identifier: MIT

package goose_test

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-iec61850/goose"
)

// TestDecodeCaptures decodes every GOOSE frame of the captures under
// IEC61850_GOOSE_CAPTURES (pcapng or pcap files, searched recursively)
// strictly, and checks that numDatSetEntries matches the decoded allData.
// Third-party captures are not committed: the test skips when the variable
// is not set; IEC61850_GOOSE_CAPTURES_REQUIRE=1 turns the skip into a
// failure.
func TestDecodeCaptures(t *testing.T) {
	dir := os.Getenv("IEC61850_GOOSE_CAPTURES")
	if dir == "" {
		if os.Getenv("IEC61850_GOOSE_CAPTURES_REQUIRE") == "1" {
			t.Fatal("IEC61850_GOOSE_CAPTURES_REQUIRE=1 but IEC61850_GOOSE_CAPTURES is not set")
		}
		t.Skip("IEC61850_GOOSE_CAPTURES is not set (a directory of GOOSE captures)")
	}
	var files []string
	if err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".pcapng") || strings.HasSuffix(p, ".pcap")) {
			files = append(files, p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no captures under %s", dir)
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			frames, err := captureFrames(path)
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, fr := range frames {
				if !isGOOSE(fr) {
					continue
				}
				n++
				f, err := goose.Decode(fr)
				if err != nil {
					t.Errorf("frame %d: %v", n, err)
					continue
				}
				if int(f.PDU.NumDatSetEntries) != len(f.PDU.AllData) {
					t.Errorf("frame %d (%s): numDatSetEntries %d, %d allData entries", n, f.PDU.GocbRef, f.PDU.NumDatSetEntries, len(f.PDU.AllData))
				}
			}
			if n == 0 {
				t.Errorf("no GOOSE frames")
			}
			t.Logf("%d GOOSE frames decoded", n)
		})
	}
}

func isGOOSE(fr []byte) bool {
	if len(fr) < 14 {
		return false
	}
	et := binary.BigEndian.Uint16(fr[12:])
	if et == goose.EtherTypeVLAN && len(fr) >= 18 {
		et = binary.BigEndian.Uint16(fr[16:])
	}
	return et == goose.EtherType
}

// captureFrames returns the Ethernet frames of a pcapng or classic pcap
// file (link type Ethernet).
func captureFrames(path string) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	switch {
	case len(data) >= 12 && string(data[:4]) == "\x0a\x0d\x0d\x0a":
		var bo binary.ByteOrder = binary.LittleEndian
		var links []uint16
		for off := 0; off+12 <= len(data); {
			if binary.LittleEndian.Uint32(data[off:]) == 0x0A0D0D0A {
				if string(data[off+8:off+12]) == "\x4d\x3c\x2b\x1a" {
					bo = binary.LittleEndian
				} else {
					bo = binary.BigEndian
				}
				links = links[:0]
			}
			typ, blen := bo.Uint32(data[off:]), int(bo.Uint32(data[off+4:]))
			if blen < 12 || off+blen > len(data) {
				return nil, errors.New("pcapng: bad block length")
			}
			body := data[off+8 : off+blen-4]
			switch typ {
			case 1: // interface description
				links = append(links, bo.Uint16(body))
			case 6: // enhanced packet
				iface, caplen := int(bo.Uint32(body)), int(bo.Uint32(body[12:]))
				if iface < len(links) && links[iface] == 1 && 20+caplen <= len(body) {
					out = append(out, body[20:20+caplen])
				}
			case 3: // simple packet
				out = append(out, body[4:])
			}
			off += blen
		}
	case len(data) >= 24 && (string(data[:4]) == "\xd4\xc3\xb2\xa1" || string(data[:4]) == "\xa1\xb2\xc3\xd4"):
		var bo binary.ByteOrder = binary.LittleEndian
		if data[0] == 0xa1 {
			bo = binary.BigEndian
		}
		for off := 24; off+16 <= len(data); {
			caplen := int(bo.Uint32(data[off+8:]))
			if off+16+caplen > len(data) {
				return nil, errors.New("pcap: truncated record")
			}
			out = append(out, data[off+16:off+16+caplen])
			off += 16 + caplen
		}
	default:
		return nil, errors.New("not a pcapng or pcap file")
	}
	return out, nil
}
