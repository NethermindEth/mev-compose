// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.19;

import "suave-std/suavelib/Suave.sol";
import "suave-std/Transactions.sol";
import "suave-std/protocols/Bundle.sol";


contract BasicBundleContract {
    event HintEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers);

    function newBundle(
        uint64 decryptionCondition,
        address[] memory dataAllowedPeekers,
        address[] memory dataAllowedStores
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());

        bytes memory bundleData = Suave.confidentialInputs();

        Suave.DataRecord memory dataRecord =
            Suave.newDataRecord(decryptionCondition, dataAllowedPeekers, dataAllowedStores, "default:v0:ethBundles");

        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundles", bundleData);

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

contract PartialBlockContract {

    struct MetaBundle {
        Suave.DataId[] bundleIds;
        uint64 value;
        address feeRecipient;
    }

    event HintEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers, MetaBundle metaBundle);
    event MatchEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers);

    function newMetaBundle(
        uint64 decryptionCondition,
        address[] memory dataAllowedPeekers,
        address[] memory dataAllowedStores,
        MetaBundle memory metaBundle
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());

        // fetch backrun bundle data.
        bytes memory matchBundleData = Suave.confidentialInputs();
        uint64 egp = Suave.simulateBundle(matchBundleData);

        Suave.DataRecord memory dataRecord = Suave.newDataRecord(
            decryptionCondition, dataAllowedPeekers, dataAllowedStores, "default:v0:ethBundles");

        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundles", matchBundleData);
        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundleSimResults", abi.encode(egp));

        // merge data records
        Suave.DataId[] memory dataRecords = new Suave.DataId[](metaBundle.bundleIds.length + 1);
        for (uint256 i = 0; i < metaBundle.bundleIds.length; i++) {
            dataRecords[i] = metaBundle.bundleIds[i];
        }
        dataRecords[metaBundle.bundleIds.length] = dataRecord.id;

        Suave.confidentialStore(dataRecord.id, "default:v0:ethMetaBundles", abi.encode(dataRecords));
        Suave.confidentialStore(dataRecord.id, "default:v0:ethMetaBundleValues", abi.encode(metaBundle.value));
        Suave.confidentialStore(dataRecord.id, "default:v0:ethMetaBundleFeeRecipient", abi.encode(metaBundle.feeRecipient));
        return emitAndReturn(dataRecord, metaBundle);
    }

    function emitHint(Suave.DataRecord memory dataRecord, MetaBundle memory metaBundle) public {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers, metaBundle);
    }

    function emitAndReturn(Suave.DataRecord memory dataRecord, MetaBundle memory metaBundle) internal returns (bytes memory) {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers, metaBundle);
        return bytes.concat(this.emitHint.selector, abi.encode(dataRecord));
    }

    function newMatch(uint64 decryptionCondition,
                      address[] memory dataAllowedPeekers,
                      address[] memory dataAllowedStores,
                      Suave.DataId dataId) external returns (bytes memory) {
        require(Suave.isConfidential());

        // Parse payment bundle.
        bytes memory paymentBundleData = Suave.confidentialInputs();
        Bundle.BundleObj memory paymentBundleObj = abi.decode(paymentBundleData, (Bundle.BundleObj));
        bytes memory paymentBundleJson = Bundle.jsonify(paymentBundleObj);

        require(paymentBundleObj.txns.length == 1, "Payment bundle must contain exactly one transaction");
        Transactions.EIP155 memory paymentTx = Transactions.decodeRLP_EIP155(paymentBundleObj.txns[0]);

        // check validity of payment
        uint64 value = abi.decode(Suave.confidentialRetrieve(dataId, "default:v0:ethMetaBundleValues"), (uint64));
        address feeRecipient = abi.decode(Suave.confidentialRetrieve(dataId, "default:v0:ethMetaBundleFeeRecipient"), (address));
        require(paymentTx.value == value, "PaymentTx amount does not match metaBundle value");
        require(paymentTx.to == feeRecipient, "PaymentTx recipient does not match metaBundle feeRecipient");
        require(Suave.simulateBundle(paymentBundleData) == 0, "Payment bundle is not valid");

        // payment bundle is valid. save it to confidential store
        Suave.DataRecord memory paymentBundleDataRecord = Suave.newDataRecord(
            decryptionCondition, dataAllowedPeekers, dataAllowedStores, "default:v0:ethBundles");
        Suave.confidentialStore(paymentBundleDataRecord.id, "default:v0:ethBundles", paymentBundleJson);

        // save the meta bundle.
        bytes memory bundleIdsData = Suave.confidentialRetrieve(dataId, "default:v0:ethMetaBundles");
        Suave.DataId[] memory bundleIds = abi.decode(bundleIdsData, (Suave.DataId[]));
        Suave.DataRecord memory dataRecord = Suave.newDataRecord(
            decryptionCondition, dataAllowedPeekers, dataAllowedStores, "default:v0:matchMetaBundles");
        Suave.DataId[] memory dataRecords = new Suave.DataId[](bundleIds.length + 1);

        for (uint256 i = 0; i < bundleIds.length; i++) {
            dataRecords[i] = bundleIds[i];
        }
        dataRecords[bundleIds.length] = paymentBundleDataRecord.id;
        Suave.confidentialStore(dataRecord.id, "default:v0:matchMetaBundles", abi.encode(dataRecords));

        // emit event
        return bytes.concat(this.emitMatch.selector, abi.encode(dataRecord));
    }

    function emitMatch(Suave.DataRecord calldata dataRecord) external {
        emit MatchEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers);
    }
}

contract ComposableBlockContract {

    struct PaymentTx {
        address signer;
        address to;
        uint amount;
        string message;
        uint nonce;
        bytes signature;
    }

    struct ConfidentialBuilderSession {
        Suave.DataId bundleId;
    }
}
