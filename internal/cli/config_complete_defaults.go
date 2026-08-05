package cli

import "time"

// providerCatalogTimeout bounds live provider metadata lookups because config
// completion runs before command contexts are threaded into these helpers.
const providerCatalogTimeout = 30 * time.Second
