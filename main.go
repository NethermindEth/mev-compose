package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"math/rand"
	"os"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/suave/sdk"
	log "github.com/inconshreveable/log15"
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
	priv  *ecdsa.PrivateKey
	nonce uint64
}

func (p *privKey) Address() common.Address {
	return crypto.PubkeyToAddress(p.priv.PublicKey)
}

func (p *privKey) MarshalPrivKey() []byte {
	return crypto.FromECDSA(p.priv)
}

func (p *privKey) Nonce() uint64 {
	return p.nonce
}

func (p *privKey) StepNonce() {
	p.nonce = p.nonce + 1
}

type Client struct {
	*sdk.Client
	Key *privKey
}

func NewClient(rpc *rpc.Client, key *privKey, addr common.Address) *Client {
	return &Client{Client: sdk.NewClient(rpc, key.priv, addr), Key: key}
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

func fundAccount(clt *Client, to common.Address, value *big.Int) error {
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

func DeployContract(artifact *Artifact, clt *Client) (*sdk.Contract, error) {
	txnResult, err := sdk.DeployContract(artifact.Code, clt.Client)

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

	return sdk.GetContract(receipt.ContractAddress, artifact.Abi, clt.Client), nil
}

func MakeBundle(txn1 *types.Transaction, txn2 *types.Transaction) ([]byte, error) {
	txs := types.Transactions{txn1}

	if txn2 != nil {
		txs = append(txs, txn2)
	}

	bundle := &types.SBundle{
		BlockNumber: big.NewInt(1),
		Txs:         txs,
	}

	bundleBytes, err := bundle.MarshalJSON()
	return bundleBytes, err
}

func SignTxnWithNonce(
	client *Client,
	txn *types.LegacyTx,
) (*types.Transaction, error) {
	if txn.Nonce == 0 {
		txn.Nonce = client.Key.nonce
		client.Key.StepNonce()
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

func getAllowedPeekers(addresses ...common.Address) []common.Address {
	peekers := []common.Address{common.HexToAddress("0x0000000000000000000000000000000042100001")} // build_eth_block addr
	for _, address := range addresses {
		peekers = append(peekers, address)
	}
	return peekers
}

func SendBundle(
	bundle *types.SBundle,
	targetBlock uint64,
	allowedPeekers []common.Address,
	bundleContract *sdk.Contract) (*[16]byte, error) {

	bundleBytes, err := bundle.MarshalJSON()
	txnResult, err := bundleContract.SendTransaction(
		"newBundle", []interface{}{targetBlock, allowedPeekers, []common.Address{}}, bundleBytes)

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
	client *Client,
	targetBlock uint64,
	allowedPeekers []common.Address,
	metaBundleContract *sdk.Contract,
	bundleIds [][16]byte,
	value *big.Int,
	address common.Address,
) (*MetaBundleHintEvent, error) {
	to := generatePrivKey().Address()
	ethTxn1, err := SignTxnWithNonce(client, &types.LegacyTx{
		To:    &to,
		Value: big.NewInt(1000),
	})
	if err != nil {
		return nil, err
	}

	bundleBytes, err := MakeBundle(ethTxn1, nil)
	if err != nil {
		return nil, err
	}

	metaBundle := MetaBundle{
		BundleIds:    bundleIds,
		Value:        value,
		FeeRecipient: address,
	}

	txnResult, err := metaBundleContract.SendTransaction("newMetaBundle",
		[]interface{}{targetBlock, allowedPeekers, []common.Address{}, metaBundle}, bundleBytes)
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
	client *Client,
	targetBlock uint64,
	allowedPeekers []common.Address,
	metaBundleContract *sdk.Contract,
	dataId [16]byte,
	value *big.Int,
	feeRecipient common.Address,
) (*[16]byte, error) {
	paymentTx, err := SignTxnWithNonce(client, &types.LegacyTx{
		To:    &feeRecipient,
		Value: value,
	})

	if err != nil {
		return nil, err
	}

	bundleBytes, err := MakeBundle(paymentTx, nil)
	log.Info("Payment bundle", "bundle", string(bundleBytes))

	if err != nil {
		return nil, err
	}

	txnResult, err := metaBundleContract.SendTransaction(
		"newMatch", []interface{}{targetBlock, allowedPeekers, []common.Address{}, dataId}, bundleBytes)

	if err != nil {
		return nil, err
	}

	receipt, err := txnResult.Wait()
	if err != nil {
		return nil, err
	}

	if receipt.Status == 0 {
		return nil, fmt.Errorf("transaction had errors: %v", receipt)
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
	header *types.Header,
	targetBlock uint64,
	allowedPeekers []common.Address,
	blockBuilderContract *sdk.Contract,
	bundleDataIds [][16]byte,
) error {
	// TODO: should communicate with CL to get the correct attributes for next block.
	blockArgs := types.BuildBlockArgs{
		Slot:         header.Number.Uint64() + 1,
		Parent:       header.Hash(),
		Timestamp:    header.Time + 12,
		GasLimit:     header.GasLimit,
		FeeRecipient: common.Address{},
		Extra:        header.Extra,
	}

	txnResult, err := blockBuilderContract.SendTransaction(
		"build", []interface{}{targetBlock, blockArgs, allowedPeekers, []common.Address{}, bundleDataIds}, nil)
	if err != nil {
		return fmt.Errorf("failed to send a transaction: %v", err)
	}

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

func main() {
	log.Info("Starting composable block test...")
	mevmRpc, _ := rpc.Dial(exNodeNetAddr)
	mevmClt := NewClient(mevmRpc, fundedAccount, kettleAddress)
	ethRpc, _ := rpc.Dial(L1NetAddr)
	ethClt := NewClient(ethRpc, fundedAccount, common.Address{})

	var latestHeader *types.Header
	var targetBlock uint64

	var (
		testAddr1    *privKey
		testAddr2    *privKey
		builderAddr1 *privKey
		builderAddr2 *privKey
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
				builderAddr1 = generatePrivKey()
				builderAddr2 = generatePrivKey()

				fundBalance := new(big.Int)
				fundBalance.Exp(big.NewInt(10), big.NewInt(18), nil)

				if err := fundAccount(ethClt, testAddr1.Address(), fundBalance); err != nil {
					return err
				}
				if err := fundAccount(ethClt, testAddr2.Address(), fundBalance); err != nil {
					return err
				}
				if err := fundAccount(ethClt, builderAddr1.Address(), fundBalance); err != nil {
					return err
				}
				if err := fundAccount(ethClt, builderAddr2.Address(), fundBalance); err != nil {
					return err
				}
				return nil
			},
		},
		{
			name: "Get target block number",
			action: func() error {
				var err error
				latestHeader, err = ethClt.RPC().HeaderByNumber(context.Background(), nil)
				if err != nil {
					return err
				}
				targetBlock = uint64(latestHeader.Number.Uint64() + 1)
				log.Info("Latest header", "header", latestHeader, "targetBlock", targetBlock)
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
				log.Info("Basic bundle contract address", "address", basicBundleContract.Address())

				metaBundleContract, err = DeployContract(MetaBundleArtifact, mevmClt)
				if err != nil {
					return err
				}
				log.Info("Meta bundle contract address", "address", metaBundleContract.Address())

				blockBuilderContract, err = DeployContract(BlockBuilderArtifact, mevmClt)
				if err != nil {
					return err
				}
				log.Info("Block builder contract address", "address", blockBuilderContract.Address())

				return nil
			},
		},
		{
			name: "Send eth bundles",
			action: func() error {
				allowedPeekers := getAllowedPeekers(basicBundleContract.Address(),
					metaBundleContract.Address(), blockBuilderContract.Address())
				gen := NewBundleGenerator(ethClt, ethRpc)
				// Start sending bundles
				for i := 0; i < 10; i++ {
					bundle, err := gen.GenerateBundle(2+rand.Intn(2), targetBlock)
					if err != nil {
						return err
					}

					dataId, err := SendBundle(bundle, targetBlock, allowedPeekers, basicBundleContract)
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
				client := NewClient(ethRpc, builderAddr1, common.Address{})
				allowedPeekers := getAllowedPeekers(metaBundleContract.Address(), blockBuilderContract.Address())

				log.Info("Sending meta bundle 1: ", "dataIds", bundleDataIds[0:3])

				metaBundleHint, err := SendMetaBundle(
					client, targetBlock, allowedPeekers, metaBundleContract, bundleDataIds[0:3], big.NewInt(1000), testAddr1.Address())
				if err != nil {
					return err
				}
				metaBundleHints = append(metaBundleHints, metaBundleHint)

				log.Info("Sending meta bundle 2: ", "dataIds", bundleDataIds[5:8])

				metaBundleHint, err = SendMetaBundle(
					client, targetBlock, allowedPeekers, metaBundleContract, bundleDataIds[5:8], big.NewInt(1000), testAddr1.Address())

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
				client := NewClient(ethRpc, builderAddr2, common.Address{})
				allowedPeekers := getAllowedPeekers(metaBundleContract.Address(), blockBuilderContract.Address())

				for _, hint := range metaBundleHints {
					log.Info("Sending meta bundle match", "hint", hint)
					dataId, err := SendMetaBundleMatch(
						client,
						targetBlock,
						allowedPeekers,
						metaBundleContract,
						hint.DataID,
						hint.MetaBundle.Value,
						hint.MetaBundle.FeeRecipient)
					if err != nil {
						return err
					}
					matchedMetaBundleIds = append(matchedMetaBundleIds, *dataId)
				}
				log.Info("Matched meta bundle ids", "ids", matchedMetaBundleIds)
				return nil
			},
		},
		{
			name: "Build block and send to relay",
			action: func() error {
				var err error
				latestHeader, err = ethClt.RPC().HeaderByNumber(context.Background(), nil)
				if err != nil {
					return err
				}

				allowedPeekers := getAllowedPeekers(blockBuilderContract.Address())
				bundleIds := [][16]byte{
					matchedMetaBundleIds[0],
					bundleDataIds[3],
					matchedMetaBundleIds[1],
				}

				log.Info("Building block with bundle ids", "bundleIds", bundleIds)
				err = SendBlock(latestHeader, targetBlock, allowedPeekers, blockBuilderContract, bundleIds)
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
