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

package core

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/history"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/params"
)

func openCutoffTestDB(t *testing.T, datadir string) ethdb.Database {
	t.Helper()
	pdb, err := pebble.New(datadir, 0, 0, "", false)
	if err != nil {
		t.Fatalf("failed to create key-value database: %v", err)
	}
	db, err := rawdb.Open(pdb, rawdb.OpenOptions{Ancient: filepath.Join(datadir, "ancient")})
	if err != nil {
		t.Fatalf("failed to create freezer database: %v", err)
	}
	return db
}

func cutoffTestConfig(cutoff *history.PrunePoint) *BlockChainConfig {
	cfg := DefaultConfig()
	cfg.SnapshotLimit = 0
	cfg.TxLookupLimit = -1
	cfg.HistoryCutoff = cutoff
	return cfg
}

// Tests the snap sync path with a history cutoff: headers below the cutoff go
// into the ancient store without bodies or receipts, and blocks from the
// cutoff on are imported with receipts as usual, with correct total difficulty.
func TestInsertHeadersBeforeCutoff(t *testing.T) {
	const (
		total  = 64
		cutoff = 32 // first block that keeps its body and receipts
	)
	gspec := &Genesis{BaseFee: big.NewInt(params.InitialBaseFee), Config: params.AllEthashProtocolChanges}
	_, blocks, receipts := GenerateChainWithGenesis(gspec, ethash.NewFaker(), total, func(i int, b *BlockGen) {})

	// Reference chain with full history, for expected total difficulties.
	ref, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), cutoffTestConfig(nil))
	if err != nil {
		t.Fatalf("failed to create reference chain: %v", err)
	}
	defer ref.Stop()
	if _, err := ref.InsertChain(blocks); err != nil {
		t.Fatalf("failed to import reference chain: %v", err)
	}

	datadir := t.TempDir()
	db := openCutoffTestDB(t, datadir)
	point := &history.PrunePoint{BlockNumber: cutoff, BlockHash: blocks[cutoff-1].Hash()}
	chain, err := NewBlockChain(db, gspec, ethash.NewFaker(), cutoffTestConfig(point))
	if err != nil {
		t.Fatalf("failed to create chain with cutoff: %v", err)
	}
	if n, h := chain.HistoryPruningCutoff(); n != cutoff || h != point.BlockHash {
		t.Fatalf("history cutoff mismatch: have %d %x, want %d %x", n, h, cutoff, point.BlockHash)
	}

	headers := make([]*types.Header, total)
	for i, b := range blocks {
		headers[i] = b.Header()
	}
	// Blocks 1..cutoff-1: headers only, in two batches like the downloader does.
	if _, err := chain.InsertHeadersBeforeCutoff(headers[:10]); err != nil {
		t.Fatalf("failed to insert first header batch before cutoff: %v", err)
	}
	if _, err := chain.InsertHeadersBeforeCutoff(headers[10 : cutoff-1]); err != nil {
		t.Fatalf("failed to insert second header batch before cutoff: %v", err)
	}
	if head := chain.CurrentHeader().Number.Uint64(); head != cutoff-1 {
		t.Fatalf("current header mismatch: have %d, want %d", head, cutoff-1)
	}
	if tail, _ := db.Tail(); tail != cutoff {
		t.Fatalf("freezer tail mismatch: have %d, want %d", tail, cutoff)
	}
	for _, n := range []uint64{1, 10, cutoff - 1} {
		if h := chain.GetHeaderByNumber(n); h == nil || h.Hash() != blocks[n-1].Hash() {
			t.Fatalf("header %d missing or wrong below cutoff", n)
		}
		if body := rawdb.ReadBody(db, blocks[n-1].Hash(), n); body != nil {
			t.Fatalf("body %d present below cutoff", n)
		}
	}
	// The last pre-cutoff header keeps its total difficulty for the next block.
	if have, want := chain.GetTd(blocks[cutoff-2].Hash(), cutoff-1), ref.GetTd(blocks[cutoff-2].Hash(), cutoff-1); have == nil || have.Cmp(want) != 0 {
		t.Fatalf("total difficulty at cutoff-1 mismatch: have %v, want %v", have, want)
	}

	// Blocks from the cutoff on: headers, then bodies and receipts.
	if _, err := chain.InsertHeaderChain(headers[cutoff-1:]); err != nil {
		t.Fatalf("failed to insert headers from cutoff: %v", err)
	}
	if _, err := chain.InsertReceiptChain(blocks[cutoff-1:], types.EncodeBlockReceiptLists(receipts[cutoff-1:]), cutoff); err != nil {
		t.Fatalf("failed to insert receipt chain from cutoff: %v", err)
	}
	if head := chain.CurrentSnapBlock().Number.Uint64(); head != total {
		t.Fatalf("snap head mismatch: have %d, want %d", head, total)
	}
	for _, n := range []uint64{cutoff, cutoff + 5, total} {
		if have, want := chain.GetTd(blocks[n-1].Hash(), n), ref.GetTd(blocks[n-1].Hash(), n); have == nil || have.Cmp(want) != 0 {
			t.Fatalf("total difficulty %d mismatch: have %v, want %v", n, have, want)
		}
		if body := rawdb.ReadBody(db, blocks[n-1].Hash(), n); body == nil {
			t.Fatalf("body %d missing at or above cutoff", n)
		}
	}
	chain.Stop()
	db.Close()

	// Reopening with the same cutoff works; without it the pruned tail is rejected.
	db = openCutoffTestDB(t, datadir)
	defer db.Close()
	if reopened, err := NewBlockChain(db, gspec, ethash.NewFaker(), cutoffTestConfig(point)); err != nil {
		t.Fatalf("failed to reopen chain with cutoff: %v", err)
	} else {
		reopened.Stop()
	}
	if _, err := NewBlockChain(db, gspec, ethash.NewFaker(), cutoffTestConfig(nil)); err == nil {
		t.Fatal("expected reopening a pruned database without the cutoff to fail")
	}
}

// Tests that a history cutoff is refused on a database that already holds
// unpruned chain history, instead of silently mixing the two layouts.
func TestHistoryCutoffRejectsUnprunedDatabase(t *testing.T) {
	gspec := &Genesis{BaseFee: big.NewInt(params.InitialBaseFee), Config: params.AllEthashProtocolChanges}
	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 8, func(i int, b *BlockGen) {})

	datadir := t.TempDir()
	db := openCutoffTestDB(t, datadir)
	chain, err := NewBlockChain(db, gspec, ethash.NewFaker(), cutoffTestConfig(nil))
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to import chain: %v", err)
	}
	chain.Stop()

	point := &history.PrunePoint{BlockNumber: 4, BlockHash: blocks[3].Hash()}
	if _, err := NewBlockChain(db, gspec, ethash.NewFaker(), cutoffTestConfig(point)); err == nil {
		t.Fatal("expected a history cutoff on an unpruned database to be rejected")
	}
	db.Close()
}
