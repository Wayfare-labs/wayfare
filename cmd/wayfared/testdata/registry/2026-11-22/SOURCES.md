# Recorded stellar.toml documents, 2026-11-22

These are the issuer documents `wayfared -verify-registry` re-reads, recorded so
the command's tests can run with no network egress (CI runs the suite inside a
network namespace with no route out). Each file is the document served at
`https://<domain>/.well-known/stellar.toml` on 2026-11-22 and is stored
byte-for-byte as fetched, including line endings.

| File | Source | SHA-256 |
|:---|:---|:---|
| `ngnc.online.toml` | https://ngnc.online/.well-known/stellar.toml | `5f551baf0ee2ad2f953d23f096c7fa5bb89763e8f5b51da731303e4e6997933e` |
| `cowrie.exchange.toml` | https://cowrie.exchange/.well-known/stellar.toml | `11c1c8f721b2ac3742682949ddcbeec8866a98bfcff6a7ca1ee04b10ce109882` |
| `zeam.money.toml` | https://zeam.money/.well-known/stellar.toml | `03c5c1227b7e5781cc247bee74ac02b98bf2f41173543747f25e4be6da1dfcd9` |
| `mtl.montelibero.org.toml` | https://mtl.montelibero.org/.well-known/stellar.toml | `c8dcc9a2aefe7c073e60b0a3aaa52282bbe72b33ac7405afd330368c2a9796b0` |
| `token-metadata.paxos.com.toml` | https://token-metadata.paxos.com/.well-known/stellar.toml | `3d6c7cca351d208e4cb026185e071229db9432d5d7b658b9d9af28a8ebf4cc94` |
| `www.circle.com.toml` | https://www.circle.com/.well-known/stellar.toml | `f070510b81d12db211816bc5de5bb784804eca6d66ece153b6d382ec61084e49` |

The fixtures are evidence, not policy. Where a recorded document no longer
corroborates its `asset/known.go` entry — for example USDC, whose registry status
is `unverified` while Circle's document declares it without a status (read as
`live` per SEP-1) — the test asserts exactly that, so the outcome records what
was observed rather than what the registry would prefer.

Refreshing a recording is a deliberate act: replace the file, update its hash
here, and re-read the expected outcomes in `registry_test.go`. Do not edit a
document's bytes to make a test pass — the point of the fixture is that nobody
wrote it.

To re-record one domain:

```
curl -fsS https://<domain>/.well-known/stellar.toml -o <domain>.toml
sha256sum <domain>.toml
```
