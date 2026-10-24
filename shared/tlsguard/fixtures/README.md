# tlsguard fixtures (embedded, used by the startup self-check)

Public test certificates only (no private keys, no secrets).

| file | source | sha256 (file) |
|---|---|---|
| emudhra-dv-g3b-chain.pem | `chains/dv-g3b-emudhra.we-have-a-problem.com.pem` from https://emudhra.we-have-a-problem.com/chains.zip (zip sha256 `7b949956955fc501a30f4d98d2f513d34c0bbbe92e29a968194c96e097ce2345`, downloaded 2026-10-24). Leaf → EM DV TLS CA - G3B → emSign Root TLS CA - G3 (cross-signed by emSign ECC Root CA - G3) | `ab6483de0cb01d53d59275baf74d2766f3599e57a48d3319e4a5536c268ac696` |
| emudhra-dv-g3b-aaa-chain.pem | `chains/dv-g3b-aaa-emudhra.we-have-a-problem.com.pem` from the same zip. Leaf → EM DV TLS CA - G3B cross-signed by Sectigo "AAA Certificate Services" | `3641ff8e872e4c90d5691edb09894914121b267e6a50455347c5f28708fa1af2` |
| root-emsign-ecc-root-ca-g3.pem | "emSign ECC Root CA - G3" from https://curl.se/ca/cacert-2026-09-25.pem (Mozilla bundle before removal). Cert SHA-256 `86A1ECBA…BE6B`, matches https://emudhra.we-have-a-problem.com/roots.json | `0718c554987fa4c6fd992c76765806989b1256d2763ef76ac36875c7a4b0fe52` |
| root-aaa-certificate-services.pem | "AAA Certificate Services" from https://crt.sh/?d=D7A7A0FB5D7E2731D771E9484EBCDEF71D5F0C3E0A2948782BC83EE0EA699EF4 ; cert SHA-256 matches roots.json | `a5ddabd1602ae1c66ce11ad078e734cc473dcb8e9f573037832d8536ae3de90b` |

Leaves are valid 2026-10-20 … 2027-05-03; the self-check verifies at a fixed time (2026-10-23 UTC).
Checked with `openssl verify -attime <2026-10-23> -CAfile <root> -untrusted <chain> <chain>` → OK.
