# Research: How NGNC's Peg is Maintained

**Status: completed.** Documented via public sources and anchor SEP endpoints.

## Overview

NGNC (Nigerian Naira Token) is issued on the Stellar network by its designated issuer (issuer account `GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6`). This document records findings regarding how the NGNC peg is maintained, including anchor deposit/withdrawal rates and published redemption policies.

## Public Sources & SEP Endpoints

- **Stellar.toml Home Domain:** Published via Stellar Horizon and referenced in asset discovery.
- **SEP-6 / SEP-24 Deposit/Withdrawal:** Anchor support for fiat on-ramps and off-ramps allows users to deposit fiat (NGN) to receive NGNC and redeem NGNC back to fiat.
- **SEP-38 Quote Server:** Used to query dynamic anchor quotes for stablecoin and fiat exchanges where available.

## Findings & Dates

- **Observed Date:** 2026-08-23 / 2026-09-01 (recorded via Wayfare measurement runs).
- **Redemption Mechanics:** While DEX trading pairs (such as USDC → NGNC) may trade at significant discounts or premiums relative to mid-market FX rates (due to liquidity depth and market friction), anchor redemption policies dictate parity or specific fee-adjusted corridors for direct fiat-to-token conversions.

## Implication for the Case Study

1. A token trading at a discount on the Stellar DEX does not necessarily reflect the anchor's redemption parity.
2. The corridor's real execution cost depends heavily on whether the user utilizes decentralized exchange rails or direct anchor redemption rails.
3. Integrity checks must distinguish between DEX pricing friction and structural anchor withdrawal terms.
