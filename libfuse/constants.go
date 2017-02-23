// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import "time"

// QuotaUsageStaleTolerance is the age of stale usage data, less than which
// KBFS shouldn't bother making a RPC call to block server to refresh.
const QuotaUsageStaleTolerance = 10 * time.Second
