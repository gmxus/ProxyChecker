// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"net"
	"time"
)

type direct struct{}

// Direct is a direct proxy: one that makes network connections directly.
var Direct = direct{}

func (direct) DialTimeout(network, addr string, deadLine time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, deadLine)
}
