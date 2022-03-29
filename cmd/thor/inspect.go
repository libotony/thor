// Copyright (c) 2022 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/olekukonko/tablewriter"
	"github.com/syndtr/goleveldb/leveldb/util"
	"github.com/vechain/thor/chain"
	"github.com/vechain/thor/kv"
	"github.com/vechain/thor/metric"
	"github.com/vechain/thor/muxdb"
	"github.com/vechain/thor/state"
)

var (
	accountTriePrefix = state.AccountTrieName[0]
	storageTriePrefix = state.StorageTrieNamePrefix[0]
	indexTriePrefix   = chain.IndexTrieName[0]

	codeStorePrefix    = append([]byte{muxdb.NamedStoreSpace}, state.CodeStoreName...)
	dataStorePrefix    = append([]byte{muxdb.NamedStoreSpace}, chain.DataStoreName...)
	txIndexStorePrefix = append([]byte{muxdb.NamedStoreSpace}, chain.TxIndexStoreName...)
)

type status struct {
	size  metric.StorageSize
	count uint64
}

func (s *status) Add(size metric.StorageSize) {
	s.size += size
	s.count++
}

func (s *status) Size() string {
	return s.size.String()
}

func (s *status) Count() string {
	return fmt.Sprintf("%d", s.count)
}

func inspectMainDB(ctx context.Context, db *muxdb.MuxDB) error {
	count := 0
	limit := 5000

	var (
		total    status
		hist     status
		deduped  status
		leafBank status

		acc     status
		storage status
		index   status

		named   status
		codes   status
		chain   status
		indexer status

		unknown status
	)

	start := mclock.Now()
	last := mclock.Now()

	iter := db.NewIterator(kv.Range(*util.BytesPrefix([]byte(nil))))
	defer iter.Release()

	log.Info("Start inspecting database")
	for iter.Next() {
		count++
		if count > limit {
			count = 0
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		key := iter.Key()
		size := metric.StorageSize(len(key) + len(iter.Value()))

		switch key[0] {
		case muxdb.TrieHistSpace, muxdb.TrieDedupedSpace:
			switch key[5] {
			case accountTriePrefix:
				acc.Add(size)
			case storageTriePrefix:
				storage.Add(size)
			case indexTriePrefix:
				index.Add(size)
			default:
				panic("unknown trie name")
			}

			if key[0] == muxdb.TrieHistSpace {
				hist.Add(size)
			} else {
				deduped.Add(size)
			}
		case muxdb.TrieLeafBankSpace:
			leafBank.Add(size)
			switch key[2] {
			case accountTriePrefix:
				acc.Add(size)
			case storageTriePrefix:
				storage.Add(size)
			default:
				panic("unknown trie name")
			}
		case muxdb.NamedStoreSpace:
			named.Add(size)
			switch {
			case bytes.HasPrefix(key, codeStorePrefix):
				codes.Add(size)
			case bytes.HasPrefix(key, dataStorePrefix):
				chain.Add(size)
			case bytes.HasPrefix(key, txIndexStorePrefix):
				indexer.Add(size)
			}
		default:
			unknown.Add(size)
		}

		total.Add(size)
		now := mclock.Now()
		if total.count%1000 == 0 && time.Duration(now-last) > 10*time.Second {
			log.Info("Inspecting database", "count", total.Count(), "elapsed", common.PrettyDuration(now-start))
			last = now
		}
	}

	if err := iter.Error(); err != nil {
		return err
	}

	stats := [][]string{
		{"Trie Space", "History", "", hist.Size(), hist.Count()},
		{"Trie Space", "De-Duplicated", "", deduped.Size(), deduped.Count()},
		{"Trie Space", "Leaf Bank", "", leafBank.Size(), leafBank.Count()},
		{"Trie Space", "", "Account Trie", acc.Size(), acc.Count()},
		{"Trie Space", "", "Storage Trie", storage.Size(), storage.Count()},
		{"Trie Space", "", "Index Trie", acc.Size(), acc.Count()},
		{"General KV", "Store", "", named.Size(), named.Count()},
		{"General KV", "", "Code", codes.Size(), codes.Count()},
		{"General KV", "", "Block/TX/Receipt", chain.Size(), chain.Count()},
		{"General KV", "", "TX Meta", indexer.Size(), indexer.Count()},
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetColumnAlignment([]int{tablewriter.ALIGN_LEFT, tablewriter.ALIGN_LEFT, tablewriter.ALIGN_LEFT, tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_RIGHT})
	table.SetHeader([]string{"Category", "Name", "Subject", "Size", "Counts"})
	table.SetFooter([]string{"", "", "Total", total.Size(), total.Count()})
	table.AppendBulk(stats)
	table.SetAutoMergeCells(true)
	table.Render()

	return nil
}
