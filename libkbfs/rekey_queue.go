// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"sync"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/kbfs/tlf"
	"golang.org/x/net/context"
	"golang.org/x/time/rate"
)

// When provisioning a new device from an existing device, the provisionee
// needs one of the existing devices to rekey for it, or it has to use paperkey
// for the rekey. For the case where an existing device does the rekey, there
// are three routines which eventually all go through this rekey queue. These
// three rekey routines are:
//
// 1. When a new device is added, the service on provisioner calls an RPC into
// KBFS, notifying the latter about the new device (provisionee) and that it
// needs rekey.
// 2. On KBFS client, a background routine runs once per hour. It asks the
// mdserver to check for TLFs that needs rekey. Note that this happens on all
// KBFS devices, no matter it has rekey capability or now.
//
// Both 1 and 2 do this by calling MDServerRemote.CheckForRekeys to send back a
// FoldersNeedRekey request.
//
// 3. When the provisionee gets provisioned, it goes through all TLFs and sends
// a MD update for each one of them, by merely copying (since it doesn't have
// access to the key yet) the existing MD revision while setting the rekey bit
// in the flag.

const (
	numConcurrentRekeys            = 64
	rekeysPerSecond     rate.Limit = 16
	rekeyQueueSize                 = 1024 // 24 KB
)

type RekeyQueueStandard struct {
	config  Config
	log     logger.Logger
	queue   chan tlf.ID
	limiter *rate.Limiter
	cancel  context.CancelFunc

	mu       sync.RWMutex // guards everything below
	pendings map[tlf.ID]bool
}

// TODO: comment
func NewRekeyQueueStandard(config Config) (rkq *RekeyQueueStandard) {
	ctx, cancel := context.WithCancel(context.Background())
	rkq = &RekeyQueueStandard{
		config:   config,
		log:      config.MakeLogger("RQ"),
		queue:    make(chan tlf.ID, rekeyQueueSize),
		limiter:  rate.NewLimiter(rekeysPerSecond, numConcurrentRekeys),
		pendings: make(map[tlf.ID]bool),
		cancel:   cancel,
	}
	rkq.start(ctx)
	return rkq
}

func (rkq *RekeyQueueStandard) start(ctx context.Context) {
	go func() {
		for id := range rkq.queue {
			if err := rkq.limiter.Wait(ctx); err != nil {
				rkq.log.Debug("Waiting on rate limiter for tlf=%v error: %v", id, err)
			} else {
				rkq.config.KBFSOps().RequestRekey(id)
			}
			go func() {
				rkq.mu.Lock()
				defer rkq.mu.Unlock()
				delete(rkq.pendings, id)
			}()
		}
	}()
}

func (rkq *RekeyQueueStandard) Enqueue(id tlf.ID) {
	rkq.mu.Lock()
	defer rkq.mu.Unlock()
	rkq.pendings[id] = true

	select {
	case rkq.queue <- id:
	default:
		// The queue is full; avoid blocking by spawning a goroutine.
		rkq.log.Debug("Rekey queue is full; enqueuing %s in the background", id)
		go func() { rkq.queue <- id }()
	}
}

func (rkq *RekeyQueueStandard) IsRekeyPending(id tlf.ID) bool {
	rkq.mu.RLock()
	defer rkq.mu.RUnlock()
	return rkq.pendings[id]
}

func (rkq *RekeyQueueStandard) Shutdown() {
	rkq.mu.Lock()
	defer rkq.mu.Unlock()
	rkq.cancel()
	rkq.cancel = nil
}

func (rkq *RekeyQueueStandard) New() RekeyQueue {
	rkq.Shutdown()
	return NewRekeyQueueStandard(rkq.config)
}
