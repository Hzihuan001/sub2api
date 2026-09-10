package service

import (
	"fmt"
	"sync"
	"time"
)

var resellerPriceChanges = struct {
	sync.Mutex
	epoch    int64
	sequence uint64
	changed  chan struct{}
}{epoch: time.Now().UnixNano(), changed: make(chan struct{})}

// NotifyResellerPricingChanged never calls a reseller or performs I/O. Receivers
// coalesce invalidations and fetch an authenticated, tenant-scoped price snapshot.
func NotifyResellerPricingChanged() {
	resellerPriceChanges.Lock()
	defer resellerPriceChanges.Unlock()
	resellerPriceChanges.sequence++
	close(resellerPriceChanges.changed)
	resellerPriceChanges.changed = make(chan struct{})
}

func ResellerPricingChangeCursor() (string, <-chan struct{}) {
	resellerPriceChanges.Lock()
	defer resellerPriceChanges.Unlock()
	return fmt.Sprintf("%d-%d", resellerPriceChanges.epoch, resellerPriceChanges.sequence), resellerPriceChanges.changed
}
