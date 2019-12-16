// Copyright 2014 The go-ethereum Authors
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

package lending_state

import (
	"fmt"
	"github.com/tomochain/tomochain/common"
	"github.com/tomochain/tomochain/log"
	"github.com/tomochain/tomochain/rlp"
	"github.com/tomochain/tomochain/tomox/database"
	"io"
	"math/big"
)

//
// The usage pattern is as follows:
// First you need to obtain a state object.
// lendingObject values can be accessed and modified through the object.
// Finally, call CommitLendingTrie to write the modified storage trie into a database.
type lendingExchangeState struct {
	lendingBook common.Hash
	data        lendingObject
	db          *LendingStateDB

	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by LendingStateDB.Commit.
	dbErr error

	investingTrie       database.Trie
	borrowingTrie       database.Trie
	lendingTrie         database.Trie
	liquidationTimeTrie database.Trie

	liquidationTimeStates      map[common.Hash]*liquidationTimeState
	liquidationTimestatesDirty map[common.Hash]struct{}

	investingStates      map[common.Hash]*itemListState
	investingStatesDirty map[common.Hash]struct{}

	borrowingStates      map[common.Hash]*itemListState
	borrowingStatesDirty map[common.Hash]struct{}

	lendingItemStates      map[common.Hash]*lendingItemState
	lendingItemStatesDirty map[common.Hash]struct{}

	onDirty func(hash common.Hash) // Callback method to mark a state object newly dirty
}

// empty returns whether the orderId is considered empty.
func (s *lendingExchangeState) empty() bool {
	return s.data.Nonce == 0 && common.EmptyHash(s.data.InvestingRoot) && common.EmptyHash(s.data.BorrowingRoot)
}

// newObject creates a state object.
func newStateExchanges(db *LendingStateDB, hash common.Hash, data lendingObject, onDirty func(addr common.Hash)) *lendingExchangeState {
	return &lendingExchangeState{
		db:                         db,
		lendingBook:                hash,
		data:                       data,
		investingStates:            make(map[common.Hash]*itemListState),
		borrowingStates:            make(map[common.Hash]*itemListState),
		lendingItemStates:          make(map[common.Hash]*lendingItemState),
		liquidationTimeStates:      make(map[common.Hash]*liquidationTimeState),
		investingStatesDirty:       make(map[common.Hash]struct{}),
		borrowingStatesDirty:       make(map[common.Hash]struct{}),
		lendingItemStatesDirty:     make(map[common.Hash]struct{}),
		liquidationTimestatesDirty: make(map[common.Hash]struct{}),
		onDirty:                    onDirty,
	}
}

// EncodeRLP implements rlp.Encoder.
func (self *lendingExchangeState) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, self.data)
}

// setError remembers the first non-nil error it is called with.
func (self *lendingExchangeState) setError(err error) {
	if self.dbErr == nil {
		self.dbErr = err
	}
}

/**
  Get Trie
*/

func (self *lendingExchangeState) getLendingItemTrie(db database.Database) database.Trie {
	if self.lendingTrie == nil {
		var err error
		self.lendingTrie, err = db.OpenStorageTrie(self.lendingBook, self.data.LendingItemRoot)
		if err != nil {
			self.lendingTrie, _ = db.OpenStorageTrie(self.lendingBook, EmptyHash)
			self.setError(fmt.Errorf("can't create Lendings trie: %v", err))
		}
	}
	return self.lendingTrie
}
func (self *lendingExchangeState) getInvestingTrie(db database.Database) database.Trie {
	if self.investingTrie == nil {
		var err error
		self.investingTrie, err = db.OpenStorageTrie(self.lendingBook, self.data.InvestingRoot)
		if err != nil {
			self.investingTrie, _ = db.OpenStorageTrie(self.lendingBook, EmptyHash)
			self.setError(fmt.Errorf("can't create Lendings trie: %v", err))
		}
	}
	return self.investingTrie
}

func (self *lendingExchangeState) getBorrowingTrie(db database.Database) database.Trie {
	if self.borrowingTrie == nil {
		var err error
		self.borrowingTrie, err = db.OpenStorageTrie(self.lendingBook, self.data.BorrowingRoot)
		if err != nil {
			self.borrowingTrie, _ = db.OpenStorageTrie(self.lendingBook, EmptyHash)
			self.setError(fmt.Errorf("can't create bids trie: %v", err))
		}
	}
	return self.borrowingTrie
}

func (self *lendingExchangeState) getLiquidationTimeTrie(db database.Database) database.Trie {
	if self.liquidationTimeTrie == nil {
		var err error
		self.liquidationTimeTrie, err = db.OpenStorageTrie(self.lendingBook, self.data.LiquidationTimeRoot)
		if err != nil {
			self.liquidationTimeTrie, _ = db.OpenStorageTrie(self.lendingBook, EmptyHash)
			self.setError(fmt.Errorf("can't create bids trie: %v", err))
		}
	}
	return self.liquidationTimeTrie
}

/**
  Get State
*/
func (self *lendingExchangeState) getBorrowingOrderList(db database.Database, rate common.Hash) (stateOrderList *itemListState) {
	// Prefer 'live' objects.
	if obj := self.borrowingStates[rate]; obj != nil {
		return obj
	}

	// Load the object from the database.
	enc, err := self.getBorrowingTrie(db).TryGet(rate[:])
	if len(enc) == 0 {
		self.setError(err)
		return nil
	}
	var data itemList
	if err := rlp.DecodeBytes(enc, &data); err != nil {
		log.Error("Failed to decode state order list object", "rate", rate, "err", err)
		return nil
	}
	// Insert into the live set.
	obj := newItemListState(self.db, BORROWING, self.lendingBook, rate, data, self.MarkBorrowingDirty)
	self.borrowingStates[rate] = obj
	return obj
}

func (self *lendingExchangeState) getInvestingOrderList(db database.Database, rate common.Hash) (stateOrderList *itemListState) {
	// Prefer 'live' objects.
	if obj := self.investingStates[rate]; obj != nil {
		return obj
	}

	// Load the object from the database.
	enc, err := self.getInvestingTrie(db).TryGet(rate[:])
	if len(enc) == 0 {
		self.setError(err)
		return nil
	}
	var data itemList
	if err := rlp.DecodeBytes(enc, &data); err != nil {
		log.Error("Failed to decode state order list object", "rate", rate, "err", err)
		return nil
	}
	// Insert into the live set.
	obj := newItemListState(self.db, INVESTING, self.lendingBook, rate, data, self.MarkBorrowingDirty)
	self.investingStates[rate] = obj
	return obj
}

func (self *lendingExchangeState) getLiquidationTimeOrderList(db database.Database, time common.Hash) (stateObject *liquidationTimeState) {
	// Prefer 'live' objects.
	if obj := self.liquidationTimeStates[time]; obj != nil {
		return obj
	}

	// Load the object from the database.
	enc, err := self.getLiquidationTimeTrie(db).TryGet(time[:])
	if len(enc) == 0 {
		self.setError(err)
		return nil
	}
	var data itemList
	if err := rlp.DecodeBytes(enc, &data); err != nil {
		log.Error("Failed to decode state liquidation time", "time", time, "err", err)
		return nil
	}
	// Insert into the live set.
	obj := newLiquidationTimeState(self.db, self.lendingBook, time, data, self.MarkInvestingDirty)
	self.liquidationTimeStates[time] = obj
	return obj
}

func (self *lendingExchangeState) getLendingItem(db database.Database, lendingId common.Hash) (stateObject *lendingItemState) {
	// Prefer 'live' objects.
	if obj := self.lendingItemStates[lendingId]; obj != nil {
		return obj
	}

	// Load the object from the database.
	enc, err := self.getLendingItemTrie(db).TryGet(lendingId[:])
	if len(enc) == 0 {
		self.setError(err)
		return nil
	}
	var data LendingItem
	if err := rlp.DecodeBytes(enc, &data); err != nil {
		log.Error("Failed to decode state lending item", "lendingId", lendingId, "err", err)
		return nil
	}
	// Insert into the live set.
	obj := newLendinItemState(self.lendingBook, lendingId, data, self.MarkLendingItemDirty)
	self.lendingItemStates[lendingId] = obj
	return obj
}

/**
  Update Trie
*/
func (self *lendingExchangeState) updateLendingTimeTrie(db database.Database) database.Trie {
	tr := self.getLendingItemTrie(db)
	for lendingId, lendingItem := range self.lendingItemStates {
		if _, isDirty := self.lendingItemStatesDirty[lendingId]; isDirty {
			delete(self.lendingItemStatesDirty, lendingId)
			if (lendingItem.empty()) {
				self.setError(tr.TryDelete(lendingId[:]))
				continue
			}
			// Encoding []byte cannot fail, ok to ignore the error.
			v, _ := rlp.EncodeToBytes(lendingItem)
			self.setError(tr.TryUpdate(lendingId[:], v))
		}
	}
	return tr
}

func (self *lendingExchangeState) updateBorrowingTrie(db database.Database) database.Trie {
	tr := self.getBorrowingTrie(db)
	for rate, orderList := range self.borrowingStates {
		if _, isDirty := self.borrowingStatesDirty[rate]; isDirty {
			delete(self.borrowingStatesDirty, rate)
			if (orderList.empty()) {
				self.setError(tr.TryDelete(rate[:]))
				continue
			}
			orderList.updateRoot(db)
			// Encoding []byte cannot fail, ok to ignore the error.
			v, _ := rlp.EncodeToBytes(orderList)
			self.setError(tr.TryUpdate(rate[:], v))
		}
	}
	return tr
}

func (self *lendingExchangeState) updateInvestingTrie(db database.Database) database.Trie {
	tr := self.getInvestingTrie(db)
	for rate, orderList := range self.investingStates {
		if _, isDirty := self.investingStatesDirty[rate]; isDirty {
			delete(self.investingStatesDirty, rate)
			if (orderList.empty()) {
				self.setError(tr.TryDelete(rate[:]))
				continue
			}
			orderList.updateRoot(db)
			// Encoding []byte cannot fail, ok to ignore the error.
			v, _ := rlp.EncodeToBytes(orderList)
			self.setError(tr.TryUpdate(rate[:], v))
		}
	}
	return tr
}

func (self *lendingExchangeState) updateLiquidationTimeTrie(db database.Database) database.Trie {
	tr := self.getLiquidationTimeTrie(db)
	for time, itemList := range self.liquidationTimeStates {
		if _, isDirty := self.liquidationTimestatesDirty[time]; isDirty {
			delete(self.liquidationTimestatesDirty, time)
			if (itemList.empty()) {
				self.setError(tr.TryDelete(time[:]))
				continue
			}
			itemList.updateRoot(db)
			// Encoding []byte cannot fail, ok to ignore the error.
			v, _ := rlp.EncodeToBytes(itemList)
			self.setError(tr.TryUpdate(time[:], v))
		}
	}
	return tr
}

/**
  Update Root
*/

func (self *lendingExchangeState) updateOrderRoot(db database.Database) {
	self.updateLendingTimeTrie(db)
	self.data.LendingItemRoot = self.lendingTrie.Hash()
}

func (self *lendingExchangeState) updateInvestingRoot(db database.Database) error {
	self.updateInvestingTrie(db)
	if self.dbErr != nil {
		return self.dbErr
	}
	self.data.InvestingRoot = self.investingTrie.Hash()
	return nil
}

func (self *lendingExchangeState) updateBorrowingRoot(db database.Database) {
	self.updateBorrowingTrie(db)
	self.data.BorrowingRoot = self.borrowingTrie.Hash()
}

func (self *lendingExchangeState) updateLiquidationTimeRoot(db database.Database) {
	self.updateLiquidationTimeTrie(db)
	self.data.LiquidationTimeRoot = self.liquidationTimeTrie.Hash()
}

/**
  Commit Trie
*/

func (self *lendingExchangeState) CommitLendingItemTrie(db database.Database) error {
	self.updateLendingTimeTrie(db)
	if self.dbErr != nil {
		return self.dbErr
	}
	root, err := self.lendingTrie.Commit(nil)
	if err == nil {
		self.data.LendingItemRoot = root
	}
	return err
}

func (self *lendingExchangeState) CommitInvestingTrie(db database.Database) error {
	self.updateInvestingTrie(db)
	if self.dbErr != nil {
		return self.dbErr
	}
	root, err := self.investingTrie.Commit(func(leaf []byte, parent common.Hash) error {
		var orderList itemList
		if err := rlp.DecodeBytes(leaf, &orderList); err != nil {
			return nil
		}
		if orderList.Root != EmptyRoot {
			db.TrieDB().Reference(orderList.Root, parent)
		}
		return nil
	})
	if err == nil {
		self.data.InvestingRoot = root
	}
	return err
}

func (self *lendingExchangeState) CommitBorrowingTrie(db database.Database) error {
	self.updateBorrowingTrie(db)
	if self.dbErr != nil {
		return self.dbErr
	}
	root, err := self.borrowingTrie.Commit(func(leaf []byte, parent common.Hash) error {
		var orderList itemList
		if err := rlp.DecodeBytes(leaf, &orderList); err != nil {
			return nil
		}
		if orderList.Root != EmptyRoot {
			db.TrieDB().Reference(orderList.Root, parent)
		}
		return nil
	})
	if err == nil {
		self.data.BorrowingRoot = root
	}
	return err
}

func (self *lendingExchangeState) CommitLiquidationTimeTrie(db database.Database) error {
	self.updateLiquidationTimeTrie(db)
	if self.dbErr != nil {
		return self.dbErr
	}
	root, err := self.liquidationTimeTrie.Commit(func(leaf []byte, parent common.Hash) error {
		var orderList itemList
		if err := rlp.DecodeBytes(leaf, &orderList); err != nil {
			return nil
		}
		if orderList.Root != EmptyRoot {
			db.TrieDB().Reference(orderList.Root, parent)
		}
		return nil
	})
	if err == nil {
		self.data.LiquidationTimeRoot = root
	}
	return err
}

/**
  Get Trie Data
*/
func (self *lendingExchangeState) getBestInvestingRate(db database.Database) common.Hash {
	trie := self.getInvestingTrie(db)
	encKey, encValue, err := trie.TryGetBestLeftKeyAndValue()
	if err != nil {
		log.Error("Failed find best investing rate", "orderbook", self.lendingBook.Hex())
		return EmptyHash
	}
	if len(encKey) == 0 || len(encValue) == 0 {
		log.Debug("Not found get best investing rate", "encKey", encKey, "encValue", encValue)
		return EmptyHash
	}
	var data itemList
	if err := rlp.DecodeBytes(encValue, &data); err != nil {
		log.Error("Failed to decode state get best investing rate", "err", err)
		return EmptyHash
	}
	return common.BytesToHash(encKey)
}

func (self *lendingExchangeState) getBestBorrowingRate(db database.Database) common.Hash {
	trie := self.getBorrowingTrie(db)
	encKey, encValue, err := trie.TryGetBestRightKeyAndValue()
	if err != nil {
		log.Error("Failed find best price bid trie ", "orderbook", self.lendingBook.Hex())
		return EmptyHash
	}
	if len(encKey) == 0 || len(encValue) == 0 {
		log.Debug("Not found get best bid trie", "encKey", encKey, "encValue", encValue)
		return EmptyHash
	}
	var data itemList
	if err := rlp.DecodeBytes(encValue, &data); err != nil {
		log.Error("Failed to decode state get best bid trie", "err", err)
		return EmptyHash
	}
	return common.BytesToHash(encKey)
}

func (self *lendingExchangeState) getLowestLiquidationTime(db database.Database) (common.Hash, *liquidationTimeState) {
	trie := self.getLiquidationTimeTrie(db)
	encKey, encValue, err := trie.TryGetBestLeftKeyAndValue()
	if err != nil {
		log.Error("Failed find best liquidation time trie ", "lendingBook", self.lendingBook.Hex())
		return EmptyHash, nil
	}
	if len(encKey) == 0 || len(encValue) == 0 {
		log.Debug("Not found get liquidation time trie", "encKey", encKey, "encValue", encValue)
		return EmptyHash, nil
	}
	var data itemList
	if err := rlp.DecodeBytes(encValue, &data); err != nil {
		log.Error("Failed to decode state get liquidation time trie", "err", err)
		return EmptyHash, nil
	}
	price := common.BytesToHash(encKey)
	obj := newLiquidationTimeState(self.db, self.lendingBook, price, data, self.MarkLiquidationTimeDirty)
	self.liquidationTimeStates[price] = obj
	return price, obj
}

func (self *lendingExchangeState) deepCopy(db *LendingStateDB, onDirty func(hash common.Hash)) *lendingExchangeState {
	stateExchanges := newStateExchanges(db, self.lendingBook, self.data, onDirty)
	if self.investingTrie != nil {
		stateExchanges.investingTrie = db.db.CopyTrie(self.investingTrie)
	}
	if self.borrowingTrie != nil {
		stateExchanges.borrowingTrie = db.db.CopyTrie(self.borrowingTrie)
	}
	if self.lendingTrie != nil {
		stateExchanges.lendingTrie = db.db.CopyTrie(self.lendingTrie)
	}
	for key, value := range self.borrowingStates {
		stateExchanges.borrowingStates[key] = value.deepCopy(db, self.MarkBorrowingDirty)
	}
	for key, _ := range self.borrowingStatesDirty {
		stateExchanges.borrowingStatesDirty[key] = struct{}{}
	}
	for key, value := range self.investingStates {
		stateExchanges.investingStates[key] = value.deepCopy(db, self.MarkInvestingDirty)
	}
	for key, _ := range self.investingStatesDirty {
		stateExchanges.investingStatesDirty[key] = struct{}{}
	}
	for key, value := range self.lendingItemStates {
		stateExchanges.lendingItemStates[key] = value.deepCopy(self.MarkLendingItemDirty)
	}
	for orderId, _ := range self.lendingItemStatesDirty {
		stateExchanges.lendingItemStatesDirty[orderId] = struct{}{}
	}
	for time, orderList := range self.liquidationTimeStates {
		stateExchanges.liquidationTimeStates[time] = orderList.deepCopy(db, self.MarkLiquidationTimeDirty)
	}
	for time, _ := range self.liquidationTimestatesDirty {
		stateExchanges.liquidationTimestatesDirty[time] = struct{}{}
	}
	return stateExchanges
}

// Returns the address of the contract/orderId
func (self *lendingExchangeState) Hash() common.Hash {
	return self.lendingBook
}

func (self *lendingExchangeState) SetNonce(nonce uint64) {
	self.setNonce(nonce)
}

func (self *lendingExchangeState) setNonce(nonce uint64) {
	self.data.Nonce = nonce
	if self.onDirty != nil {
		self.onDirty(self.Hash())
		self.onDirty = nil
	}
}

func (self *lendingExchangeState) Nonce() uint64 {
	return self.data.Nonce
}

func (self *lendingExchangeState) removeInvestingOrderList(db database.Database, stateOrderList *itemListState) {
	self.setError(self.investingTrie.TryDelete(stateOrderList.price[:]))
}

func (self *lendingExchangeState) removeBorrowingOrderList(db database.Database, stateOrderList *itemListState) {
	self.setError(self.borrowingTrie.TryDelete(stateOrderList.price[:]))
}

func (self *lendingExchangeState) createInvestingOrderList(db database.Database, price common.Hash) (newobj *itemListState) {
	newobj = newItemListState(self.db, INVESTING, self.lendingBook, price, itemList{}, self.MarkInvestingDirty)
	self.investingStates[price] = newobj
	self.investingStatesDirty[price] = struct{}{}
	data, err := rlp.EncodeToBytes(newobj)
	if err != nil {
		panic(fmt.Errorf("can't encode order list object at %x: %v", price[:], err))
	}
	self.setError(self.investingTrie.TryUpdate(price[:], data))
	if self.onDirty != nil {
		self.onDirty(self.Hash())
		self.onDirty = nil
	}
	return newobj
}

func (self *lendingExchangeState) MarkBorrowingDirty(price common.Hash) {
	self.borrowingStatesDirty[price] = struct{}{}
	if self.onDirty != nil {
		self.onDirty(self.Hash())
		self.onDirty = nil
	}
}

func (self *lendingExchangeState) MarkInvestingDirty(price common.Hash) {
	self.investingStatesDirty[price] = struct{}{}
	if self.onDirty != nil {
		self.onDirty(self.Hash())
		self.onDirty = nil
	}
}

func (self *lendingExchangeState) MarkLendingItemDirty(orderId common.Hash) {
	self.lendingItemStatesDirty[orderId] = struct{}{}
	if self.onDirty != nil {
		self.onDirty(self.Hash())
		self.onDirty = nil
	}
}

func (self *lendingExchangeState) MarkLiquidationTimeDirty(orderId common.Hash) {
	self.liquidationTimestatesDirty[orderId] = struct{}{}
	if self.onDirty != nil {
		self.onDirty(self.Hash())
		self.onDirty = nil
	}
}

func (self *lendingExchangeState) createBorrowingOrderList(db database.Database, price common.Hash) (newobj *itemListState) {
	newobj = newItemListState(self.db, BORROWING, self.lendingBook, price, itemList{}, self.MarkBorrowingDirty)
	self.borrowingStates[price] = newobj
	self.borrowingStatesDirty[price] = struct{}{}
	data, err := rlp.EncodeToBytes(newobj)
	if err != nil {
		panic(fmt.Errorf("can't encode order list object at %x: %v", price[:], err))
	}
	self.setError(self.borrowingTrie.TryUpdate(price[:], data))
	if self.onDirty != nil {
		self.onDirty(self.Hash())
		self.onDirty = nil
	}
	return newobj
}

func (self *lendingExchangeState) createLendingItem(db database.Database, orderId common.Hash, order LendingItem) (newobj *lendingItemState) {
	newobj = newLendinItemState(self.lendingBook, orderId, order, self.MarkLendingItemDirty)
	orderIdHash := common.BigToHash(new(big.Int).SetUint64(order.OrderID))
	self.lendingItemStates[orderIdHash] = newobj
	self.lendingItemStatesDirty[orderIdHash] = struct{}{}
	if self.onDirty != nil {
		self.onDirty(self.lendingBook)
		self.onDirty = nil
	}
	return newobj
}

func (self *lendingExchangeState) createLiquidationTime(db database.Database, time common.Hash) (newobj *liquidationTimeState) {
	newobj = newLiquidationTimeState(self.db, time, self.lendingBook, itemList{}, self.MarkLendingItemDirty)
	self.liquidationTimeStates[time] = newobj
	self.lendingItemStatesDirty[time] = struct{}{}
	return newobj
}
