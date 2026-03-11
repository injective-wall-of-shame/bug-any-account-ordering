# One Missing Check, $500M at Risk: `MsgBatchUpdateOrders` Let Anyone Drain Any Account on Injective

## Severity: Critical

- No special conditions or permissions are required. Any user on the Injective chain can exploit this to steal all assets from any other account.
- All funds in on Injective are at risk. At the time of submitting this report, total on-chain assets exceeded **$500M**. Data source: [https://intel.arkm.com/explorer/address/0xF955C57f9EA9Dc8781965FEaE0b6A2acE2BAD6f3](https://intel.arkm.com/explorer/address/0xF955C57f9EA9Dc8781965FEaE0b6A2acE2BAD6f3)

## Background

### Subaccount Ownership Model

On Injective, each user address owns one or more **subaccounts**. A subaccount ID is a 32-byte hex string where the first 20 bytes encode the owner's Ethereum address. When a user submits an order, the chain checks that the subaccount referenced in the order actually belongs to the transaction signer. This is the fundamental authorization boundary that prevents users from trading with other people's funds.

This ownership check is performed by `CheckValidSubaccountIDOrNonce()`, which compares the address bytes embedded in the subaccount ID against the transaction sender's address:

```go
// code link: https://github.com/injective-wall-of-shame/injective-core/blob/v1.17.0/injective-chain/modules/exchange/types/common_utils.go#L71-L88
func CheckValidSubaccountIDOrNonce(sender sdk.AccAddress, subaccountId string) error {
    // ...
    subaccountAddress, ok := IsValidSubaccountID(subaccountId)
    if !ok {
        return errors.Wrap(ErrBadSubaccountID, subaccountId)
    }

    if !bytes.Equal(subaccountAddress.Bytes(), sender.Bytes()) {
        return errors.Wrap(ErrBadSubaccountID, subaccountId)
    }
    return nil
}
```

This function is called indirectly through the chain:

```text
SpotOrder.ValidateBasic(sender)
  -> OrderInfo.ValidateBasic(sender, ...)
    -> CheckValidSubaccountIDOrNonce(sender, subaccountId)
```

### The `MsgBatchUpdateOrders` message

`MsgBatchUpdateOrders` is a powerful batch transaction in the exchange module. It allows a user to cancel and create multiple orders — including limit orders, market orders, and binary options orders — all in a single transaction. Its `ValidateBasic` method is responsible for validating every sub-order embedded in the message before the transaction is accepted.

## Vulnerability Details

The `ValidateBasic` method of `MsgBatchUpdateOrders` correctly validates cancel orders and limit orders by passing the `sender` address into each sub-order's `ValidateBasic`:

```go
// code link: https://github.com/injective-wall-of-shame/injective-core/blob/v1.17.0/injective-chain/modules/exchange/types/v2/msgs.go#L1964-L2142

// ...

// ✅ Limit orders are validated — ownership is checked
for idx := range msg.SpotOrdersToCreate {
    if err := msg.SpotOrdersToCreate[idx].ValidateBasic(sender); err != nil {
        return err
    }
}

for idx := range msg.DerivativeOrdersToCreate {
    if err := msg.DerivativeOrdersToCreate[idx].ValidateBasic(sender, false); err != nil {
        return err
    }
}

for idx := range msg.BinaryOptionsOrdersToCreate {
    if err := msg.BinaryOptionsOrdersToCreate[idx].ValidateBasic(sender, true); err != nil {
        return err
    }
}

// ...
```

However, the method **never validates** the three market order arrays. The function jumps directly to a duplicate-check helper that does not perform ownership validation:

```go
// code link: https://github.com/injective-wall-of-shame/injective-core/blob/v1.17.0/injective-chain/modules/exchange/types/v2/msgs.go#L1964-L2142

// ...
for idx := range msg.BinaryOptionsOrdersToCreate {
    if err := msg.BinaryOptionsOrdersToCreate[idx].ValidateBasic(sender, true); err != nil {
        return err
    }
    if msg.BinaryOptionsOrdersToCreate[idx].OrderType.IsAtomic() {
        return errors.Wrap(types.ErrInvalidOrderTypeForMessage, "Binary limit orders can't be atomic orders")
    }
}

// ❌ Market orders are NOT validated — no ownership check at all

// Check for duplicate derivative market orders (same market and subaccount)
if err := ensureNoDuplicateMarketOrders(sender, msg.DerivativeMarketOrdersToCreate); err != nil {
    return err
}

// Check for duplicate binary options market orders (same market and subaccount)
return ensureNoDuplicateMarketOrders(sender, msg.BinaryOptionsMarketOrdersToCreate)
```

The three unvalidated fields are:

- `SpotMarketOrdersToCreate`
- `DerivativeMarketOrdersToCreate`
- `BinaryOptionsMarketOrdersToCreate`

For contrast, when a user submits a standalone `MsgCreateSpotMarketOrder` or `MsgCreateDerivativeMarketOrder`, the ownership check is correctly enforced:

```go
// code link: https://github.com/injective-wall-of-shame/injective-core/blob/v1.17.0/injective-chain/modules/exchange/types/v2/msgs.go#L966-L981

func (msg MsgCreateSpotMarketOrder) ValidateBasic() error {
    senderAddr, _ := sdk.AccAddressFromBech32(msg.Sender)
    // ...
    if err := msg.Order.ValidateBasic(senderAddr); err != nil {  // ✅ ownership checked
        return err
    }
    return nil
}
```

The `MsgBatchUpdateOrders` path bypasses this entirely for market orders.

## Attack Scenario

Injective allows permissionless token creation (via the tokenfactory module) and permissionless spot market creation. This means anyone can mint an arbitrary token and list a trading pair against a real asset like USDT — no governance approval needed. Combined with the missing validation, this enables a fund-theft attack:

1. **Create a worthless token.** The attacker uses the tokenfactory module to create a new token (e.g., `FAKE`) and mints an arbitrary supply to themselves. This requires no permission.

2. **Create a spot market.** The attacker creates a `FAKE/USDT` spot market. This is also permissionless on Injective.

3. **Place a sell order.** The attacker places a limit sell order on the `FAKE/USDT` market from their own subaccount, offering to sell their worthless `FAKE` tokens at a high price denominated in USDT.

4. **Force the victim to buy.** The attacker submits a `MsgBatchUpdateOrders` transaction with a market buy order in `SpotMarketOrdersToCreate`, but sets the `SubaccountId` to a **victim's subaccount**. Because `ValidateBasic` does not validate market orders, the transaction passes all checks. The market buy order executes using the victim's USDT to purchase the attacker's worthless `FAKE` tokens at the attacker's chosen price.

5. **Bridge out of Injective.** The attacker's sell order is filled, and they receive the victim's USDT. The victim is left holding worthless `FAKE` tokens. The attacker then uses Injective's permissionless cross-chain transfer (Peggo bridge) to bridge the stolen USDT out to Ethereum, making the theft irreversible.

This entire attack can be carried out in just a few transactions. It requires no special permissions, no governance action, and no cooperation from the victim. The attacker only needs to know the victim's subaccount ID, which is publicly visible on-chain.

## The PoC

See [`poc/`](poc/)

## Timeline

- **November 30, 2025**: The whitehat submitted the vulnerability report and PoC to Injective via Immunefi.
- **December 1, 2025**: A mainnet upgrade [proposal](https://injhub.com/proposal/601/) to fix this bug went to on-chain vote: before any acknowledgment was sent to me.
- **December 1, 2025**: Injective "acknowledged" receipt of the report (suspected auto-reply).
- **......**: The whitehat sent multiple follow-up messages. No response from Injective.
- **December 14, 2025**: Immunefi's SLA enforcement kicked in automatically due to Injective's lack of response.
- **......**: The whitehat sent multiple follow-up messages. No response from Injective.
- **February 11, 2026**: Injective finally confirmed the validity of the report.
- **March 5, 2026**: Injective offered a $50,000 bounty — only 10% of the $500,000 Critical payout specified in their bug bounty program.
- **......**: The whitehat raised an objection to the payout. No response from Injective.
