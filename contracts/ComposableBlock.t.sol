pragma solidity ^0.8.19;

import "forge-std/Test.sol";
import "forge-std/console.sol";
import "suave-std/Test.sol";
import "suave-std/suavelib/Suave.sol";
import "suave-std/protocols/Bundle.sol";
import "suave-std/Transactions.sol";
import "solady/src/utils/LibString.sol";
import "solady/src/utils/JSONParserLib.sol";
import "./ComposableBlock.sol";

contract ComposableBlock is Test, SuaveEnabled {
    using JSONParserLib for *;
    using LibString for *;

    string fundedAccountPrivKey = "6c45335a22461ccdb978b78ab61b238bad2fae4544fb55c14eb096c875ccfc52";
    string fundedAccountAddr = "b5feafbdd752ad52afb7e1bd2e40432a485bbb7f";

    address accountAddr = address(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266);
    string accountPrivKey = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80";

    function testParseBundleJson() public {
        MetaBundleContract metaBundleContract = new MetaBundleContract();

        string memory json = '{'
            '"blockNumber": 11223344,'
            '"minTimestamp": 1625072400,'
            '"maxTimestamp": 1625076000,'
            '"txns": ['
                '"0xdeadbeef",'
                '"0xc0ffee",'
                '"0x00aabb"'
                ']'
            '}';

        Bundle.BundleObj memory bundle = metaBundleContract.parseBundleJson(json);
        assertEq(bundle.blockNumber, 11223344);
        assertEq(bundle.minTimestamp, 1625072400);
        assertEq(bundle.maxTimestamp, 1625076000);
        assertEq(bundle.txns.length, 3);
        assertEq(bundle.txns[0].toHexString(), "0xdeadbeef");
        assertEq(bundle.txns[1].toHexString(), "0xc0ffee");
        assertEq(bundle.txns[2].toHexString(), "0x00aabb");
    }

    address[] public addressList = [0xC8df3686b4Afb2BB53e60EAe97EF043FE03Fb829];

    using Transactions for Transactions.EIP155Request;
    using Transactions for Transactions.EIP155;

    function createBundle(uint256 blockNumber, Transactions.EIP155Request memory txData,
                          string memory privKey) internal returns (Bundle.BundleObj memory) {
        Transactions.EIP155 memory txn = txData.signTxn(privKey);
        bytes[] memory txns = new bytes[](1);
        txns[0] = txn.encodeRLP();
        Bundle.BundleObj memory bundle = Bundle.BundleObj({
            blockNumber: uint64(blockNumber),
            minTimestamp: 0,
            maxTimestamp: 0,
            txns: txns
        });
        return bundle;
    }

    function encodeBundleJSON(Bundle.BundleObj memory args) internal returns (bytes memory) {
        bytes memory params =
            abi.encodePacked('{"blockNumber": "', LibString.toHexString(args.blockNumber), '", "txs": [');
        for (uint256 i = 0; i < args.txns.length; i++) {
            params = abi.encodePacked(params, '"', LibString.toHexString(args.txns[i]), '"');
            if (i < args.txns.length - 1) {
                params = abi.encodePacked(params, ",");
            } else {
                params = abi.encodePacked(params, "]");
            }
        }
        if (args.minTimestamp > 0) {
            params = abi.encodePacked(params, ', "minTimestamp": ', LibString.toString(args.minTimestamp));
        }
        if (args.maxTimestamp > 0) {
            params = abi.encodePacked(params, ', "maxTimestamp": ', LibString.toString(args.maxTimestamp));
        }
        params = abi.encodePacked(params, "}");
        return bytes(params);
    }

    function testNewMetaBundle() public {
        BasicBundleContract basicBundleContract = new BasicBundleContract();
        MetaBundleContract metaBundleContract = new MetaBundleContract();
        Transactions.EIP155Request memory txData = Transactions.EIP155Request({
            nonce: 0,
            gasPrice: 13,
            gas: 21000,
            to: address(0),
            value: 0,
            data: new bytes(0),
            chainId: 1337
        });
        uint64 blockNumber = 11223344;
        Bundle.BundleObj memory bundle = createBundle(blockNumber, txData, fundedAccountPrivKey);
        bytes memory bundleData = encodeBundleJSON(bundle);
        setConfidentialInputs(bundleData);
        Suave.DataRecord memory record = basicBundleContract.createBundle(blockNumber, addressList, addressList);

        Suave.DataId[] memory bundleIds = new Suave.DataId[](1);
        bundleIds[0] = record.id;

        MetaBundleContract.MetaBundle memory metaBundle = MetaBundleContract.MetaBundle({
            bundleIds: bundleIds,
            value: 10000,
            feeRecipient: address(0)
        });
        Suave.DataRecord memory metaBundleRecord = metaBundleContract.createMetaBundle(blockNumber, metaBundle);
    }
}
