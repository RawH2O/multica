package protocol

import "time"

// DaemonBatchClaimRequestTimeout is the HTTP client timeout and WS execution
// budget for a machine-level batch claim. The claim response may require
// several cross-region database round trips while loading agent skills,
// plugins, and MCP connections, so keep enough time for those payload queries
// while still leaving room inside the budget for claim recovery and response
// delivery.
const DaemonBatchClaimRequestTimeout = 15 * time.Second
