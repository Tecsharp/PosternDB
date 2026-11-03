package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// domainPattern accepts a lowercase DNS name with at least two labels.
var domainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// hostPrefixPattern restricts CLIENT_HOST_PREFIX to characters valid at the
// start of a DNS label. It may end with "-" because the client slug that
// follows always ends with an alphanumeric character.
var hostPrefixPattern = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*)?$`)

// Config is the fully validated runtime configuration.
type Config struct {
	AdminUser          string
	AdminPassword      string
	JWTSecret          string
	BaseDomain         string
	PanelHostname      string
	ClientHostPrefix   string
	CloudflareAPIToken string
	CloudflareZoneID   string
	Tunnel             TunnelCredentials
	DBExpiration       time.Duration
	ExpiringSoon       time.Duration
	CheckInterval      time.Duration
	BackupBeforeDelete bool
	Postgres           PostgresConfig
	HTTPAddr           string
	TunnelDBService    string
	TunnelPanelService string
	ClientLocalPort    int
	// LocalMode runs without Cloudflare: databases are reachable only on
	// 127.0.0.1:LocalDBPort of the host running PosternDB.
	LocalMode   bool
	LocalDBPort int
	BackupDir   string
	Notify      NotifyConfig
	// AllowSelfRegister enables the public /auth/register endpoint and the
	// "panel de usuario" sign-up link. false = only administrators create
	// client accounts.
	AllowSelfRegister   bool
	MaxDatabasesPerUser int
}

// NotifyConfig configures expiry warnings. Every channel is optional.
type NotifyConfig struct {
	WebhookURL   string
	EmailTo      []string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
}

// EmailEnabled reports whether enough SMTP settings exist to send email.
func (n NotifyConfig) EmailEnabled() bool {
	return n.SMTPHost != "" && n.SMTPFrom != "" && len(n.EmailTo) > 0
}

// PostgresConfig holds the platform database connection settings.
type PostgresConfig struct {
	User     string
	Password string
	DB       string
	Host     string
	Port     int
}

// DSN returns a pgx-compatible connection string. The password is URL-escaped
// so generated secrets containing reserved characters do not break parsing.
func (p PostgresConfig) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(p.User, p.Password),
		Host:     net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:     "/" + p.DB,
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

// TunnelCredentials are decoded from CLOUDFLARE_TUNNEL_TOKEN. The token is a
// base64 JSON document {"a": accountTag, "t": tunnelID, "s": secret}; reading
// it here means the operator does not have to supply account and tunnel IDs
// as separate variables.
type TunnelCredentials struct {
	AccountID string
	TunnelID  string
	Token     string
}

// String deliberately hides the token so the struct is safe to log.
func (t TunnelCredentials) String() string {
	return fmt.Sprintf("tunnel=%s account=%s token=[redacted]", t.TunnelID, t.AccountID)
}

// ErrMissingVariables is returned when one or more required variables are empty.
var ErrMissingVariables = errors.New("missing required environment variables")

// Getenv abstracts os.Getenv so Load can be unit tested.
type Getenv func(string) string

// Load reads, defaults and validates the configuration. Every problem is
// collected so the operator sees all of them in a single run instead of
// fixing one variable per restart.
func Load(getenv Getenv) (*Config, error) {
	p := &parser{getenv: getenv}
	local := p.boolean(EnvLocalMode, false)
	// In local mode Cloudflare and the domain are not used at all.
	cloud := func(name string) string {
		if local {
			return p.value(name)
		}
		return p.required(name)
	}
	cfg := &Config{
		AdminUser:          p.required(EnvAdminUser),
		AdminPassword:      p.required(EnvAdminPassword),
		JWTSecret:          p.required(EnvJWTSecret),
		BaseDomain:         strings.ToLower(cloud(EnvBaseDomain)),
		ClientHostPrefix:   strings.ToLower(p.optional(EnvClientHostPrefix, "")),
		CloudflareAPIToken: cloud(EnvCloudflareAPIToken),
		CloudflareZoneID:   cloud(EnvCloudflareZoneID),
		DBExpiration:       days(p.positiveInt(EnvDBExpirationDays, DefaultDBExpirationDays)),
		ExpiringSoon:       days(p.positiveInt(EnvExpiringSoonDays, DefaultExpiringSoonDays)),
		CheckInterval:      time.Duration(p.positiveInt(EnvCheckIntervalHours, DefaultCheckIntervalHours)) * time.Hour,
		BackupBeforeDelete: p.boolean(EnvBackupBeforeDelete, DefaultBackupBeforeDelete),
		Postgres: PostgresConfig{
			User:     p.required(EnvPostgresUser),
			Password: p.required(EnvPostgresPassword),
			DB:       p.required(EnvPostgresDB),
			Host:     p.optional(EnvPostgresHost, DefaultPostgresHost),
			Port:     p.positiveInt(EnvPostgresPort, DefaultPostgresPort),
		},
		HTTPAddr:            p.optional(EnvHTTPAddr, DefaultHTTPAddr),
		TunnelDBService:     p.optional(EnvTunnelDBService, DefaultTunnelDBService),
		TunnelPanelService:  p.optional(EnvTunnelPanelService, DefaultTunnelPanelService),
		ClientLocalPort:     p.positiveInt(EnvClientDBPort, DefaultClientLocalPort),
		LocalMode:           local,
		LocalDBPort:         p.positiveInt(EnvLocalDBPort, DefaultLocalDBPort),
		BackupDir:           p.optional(EnvBackupDir, DefaultBackupDir),
		AllowSelfRegister:   p.boolean(EnvAllowSelfRegister, DefaultAllowSelfRegister),
		MaxDatabasesPerUser: p.positiveInt(EnvMaxDatabasesPerClient, DefaultMaxDBsPerClient),
		Notify: NotifyConfig{
			WebhookURL:   p.value(EnvNotifyWebhookURL),
			EmailTo:      splitList(p.value(EnvNotifyEmailTo)),
			SMTPHost:     p.value(EnvSMTPHost),
			SMTPPort:     p.positiveInt(EnvSMTPPort, DefaultSMTPPort),
			SMTPUsername: p.value(EnvSMTPUsername),
			SMTPPassword: p.value(EnvSMTPPassword),
			SMTPFrom:     p.value(EnvSMTPFrom),
		},
	}
	defaultPanel := ""
	if cfg.BaseDomain != "" {
		defaultPanel = DefaultPanelSubdomain + "." + cfg.BaseDomain
	}
	cfg.PanelHostname = strings.ToLower(p.optional(EnvPanelHostname, defaultPanel))
	if token := cloud(EnvCloudflareTunnelToken); token != "" {
		cfg.Tunnel = p.tunnel(token)
	}
	p.validate(cfg)
	return cfg, p.err()
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

type parser struct {
	getenv  Getenv
	missing []string
	invalid []string
}

func (p *parser) value(name string) string {
	return strings.TrimSpace(p.getenv(name))
}

func (p *parser) required(name string) string {
	v := p.value(name)
	if v == "" {
		p.missing = append(p.missing, name)
	}
	return v
}

func (p *parser) optional(name, fallback string) string {
	if v := p.value(name); v != "" {
		return v
	}
	return fallback
}

func (p *parser) positiveInt(name string, fallback int) int {
	raw := p.value(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		p.invalidf("%s must be a positive integer (got %q)", name, raw)
		return fallback
	}
	return n
}

func (p *parser) boolean(name string, fallback bool) bool {
	raw := p.value(name)
	if raw == "" {
		return fallback
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		p.invalidf("%s must be true or false (got %q)", name, raw)
		return fallback
	}
	return b
}

func (p *parser) tunnel(token string) TunnelCredentials {
	creds, err := ParseTunnelToken(token)
	if err != nil {
		// Never echo the token itself: it grants control of the tunnel.
		p.invalidf("%s is not a valid tunnel token: %v", EnvCloudflareTunnelToken, err)
	}
	return creds
}

func (p *parser) invalidf(format string, args ...any) {
	p.invalid = append(p.invalid, fmt.Sprintf(format, args...))
}

func (p *parser) validate(cfg *Config) {
	if cfg.Notify.WebhookURL != "" && !strings.HasPrefix(cfg.Notify.WebhookURL, "https://") && !strings.HasPrefix(cfg.Notify.WebhookURL, "http://") {
		p.invalidf("%s must be an http(s) URL", EnvNotifyWebhookURL)
	}
	if cfg.BaseDomain != "" && !domainPattern.MatchString(cfg.BaseDomain) {
		p.invalidf("%s must be a domain like example.com (got %q)", EnvBaseDomain, cfg.BaseDomain)
	}
	if strings.Contains(cfg.PanelHostname, "://") || strings.Contains(cfg.BaseDomain, "://") {
		p.invalidf("%s and %s take a hostname such as postern.example.com, not a URL; leave %s empty to use postern.<%s>",
			EnvBaseDomain, EnvPanelHostname, EnvPanelHostname, EnvBaseDomain)
	} else if cfg.BaseDomain != "" && !isWithinDomain(cfg.PanelHostname, cfg.BaseDomain) {
		p.invalidf("%s must be %s or a subdomain of it (got %q)", EnvPanelHostname, cfg.BaseDomain, cfg.PanelHostname)
	}
	if !hostPrefixPattern.MatchString(cfg.ClientHostPrefix) || len(cfg.ClientHostPrefix) > MaxClientHostPrefixLength {
		p.invalidf("%s may only contain a-z, 0-9 and '-', start with a letter or digit and be at most %d characters (got %q)",
			EnvClientHostPrefix, MaxClientHostPrefixLength, cfg.ClientHostPrefix)
	}
	if cfg.JWTSecret != "" && len(cfg.JWTSecret) < MinJWTSecretLength {
		p.invalidf("%s must be at least %d characters; re-run setup to generate one", EnvJWTSecret, MinJWTSecretLength)
	}
	if cfg.AdminPassword != "" && len(cfg.AdminPassword) < MinAdminPasswordLength {
		p.invalidf("%s must be at least %d characters", EnvAdminPassword, MinAdminPasswordLength)
	}
	if cfg.ExpiringSoon >= cfg.DBExpiration {
		p.invalidf("%s must be smaller than %s", EnvExpiringSoonDays, EnvDBExpirationDays)
	}
}

func (p *parser) err() error {
	var parts []string
	if len(p.missing) > 0 {
		parts = append(parts, fmt.Sprintf("%v: %s (set them in .env or run ./setup.sh)", ErrMissingVariables, strings.Join(p.missing, ", ")))
	}
	parts = append(parts, p.invalid...)
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(parts, "\n  - "))
}

// ParseTunnelToken decodes a Cloudflare tunnel token without validating the
// secret against Cloudflare.
func ParseTunnelToken(token string) (TunnelCredentials, error) {
	raw, err := decodeBase64(token)
	if err != nil {
		return TunnelCredentials{}, errors.New("token is not base64")
	}
	var payload struct {
		AccountTag string `json:"a"`
		TunnelID   string `json:"t"`
		Secret     string `json:"s"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return TunnelCredentials{}, errors.New("token payload is not JSON")
	}
	if payload.AccountTag == "" || payload.TunnelID == "" || payload.Secret == "" {
		return TunnelCredentials{}, errors.New("token payload is missing account, tunnel or secret")
	}
	return TunnelCredentials{AccountID: payload.AccountTag, TunnelID: payload.TunnelID, Token: token}, nil
}

func decodeBase64(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("invalid base64")
}

func isWithinDomain(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func days(n int) time.Duration {
	return time.Duration(n) * 24 * time.Hour
}
