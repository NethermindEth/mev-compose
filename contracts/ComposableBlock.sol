// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.19;

import "../suave-geth/suave/sol/libraries/Suave.sol";


struct LegacyTransaction {
    uint64 nonce;
    uint64 gasPrice;
    uint64 gasLimit;
    address to;
    uint64 value;
    bytes data;
    bytes signature;
}

struct Bundle {
    LegacyTransaction[] txs;
}

library Verifier {
    function getMessageHash(
        address _to,
        uint _amount,
        string memory _message,
        uint _nonce
    ) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(_to, _amount, _message, _nonce));
    }

    function getEthSignedMessageHash(
        bytes32 _messageHash
    ) public pure returns (bytes32) {
        return keccak256(
            abi.encodePacked("\x19Ethereum Signed Message:\n32", _messageHash)
        );
    }

    function verify(
        address _signer,
        address _to,
        uint _amount,
        string memory _message,
        uint _nonce,
        bytes memory signature
    ) public pure returns (bool) {
        bytes32 messageHash = getMessageHash(_to, _amount, _message, _nonce);
        bytes32 ethSignedMessageHash = getEthSignedMessageHash(messageHash);
        return recoverSigner(ethSignedMessageHash, signature) == _signer;
    }

    function recoverSigner(
        bytes32 _ethSignedMessageHash,
        bytes memory _signature
    ) public pure returns (address) {
        (bytes32 r, bytes32 s, uint8 v) = splitSignature(_signature);
        return ecrecover(_ethSignedMessageHash, v, r, s);
    }

    function splitSignature(
        bytes memory sig
    ) public pure returns (bytes32 r, bytes32 s, uint8 v) {
        require(sig.length == 65, "invalid signature length");
        assembly {
            /*
            First 32 bytes stores the length of the signature

            add(sig, 32) = pointer of sig + 32
            effectively, skips first 32 bytes of signature

            mload(p) loads next 32 bytes starting at the memory address p into memory
            */

            // first 32 bytes, after the length prefix
            r := mload(add(sig, 32))
            // second 32 bytes
            s := mload(add(sig, 64))
            // final byte (first byte of the next 32 bytes)
            v := byte(0, mload(add(sig, 96)))
        }
        // implicitly return (r, s, v)
    }
}


contract BundleContract {
    event HintEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers);

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

    function emitHint(Suave.DataRecord calldata dataRecord) external override {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers);
    }

    function emitAndReturn(Suave.DataRecord memory dataRecord) internal virtual returns (bytes memory) {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers);
        return bytes.concat(this.emitHint.selector, abi.encode(dataRecord));
    }
}

contract MetaBundleContract {

    struct MetaBundle {
        Suave.DataId[] bundleIds; // bundleIds contain bundles including nested bundles.
        uint64 value;
        address feeRecipient;
    };

    event HintEvent(Suave.DataId dataId, uint64 decryptionCondition, address[] allowedPeekers, MetaBundleData metaBundle);

    function newBundle(
        uint64 decryptionCondition,
        address[] memory dataAllowedPeekers,
        address[] memory dataAllowedStores,
        MetaBundle memory metaBundle
    ) external payable returns (bytes memory) {
        require(Suave.isConfidential());

        // fetch backrun bundle data.
        bytes memory matchBundleData = Suave.confidentialInputs();

        // sim backrun bundle.
        uint64 egp = Suave.simulateBundle(matchBundleData);

        Suave.DataRecord memory dataRecord = Suave.newDataRecord(
            decryptionCondition, dataAllowedPeekers, dataAllowedStores, "default:v0:matchDataRecords");

        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundles", matchBundleData);
        Suave.confidentialStore(dataRecord.id, "default:v0:ethBundleSimResults", abi.encode(egp));

        // merge data records
        Suave.DataId[] memory dataRecords = new Suave.DataId[](metaBundle.bundleIds.length + 1);
        for (uint256 i = 0; i < bundleIds.length; i++) {
            dataRecords[i] = bundleIds[i];
        }
        dataRecords[bundleIds.length] = dataRecord.id;

        Suave.confidentialStore(dataRecord.id, "default:v0:ethMetaBundles", abi.encode(dataRecords));
        Suave.confidentialStore(dataRecord.id, "default:v0:ethMetaBundleValues", abi.encode(metaBundle.value));
        Suave.confidentialStore(dataRecord.id, "default:v0:ethMetaBundleFeeRecipient", abi.encode(metaBundle.feeRecipient));
        return emitAndReturn(dataRecord, metabundle);
    }

    function emitHint(Suave.DataRecord calldata dataRecord, MetaBundle metaBundle) public {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers, metaBundle);
    }

    function emitAndReturn(Suave.DataRecord memory dataRecord, MetaBundle metaBundle) internal returns (bytes memory) {
        emit HintEvent(dataRecord.id, dataRecord.decryptionCondition, dataRecord.allowedPeekers, metaBundle);
        return bytes.concat(this.emitMetaBundleEvent.selector, abi.encode(dataRecord));
    }

    function newMatch(uint64 decryptionCondition,
                      address[] memory dataAllowedPeekers,
                      address[] memory dataAllowedStores,
                      Suave.DataId memory dataId) external override returns (bytes memory) {
        require(Suave.isConfidential());
        bytes memory txData = Suave.confidentialInputs();
        Bundle bundle = abi.decode(txData, (Bundle))
        require(bundle.txs.length == 1, "Bundle must contain exactly one transaction");
        PaymentTx memory paymentTx = bundle.txs[0];

        bytes memory metaBundleData = Suave.confidentialRetrieve(dataId, "default:v0:ethMetaBundles");
        MetaBundle metaBundle = abi.decode(metaBundleData, (MetaBundle));
        require(paymentTx.amount == metaBundle.value, "PaymentTx amount does not match metaBundle value");
        require(paymentTx.to == metaBundle.feeRecipient, "PaymentTx recipient does not match metaBundle feeRecipient");
        require(Verifier.verify(paymentTx.signer, paymentTx.to, paymentTx.amount, paymentTx.message, paymentTx.nonce, paymentTx.signature), "PaymentTx is not valid");

        Suave.DataRecord dataRecord = Suave.newDataRecord(decryptionCondition, dataAllowedPeekers, dataAllowedStores, "default:v0:matchDataRecords");
        Suave.DataId[] memory dataRecords = new Suave.DataId[](metaBundle.bundleIds.length + 1);

        for (uint256 i = 0; i < metaBundle.bundleIds.length; i++) {
            dataRecords[i] = bundleIds[i];
        }
        dataRecords[metaBundle.bundleIds.length] = dataRecord.id;
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
    };

    struct ConfidentialBuilderSession {
        Suave.DataId bundleId;
    }

    // applies bundle referenced by bundleId to a block referenced by targetId. Also verifies if payment transaction is valid.
    function build(Suave.DataId memory bundleId, Suave.DataId memory blockId, PaymentTx memory payment) external payable returns (bytes memory) {
        require(Suave.isConfidential());
        require(verify(payment.signer, payment.to, payment.amount, payment.message, payment.nonce, payment.signature), "PaymentTx is not valid");
        require(payment.amount == metaBundle.value, "PaymentTx amount does not match metaBundle value");
    }

    function getMessageHash(
        address _to,
        uint _amount,
        string memory _message,
        uint _nonce
    ) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(_to, _amount, _message, _nonce));
    }

    function getEthSignedMessageHash(
        bytes32 _messageHash
    ) public pure returns (bytes32) {
        return keccak256(
            abi.encodePacked("\x19Ethereum Signed Message:\n32", _messageHash)
        );
    }

    function verify(
        address _signer,
        address _to,
        uint _amount,
        string memory _message,
        uint _nonce,
        bytes memory signature
    ) public pure returns (bool) {
        bytes32 messageHash = getMessageHash(_to, _amount, _message, _nonce);
        bytes32 ethSignedMessageHash = getEthSignedMessageHash(messageHash);
        return recoverSigner(ethSignedMessageHash, signature) == _signer;
    }

    function recoverSigner(
        bytes32 _ethSignedMessageHash,
        bytes memory _signature
    ) public pure returns (address) {
        (bytes32 r, bytes32 s, uint8 v) = splitSignature(_signature);
        return ecrecover(_ethSignedMessageHash, v, r, s);
    }

    function splitSignature(
        bytes memory sig
    ) public pure returns (bytes32 r, bytes32 s, uint8 v) {
        require(sig.length == 65, "invalid signature length");
        assembly {
            /*
            First 32 bytes stores the length of the signature

            add(sig, 32) = pointer of sig + 32
            effectively, skips first 32 bytes of signature

            mload(p) loads next 32 bytes starting at the memory address p into memory
            */

            // first 32 bytes, after the length prefix
            r := mload(add(sig, 32))
            // second 32 bytes
            s := mload(add(sig, 64))
            // final byte (first byte of the next 32 bytes)
            v := byte(0, mload(add(sig, 96)))
        }
        // implicitly return (r, s, v)
    }

}
