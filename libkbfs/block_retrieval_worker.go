// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"io"
)

// blockRetrievalWorker processes blockRetrievalQueue requests
type blockRetrievalWorker struct {
	blockGetter
	stopCh chan struct{}
	queue  *blockRetrievalQueue
}

// run runs the worker loop until Shutdown is called
func (brw *blockRetrievalWorker) run() {
	for {
		err := brw.HandleRequest()
		// Only io.EOF is relevant to the loop; other errors are handled in
		// FinalizeRequest
		if err == io.EOF {
			return
		}
	}
}

// newBlockRetrievalWorker returns a blockRetrievalWorker for a given
// blockRetrievalQueue, using the passed in blockGetter to obtain blocks for
// requests.
func newBlockRetrievalWorker(bg blockGetter, q *blockRetrievalQueue) *blockRetrievalWorker {
	brw := &blockRetrievalWorker{
		blockGetter: bg,
		stopCh:      make(chan struct{}),
		queue:       q,
	}
	go brw.run()
	return brw
}

// HandleRequest is the main work method for the worker. It obtains a
// blockRetrieval from the queue, retrieves the block using
// blockGetter.getBlock, and responds to the subscribed requestors with the
// results.
func (brw *blockRetrievalWorker) HandleRequest() (err error) {
	retrievalCh := brw.queue.WorkOnRequest()
	var retrieval *blockRetrieval
	select {
	case retrieval = <-retrievalCh:
		if retrieval == nil {
			panic("Received a nil block retrieval. This should never happen.")
		}
	case <-brw.stopCh:
		return io.EOF
	}

	var block Block
	defer func() {
		brw.queue.FinalizeRequest(retrieval, block, err)
	}()

	// Handle canceled contexts.
	select {
	case <-retrieval.ctx.Done():
		return retrieval.ctx.Err()
	default:
	}

	wasBlockCached := func() bool {
		retrieval.reqMtx.RLock()
		defer retrieval.reqMtx.RUnlock()
		if retrieval.requests[0].block == nil {
			panic("Nil block passed in for first request. This should never happen.")
		}
		// Attempt to retrieve the block from the cache. This might be a
		// specific type where the request blocks are CommonBlocks, but that
		// direction can Set correctly. The cache will never have CommonBlocks.
		block, err = brw.queue.cacheFunc().Get(retrieval.blockPtr)
		if err == nil && block != nil {
			return true
		}
		// Create a new block of the same type as the first request
		block = retrieval.requests[0].block.NewEmpty()
		return false
	}()

	if wasBlockCached {
		return nil
	}

	return brw.getBlock(retrieval.ctx, retrieval.kmd, retrieval.blockPtr, block)
}

// Shutdown shuts down the blockRetrievalWorker once its current work is done.
func (brw *blockRetrievalWorker) Shutdown() {
	select {
	case <-brw.stopCh:
	default:
		close(brw.stopCh)
	}
}
