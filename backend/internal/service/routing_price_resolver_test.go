package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type routingPriceBookRepositoryStub struct {
	UpstreamPriceBookRepository
	revisionErr error
}

func (r routingPriceBookRepositoryStub) GetPublishedRevision(context.Context, int64) (*UpstreamPriceBookRevision, error) {
	return nil, r.revisionErr
}

func TestRepositoryRoutingPriceResolverTreatsMissingEffectiveRevisionAsUnknownPrice(t *testing.T) {
	bookID := int64(3)
	resolver := NewRepositoryRoutingPriceResolver(routingPriceBookRepositoryStub{revisionErr: ErrUpstreamPriceBookRevisionNotFound})

	quote, known, err := resolver.Quote(context.Background(), &Account{PriceBookID: &bookID}, "gpt-test")
	require.NoError(t, err)
	require.False(t, known)
	require.Equal(t, RoutingPriceQuote{}, quote)
}
