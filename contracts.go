package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	BasicBundleArtifact  = newArtifact("ComposableBlock.sol/BasicBundleContract.json")
	MetaBundleArtifact   = newArtifact("ComposableBlock.sol/MetaBundleContract.json")
	BlockBuilderArtifact = newArtifact("ComposableBlock.sol/BlockBuilderContract.json")
)

func newArtifact(name string) *Artifact {
	// Get the caller's file path.
	_, filename, _, _ := runtime.Caller(1)

	// Resolve the directory of the caller's file.
	callerDir := filepath.Dir(filename)

	// Construct the absolute path to the target file.
	targetFilePath := filepath.Join(callerDir, "./out", name)

	data, err := os.ReadFile(targetFilePath)
	if err != nil {
		panic(fmt.Sprintf("failed to read artifact %s: %v. Maybe you forgot to generate the artifacts? `cd suave && forge build`", name, err))
	}

	var artifactObj struct {
		Abi              *abi.ABI `json:"abi"`
		DeployedBytecode struct {
			Object string
		} `json:"deployedBytecode"`
		Bytecode struct {
			Object string
		} `json:"bytecode"`
	}
	if err := json.Unmarshal(data, &artifactObj); err != nil {
		panic(fmt.Sprintf("failed to unmarshal artifact %s: %v", name, err))
	}

	return &Artifact{
		Abi:          artifactObj.Abi,
		Code:         hexutil.MustDecode(artifactObj.Bytecode.Object),
		DeployedCode: hexutil.MustDecode(artifactObj.DeployedBytecode.Object),
	}
}

type Artifact struct {
	Abi          *abi.ABI
	DeployedCode []byte
	Code         []byte
}

type MetaBundle struct {
	BundleIds    [][16]byte     `json:"bundleIds"`
	Value        *big.Int       `json:"value"`
	FeeRecipient common.Address `json:"feeRecipient"`
}

type MBundle struct {
	BundleIds    [][16]byte     `json:"bundleIds"`
	Value        uint64         `json:"value"`
	FeeRecipient common.Address `json:"feeRecipient"`
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
	tempStruct := unpacked[3].(struct {
		BundleIds    [][16]byte     `json:"bundleIds"`
		Value        *big.Int       `json:"value"`
		FeeRecipient common.Address `json:"feeRecipient"`
	})
	b.MetaBundle = MetaBundle(tempStruct)
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
