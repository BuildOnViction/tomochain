// Copyright 2016 The go-ethereum Authors
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

package les

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"math/big"
	"path/filepath"

	"github.com/tomochain/tomochain/accounts"
	"github.com/tomochain/tomochain/common"
	"github.com/tomochain/tomochain/common/math"
	"github.com/tomochain/tomochain/consensus"
	"github.com/tomochain/tomochain/core"
	"github.com/tomochain/tomochain/core/bloombits"
	"github.com/tomochain/tomochain/core/rawdb"
	"github.com/tomochain/tomochain/core/state"
	"github.com/tomochain/tomochain/core/types"
	"github.com/tomochain/tomochain/core/vm"
	"github.com/tomochain/tomochain/eth/downloader"
	"github.com/tomochain/tomochain/eth/gasprice"
	"github.com/tomochain/tomochain/ethclient"
	"github.com/tomochain/tomochain/ethdb"
	"github.com/tomochain/tomochain/event"
	"github.com/tomochain/tomochain/light"
	"github.com/tomochain/tomochain/params"
	"github.com/tomochain/tomochain/rpc"
	"github.com/tomochain/tomochain/tomox"
	"github.com/tomochain/tomochain/tomox/tradingstate"
	"github.com/tomochain/tomochain/tomoxlending"
)

type LesApiBackend struct {
	eth *LightEthereum
	gpo *gasprice.Oracle
}

func (b *LesApiBackend) ChainConfig() *params.ChainConfig {
	return b.eth.chainConfig
}

func (b *LesApiBackend) CurrentBlock() *types.Block {
	return types.NewBlockWithHeader(b.eth.BlockChain().CurrentHeader())
}

func (b *LesApiBackend) SetHead(number uint64) {
	b.eth.protocolManager.downloader.Cancel()
	b.eth.blockchain.SetHead(number)
}

func (b *LesApiBackend) HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error) {
	if blockNr == rpc.LatestBlockNumber || blockNr == rpc.PendingBlockNumber {
		return b.eth.blockchain.CurrentHeader(), nil
	}

	return b.eth.blockchain.GetHeaderByNumberOdr(ctx, uint64(blockNr))
}

func (b *LesApiBackend) BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	header, err := b.HeaderByNumber(ctx, blockNr)
	if header == nil || err != nil {
		return nil, err
	}
	return b.GetBlock(ctx, header.Hash())
}

func (b *LesApiBackend) StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	header, err := b.HeaderByNumber(ctx, blockNr)
	if header == nil || err != nil {
		return nil, nil, err
	}
	return light.NewState(ctx, header, b.eth.odr), header, nil
}

func (b *LesApiBackend) GetBlock(ctx context.Context, blockHash common.Hash) (*types.Block, error) {
	return b.eth.blockchain.GetBlockByHash(ctx, blockHash)
}

func (b *LesApiBackend) GetReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
	return light.GetBlockReceipts(ctx, b.eth.odr, blockHash, rawdb.GetBlockNumber(b.eth.chainDb, blockHash), b.ChainConfig())
}

func (b *LesApiBackend) GetLogs(ctx context.Context, blockHash common.Hash) ([][]*types.Log, error) {
	return light.GetBlockLogs(ctx, b.eth.odr, blockHash, rawdb.GetBlockNumber(b.eth.chainDb, blockHash), b.ChainConfig())
}

func (b *LesApiBackend) GetTd(blockHash common.Hash) *big.Int {
	return b.eth.blockchain.GetTdByHash(blockHash)
}

func (b *LesApiBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, tomoxState *tradingstate.TradingStateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error) {
	state.SetBalance(msg.From(), math.MaxBig256)
	context := core.NewEVMContext(msg, header, b.eth.blockchain, nil)
	return vm.NewEVM(context, state, tomoxState, b.eth.chainConfig, vmCfg), state.Error, nil
}

func (b *LesApiBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return b.eth.txPool.Add(ctx, signedTx)
}
func (b *LesApiBackend) SendOrderTx(ctx context.Context, signedTx *types.OrderTransaction) error {
	return nil
}
func (b *LesApiBackend) SendLendingTx(ctx context.Context, signedTx *types.LendingTransaction) error {
	return nil
}

func (b *LesApiBackend) RemoveTx(txHash common.Hash) {
	b.eth.txPool.RemoveTx(txHash)
}

func (b *LesApiBackend) GetPoolTransactions() (types.Transactions, error) {
	return b.eth.txPool.GetTransactions()
}

func (b *LesApiBackend) GetPoolTransaction(txHash common.Hash) *types.Transaction {
	return b.eth.txPool.GetTransaction(txHash)
}

func (b *LesApiBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.eth.txPool.GetNonce(ctx, addr)
}

func (b *LesApiBackend) Stats() (pending int, queued int) {
	return b.eth.txPool.Stats(), 0
}

func (b *LesApiBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.eth.txPool.Content()
}

func (b *LesApiBackend) OrderTxPoolContent() (map[common.Address]types.OrderTransactions, map[common.Address]types.OrderTransactions) {
	return make(map[common.Address]types.OrderTransactions), make(map[common.Address]types.OrderTransactions)
}
func (b *LesApiBackend) OrderStats() (pending int, queued int) {
	return 0, 0
}

func (b *LesApiBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.eth.txPool.SubscribeNewTxsEvent(ch)
}

func (b *LesApiBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.eth.blockchain.SubscribeChainEvent(ch)
}

func (b *LesApiBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.eth.blockchain.SubscribeChainHeadEvent(ch)
}

func (b *LesApiBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.eth.blockchain.SubscribeChainSideEvent(ch)
}

func (b *LesApiBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.eth.blockchain.SubscribeLogsEvent(ch)
}

func (b *LesApiBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.eth.blockchain.SubscribeRemovedLogsEvent(ch)
}

func (b *LesApiBackend) Downloader() *downloader.Downloader {
	return b.eth.Downloader()
}

func (b *LesApiBackend) ProtocolVersion() int {
	return b.eth.LesVersion() + 10000
}

func (b *LesApiBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestPrice(ctx)
}

func (b *LesApiBackend) ChainDb() ethdb.Database {
	return b.eth.chainDb
}

func (b *LesApiBackend) EventMux() *event.TypeMux {
	return b.eth.eventMux
}

func (b *LesApiBackend) AccountManager() *accounts.Manager {
	return b.eth.accountManager
}

func (b *LesApiBackend) BloomStatus() (uint64, uint64) {
	if b.eth.bloomIndexer == nil {
		return 0, 0
	}
	sections, _, _ := b.eth.bloomIndexer.Sections()
	return light.BloomTrieFrequency, sections
}

func (b *LesApiBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.eth.bloomRequests)
	}
}

func (b *LesApiBackend) GetIPCClient() (*ethclient.Client, error) {
	return nil, nil
}

func (b *LesApiBackend) GetEngine() consensus.Engine {
	return b.eth.engine
}
func (s *LesApiBackend) GetRewardByHash(hash common.Hash) map[string]map[string]map[string]*big.Int {
	header := s.eth.blockchain.GetHeaderByHash(hash)
	if header != nil {
		data, err := ioutil.ReadFile(filepath.Join(common.StoreRewardFolder, header.Number.String()+"."+header.Hash().Hex()))
		if err == nil {
			rewards := make(map[string]map[string]map[string]*big.Int)
			err = json.Unmarshal(data, &rewards)
			if err == nil {
				return rewards
			}
		} else {
			data, err = ioutil.ReadFile(filepath.Join(common.StoreRewardFolder, header.Number.String()+"."+header.HashNoValidator().Hex()))
			if err == nil {
				rewards := make(map[string]map[string]map[string]*big.Int)
				err = json.Unmarshal(data, &rewards)
				if err == nil {
					return rewards
				}
			}
		}
	}
	return make(map[string]map[string]map[string]*big.Int)
}

// GetVotersRewards return a map of voters of snapshot at given block hash
func (b *LesApiBackend) GetVotersRewards(masternodeAddr common.Address) map[common.Address]*big.Int {
	return map[common.Address]*big.Int{}
}

// GetVotersCap return all voters's capability at a checkpoint
func (b *LesApiBackend) GetVotersCap(checkpoint *big.Int, masterAddr common.Address, voters []common.Address) map[common.Address]*big.Int {
	return map[common.Address]*big.Int{}
}

func (b *LesApiBackend) GetEpochDuration() *big.Int {
	return nil
}

// GetMasternodesCap return a cap of all masternode at a checkpoint
func (b *LesApiBackend) GetMasternodesCap(checkpoint uint64) map[common.Address]*big.Int {
	return nil
}

func (b *LesApiBackend) GetBlocksHashCache(blockNr uint64) []common.Hash {
	return []common.Hash{}
}

func (b *LesApiBackend) AreTwoBlockSamePath(bh1 common.Hash, bh2 common.Hash) bool {
	return true
}

// GetOrderNonce get order nonce
func (b *LesApiBackend) GetOrderNonce(address common.Hash) (uint64, error) {
	return 0, errors.New("cannot find tomox service")
}

func (b *LesApiBackend) TomoxService() *tomox.TomoX {
	return nil
}

func (b *LesApiBackend) LendingService() *tomoxlending.Lending {
	return nil
}
