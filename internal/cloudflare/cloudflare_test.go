package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	cf "github.com/cloudflare/cloudflare-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
)

// fakeAPI emulates the handful of Cloudflare endpoints the client uses.
type fakeAPI struct {
	mu        sync.Mutex
	records   []cf.DNSRecord
	ingress   []cf.UnvalidatedIngressRule
	updates   int
	failPaths map[string]bool
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		key := r.Method + " " + r.URL.Path
		if f.failPaths[key] || f.failPaths[r.Method+" "+pathKind(r.URL.Path)] {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"boom"}]}`))
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/dns_records"):
			var out []cf.DNSRecord
			for _, rec := range f.records {
				if rec.Name == r.URL.Query().Get("name") {
					out = append(out, rec)
				}
			}
			reply(w, out, true)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/dns_records"):
			var rec cf.DNSRecord
			_ = json.NewDecoder(r.Body).Decode(&rec)
			rec.ID = "rec-" + rec.Name
			f.records = append(f.records, rec)
			reply(w, rec, false)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/dns_records/"):
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			kept := f.records[:0]
			for _, rec := range f.records {
				if rec.ID != id {
					kept = append(kept, rec)
				}
			}
			f.records = kept
			reply(w, map[string]string{"id": id}, false)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/configurations"):
			reply(w, cf.TunnelConfigurationResult{TunnelID: "tun", Config: cf.TunnelConfiguration{Ingress: f.ingress}}, false)
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/configurations"):
			var body cf.TunnelConfigurationParams
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.ingress = body.Config.Ingress
			f.updates++
			reply(w, cf.TunnelConfigurationResult{TunnelID: "tun", Config: body.Config}, false)
		default:
			t.Errorf("unexpected request %s", key)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func pathKind(p string) string {
	if strings.HasSuffix(p, "/configurations") {
		return "configurations"
	}
	if strings.Contains(p, "/dns_records") {
		return "dns_records"
	}
	return p
}

func reply(w http.ResponseWriter, result any, paged bool) {
	body := map[string]any{"success": true, "errors": []any{}, "messages": []any{}, "result": result}
	if paged {
		body["result_info"] = map[string]int{"page": 1, "per_page": 100, "count": 1, "total_count": 1, "total_pages": 1}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func newTestClient(t *testing.T, api *fakeAPI) *Client {
	srv := httptest.NewServer(api.handler(t))
	t.Cleanup(srv.Close)
	c, err := New("test-token", "zone", config.TunnelCredentials{AccountID: "acc", TunnelID: "tun"},
		cf.BaseURL(srv.URL), cf.UsingRetryPolicy(0, 0, 0), cf.UsingRateLimit(1000))
	require.NoError(t, err)
	return c
}

const host = "postern-acme.example.com"

func TestEnsureRouteCreatesRecordAndIngress(t *testing.T) {
	api := &fakeAPI{ingress: []cf.UnvalidatedIngressRule{
		{Hostname: "postern.example.com", Service: "http://app:8080"},
		{Service: config.IngressCatchAll},
	}}
	c := newTestClient(t, api)

	require.NoError(t, c.EnsureRoute(context.Background(), host, "tcp://db:5432"))

	require.Len(t, api.records, 1)
	assert.Equal(t, "tun.cfargotunnel.com", api.records[0].Content)
	assert.Equal(t, config.DNSRecordTypeCNAME, api.records[0].Type)
	assert.True(t, *api.records[0].Proxied)
	require.Len(t, api.ingress, 3)
	assert.Equal(t, cf.UnvalidatedIngressRule{Hostname: host, Service: "tcp://db:5432"}, api.ingress[1])
	assert.Equal(t, config.IngressCatchAll, api.ingress[2].Service, "catch-all must stay last")
}

func TestEnsureRouteIsIdempotent(t *testing.T) {
	api := &fakeAPI{}
	c := newTestClient(t, api)
	ctx := context.Background()
	require.NoError(t, c.EnsureRoute(ctx, host, "tcp://db:5432"))
	require.NoError(t, c.EnsureRoute(ctx, host, "tcp://db:5432"))
	assert.Len(t, api.records, 1)
	assert.Equal(t, 1, api.updates)
}

func TestEnsureRouteUpdatesService(t *testing.T) {
	api := &fakeAPI{ingress: []cf.UnvalidatedIngressRule{{Hostname: host, Service: "tcp://old:5432"}}}
	c := newTestClient(t, api)
	require.NoError(t, c.EnsureRoute(context.Background(), host, "tcp://db:5432"))
	assert.Equal(t, "tcp://db:5432", api.ingress[0].Service)
	assert.Len(t, api.ingress, 2)
}

func TestEnsureRouteRefusesForeignRecord(t *testing.T) {
	api := &fakeAPI{records: []cf.DNSRecord{{ID: "x", Type: "A", Name: host, Content: "1.2.3.4"}}}
	c := newTestClient(t, api)
	err := c.EnsureRoute(context.Background(), host, "tcp://db:5432")
	assert.ErrorIs(t, err, ErrForeignRecord)
	assert.Zero(t, api.updates)
}

func TestRemoveRouteDeletesOnlyOwnRecords(t *testing.T) {
	api := &fakeAPI{
		records: []cf.DNSRecord{
			{ID: "own", Type: "CNAME", Name: host, Content: "tun.cfargotunnel.com"},
			{ID: "foreign", Type: "TXT", Name: host, Content: "verification"},
		},
		ingress: []cf.UnvalidatedIngressRule{{Hostname: host, Service: "tcp://db:5432"}, {Service: config.IngressCatchAll}},
	}
	c := newTestClient(t, api)
	require.NoError(t, c.RemoveRoute(context.Background(), host))

	require.Len(t, api.records, 1)
	assert.Equal(t, "foreign", api.records[0].ID)
	assert.Equal(t, []cf.UnvalidatedIngressRule{{Service: config.IngressCatchAll}}, api.ingress)
}

func TestRemoveRouteWhenNothingExists(t *testing.T) {
	api := &fakeAPI{}
	c := newTestClient(t, api)
	require.NoError(t, c.RemoveRoute(context.Background(), host))
	assert.Zero(t, api.updates)
}

func TestErrorsAreWrapped(t *testing.T) {
	cases := map[string]func(*Client) error{
		"GET dns_records":    func(c *Client) error { return c.EnsureRoute(context.Background(), host, "s") },
		"POST dns_records":   func(c *Client) error { return c.EnsureRoute(context.Background(), host, "s") },
		"GET configurations": func(c *Client) error { return c.EnsureRoute(context.Background(), host, "s") },
		"PUT configurations": func(c *Client) error { return c.EnsureRoute(context.Background(), host, "s") },
	}
	for fail, call := range cases {
		t.Run(fail, func(t *testing.T) {
			c := newTestClient(t, &fakeAPI{failPaths: map[string]bool{fail: true}})
			err := call(c)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "test-token")
		})
	}
}

func TestRemoveRouteErrors(t *testing.T) {
	api := &fakeAPI{
		records:   []cf.DNSRecord{{ID: "own", Type: "CNAME", Name: host, Content: "tun.cfargotunnel.com"}},
		failPaths: map[string]bool{"DELETE dns_records": true},
	}
	c := newTestClient(t, api)
	assert.ErrorContains(t, c.RemoveRoute(context.Background(), host), "delete DNS record")

	c = newTestClient(t, &fakeAPI{failPaths: map[string]bool{"GET configurations": true}})
	assert.ErrorContains(t, c.RemoveRoute(context.Background(), host), "tunnel configuration")

	c = newTestClient(t, &fakeAPI{failPaths: map[string]bool{"GET dns_records": true}})
	assert.ErrorContains(t, c.RemoveRoute(context.Background(), host), "list DNS records")
}

func TestNewRejectsEmptyToken(t *testing.T) {
	_, err := New("", "zone", config.TunnelCredentials{})
	assert.Error(t, err)
}
