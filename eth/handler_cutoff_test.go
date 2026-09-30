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

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/history"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/params"
)

// Tests that a fresh node configured for full sync but with a history cutoff
// snap syncs instead: blocks below the cutoff never get bodies, so they can't
// be executed, and a full sync would silently ignore the cutoff.
func TestFullSyncSwitchesToSnapWithHistoryCutoff(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cutoff *history.PrunePoint
		snap   bool
	}{
		{"no cutoff", nil, false},
		{"cutoff", &history.PrunePoint{BlockNumber: 100, BlockHash: common.Hash{0x01}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			gspec := &core.Genesis{
				Config: params.TestChainConfig,
				Alloc:  types.GenesisAlloc{testAddr: {Balance: big.NewInt(1000000)}},
			}
			cfg := core.DefaultConfig()
			cfg.HistoryCutoff = tt.cutoff
			chain, err := core.NewBlockChain(db, gspec, ethash.NewFaker(), cfg)
			if err != nil {
				t.Fatalf("failed to create chain: %v", err)
			}
			defer chain.Stop()

			h, err := newHandler(&handlerConfig{
				Database:   db,
				Chain:      chain,
				TxPool:     newTestTxPool(),
				Network:    1,
				Sync:       downloader.FullSync,
				BloomCache: 1,
			})
			if err != nil {
				t.Fatalf("failed to create handler: %v", err)
			}
			if have := h.snapSync.Load(); have != tt.snap {
				t.Fatalf("snap sync mismatch: have %v, want %v", have, tt.snap)
			}
		})
	}
}
