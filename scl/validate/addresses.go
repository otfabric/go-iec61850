// SPDX-License-Identifier: MIT

package validate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/otfabric/go-iec61850/scl/index"
)

// XSD patterns of the address P types (SCL_Communication.xsd tP_*).
// tAddress uses the generic tP, so a schema validator does not apply
// them unless xsi:type names the restricted type; a value that the
// runtime parses although it deviates (lower case, ':' separators, a
// 0x prefix) is a warning, a value that cannot be parsed an error.
var (
	reMAC      = regexp.MustCompile(`^[0-9A-F]{2}(-[0-9A-F]{2}){5}$`)
	reAPPID    = regexp.MustCompile(`^[0-9A-F]{4}$`)
	reVLANID   = regexp.MustCompile(`^[0-9A-F]{3}$`)
	reVLANPrio = regexp.MustCompile(`^[0-7]$`)
	reIP       = regexp.MustCompile(`^(([0-9]{1,2}|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]{1,2}|1[0-9]{2}|2[0-4][0-9]|25[0-5])$`)
)

// Addresses checks the P values of ConnectedAP, GSE and SMV addresses:
//
//   - the tP_MAC-Address, tP_APPID, tP_VLAN-ID, tP_VLAN-PRIORITY and tP_IP
//     patterns (SCL_Communication.xsd): warning "address-format" when
//     the value deviates but can be read, error "invalid-address" when
//     it cannot (for example VLAN-PRIORITY 8);
//   - a P type given twice in one Address: warning "duplicate-p-type"
//     (the XSD only keys P types in PhysConn, uniquePTypeInPhysConn);
//   - VLAN-PRIORITY without VLAN-ID: warning "address-format" (8-1
//     Table 133 / 9-2 Table 19 condition c1);
//   - APPID ranges: GOOSE 0x0000-0x3FFF or 0x8000-0xBFFF (8-1 Annex C),
//     SV 0x4000-0x7FFF (9-2 5.3.3.4.2): warning "appid-range";
//   - destination MAC without the multicast bit (8-1 Table 12): warning
//     "mac-not-multicast"; outside the recommended 01-0C-CD-01-xx-xx
//     (GOOSE) / 01-0C-CD-04-xx-xx (SV) ranges (8-1 Table B.1, 9-2 Table
//     B.1): info "mac-range";
//   - the same MAC-Address and APPID used by two control blocks of one
//     SubNetwork: warning "duplicate-address" (8-1 Annex C leaves APPID
//     uniqueness to the configuration tool).
func Addresses(s *scl.SCL, _ *index.Index) []scl.Diagnostic {
	if s.Communication == nil {
		return nil
	}
	var diags []scl.Diagnostic
	add := func(sev scl.DiagSeverity, code, path, format string, args ...any) {
		diags = append(diags, scl.Diagnostic{Severity: sev, Code: code, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	for _, sn := range s.Communication.SubNetworks {
		snPath := "SubNetwork[" + q(sn.Name) + "]"
		seen := map[string]string{} // "MAC/APPID" -> first path
		for _, cap := range sn.ConnectedAPs {
			capPath := snPath + "/ConnectedAP[" + q(cap.IEDName) + "/" + q(cap.APName) + "]"
			checkPs(add, capPath+"/Address", cap.Address, "")
			for _, g := range cap.GSEs {
				p := capPath + "/GSE[" + q(g.LDInst) + "/" + q(g.CBName) + "]"
				if mac, app, ok := checkPs(add, p+"/Address", g.Address, "GOOSE"); ok {
					dupAddress(add, seen, p, mac, app)
				}
			}
			for _, v := range cap.SMVs {
				p := capPath + "/SMV[" + q(v.LDInst) + "/" + q(v.CBName) + "]"
				if mac, app, ok := checkPs(add, p+"/Address", v.Address, "SV"); ok {
					dupAddress(add, seen, p, mac, app)
				}
			}
		}
	}
	return diags
}

type addFunc func(sev scl.DiagSeverity, code, path, format string, args ...any)

func dupAddress(add addFunc, seen map[string]string, path string, mac [6]byte, app uint16) {
	k := fmt.Sprintf("%x/%04x", mac, app)
	if first, dup := seen[k]; dup {
		add(scl.DiagWarning, "duplicate-address", path, "MAC-Address and APPID 0x%04X also used by %s (8-1 Annex C)", app, first)
		return
	}
	seen[k] = path
}

// checkPs checks one Address. service is "GOOSE", "SV" or "" (a
// ConnectedAP address). It returns the parsed MAC and APPID when both
// are present and readable.
func checkPs(add addFunc, path string, ps []scl.P, service string) (mac [6]byte, appID uint16, ok bool) {
	types := map[string]bool{}
	var haveMAC, haveAppID, haveVID, havePrio bool
	for _, p := range ps {
		v := strings.TrimSpace(p.Value)
		if types[p.Type] {
			add(scl.DiagWarning, "duplicate-p-type", path, "P type %q given more than once", q(p.Type))
		}
		types[p.Type] = true
		format := func(pattern string) {
			add(scl.DiagWarning, "address-format", path, "%s %q does not match the SCL pattern %s", q(p.Type), q(p.Value), pattern)
		}
		invalid := func(why string) {
			add(scl.DiagError, "invalid-address", path, "%s %q: %s", q(p.Type), q(p.Value), why)
		}
		switch p.Type {
		case "MAC-Address":
			m, err := parseMAC(v)
			if err != nil {
				invalid(err.Error())
				continue
			}
			if !reMAC.MatchString(v) {
				format("XX-XX-XX-XX-XX-XX (upper-case hex)")
			}
			mac, haveMAC = m, true
		case "APPID":
			x, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(v), "0x"), 16, 16)
			if err != nil {
				invalid("not a 16-bit hexadecimal value")
				continue
			}
			if !reAPPID.MatchString(v) {
				format("XXXX (4 upper-case hex digits)")
			}
			appID, haveAppID = uint16(x), true
		case "VLAN-ID":
			x, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(v), "0x"), 16, 16)
			if err != nil || x > 0xfff {
				invalid("not a VLAN identifier 000-FFF")
				continue
			}
			if !reVLANID.MatchString(v) {
				format("XXX (3 upper-case hex digits)")
			}
			haveVID = true
		case "VLAN-PRIORITY":
			if !reVLANPrio.MatchString(v) {
				invalid("not a priority 0-7")
				continue
			}
			havePrio = true
		case "IP", "IP-SUBNET", "IP-GATEWAY":
			if !reIP.MatchString(v) {
				invalid("not a dotted-quad IPv4 address")
			}
		}
	}
	if havePrio && !haveVID && service != "" {
		add(scl.DiagWarning, "address-format", path, "VLAN-PRIORITY without VLAN-ID (8-1 Table 133 / 9-2 Table 19: c1)")
	}
	if service == "" {
		return mac, appID, false
	}
	if haveAppID {
		switch {
		case service == "GOOSE" && appID > 0x3fff && (appID < 0x8000 || appID > 0xbfff):
			add(scl.DiagWarning, "appid-range", path, "GOOSE APPID 0x%04X outside 0x0000-0x3FFF / 0x8000-0xBFFF (8-1 Annex C)", appID)
		case service == "SV" && (appID < 0x4000 || appID > 0x7fff):
			add(scl.DiagWarning, "appid-range", path, "SV APPID 0x%04X outside 0x4000-0x7FFF (9-2 5.3.3.4.2)", appID)
		}
	}
	if haveMAC {
		if mac[0]&0x01 == 0 {
			add(scl.DiagWarning, "mac-not-multicast", path, "MAC-Address %s has no multicast bit (8-1 Table 12)", macString(mac))
		} else {
			want := byte(0x01)
			if service == "SV" {
				want = 0x04
			}
			if mac[0] != 0x01 || mac[1] != 0x0c || mac[2] != 0xcd || mac[3] != want {
				add(scl.DiagInfo, "mac-range", path, "MAC-Address %s outside the recommended 01-0C-CD-%02X-xx-xx range", macString(mac), want)
			}
		}
	}
	return mac, appID, haveMAC && haveAppID
}

func parseMAC(v string) ([6]byte, error) {
	var m [6]byte
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '-' || r == ':' })
	if len(parts) != 6 {
		return m, fmt.Errorf("want 6 octets")
	}
	for i, s := range parts {
		b, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			return m, fmt.Errorf("octet %q is not hexadecimal", q(s))
		}
		m[i] = byte(b)
	}
	return m, nil
}

func macString(m [6]byte) string {
	return fmt.Sprintf("%02X-%02X-%02X-%02X-%02X-%02X", m[0], m[1], m[2], m[3], m[4], m[5])
}
