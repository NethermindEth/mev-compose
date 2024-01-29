// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.19;

import "../suave-geth/suave/sol/libraries/Suave.sol";

contract BundleContract {
    function newBundle(
        uint64 decryptionCondition,
        address[] memory dataAllowedPeekers,
        address[] memory dataAllowedStores
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());

        bytes memory bundleData = Suave.confidentialInputs();

        uint64 egp = Suave.simulateBundle(bundleData);

        Suave.DataRecord memory dataRecord =
            Suave.newDataRecord(decryptionCondition, dataAllowedPeekers, dataAllowedStores, "default:v0:ethBundles");

        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundles", bundleData);
        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundleSimResults", abi.encode(egp));

        return emitAndReturn(dataRecord, bundleData);
    }

    function emitDataRecord(Suave.DataRecord calldata dataRecord) public {
        emit DataRecordEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers);
    }

    function emitAndReturn(Suave.DataRecord memory dataRecord, bytes memory) internal virtual returns (bytes memory) {
        emit DataRecordEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers);
        return bytes.concat(this.emitDataRecord.selector, abi.encode(dataRecord));
    }

    struct MetaBundle {
        Suave.DataId[] bundleIds; // bundleIds contain ALL bundles including nested bundles.
        uint64 value;
    };

    function newMerge(
        uint64 decryptionCondition,
        address[] memory dataAllowedPeekers,
        address[] memory dataAllowedStores,
        Suave.DataId[] memory dataIds
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());

        bytes memory bundleData = Suave.confidentialInputs();

        uint64 egp = Suave.simulateBundle(bundleData);

        Suave.DataRecord memory dataRecord =
            Suave.newDataRecord(decryptionCondition, dataAllowedPeekers, dataAllowedStores, "default:v0:ethBundles");

        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundles", bundleData);
        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundleSimResults", abi.encode(egp));

        return emitAndReturn(dataRecord, bundleData);
    }
}

contract MetaBundleContract {

    struct MetaBundleData {
        Suave.DataId[] bundleIds; // bundleIds contain ALL bundles including nested bundles.
        uint64 value;
    };

    function newMetaBundle(
        uint64 decryptionCondition,
        address[] memory dataAllowedPeekers,
        address[] memory dataAllowedStores,
        Suave.DataId[] memory dataIds
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());
    }
}

contract ComposableBlockContract {

    address[] public addressList = [SUAVE.ANYALLOWED];

    struct MetaBundle {

    }

    event NewBundleEvent(
        Suave.DataId dataId,
        uint64 decryptionCondition,
        address[] allowedPeekers
    );

    function newBundle(uint blockNumber) external returns (bytes memory) {
        require(Suave.isConfidential());
        bytes memory bundleData = Suave.confidentialInputs();
        Suave.DataRecord memory record = Suave.newDataRecord(blockNumber, addressList, addressList, "default:v0:ethBundles");
        Suave.confidentialStore(record.id, "default:v0:ethBundles", bundleData);
        return bytes.concat(this.emitNewBundleEvent.selector, abi.encode(record));
    }

    function emitNewBundleEvent(Suave.DataRecord memory record) public {
        emit NewBundleEvent(record.id, record.decryptionCondition, record.allowedPeekers);
    }

    function newMetaBundle(uint blockNumber, Suave.DataId[] ids, bytes ethTxMsg, uint8 v, bytes32 r, bytes32 s) external payable returns (bytes memory) {
        require(Suave.isConfidential());
        bytes memory bundleData = Suave.confidentialInputs();
        Suave.DataRecord memory record = Suave.newDataRecord(blockNumber, addressList, addressList, "default:v0:ethMetaBundles");
        Suave.confidentialStore(record.id, "default:v0:ethMetaBundles", bundleData);
        return bytes.concat(this.emitNewMetaBundleEvent.selector, abi.encode(record));
    }
}
