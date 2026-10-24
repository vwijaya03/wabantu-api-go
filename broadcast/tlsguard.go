// Installs the eMudhra TLS guard + startup self-check in this service process (docs/TLS_GUARD.md).

package broadcast

import _ "encore.app/wabantu/shared/tlsguard/startup" // eMudhra TLS guard + self-check (see docs/TLS_GUARD.md)
