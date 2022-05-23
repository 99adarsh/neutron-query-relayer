package submit

import (
	"context"
	"fmt"
	"github.com/cosmos/cosmos-sdk/api/tendermint/abci"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtxtypes "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/lidofinance/cosmos-query-relayer/internal/config"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

var mode = signing.SignMode_SIGN_MODE_DIRECT

type TxSender struct {
	ctx           context.Context
	baseTxf       tx.Factory
	txConfig      client.TxConfig
	rpcClient     rpcclient.Client
	chainID       string
	addressPrefix string
	signKeyName   string
	gasPrices     string
}

func TestKeybase(chainID string, keyringRootDir string) (keyring.Keyring, error) {
	keybase, err := keyring.New(chainID, "test", keyringRootDir, nil)
	if err != nil {
		return keybase, err
	}

	return keybase, nil
}

func NewTxSender(ctx context.Context, rpcClient rpcclient.Client, marshaller codec.ProtoCodecMarshaler, keybase keyring.Keyring, cfg config.CosmosQueryRelayerConfig) (*TxSender, error) {
	lidoCfg := cfg.LidoChain
	txConfig := authtxtypes.NewTxConfig(marshaller, authtxtypes.DefaultSignModes)
	baseTxf := tx.Factory{}.
		WithKeybase(keybase).
		WithSignMode(mode).
		WithTxConfig(txConfig).
		WithChainID(lidoCfg.ChainID).
		WithGasAdjustment(lidoCfg.GasAdjustment).
		WithGasPrices(lidoCfg.GasPrices)

	return &TxSender{
		ctx:           ctx,
		txConfig:      txConfig,
		baseTxf:       baseTxf,
		rpcClient:     rpcClient,
		chainID:       lidoCfg.ChainID,
		addressPrefix: lidoCfg.ChainPrefix,
		signKeyName:   lidoCfg.Keyring.SignKeyName,
		gasPrices:     lidoCfg.GasPrices,
	}, nil
}

// Send builds transaction with calculated input msgs, calculated gas and fees, signs it and submits to chain
func (cc *TxSender) Send(sender string, msgs []types.Msg) error {
	account, err := cc.queryAccount(sender)
	if err != nil {
		return err
	}

	txf := cc.baseTxf.
		WithAccountNumber(account.AccountNumber).
		WithSequence(account.Sequence)

	//gasNeeded, err := cc.calculateGas(txf, msgs...)
	//if err != nil {
	//	return err
	//}

	gasNeeded := uint64(2000000)

	txf = txf.
		WithGas(gasNeeded).
		WithGasPrices(cc.gasPrices)

	bz, err := cc.buildTxBz(txf, msgs, gasNeeded)
	if err != nil {
		return err
	}
	res, err := cc.rpcClient.BroadcastTxSync(cc.ctx, bz)

	fmt.Printf("Broadcast result: code=%+v log=%v err=%+v hash=%b", res.Code, res.Log, err, res.Hash)

	if res.Code == 0 {
		return nil
	} else {
		return fmt.Errorf("error broadcasting transaction with log=%s", res.Log)
	}
}

// queryAccount returns BaseAccount for given account address
func (cc *TxSender) queryAccount(address string) (*authtypes.BaseAccount, error) {
	request := authtypes.QueryAccountRequest{Address: address}
	req, err := request.Marshal()
	if err != nil {
		return nil, err
	}
	simQuery := abci.RequestQuery{
		Path: "/cosmos.auth.v1beta1.Query/Account",
		Data: req,
	}
	res, err := cc.rpcClient.ABCIQueryWithOptions(cc.ctx, simQuery.Path, simQuery.Data, rpcclient.DefaultABCIQueryOptions)
	if err != nil {
		return nil, err
	}

	if res.Response.Code != 0 {
		return nil, fmt.Errorf("error fetching account with address=%s log=%s", address, res.Response.Log)
	}

	var response authtypes.QueryAccountResponse
	if err := response.Unmarshal(res.Response.Value); err != nil {
		return nil, err
	}

	var account authtypes.BaseAccount
	err = account.Unmarshal(response.Account.Value)

	if err != nil {
		return nil, err
	}

	return &account, nil
}

func (cc *TxSender) buildTxBz(txf tx.Factory, msgs []types.Msg, gasAmount uint64) ([]byte, error) {
	txBuilder := cc.txConfig.NewTxBuilder()
	err := txBuilder.SetMsgs(msgs...)
	if err != nil {
		fmt.Printf("set msgs failure")
		return nil, err
	}

	txBuilder.SetGasLimit(gasAmount)

	if err != nil {
		return nil, err
	}
	// TODO: shouldn't set it like this. use gas limit and gas prices
	txBuilder.SetFeeAmount(types.NewCoins(types.NewInt64Coin("stake", 500000)))

	fmt.Printf("\nAbout to sign with txf: %+v\n\n", txf)
	err = tx.Sign(txf, cc.signKeyName, txBuilder, true)

	if err != nil {
		return nil, err
	}

	bz, err := cc.txConfig.TxEncoder()(txBuilder.GetTx())
	return bz, err
}

func (cc *TxSender) calculateGas(txf tx.Factory, msgs ...types.Msg) (uint64, error) {
	simulation, err := cc.buildSimulationTx(txf, msgs...)
	if err != nil {
		return 0, err
	}
	// We then call the Simulate method on this client.
	simQuery := abci.RequestQuery{
		Path: "/cosmos.tx.v1beta1.Service/Simulate",
		Data: simulation,
	}
	res, err := cc.rpcClient.ABCIQueryWithOptions(cc.ctx, simQuery.Path, simQuery.Data, rpcclient.DefaultABCIQueryOptions)
	if err != nil {
		return 0, err
	}

	var simRes txtypes.SimulateResponse

	if err := simRes.Unmarshal(res.Response.Value); err != nil {
		return 0, err
	}
	if simRes.GasInfo == nil {
		return 0, fmt.Errorf("no result in simulation response with log=%s code=%d", res.Response.Log, res.Response.Code)
	}

	return uint64(txf.GasAdjustment() * float64(simRes.GasInfo.GasUsed)), nil
}

// buildSimulationTx creates an unsigned tx with an empty single signature and returns
// the encoded transaction or an error if the unsigned transaction cannot be built.
func (cc *TxSender) buildSimulationTx(txf tx.Factory, msgs ...types.Msg) ([]byte, error) {
	txb, err := cc.baseTxf.BuildUnsignedTx(msgs...)
	if err != nil {
		return nil, err
	}

	// Create an empty signature literal as the ante handler will populate with a
	// sentinel pubkey.
	sig := signing.SignatureV2{
		PubKey: &secp256k1.PubKey{},
		Data: &signing.SingleSignatureData{
			SignMode: cc.baseTxf.SignMode(),
		},
		Sequence: txf.Sequence(),
	}
	if err := txb.SetSignatures(sig); err != nil {
		return nil, err
	}

	bz, err := cc.txConfig.TxEncoder()(txb.GetTx())
	if err != nil {
		return nil, nil
	}
	simReq := txtypes.SimulateRequest{TxBytes: bz}
	return simReq.Marshal()
}
