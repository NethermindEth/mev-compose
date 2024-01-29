package app

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/suave/sdk"
)

var (
	exNodeEthAddr = common.HexToAddress("b5feafbdd752ad52afb7e1bd2e40432a485bbb7f")
	exNodeNetAddr = "http://localhost:8545"

	// This account is funded in both devnev networks
	// address: 0xBE69d72ca5f88aCba033a063dF5DBe43a4148De0
	fundedAccount = newPrivKeyFromHex("91ab9a7e53c220e6210460b65a7a3bb2ca181412a8a7b43ff336b3df1737ce12")
)

var (
	bundleContract                 *Artifact
	ComposableBlockBuilderCtonract *Artifact
)

type step struct {
	name   string
	action func() error
}

type privKey struct {
	priv *ecdsa.PrivateKey
}

func (p *privKey) Address() common.Address {
	return crypto.PubkeyToAddress(p.priv.PublicKey)
}

func (p *privKey) MarshalPrivKey() []byte {
	return crypto.FromECDSA(p.priv)
}

func newPrivKeyFromHex(hex string) *privKey {
	key, err := crypto.HexToECDSA(hex)
	if err != nil {
		panic(fmt.Sprintf("failed to parse private key: %v", err))
	}
	return &privKey{priv: key}
}

func generatePrivKey() *privKey {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic(fmt.Sprintf("failed to generate private key: %v", err))
	}
	return &privKey{priv: key}
}

func fundAccount(clt *sdk.Client, to common.Address, value *big.Int) error {
	txn := &types.LegacyTx{
		Value: value,
		To:    &to,
	}
	result, err := clt.SendTransaction(txn)
	if err != nil {
		return err
	}
	_, err = result.Wait()
	if err != nil {
		return err
	}
	// check balance
	balance, err := clt.RPC().BalanceAt(context.Background(), to, nil)
	if err != nil {
		return err
	}
	if balance.Cmp(value) != 0 {
		return fmt.Errorf("failed to fund account")
	}
	return nil
}

func main() {
	rpc, _ := rpc.Dial(exNodeNetAddr)
	mevmClt := sdk.NewClient(rpc, fundedAccount.priv, exNodeEthAddr)

	fundBalance := big.NewInt(100000000)

	var (
		testAddr1 *privKey
		testAddr2 *privKey
	)

	var (
		ethTxn1       *types.Transaction
		ethTxnBackrun *types.Transaction
	)

	var mevShareContract *sdk.Contract

	steps := []step{
		{
			name: "Create and fund test accounts",
			action: func() error {
				testAddr1 = generatePrivKey()
				testAddr2 = generatePrivKey()

				if err := fundAccount(mevmClt, testAddr1.Address(), fundBalance); err != nil {
					return err
				}

				cltAcct1 := sdk.NewClient(rpc, testAddr1.priv, common.Address{})
				cltAcct2 := sdk.NewClient(rpc, testAddr2.priv, common.Address{})

				targeAddr := testAddr1.Address()

				ethTxn1, _ = cltAcct1.SignTxn(&types.LegacyTx{
					To:       &targeAddr,
					Value:    big.NewInt(1000),
					Gas:      21000,
					GasPrice: big.NewInt(13),
				})

				ethTxnBackrun, _ = cltAcct2.SignTxn(&types.LegacyTx{
					To:       &targeAddr,
					Value:    big.NewInt(1000),
					Gas:      21420,
					GasPrice: big.NewInt(13),
				})
				return nil
			},
		},
		{
			name: "Deploy contract",
			action: func() error {
				txnResult, err := sdk.DeployContract(MevShareBundleSenderContract.Code, mevmClt)

				if err != nil {
					return err
				}

				receipt, err := txnResult.Wait()
				if err != nil {
					return err
				}

				if receipt.Status == 0 {
					return fmt.Errorf("failed to deploy contract")
				}

				log.Info("Contract deployed", "address", receipt.ContractAddress.Hex())

				return nil
			},
		},
	}

	for i, step := range steps {
		log.Info("Running step", "step", i, "name", step.name)
		if err := step.action(); err != nil {
			log.Error("Failed to run step", "step", i, "name", step.name, "error", err)
			os.Exit(1)
		}
	}
}
