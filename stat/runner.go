package stat

import (
	"time"
)

type FetcherRunner interface {
	GetBlockTicker() <-chan time.Time
	GetReserveRatesTicker() <-chan time.Time
	GetAggregateTradeStatsTicker() <-chan time.Time
	Start() error
	Stop() error
}
