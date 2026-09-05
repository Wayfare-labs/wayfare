# SEP-38 fee-denomination identity

SEP-38 is how a Stellar anchor answers "what will you give me for this?" —
sell USDC, buy naira, with a fee. The fee is where the trap lives.

---

## The problem

SEP-38 returns a fee that may be denominated in **either** the sell asset or
the buy asset. The response tells you which via `fee.asset`. This is easy to
miss, and getting it wrong produces a **unit error** rather than a crash — the
arithmetic succeeds and the number is simply wrong.

### A worked example of the bug

The spec's own worked example is exactly the shape a remittance uses:

```
sell 100 USDC → buy 500 BRL, price 0.18, fee 10.00 in USDC
```

Naively computing a pre-fee gross as `buy_amount + fee`:

```
500 BRL + 10 USDC = 510 ???
```

Ten units of USDC added to five hundred units of BRL — a meaningless quantity
that looks plausible enough to ship. No error is raised. The published figure
is wrong.

---

## The identity

The spec gives two definitions of price depending on where the fee sits:

```
fee in sell asset:  price = (sell_amount - fee) / buy_amount
fee in buy asset:   price = sell_amount / (buy_amount + fee)
```

Solving each for the pre-fee gross, expressed in buy-asset units, gives the
**same expression** in both cases:

```
gross_in_buy_asset = sell_amount / price
```

This is the identity. It needs no branch on `fee.asset`, and it is correct
whichever denomination the anchor chose.

### Verification against the spec example

With a sell-asset fee of 10 USDC:

```
gross = 100 / 0.18 = 555.56 BRL
fee   = 555.56 - 500 = 55.56 BRL  (not 10 USDC)
```

The fee the user sees is 55.56 BRL — five times the raw `fee.total` — because
the anchor denominated its fee in the sell asset while the user counts in the
buy asset. Converting the fee into the recipient's currency is the correct
thing to show; showing 10 USDC alongside 500 BRL would be mixing units.

---

## How the implementation uses it

`sep38.Quote.normalize()` computes:

```go
q.GrossBuyAmount = q.SellAmount.Div(q.Price)
q.FeeInBuyAsset  = q.GrossBuyAmount.Sub(q.BuyAmount)
```

No branch. No switch on `fee.asset`. The same two lines handle both
denominations because the identity is the same in both cases.

`FeeInBuyAsset` is the figure shown to users: the fee expressed in the
currency the recipient is counting, whatever denomination the anchor used.

---

## Why the golden tests exist

A regression in this arithmetic would not crash. It would quietly change a
published figure — a worse failure mode than a panic, because it ships.

The golden files in `sep38/testdata/golden/` pin the expected output on disk
rather than in an assertion that somebody could adjust while "fixing" a failing
test. Three cases are pinned:

| Case | Fee denomination | Why it matters |
|:-----|:----------------|:---------------|
| Spec worked example (542 BRL → 100 USDC, fee 42 BRL) | sell asset | The spec's own example |
| Assets reversed (100 USDC → 500 BRL, fee 10 USDC) | sell asset | The package doc's example |
| Buy-asset fee (100 USDC → 500 BRL, fee 10 BRL) | buy asset | The only case where `FeeInBuyAsset` equals `fee.total` exactly |

The first two both denominate in the sell asset — pinning only those two
would exercise one branch twice and leave the buy-asset branch unpinned.
`TestGoldenCoversBothDenominations` guards this coverage claim.

---

## Related

- `sep38/sep38.go` — the implementation
- `sep38/golden_test.go` — the golden-file tests
- `sep38/testdata/golden/` — the pinned expected outputs
- [SEP-38 spec](https://stellar.org/protocol/sep-0038) — the standard this identity comes from
