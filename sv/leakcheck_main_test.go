// SPDX-License-Identifier: MIT

package sv

import (
	"os"
	"testing"

	"github.com/otfabric/go-iec61850/internal/leakcheck"
)

// TestMain fails the run when a goroutine started by the tests is still
// running after they have passed (internal/leakcheck: by goroutine, no
// tolerance).
func TestMain(m *testing.M) { os.Exit(leakcheck.Main(m)) }
