// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO It would be nice to use a mock DNS server, to eliminate
// external dependencies.

package net

import (
	"runtime"
	"testing"
)

var avoidMacFirewall = runtime.GOOS == "darwin"

func TestGoogleSRV(t *testing.T) {
	if testing.Short() || avoidMacFirewall {
		t.Logf("skipping test to avoid external network")
		return
	}
	_, addrs, err := LookupSRV("xmpp-server", "tcp", "google.com")
	if err != nil {
		t.Errorf("failed: %s", err)
	}
	if len(addrs) == 0 {
		t.Errorf("no results")
	}
}
