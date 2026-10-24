# tlsguard test fixtures

- `chains/*.pem` + `chains/README.txt`: all 14 chains from https://emudhra.we-have-a-problem.com/chains.zip
  (sha256 `7b949956955fc501a30f4d98d2f513d34c0bbbe92e29a968194c96e097ce2345`, downloaded 2026-10-24 WIB),
  unmodified. Covers the 6 TLS intermediates (DV/OV/EV × G3B/G3C), each via emSign Root TLS CA - G3 and via
  the Sectigo "AAA Certificate Services" cross-sign. The two CS (code-signing) intermediates have no TLS chain.
- `google-www-chain.pem`: leaf `www.google.com` + `WE2`, captured 2026-10-24 with
  `openssl s_client -connect www.google.com:443 -showcerts` (valid 2026-09-28 … 2026-12-21).
- `gts-root-r4.pem`: GTS Root R4 from the system Mozilla bundle (`/etc/ssl/certs/GTS_Root_R4.pem`,
  SHA-256 `349DFA40…3C7D`).
Tests verify at fixed time 2026-10-23 UTC.
