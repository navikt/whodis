package teamkatalogen

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"time"

	"github.com/navikt/whodis/internal/cache"
	"github.com/navikt/whodis/internal/httpsupport"
	"go.opentelemetry.io/otel/trace"
)

var apiBaseURI = "http://team-catalog-backend.org"

const (
	detailsTTL        = 30 * time.Minute
	detailsMaxEntries = 10000
)

type Client struct {
	// A nil cache means every lookup goes to Teamkatalogen.
	detailsCache *cache.Cache[*UserDetails]
}

func New() *Client {
	return &Client{detailsCache: newDetailsCache()}
}

// newDetailsCache has no negative caching: failures must be retried, and a user without teams is stable data.
func newDetailsCache() *cache.Cache[*UserDetails] {
	return cache.New(cache.Options[*UserDetails]{
		TTL:        detailsTTL,
		MaxEntries: detailsMaxEntries,
		Clone: func(d *UserDetails) *UserDetails {
			if d == nil {
				return nil
			}
			clone := *d
			clone.Teams = slices.Clone(d.Teams)
			for i := range clone.Teams {
				clone.Teams[i].NaisTeams = slices.Clone(clone.Teams[i].NaisTeams)
			}
			return &clone
		},
	})
}

func (c *Client) DetailsForUser(email string, ctx context.Context) (*UserDetails, error) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	if c.detailsCache == nil {
		return fetchDetailsForUser(email)
	}
	return c.detailsCache.GetOrLoad(ctx, email, func(context.Context) (*UserDetails, error) {
		return fetchDetailsForUser(email)
	})
}

func fetchDetailsForUser(email string) (*UserDetails, error) {
	resp, err := httpsupport.MakeUnauthenticatedGetRequest(apiBaseURI +
		"/member/membership/byUserEmail?email=" + url.QueryEscape(email))
	if err != nil {
		return nil, err
	}
	var userDetalis UserDetails
	if err := json.Unmarshal(resp, &userDetalis); err != nil {
		return nil, err
	}
	return &userDetalis, nil
}

type UserDetails struct {
	Teams []Team `json:"teams"`
}

type Team struct {
	Id           string   `json:"id"`
	Name         string   `json:"name"`
	SlackChannel string   `json:"slackChannel"`
	NaisTeams    []string `json:"naisTeams"`
	Status       string   `json:"status"`
}

func (t Team) IsActive() bool {
	return t.Status != "INACTIVE"
}

func (c *Client) CacheStats() map[string]cache.Stats {
	stats := make(map[string]cache.Stats)
	if c.detailsCache != nil {
		stats["teamkatalogen_user_details"] = c.detailsCache.Stats()
	}
	return stats
}
