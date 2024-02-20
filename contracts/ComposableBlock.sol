// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.19;

import "suave-std/suavelib/Suave.sol";
import "suave-std/Transactions.sol";
import "suave-std/protocols/Bundle.sol";
import "solady/src/utils/JSONParserLib.sol";
import "forge-std/console.sol";

contract BasicBundleContract {
    event HintEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers);

    function newBundle(uint64 decryptionCondition, address[] calldata allowedPeekers, address[] calldata allowedStores)
        external
        payable
        returns (bytes memory)
    {
        require(Suave.isConfidential());

        // verify bundle validity.
        bytes memory bundleData = Suave.confidentialInputs();
        uint64 egp = Suave.simulateBundle(bundleData);
        require(egp > 0, "Bundle simulation failed");

        // save bundle to conf store.
        Suave.DataRecord memory dataRecord =
            Suave.newDataRecord(decryptionCondition, allowedPeekers, allowedStores, "default:v0:ethBundles");
        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundles", bundleData);
        Suave.confidentialStore(dataRecord.id, "mevcompose:v0:ethBundles", bundleData);

        return emitAndReturn(dataRecord);
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

    using JSONParserLib for JSONParserLib.Item;
    using JSONParserLib for string;
    using LibString for string;

    function createMetaBundle(
        uint64 decryptionCondition,
        address[] calldata allowedPeekers,
        address[] calldata allowedStores,
        MetaBundle memory metaBundle
    ) public returns (Suave.DataRecord memory) {
        require(Suave.isConfidential());

        // fetch backrun bundle data.
        bytes memory backrunBundleData = Suave.confidentialInputs();
        uint64 egp = Suave.simulateBundle(backrunBundleData);
        if (egp == 0) {
            revert("Backrun bundle is not valid");
        }

        Suave.DataRecord memory dataRecord = Suave.newDataRecord(
            decryptionCondition, allowedPeekers, allowedStores, "mevcompose:v0:unmatchedMetaBundles"
        );
        Suave.DataRecord memory backrunBundleRecord =
            Suave.newDataRecord(decryptionCondition, allowedPeekers, allowedStores, "mevcompose:v0:backrunBundles");
        Suave.confidentialStore(backrunBundleRecord.id, "mevcompose:v0:ethBundles", backrunBundleData);

        Suave.DataId[] memory bundleIds = new Suave.DataId[](metaBundle.bundleIds.length + 1);
        for (uint256 i = 0; i < metaBundle.bundleIds.length; i++) {
            bundleIds[i] = metaBundle.bundleIds[i];
        }
        bundleIds[metaBundle.bundleIds.length] = backrunBundleRecord.id;

        // TODO: Check if the meta bundle is buildable.
        Suave.confidentialStore(dataRecord.id, "mevcompose:v0:mergedDataRecords", abi.encode(bundleIds));
        Suave.confidentialStore(dataRecord.id, "mevcompose:v0:value", abi.encode(metaBundle.value));
        Suave.confidentialStore(dataRecord.id, "mevcompose:v0:feeRecipient", abi.encode(metaBundle.feeRecipient));
        return dataRecord;
    }

    function newMetaBundle(
        uint64 decryptionCondition,
        address[] calldata allowedPeekers,
        address[] calldata allowedStores,
        MetaBundle memory metaBundle
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());
        Suave.DataRecord memory dataRecord =
            createMetaBundle(decryptionCondition, allowedPeekers, allowedStores, metaBundle);
        return bytes.concat(this.emitHint.selector, abi.encode(dataRecord, metaBundle));
    }

    function emitHint(Suave.DataRecord memory dataRecord, MetaBundle memory metaBundle) public {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers, metaBundle);
    }

    function fromHexChar(uint8 c) internal pure returns (uint8) {
        if (bytes1(c) >= bytes1("0") && bytes1(c) <= bytes1("9")) {
            return c - uint8(bytes1("0"));
        }
        if (bytes1(c) >= bytes1("a") && bytes1(c) <= bytes1("f")) {
            return 10 + c - uint8(bytes1("a"));
        }
        if (bytes1(c) >= bytes1("A") && bytes1(c) <= bytes1("F")) {
            return 10 + c - uint8(bytes1("A"));
        }
        revert("fail");
    }

    // Convert an hexadecimal string to raw bytes
    function fromHex(string memory s) internal pure returns (bytes memory) {
        bytes memory ss = bytes(s);
        require(ss.length % 2 == 0); // length must be even
        bytes memory r = new bytes(ss.length / 2);
        for (uint256 i = 0; i < ss.length / 2; ++i) {
            r[i] = bytes1(fromHexChar(uint8(ss[2 * i])) * 16 + fromHexChar(uint8(ss[2 * i + 1])));
        }
        return r;
    }

    function _stripQuotesAndPrefix(string memory s) internal pure returns (string memory) {
        bytes memory strBytes = bytes(s);
        bytes memory result = new bytes(strBytes.length - 4);
        for (uint256 i = 3; i < strBytes.length - 1; i++) {
            result[i - 3] = strBytes[i];
        }
        return string(result);
    }

    function parseBundleJson(string memory bundleJson) public pure returns (Bundle.BundleObj memory) {
        JSONParserLib.Item memory root = bundleJson.parse();
        JSONParserLib.Item memory txnsNode = root.at('"txs"');
        uint256 txnsLength = txnsNode.size();
        bytes[] memory txns = new bytes[](txnsLength);

        for (uint256 i = 0; i < txnsLength; i++) {
            JSONParserLib.Item memory txnNode = txnsNode.at(i);
            bytes memory txn = fromHex(_stripQuotesAndPrefix(txnNode.value()));
            txns[i] = txn;
        }

        uint256 blockNumber;
        if (root.at('"blockNumber"').isUndefined()) {
            blockNumber = 0;
        } else {
            blockNumber = root.at('"blockNumber"').value().decodeString().parseUintFromHex();
        }

        uint256 minTimestamp;
        if (root.at('"minTimestamp"').isUndefined()) {
            minTimestamp = 0;
        } else {
            minTimestamp = root.at('"minTimestamp"').value().parseUint();
        }

        uint256 maxTimestamp;
        if (root.at('"maxTimestamp"').isUndefined()) {
            maxTimestamp = 0;
        } else {
            maxTimestamp = root.at('"maxTimestamp"').value().parseUint();
        }

        Bundle.BundleObj memory bundle;
        bundle.blockNumber = uint64(blockNumber);
        bundle.minTimestamp = uint64(minTimestamp);
        bundle.maxTimestamp = uint64(maxTimestamp);
        bundle.txns = txns;
        return bundle;
    }

    function newMatch(
        uint64 decryptionCondition,
        address[] calldata allowedPeekers,
        address[] calldata allowedStores,
        Suave.DataId dataId
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());

        // Parse payment bundle.
        bytes memory paymentBundleData = Suave.confidentialInputs();
        Bundle.BundleObj memory paymentBundleObj = parseBundleJson(string(paymentBundleData));

        require(paymentBundleObj.txns.length == 1, "Payment bundle must contain exactly one transaction");
        Transactions.EIP155 memory paymentTx = Transactions.decodeRLP_EIP155(paymentBundleObj.txns[0]);

        // check validity of payment
        uint64 value = abi.decode(Suave.confidentialRetrieve(dataId, "mevcompose:v0:value"), (uint64));
        address feeRecipient = abi.decode(Suave.confidentialRetrieve(dataId, "mevcompose:v0:feeRecipient"), (address));
        require(paymentTx.value == value, "PaymentTx amount does not match metaBundle value");
        require(paymentTx.to == feeRecipient, "PaymentTx recipient does not match metaBundle feeRecipient");
        uint64 egp = Suave.simulateBundle(paymentBundleData);
        require(egp > 0, "Payment bundle simulation failed");

        Suave.DataRecord memory dataRecord =
            Suave.newDataRecord(decryptionCondition, allowedPeekers, allowedStores, "mevcompose:v0:matchMetaBundles");

        // payment bundle is valid. save it to confidential store
        Suave.confidentialStore(dataRecord.id, "mevcompose:v0:paymentBundles", paymentBundleData);

        // save the meta bundle.
        bytes memory bundleIdsData = Suave.confidentialRetrieve(dataId, "mevcompose:v0:mergedDataRecords");
        Suave.confidentialStore(dataRecord.id, "mevcompose:v0:mergedDataRecords", bundleIdsData);

        // emit event
        return bytes.concat(this.emitMatch.selector, abi.encode(dataRecord));
    }

    function emitMatch(Suave.DataRecord calldata dataRecord) external {
        emit MatchEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers);
    }
}

contract BlockBuilderContract {
    event NewBuilderBidEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers, bytes envelope);

    struct BundleDataId {
        Suave.DataId dataId;
        bool isMetaBundle;
    }

    function emitNewBuilderBidEvent(Suave.DataRecord memory record, bytes memory envelope) public {
        emit NewBuilderBidEvent(record.id, record.decryptionCondition, record.allowedPeekers, envelope);
    }

    function build(
        BundleDataId[] memory bundleDataIds,
        address[] calldata allowedPeekers,
        address[] calldata allowedStores,
        uint64 blockNumber
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());

        Suave.DataRecord memory record =
            Suave.newDataRecord(blockNumber, allowedPeekers, allowedStores, "default:v0:ethBlocks");
        Suave.confidentialStore(record.id, "default:v0:mergedDataRecords", abi.encode(bundleDataIds));
        Suave.BuildBlockArgs memory blockArgs;
        (bytes memory builderBid, bytes memory envelope) = Suave.buildEthBlock(blockArgs, record.id, ""); // namespace not used.
        Suave.confidentialStore(record.id, "default:v0:builderBids", builderBid);
        Suave.confidentialStore(record.id, "default:v0:payloads", envelope);
        return bytes.concat(this.emitNewBuilderBidEvent.selector, abi.encode(record, envelope));
    }
}
