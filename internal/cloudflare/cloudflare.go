// Package cloudflare publishes hostnames through the Cloudflare Tunnel.
//
// Publishing a hostname needs two things:
//  1. A proxied DNS CNAME <hostname> -> <tunnel-id>.cfargotunnel.com.
//  2. An ingress rule in the (remotely managed) tunnel configuration that
//     tells cloudflared which local service serves that hostname.
//
// The CNAME alone is not enough: without the ingress rule cloudflared falls
// through to the catch-all and answers 404. Ingress rules cannot express a
// prefix wildcard such as "postern-*.example.com", so one rule per hostname
// is managed through the API.
package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	cf "github.com/cloudflare/cloudflare-go"

	"github.com/tecsharp/posterndb/internal/config"
)

// Router is the minimal interface the rest of the app depends on, which
// keeps handlers and the auditor testable with a fake.
type Router interface {
	EnsureRoute(ctx context.Context, hostname, service string) error
	RemoveRoute(ctx context.Context, hostname string) error
}

// ErrForeignRecord means a DNS record with the hostname exists but does not
// point at our tunnel. We refuse to overwrite records we did not create.
var ErrForeignRecord = errors.New("ya existe un registro DNS con ese nombre que apunta a otro lado: bórralo en Cloudflare (DNS > Registros) o usa otro PANEL_HOSTNAME / prefijo")

// Client implements Router with the official cloudflare-go SDK.
type Client struct {
	api       *cf.API
	zone      *cf.ResourceContainer
	account   *cf.ResourceContainer
	tunnelID  string
	cnameDest string
	// mu serialises read-modify-write cycles on the tunnel configuration.
	// The API replaces the whole ingress list, so two concurrent updates
	// would otherwise silently drop each other's rules.
	mu sync.Mutex
}

// New builds a Client. Extra SDK options (e.g. BaseURL in tests) may be given.
func New(apiToken, zoneID string, tunnel config.TunnelCredentials, opts ...cf.Option) (*Client, error) {
	api, err := cf.NewWithAPIToken(apiToken, opts...)
	if err != nil {
		// The SDK error never contains the token, but wrap it generically
		// anyway so nothing secret can reach the logs.
		return nil, fmt.Errorf("init %s client: %w", config.ProviderCloudflare, err)
	}
	return &Client{
		api:       api,
		zone:      cf.ZoneIdentifier(zoneID),
		account:   cf.AccountIdentifier(tunnel.AccountID),
		tunnelID:  tunnel.TunnelID,
		cnameDest: tunnel.TunnelID + config.TunnelCNAMESuffix,
	}, nil
}

// EnsureRoute makes hostname reachable through the tunnel. Idempotent.
func (c *Client) EnsureRoute(ctx context.Context, hostname, service string) error {
	if err := c.ensureDNS(ctx, hostname); err != nil {
		return err
	}
	return c.ensureIngress(ctx, hostname, service)
}

// RemoveRoute unpublishes hostname. Idempotent: missing pieces are ignored.
func (c *Client) RemoveRoute(ctx context.Context, hostname string) error {
	if err := c.removeIngress(ctx, hostname); err != nil {
		return err
	}
	return c.removeDNS(ctx, hostname)
}

func (c *Client) ensureDNS(ctx context.Context, hostname string) error {
	records, err := c.findRecords(ctx, hostname)
	if err != nil {
		return err
	}
	for _, r := range records {
		if !c.ownsRecord(r) {
			return fmt.Errorf("%s: %w", hostname, ErrForeignRecord)
		}
	}
	if len(records) > 0 {
		return nil
	}
	proxied := true
	_, err = c.api.CreateDNSRecord(ctx, c.zone, cf.CreateDNSRecordParams{
		Type:    config.DNSRecordTypeCNAME,
		Name:    hostname,
		Content: c.cnameDest,
		Proxied: &proxied,
		TTL:     1, // 1 = automatic, required for proxied records
		Comment: config.DNSRecordComment,
	})
	if err != nil {
		return fmt.Errorf("create DNS record %s: %w", hostname, err)
	}
	return nil
}

func (c *Client) removeDNS(ctx context.Context, hostname string) error {
	records, err := c.findRecords(ctx, hostname)
	if err != nil {
		return err
	}
	for _, r := range records {
		if !c.ownsRecord(r) {
			continue
		}
		if err := c.api.DeleteDNSRecord(ctx, c.zone, r.ID); err != nil {
			return fmt.Errorf("delete DNS record %s: %w", hostname, err)
		}
	}
	return nil
}

func (c *Client) findRecords(ctx context.Context, hostname string) ([]cf.DNSRecord, error) {
	records, _, err := c.api.ListDNSRecords(ctx, c.zone, cf.ListDNSRecordsParams{Name: hostname})
	if err != nil {
		return nil, fmt.Errorf("list DNS records %s: %w", hostname, err)
	}
	return records, nil
}

func (c *Client) ownsRecord(r cf.DNSRecord) bool {
	return r.Type == config.DNSRecordTypeCNAME && strings.EqualFold(r.Content, c.cnameDest)
}

func (c *Client) ensureIngress(ctx context.Context, hostname, service string) error {
	return c.updateIngress(ctx, func(rules []cf.UnvalidatedIngressRule) ([]cf.UnvalidatedIngressRule, bool) {
		for i, r := range rules {
			if r.Hostname != hostname {
				continue
			}
			if r.Service == service {
				return rules, false
			}
			rules[i].Service = service
			return rules, true
		}
		return append(rules, cf.UnvalidatedIngressRule{Hostname: hostname, Service: service}), true
	})
}

func (c *Client) removeIngress(ctx context.Context, hostname string) error {
	return c.updateIngress(ctx, func(rules []cf.UnvalidatedIngressRule) ([]cf.UnvalidatedIngressRule, bool) {
		kept := rules[:0]
		for _, r := range rules {
			if r.Hostname != hostname {
				kept = append(kept, r)
			}
		}
		return kept, len(kept) != len(rules)
	})
}

// updateIngress runs a locked read-modify-write of the ingress list. The
// mutate function receives the rules without the catch-all; the catch-all
// is re-appended last because cloudflared requires it to be final.
func (c *Client) updateIngress(ctx context.Context, mutate func([]cf.UnvalidatedIngressRule) ([]cf.UnvalidatedIngressRule, bool)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, err := c.api.GetTunnelConfiguration(ctx, c.account, c.tunnelID)
	if err != nil {
		return fmt.Errorf("read tunnel configuration: %w", err)
	}
	rules, changed := mutate(withoutCatchAll(current.Config.Ingress))
	if !changed {
		return nil
	}
	next := current.Config
	next.Ingress = append(rules, cf.UnvalidatedIngressRule{Service: config.IngressCatchAll})
	_, err = c.api.UpdateTunnelConfiguration(ctx, c.account, cf.TunnelConfigurationParams{TunnelID: c.tunnelID, Config: next})
	if err != nil {
		return fmt.Errorf("update tunnel configuration: %w", err)
	}
	return nil
}

func withoutCatchAll(rules []cf.UnvalidatedIngressRule) []cf.UnvalidatedIngressRule {
	out := make([]cf.UnvalidatedIngressRule, 0, len(rules)+1)
	for _, r := range rules {
		if r.Hostname == "" && r.Path == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// NoopRouter is used in local mode, where nothing is published.
type NoopRouter struct{}

// EnsureRoute does nothing.
func (NoopRouter) EnsureRoute(context.Context, string, string) error { return nil }

// RemoveRoute does nothing.
func (NoopRouter) RemoveRoute(context.Context, string) error { return nil }
