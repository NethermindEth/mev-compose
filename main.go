package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/suave/sdk"
	log "github.com/inconshreveable/log15"
	"github.com/umbracle/ethgo/abi"
)

var (
	kettleAddress = common.HexToAddress("b5feafbdd752ad52afb7e1bd2e40432a485bbb7f")
	exNodeNetAddr = "http://localhost:8545"

	L1NetAddr = "http://localhost:8555"

	// This account is funded in both devnev networks
	// address: 0xb5feafbdd752ad52afb7e1bd2e40432a485bbb7f
	fundedAccount = newPrivKeyFromHex("6c45335a22461ccdb978b78ab61b238bad2fae4544fb55c14eb096c875ccfc52")
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
	return sdk.GetContract(receipt.ContractAddress, artifact.Abi, clt), nil
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

func SignTxnWithNonce(
	client *sdk.Client,
	txn *types.LegacyTx,
) (*types.Transaction, error) {
	if txn.Nonce == 0 {
		nonce, err := client.RPC().PendingNonceAt(context.Background(), client.Addr())
		if err != nil {
			return nil, err
		}
		txn.Nonce = nonce
	}

	if txn.GasPrice == nil {
		gasPrice, err := client.RPC().SuggestGasPrice(context.Background())
		if err != nil {
			return nil, err
		}
		txn.GasPrice = gasPrice
	}

	if txn.Gas == 0 {
		estimateMsg := ethereum.CallMsg{
			From:     client.Addr(),
			To:       txn.To,
			GasPrice: txn.GasPrice,
			Value:    txn.Value,
			Data:     txn.Data,
		}
		gasLimit, err := client.RPC().EstimateGas(context.Background(), estimateMsg)
		if err != nil {
			return nil, err
		}
		txn.Gas = gasLimit
	}

	signed, err := client.SignTxn(txn)
	if err != nil {
		return nil, err
	}
	return signed, nil
}

func SendBundle(
	client1 *sdk.Client,
	client2 *sdk.Client,
	targetAddr common.Address,
	targetBlock uint64,
	bundleContract *sdk.Contract) (*[16]byte, error) {
	ethTxn1, err := SignTxnWithNonce(client1, &types.LegacyTx{
		To:    &targetAddr,
		Value: big.NewInt(1000),
	})

	if err != nil {
		return nil, err
	}

	ethTxnBackrun, err := SignTxnWithNonce(client2, &types.LegacyTx{
		To:    &targetAddr,
		Value: big.NewInt(1000),
	})

	if err != nil {
		return nil, err
	}

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
	value *big.Int,
	address common.Address,
) (*MetaBundleHintEvent, error) {
	ethTxn1, err := SignTxnWithNonce(client, &types.LegacyTx{
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(13),
	})
	if err != nil {
		return nil, err
	}

	bundleBytes, err := MakeBundle(ethTxn1, nil)
	if err != nil {
		return nil, err
	}

	confidentialDataBytes, err := MetaBundleArtifact.Abi.Methods["newMetaBundle"].Outputs.Pack(bundleBytes)
	if err != nil {
		return nil, err
	}
	metaBundle := MetaBundle{
		BundleIds:    bundleIds,
		Value:        value,
		FeeRecipient: address,
	}

	txnResult, err := metaBundleContract.SendTransaction("newMetaBundle",
		[]interface{}{targetBlock, metaBundle}, confidentialDataBytes)
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

	bundleBytes, err := MakeBundle(paymentTx, nil)

	if err != nil {
		return nil, err
	}

	// Set the paid metabundle peekable to builder contract
	confidentialDataBytes, err := PackToBytes(bundleBytes)
	if err != nil {
		return nil, err
	}

	txnResult, err := metaBundleContract.SendTransaction("newMatch", []interface{}{targetBlock, dataId}, confidentialDataBytes)

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
		[]interface{}{dataIds, targetBlock, "http://localhost:18550"}, nil)

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
	log.Info("Starting PoC")
	mevmRpc, _ := rpc.Dial(exNodeNetAddr)
	mevmClt := sdk.NewClient(mevmRpc, fundedAccount.priv, kettleAddress)
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

				log.Info("Test account 1", "address", testAddr1.Address().Hex())
				log.Info("Test account 2", "address", testAddr2.Address().Hex())

				fundBalance := new(big.Int)
				fundBalance.Exp(big.NewInt(10), big.NewInt(18), nil)

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
				targetblock := uint64(1)

				// Start sending bundles
				for i := 0; i < 10; i++ {
					log.Info("Sending bundle", "i", i)
					to := generatePrivKey().Address()
					dataId, err := SendBundle(client1, client2, to, targetblock, basicBundleContract)
					if err != nil {
						return err
					}
					bundleDataIds = append(bundleDataIds, *dataId)
				}

				log.Info("Bundle data ids", "ids", bundleDataIds)

				return nil
			},
		},
		{
			name: "Send meta bundles",
			action: func() error {
				client := sdk.NewClient(ethRpc, testAddr1.priv, common.Address{})
				to := testAddr1.Address()
				targetBlock := uint64(1)

				log.Info("Sending meta bundle 1: ", "ids", bundleDataIds[0:3])

				metaBundleHint, err := SendMetaBundle(
					client, to, targetBlock, metaBundleContract, bundleDataIds[0:3], big.NewInt(1000), testAddr1.Address())
				if err != nil {
					return err
				}
				metaBundleHints = append(metaBundleHints, metaBundleHint)

				log.Info("Sending meta bundle 2: ", "ids", bundleDataIds[5:8])

				metaBundleHint, err = SendMetaBundle(
					client, to, targetBlock, metaBundleContract, bundleDataIds[5:8], big.NewInt(1000), testAddr1.Address())

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
					log.Info("Sending meta bundle match", "hint", hint)
					dataId, err := SendMetaBundleMatch(
						client,
						targetBlock,
						metaBundleContract,
						hint.DataID,
						hint.MetaBundle.Value,
						hint.MetaBundle.FeeRecipient)
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
