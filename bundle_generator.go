package main

import (
	"math/big"
	"math/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

type BundleGenerator struct {
	client *Client
	rpc    *rpc.Client
}

func NewBundleGenerator(client *Client, rpc *rpc.Client) *BundleGenerator {
	return &BundleGenerator{
		client: client,
		rpc:    rpc,
	}
}

func (bg *BundleGenerator) GenerateBundle(size int, blockNumber uint64) (*types.SBundle, error) {
	fundBalance := new(big.Int)
	fundBalance.Exp(big.NewInt(10), big.NewInt(18), nil)
	txs := make(types.Transactions, size)
	for i := 0; i < size; i++ {
		from := generatePrivKey()
		err := fundAccount(bg.client, from.Address(), fundBalance)
		if err != nil {
			return nil, err
		}
		to := generatePrivKey().Address()
		client := NewClient(bg.rpc, from, common.Address{})
		value := rand.Int63n(100000)
		if err != nil {
			return nil, err
		}

		tx, err := SignTxnWithNonce(client, &types.LegacyTx{
			To:    &to,
			Value: big.NewInt(value),
		})
		if err != nil {
			return nil, err
		}
		txs[i] = tx
	}

	bundle := &types.SBundle{
		BlockNumber: big.NewInt(int64(blockNumber)),
		Txs:         txs,
	}
	return bundle, nil
}
