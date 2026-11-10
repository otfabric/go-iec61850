// SPDX-License-Identifier: MIT

package scl

// defaultTrue is the value of an optional boolean attribute whose schema
// default is true (for example gi, reasonCode and bufOvfl of a report
// control block, multicast of an SMV control block, modify of ConfDataSet):
// an absent attribute takes the default.
func defaultTrue(p *bool) bool {
	return p == nil || *p
}
