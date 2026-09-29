// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package eth

import "testing"

// Tests that the message cap admits the 11-14 MB responses seen while snap
// syncing Amoy, while staying below the largest frame RLPx can carry (the frame
// size is a 24-bit field).
func TestMaxMessageSize(t *testing.T) {
	const observed = 14_328_926 // largest response that got peers dropped under the 10 MiB cap
	if maxMessageSize < observed {
		t.Fatalf("maxMessageSize %d rejects observed %d-byte responses", maxMessageSize, observed)
	}
	if maxMessageSize >= 1<<24 {
		t.Fatalf("maxMessageSize %d does not fit an RLPx frame (%d)", maxMessageSize, 1<<24)
	}
}
