// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.19;

import "suave-std/suavelib/Suave.sol";
import "suave-std/Transactions.sol";
import "suave-std/protocols/Bundle.sol";
import "solady/src/utils/JSONParserLib.sol";

contract BasicBundleContract {
    event HintEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers);

    function newBundle(
        uint64 decryptionCondition,
        address[] memory dataAllowedPeekers,
        address[] memory dataAllowedStores
    ) external payable returns (bytes memory) {
        Suave.DataRecord memory dataRecord = createBundle(decryptionCondition, dataAllowedPeekers, dataAllowedStores);
        return emitAndReturn(dataRecord);
    }

    function createBundle(
        uint64 decryptionCondition,
        address[] memory dataAllowedPeekers,
        address[] memory dataAllowedStores
    ) public returns (Suave.DataRecord memory) {
        require(Suave.isConfidential());
        bytes memory bundleData = Suave.confidentialInputs();
        Suave.DataRecord memory dataRecord =
          Suave.newDataRecord(decryptionCondition, dataAllowedPeekers, dataAllowedStores, "default:v0:ethBundles");

        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundles", bundleData);
        return dataRecord;
    }

    function emitHint(Suave.DataRecord calldata dataRecord) external {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers);
    }

    function emitAndReturn(Suave.DataRecord memory dataRecord) internal virtual returns (bytes memory) {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers);
        return bytes.concat(this.emitHint.selector, abi.encode(dataRecord));
    }
}

contract MetaBundleContract {

    struct MetaBundle {
        Suave.DataId[] bundleIds;
        uint256 value;
        address feeRecipient;
    }

    event HintEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers, MetaBundle metaBundle);
    event MatchEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers);

    address[] public allowedPeekers = [address(this), Suave.BUILD_ETH_BLOCK];
    address[] public allowedStores;

    using JSONParserLib for JSONParserLib.Item;
    using JSONParserLib for string;
    using LibString for string;

    function createMetaBundle(
        uint64 decryptionCondition,
        MetaBundle memory metaBundle
    ) public returns (Suave.DataRecord memory) {
        require(Suave.isConfidential());

        // fetch backrun bundle data.
        bytes memory backrunBundleData = Suave.confidentialInputs();
        uint64 egp = Suave.simulateBundle(backrunBundleData);

        Suave.DataRecord memory backrunDataRecord = Suave.newDataRecord(
            decryptionCondition, allowedPeekers, allowedStores, "default:v0:ethBundles");
        Suave.confidentialStore(backrunDataRecord.id, "default:v0:ethBundles", backrunBundleData);
        Suave.confidentialStore(backrunDataRecord.id, "default:v0:ethBundleSimResults", abi.encode(egp));

        // merge data records
        Suave.DataId[] memory dataRecords = new Suave.DataId[](metaBundle.bundleIds.length + 1);
        for (uint256 i = 0; i < metaBundle.bundleIds.length; i++) {
            dataRecords[i] = metaBundle.bundleIds[i];
        }
        dataRecords[metaBundle.bundleIds.length] = backrunDataRecord.id;

        Suave.DataRecord memory dataRecord = Suave.newDataRecord(decryptionCondition, allowedPeekers, allowedStores, "default:v0:ethMetaBundles");
        Suave.confidentialStore(dataRecord.id, "default:v0:ethMetaBundles", abi.encode(dataRecords));
        Suave.confidentialStore(dataRecord.id, "default:v0:ethMetaBundleValue", abi.encode(metaBundle.value));
        Suave.confidentialStore(dataRecord.id, "default:v0:ethMetaBundleFeeRecipient", abi.encode(metaBundle.feeRecipient));
        return dataRecord;
    }

    function newMetaBundle(
        uint64 decryptionCondition,
        MetaBundle memory metaBundle
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());
        Suave.DataRecord memory dataRecord = createMetaBundle(decryptionCondition, metaBundle);
        return bytes.concat(this.emitHint.selector, abi.encode(dataRecord, metaBundle));
    }

    function emitHint(Suave.DataRecord memory dataRecord, MetaBundle memory metaBundle) public {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers, metaBundle);
    }

    function fromHexChar(uint8 c) internal pure returns (uint8) {
        if (bytes1(c) >= bytes1('0') && bytes1(c) <= bytes1('9')) {
            return c - uint8(bytes1('0'));
        }
        if (bytes1(c) >= bytes1('a') && bytes1(c) <= bytes1('f')) {
            return 10 + c - uint8(bytes1('a'));
        }
        if (bytes1(c) >= bytes1('A') && bytes1(c) <= bytes1('F')) {
            return 10 + c - uint8(bytes1('A'));
        }
        revert("fail");
    }

    // Convert an hexadecimal string to raw bytes
    function fromHex(string memory s) internal pure returns (bytes memory) {
        bytes memory ss = bytes(s);
        require(ss.length%2 == 0); // length must be even
        bytes memory r = new bytes(ss.length/2);
        for (uint i=0; i<ss.length/2; ++i) {
            r[i] = bytes1(fromHexChar(uint8(ss[2*i])) * 16 +
                          fromHexChar(uint8(ss[2*i+1])));
        }
        return r;
    }

    function _stripQuotesAndPrefix(string memory s) internal pure returns (string memory) {
        bytes memory strBytes = bytes(s);
        bytes memory result = new bytes(strBytes.length-4);
        for (uint i = 3; i < strBytes.length-1; i++) {
            result[i-3] = strBytes[i];
        }
        return string(result);
    }

    function parseBundleJson(string memory bundleJson) public pure returns (Bundle.BundleObj memory) {
        JSONParserLib.Item memory root = bundleJson.parse();
        JSONParserLib.Item memory txnsNode = root.at('"txns"');
        uint256 txnsLength = txnsNode.size();
        bytes[] memory txns = new bytes[](txnsLength);

        for (uint256 i=0; i < txnsLength; i++) {
            JSONParserLib.Item memory txnNode = txnsNode.at(i);
            bytes memory txn = fromHex(_stripQuotesAndPrefix(txnNode.value()));
            txns[i] = txn;
        }

        uint256 blockNumber = root.at('"blockNumber"').value().parseUint();
        uint256 minTimestamp = root.at('"minTimestamp"').value().parseUint();
        uint256 maxTimestamp = root.at('"maxTimestamp"').value().parseUint();

        Bundle.BundleObj memory bundle;
        bundle.blockNumber = uint64(blockNumber);
        bundle.minTimestamp = uint64(minTimestamp);
        bundle.maxTimestamp = uint64(maxTimestamp);
        bundle.txns = txns;
        return bundle;
    }

    function newMatch(uint64 decryptionCondition,
                      Suave.DataId dataId) external returns (bytes memory) {
        require(Suave.isConfidential());

        // Parse payment bundle.
        bytes memory paymentBundleData = Suave.confidentialInputs();
        Bundle.BundleObj memory paymentBundleObj = parseBundleJson(string(paymentBundleData));

        require(paymentBundleObj.txns.length == 1, "Payment bundle must contain exactly one transaction");
        Transactions.EIP155 memory paymentTx = Transactions.decodeRLP_EIP155(paymentBundleObj.txns[0]);

        // check validity of payment
        uint64 value = abi.decode(Suave.confidentialRetrieve(dataId, "default:v0:ethMetaBundleValue"), (uint64));
        address feeRecipient = abi.decode(Suave.confidentialRetrieve(dataId, "default:v0:ethMetaBundleFeeRecipient"), (address));
        require(paymentTx.value == value, "PaymentTx amount does not match metaBundle value");
        require(paymentTx.to == feeRecipient, "PaymentTx recipient does not match metaBundle feeRecipient");
        require(Suave.simulateBundle(paymentBundleData) == 0, "Payment bundle is not valid");

        // payment bundle is valid. save it to confidential store
        Suave.DataRecord memory paymentBundleDataRecord = Suave.newDataRecord(
            decryptionCondition, allowedPeekers, allowedStores, "default:v0:ethBundles");
        Suave.confidentialStore(paymentBundleDataRecord.id, "default:v0:ethBundles", paymentBundleData);

        // save the meta bundle.
        bytes memory bundleIdsData = Suave.confidentialRetrieve(dataId, "default:v0:ethMetaBundles");
        Suave.DataId[] memory bundleIds = abi.decode(bundleIdsData, (Suave.DataId[]));
        Suave.DataRecord memory dataRecord = Suave.newDataRecord(
            decryptionCondition, allowedPeekers, allowedStores, "default:v0:mergedDataRecords");
        Suave.DataId[] memory dataRecords = new Suave.DataId[](bundleIds.length + 1);

        for (uint256 i = 0; i < bundleIds.length; i++) {
            dataRecords[i] = bundleIds[i];
        }
        dataRecords[bundleIds.length] = paymentBundleDataRecord.id;
        Suave.confidentialStore(dataRecord.id, "default:v0:mergedDataRecords", abi.encode(dataRecords));

        // emit event
        return bytes.concat(this.emitMatch.selector, abi.encode(dataRecord));
    }

    function emitMatch(Suave.DataRecord calldata dataRecord) external {
        emit MatchEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers);
    }
}

contract BlockBuilderContract {
    address[] public allowedPeekers = [address(this), Suave.BUILD_ETH_BLOCK];
    address[] public allowedStores;

    event NewBuilderBidEvent(
        Suave.DataId dataId,
        uint64 decryptionCondition,
        address[] allowedPeekers,
        bytes envelope
    );

    struct BundleDataId {
        Suave.DataId dataId;
        bool isMetaBundle;
    }

    function emitNewBuilderBidEvent(Suave.DataRecord memory record, bytes memory envelope) public {
        emit NewBuilderBidEvent(record.id, record.decryptionCondition, record.allowedPeekers, envelope);
    }

    // Merges bundles and meta bundles.
    function mergeBundles(BundleDataId[] memory bundleDataIds, uint64 blockNumber) internal returns (Suave.DataRecord memory) {
        uint256 totalLength = 0;
        for (uint256 i = 0; i < bundleDataIds.length; i++) {
            if (bundleDataIds[i].isMetaBundle) {
                bytes memory record = Suave.confidentialRetrieve(bundleDataIds[i].dataId, "default:v0:mergedDataRecords");
                Suave.DataId[] memory ids = abi.decode(record, (Suave.DataId[]));
                totalLength += ids.length;
            } else {
                totalLength += 1;
            }
        }

        Suave.DataId[] memory bundleIds = new Suave.DataId[](totalLength);
        for (uint256 i = 0; i < bundleDataIds.length; i++) {
            if (bundleDataIds[i].isMetaBundle) {
                bytes memory record = Suave.confidentialRetrieve(bundleDataIds[i].dataId, "default:v0:mergedDataRecords");
                Suave.DataId[] memory ids = abi.decode(record, (Suave.DataId[]));
                for (uint256 j = 0; j < ids.length; j++) {
                    bundleIds[bundleIds.length] = ids[j];
                }
            } else {
                bundleIds[bundleIds.length] = bundleDataIds[i].dataId;
            }
        }
        Suave.DataRecord memory dataRecord = Suave.newDataRecord(
            blockNumber, allowedPeekers, allowedStores, "default:v0:mergedDataRecords");
        Suave.confidentialStore(dataRecord.id, "default:v0:mergedDataRecords", abi.encode(bundleIds));
        return dataRecord;
    }

    function build(
            BundleDataId[] memory bundleDataIds, uint64 blockNumber, string memory boostRelayUrl) external returns (bytes memory) {
        require(Suave.isConfidential());
        Suave.DataRecord memory mergedBundleDataRecord = mergeBundles(bundleDataIds, blockNumber);

        Suave.BuildBlockArgs memory blockArgs;
        (bytes memory builderBid, bytes memory envelope) = Suave.buildEthBlock(
            blockArgs, mergedBundleDataRecord.id, ""); // namespace not used.
        Suave.DataRecord memory builderBidRecord = Suave.newDataRecord(
            blockNumber, allowedPeekers, allowedStores, "default:v0:builderBids");
        Suave.confidentialStore(builderBidRecord.id, "default:v0:builderBids", builderBid);
        Suave.submitEthBlockToRelay(boostRelayUrl, builderBid);
        return bytes.concat(this.emitNewBuilderBidEvent.selector, abi.encode(builderBidRecord, envelope));
    }
}
