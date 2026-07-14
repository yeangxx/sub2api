package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type cachedRoutingPriceBook struct {
	revisionID int64
	prices     []UpstreamModelPrice
	expiresAt  time.Time
}

// RepositoryRoutingPriceResolver keeps the database out of the normal hot
// path while still allowing price-book revisions to become visible quickly.
type RepositoryRoutingPriceResolver struct {
	repo  UpstreamPriceBookRepository
	ttl   time.Duration
	mu    sync.RWMutex
	cache map[int64]cachedRoutingPriceBook
}

func NewRepositoryRoutingPriceResolver(repo UpstreamPriceBookRepository) *RepositoryRoutingPriceResolver {
	return &RepositoryRoutingPriceResolver{repo: repo, ttl: 30 * time.Second, cache: make(map[int64]cachedRoutingPriceBook)}
}

func (r *RepositoryRoutingPriceResolver) Quote(ctx context.Context, account *Account, model string) (RoutingPriceQuote, bool, error) {
	if r == nil || r.repo == nil || account == nil || account.PriceBookID == nil || *account.PriceBookID <= 0 {
		return RoutingPriceQuote{}, false, nil
	}
	bookID := *account.PriceBookID
	prices, err := r.load(ctx, bookID)
	if err != nil {
		if errors.Is(err, ErrUpstreamPriceBookRevisionNotFound) {
			return RoutingPriceQuote{}, false, nil
		}
		return RoutingPriceQuote{}, false, err
	}
	price, ok := matchUpstreamModelPrice(prices, model)
	if !ok {
		return RoutingPriceQuote{}, false, nil
	}
	return RoutingPriceQuote{
		InputPerMillion:  price.InputPricePerMillion.InexactFloat64(),
		OutputPerMillion: price.OutputPricePerMillion.InexactFloat64(),
		RequestPrice:     price.RequestPrice.InexactFloat64(),
	}, true, nil
}

func (r *RepositoryRoutingPriceResolver) load(ctx context.Context, bookID int64) ([]UpstreamModelPrice, error) {
	now := time.Now()
	r.mu.RLock()
	entry, ok := r.cache[bookID]
	r.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.prices, nil
	}
	revision, err := r.repo.GetPublishedRevision(ctx, bookID)
	if err != nil {
		return nil, err
	}
	prices, err := r.repo.ListModelPrices(ctx, revision.ID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cache[bookID] = cachedRoutingPriceBook{revisionID: revision.ID, prices: prices, expiresAt: now.Add(r.ttl)}
	r.mu.Unlock()
	return prices, nil
}

func matchUpstreamModelPrice(prices []UpstreamModelPrice, model string) (UpstreamModelPrice, bool) {
	for _, price := range prices {
		if price.ModelPattern == model {
			return price, true
		}
	}
	for _, price := range prices {
		pattern := strings.TrimSuffix(price.ModelPattern, "*")
		if strings.HasSuffix(price.ModelPattern, "*") && strings.HasPrefix(model, pattern) {
			return price, true
		}
	}
	return UpstreamModelPrice{}, false
}
