package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/realkarych/catacomb/pricing"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

func openReadStore(open storeOpener, dbPath string) (store.Store, error) {
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return nil, ErrStoreNotFound
	}
	s, err := open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("store open: %w", err)
	}
	return s, nil
}

func openWriteStore(open storeOpener, dbPath string) (store.Store, error) {
	s, err := open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("store open: %w", err)
	}
	return s, nil
}

func newPricer() reduce.Pricer {
	eng := pricing.New()
	return reduce.PricerFunc(func(in reduce.PriceInputs) (reduce.PriceResult, bool) {
		r, ok := eng.Cost(pricing.Inputs{
			ModelID:     in.ModelID,
			TokensIn:    in.TokensIn,
			TokensOut:   in.TokensOut,
			CacheReadIn: in.CacheReadIn,
			CacheWrite:  in.CacheWrite,
			ReportedUSD: in.ReportedUSD,
		})
		return reduce.PriceResult{USD: r.USD, Source: r.Source}, ok
	})
}
