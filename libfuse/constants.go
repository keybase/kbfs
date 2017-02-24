// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import "time"

// QuotaUsageStaleTolerance is the lifespan of stale usage data that libfuse
// accetps in the Statfs handler. In other words, this causes libkbfs to issue
// a fresh RPC call if cached usage data is older than 10s.
const QuotaUsageStaleTolerance = 10 * time.Second
