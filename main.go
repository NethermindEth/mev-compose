package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/suave/sdk"
	"github.com/umbracle/ethgo/abi"
)

var (
	exNodeEthAddr = common.HexToAddress("b5feafbdd752ad52afb7e1bd2e40432a485bbb7f")
	exNodeNetAddr = "http://localhost:8545"

	L1NetAddr = "http://localhost:9545"

	// This account is funded in both devnev networks
	// address: 0xBE69d72ca5f88aCba033a063dF5DBe43a4148De0
	fundedAccount = newPrivKeyFromHex("91ab9a7e53c220e6210460b65a7a3bb2ca181412a8a7b43ff336b3df1737ce12")
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

func DeployContract(artifact *Artifact, clt *sdk.Client) (*sdk.Contract, error) {
	txnResult, err := sdk.DeployContract(artifact.Code, clt)

	if err != nil {
		return nil, err
	}

	receipt, err := txnResult.Wait()
	if err != nil {
		return nil, err
	}

	if receipt.Status == 0 {
		return nil, fmt.Errorf("failed to deploy contract")
	}

	log.Info("Contract deployed", "address", receipt.ContractAddress.Hex())
	return sdk.GetContract(receipt.ContractAddress, BasicBundleArtifact.Abi, clt), nil
}

func MakeBundle(txn1 *types.Transaction, txn2 *types.Transaction) ([]byte, error) {
	txs := types.Transactions{txn1}

	if txn2 != nil {
		txs = append(txs, txn2)
	}

	bundle := &types.SBundle{
		Txs: txs,
	}

	bundleBytes, err := json.Marshal(bundle)
	return bundleBytes, err
}

func SendBundle(
	client1 *sdk.Client,
	client2 *sdk.Client,
	targetAddr common.Address,
	targetBlock uint64,
	bundleContract *sdk.Contract) (*[16]byte, error) {
	ethTxn1, _ := client1.SignTxn(&types.LegacyTx{
		To:       &targetAddr,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(13),
	})

	ethTxnBackrun, _ := client2.SignTxn(&types.LegacyTx{
		To:       &targetAddr,
		Value:    big.NewInt(1000),
		Gas:      21420,
		GasPrice: big.NewInt(13),
	})

	bundleBytes, _ := MakeBundle(ethTxn1, ethTxnBackrun)
	allowedPeekers := []common.Address{bundleContract.Address()}
	confidentialDataBytes, _ := BasicBundleArtifact.Abi.Methods["newBundle"].Outputs.Pack(bundleBytes)
	txnResult, err := bundleContract.SendTransaction(
		"newBundle", []interface{}{targetBlock, allowedPeekers, []common.Address{}}, confidentialDataBytes)

	if err != nil {
		return nil, err
	}

	receipt, err := txnResult.Wait()
	if err != nil {
		return nil, err
	}

	if receipt.Status == 0 {
		return nil, fmt.Errorf("failed to send a transaction")
	}

	hintEvent := &HintEvent{}
	if err := hintEvent.Unpack(receipt.Logs[0]); err != nil {
		return nil, err
	}

	log.Info("- transaction sent at txn:", "txHash", receipt.TxHash.Hex())
	log.Info("- Hint data id:", "DataID", hintEvent.DataID)
	return &hintEvent.DataID, nil
}

func SendMetaBundle(
	client *sdk.Client,
	to common.Address,
	targetBlock uint64,
	metaBundleContract *sdk.Contract,
	bundleIds [][16]byte,
	value uint64,
	address common.Address,
) (*MetaBundleHintEvent, error) {
	ethTxn1, _ := client.SignTxn(&types.LegacyTx{
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(13),
	})

	bundleBytes, _ := MakeBundle(ethTxn1, nil)
	allowedPeekers := []common.Address{metaBundleContract.Address()}
	confidentialDataBytes, _ := PackToBytes(bundleBytes)
	txnResult, err := metaBundleContract.SendTransaction("newMetaBundle",
		[]interface{}{targetBlock, allowedPeekers, []common.Address{}, []interface{}{
			bundleIds, value, address,
		}}, confidentialDataBytes)

	receipt, err := txnResult.Wait()
	if err != nil {
		return nil, err
	}

	if receipt.Status == 0 {
		return nil, fmt.Errorf("failed to send a transaction")
	}

	hintEvent := &MetaBundleHintEvent{}
	if err := hintEvent.Unpack(receipt.Logs[0]); err != nil {
		return nil, err
	}

	log.Info("- transaction sent at txn:", "txHash", receipt.TxHash.Hex())
	log.Info("- Hint data id:", "DataID", hintEvent.DataID)

	return hintEvent, nil
}

func SendMetaBundleMatch(
	client *sdk.Client,
	targetBlock uint64,
	metaBundleContract *sdk.Contract,
	blockBuilderContract *sdk.Contract,
	dataId [16]byte,
	value *big.Int,
	feeRecipient common.Address,
) (*[16]byte, error) {
	paymentTx, _ := client.SignTxn(&types.LegacyTx{
		To:       &feeRecipient,
		Value:    value,
		Gas:      21000,
		GasPrice: big.NewInt(13),
	})

	bundleBytes, _ := MakeBundle(paymentTx, nil)
	// Set the paid metabundle peekable to builder contract
	allowedPeekers := []common.Address{metaBundleContract.Address(), blockBuilderContract.Address()}
	confidentialDataBytes, _ := PackToBytes(bundleBytes)
	txnResult, err := metaBundleContract.SendTransaction("newMatch",
		[]interface{}{targetBlock, allowedPeekers, []common.Address{}, dataId}, confidentialDataBytes)

	receipt, err := txnResult.Wait()
	if err != nil {
		return nil, err
	}

	if receipt.Status == 0 {
		return nil, fmt.Errorf("failed to send a transaction")
	}

	matchEvent := &MetaBundleMatchEvent{}
	if err := matchEvent.Unpack(receipt.Logs[0]); err != nil {
		return nil, err
	}

	log.Info("- transaction sent at txn:", "txHash", receipt.TxHash.Hex())
	log.Info("- Match data id:", "DataID", matchEvent.DataID)
	return &matchEvent.DataID, nil
}

func SendBlock(
	client *sdk.Client,
	targetBlock uint64,
	blockBuilderContract *sdk.Contract,
	dataIds [][16]byte,
) error {
	txnResult, err := blockBuilderContract.SendTransaction("build",
		[]interface{}{dataIds, targetBlock}, nil)

	receipt, err := txnResult.Wait()
	if err != nil {
		return err
	}

	if receipt.Status == 0 {
		return fmt.Errorf("failed to send a transaction")
	}

	builderBidEvent := &NewBuilderBidEvent{}
	if err := builderBidEvent.Unpack(receipt.Logs[0]); err != nil {
		return err
	}

	log.Info("- transaction sent at txn:", "txHash", receipt.TxHash.Hex())
	log.Info("- Builder bid data id:", "DataID", builderBidEvent.DataID)
	return nil
}

func PackToBytes(data []byte) ([]byte, error) {
	byteType, _ := abi.NewType("bytes")
	encoded, err := byteType.Encode(data)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func main() {
	mevmRpc, _ := rpc.Dial(exNodeNetAddr)
	mevmClt := sdk.NewClient(mevmRpc, fundedAccount.priv, exNodeEthAddr)
	ethRpc, _ := rpc.Dial(L1NetAddr)
	ethClt := sdk.NewClient(ethRpc, fundedAccount.priv, common.Address{})

	var (
		testAddr1 *privKey
		testAddr2 *privKey
	)

	var basicBundleContract *sdk.Contract
	var metaBundleContract *sdk.Contract
	var blockBuilderContract *sdk.Contract

	var bundleDataIds [][16]byte
	var metaBundleHints []*MetaBundleHintEvent
	var matchedMetaBundleIds [][16]byte

	steps := []step{
		{
			name: "Create and fund test accounts",
			action: func() error {
				testAddr1 = generatePrivKey()
				testAddr2 = generatePrivKey()

				fundBalance := big.NewInt(100000000)

				if err := fundAccount(ethClt, testAddr1.Address(), fundBalance); err != nil {
					return err
				}

				if err := fundAccount(ethClt, testAddr2.Address(), fundBalance); err != nil {
					return err
				}

				return nil
			},
		},
		{
			name: "Deploy contracts",
			action: func() error {
				var err error
				basicBundleContract, err = DeployContract(BasicBundleArtifact, mevmClt)
				if err != nil {
					return err
				}

				metaBundleContract, err = DeployContract(MetaBundleArtifact, mevmClt)
				if err != nil {
					return err
				}

				blockBuilderContract, err = DeployContract(BlockBuilderArtifact, mevmClt)
				if err != nil {
					return err
				}

				return nil
			},
		},
		{
			name: "Send eth bundles",
			action: func() error {
				client1 := sdk.NewClient(ethRpc, testAddr1.priv, common.Address{})
				client2 := sdk.NewClient(ethRpc, testAddr2.priv, common.Address{})
				to := testAddr1.Address()
				targetblock := uint64(1)

				// Start sending bundles
				for i := 0; i < 10; i++ {
					dataId, err := SendBundle(client1, client2, to, targetblock, basicBundleContract)
					if err != nil {
						return err
					}
					bundleDataIds = append(bundleDataIds, *dataId)
				}

				return nil
			},
		},
		{
			name: "Send meta bundles",
			action: func() error {
				client := sdk.NewClient(ethRpc, testAddr1.priv, common.Address{})
				to := testAddr1.Address()
				targetBlock := uint64(1)

				metaBundleHint, err := SendMetaBundle(
					client, to, targetBlock, metaBundleContract, bundleDataIds[0:3], 1000, testAddr1.Address())
				if err != nil {
					return err
				}
				metaBundleHints = append(metaBundleHints, metaBundleHint)

				metaBundleHint, err = SendMetaBundle(
					client, to, targetBlock, metaBundleContract, bundleDataIds[5:7], 1000, testAddr1.Address())

				if err != nil {
					return err
				}
				metaBundleHints = append(metaBundleHints, metaBundleHint)

				return nil
			},
		},
		{
			name: "Send meta bundle matches",
			action: func() error {
				client := sdk.NewClient(ethRpc, testAddr1.priv, common.Address{})
				targetBlock := uint64(1)

				for _, hint := range metaBundleHints {
					dataId, err := SendMetaBundleMatch(
						client,
						targetBlock,
						metaBundleContract,
						blockBuilderContract,
						hint.DataID,
						big.NewInt(int64(hint.MetaBundle.Value)),
						hint.MetaBundle.Address)
					if err != nil {
						return err
					}
					matchedMetaBundleIds = append(matchedMetaBundleIds, *dataId)
				}
				return nil
			},
		},
		{
			name: "Build block and send to relay",
			action: func() error {
				client := sdk.NewClient(ethRpc, testAddr1.priv, common.Address{})
				targetBlock := uint64(1)
				err := SendBlock(client, targetBlock, blockBuilderContract, matchedMetaBundleIds)
				if err != nil {
					return err
				}
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

type HintEvent struct {
	DataID              [16]byte
	DecryptionCondition uint64
	AllowedPeekers      []common.Address
}

func (b *HintEvent) Unpack(log *types.Log) error {
	unpacked, err := BasicBundleArtifact.Abi.Events["HintEvent"].Inputs.Unpack(log.Data)
	if err != nil {
		return err
	}
	b.DataID = unpacked[0].([16]byte)
	b.DecryptionCondition = unpacked[1].(uint64)
	b.AllowedPeekers = unpacked[2].([]common.Address)
	return nil
}

type MetaBundle struct {
	BundleIds [][16]byte
	Value     uint64
	Address   common.Address
}

type MetaBundleHintEvent struct {
	DataID              [16]byte
	DecryptionCondition uint64
	AllowedPeekers      []common.Address
	MetaBundle          MetaBundle
}

func (b *MetaBundleHintEvent) Unpack(log *types.Log) error {
	unpacked, err := MetaBundleArtifact.Abi.Events["HintEvent"].Inputs.Unpack(log.Data)
	if err != nil {
		return err
	}
	b.DataID = unpacked[0].([16]byte)
	b.DecryptionCondition = unpacked[1].(uint64)
	b.AllowedPeekers = unpacked[2].([]common.Address)
	b.MetaBundle = unpacked[3].(MetaBundle)
	return nil
}

type MetaBundleMatchEvent struct {
	DataID              [16]byte
	DecryptionCondition uint64
	AllowedPeekers      []common.Address
}

func (b *MetaBundleMatchEvent) Unpack(log *types.Log) error {
	unpacked, err := MetaBundleArtifact.Abi.Events["MatchEvent"].Inputs.Unpack(log.Data)
	if err != nil {
		return err
	}
	b.DataID = unpacked[0].([16]byte)
	b.DecryptionCondition = unpacked[1].(uint64)
	b.AllowedPeekers = unpacked[2].([]common.Address)
	return nil
}

type NewBuilderBidEvent struct {
	DataID              [16]byte
	DecryptionCondition uint64
	AllowedPeekers      []common.Address
	Envelope            []byte
}

func (b *NewBuilderBidEvent) Unpack(log *types.Log) error {
	unpacked, err := BlockBuilderArtifact.Abi.Events["NewBuilderBidEvent"].Inputs.Unpack(log.Data)
	if err != nil {
		return err
	}
	b.DataID = unpacked[0].([16]byte)
	b.DecryptionCondition = unpacked[1].(uint64)
	b.AllowedPeekers = unpacked[2].([]common.Address)
	b.Envelope = unpacked[3].([]byte)
	return nil
}
