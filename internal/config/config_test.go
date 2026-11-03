package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tunnelToken(account, tunnel, secret string) string {
	payload := `{"a":"` + account + `","t":"` + tunnel + `","s":"` + secret + `"}`
	return base64.StdEncoding.EncodeToString([]byte(payload))
}

func validEnv() map[string]string {
	return map[string]string{
		EnvAdminUser:             "admin",
		EnvAdminPassword:         "a-very-long-password",
		EnvJWTSecret:             strings.Repeat("s", 40),
		EnvBaseDomain:            "PachisLab.com",
		EnvClientHostPrefix:      "postern-",
		EnvCloudflareAPIToken:    "cf-token",
		EnvCloudflareZoneID:      "zone",
		EnvCloudflareTunnelToken: tunnelToken("acc", "tun-id", "secret"),
		EnvPostgresUser:          "posterndb",
		EnvPostgresPassword:      "p@ss:word/#",
		EnvPostgresDB:            "posterndb",
	}
}

func getenv(m map[string]string) Getenv {
	return func(k string) string { return m[k] }
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	require.NoError(t, err)

	assert.Equal(t, "pachislab.com", cfg.BaseDomain)
	assert.Equal(t, "postern.pachislab.com", cfg.PanelHostname)
	assert.Equal(t, "postern-", cfg.ClientHostPrefix)
	assert.Equal(t, 30*24*time.Hour, cfg.DBExpiration)
	assert.Equal(t, 7*24*time.Hour, cfg.ExpiringSoon)
	assert.Equal(t, 6*time.Hour, cfg.CheckInterval)
	assert.True(t, cfg.BackupBeforeDelete)
	assert.Equal(t, DefaultPostgresHost, cfg.Postgres.Host)
	assert.Equal(t, DefaultPostgresPort, cfg.Postgres.Port)
	assert.Equal(t, DefaultTunnelDBService, cfg.TunnelDBService)
	assert.Equal(t, "acc", cfg.Tunnel.AccountID)
	assert.Equal(t, "tun-id", cfg.Tunnel.TunnelID)
}

func TestLoadOverrides(t *testing.T) {
	env := validEnv()
	env[EnvBaseDomain] = "posterndb.com"
	env[EnvClientHostPrefix] = ""
	env[EnvPanelHostname] = "posterndb.com"
	env[EnvDBExpirationDays] = "10"
	env[EnvExpiringSoonDays] = "2"
	env[EnvCheckIntervalHours] = "1"
	env[EnvBackupBeforeDelete] = "false"
	env[EnvPostgresPort] = "6543"

	cfg, err := Load(getenv(env))
	require.NoError(t, err)
	assert.Equal(t, "posterndb.com", cfg.PanelHostname)
	assert.Empty(t, cfg.ClientHostPrefix)
	assert.Equal(t, 10*24*time.Hour, cfg.DBExpiration)
	assert.Equal(t, time.Hour, cfg.CheckInterval)
	assert.False(t, cfg.BackupBeforeDelete)
	assert.Equal(t, 6543, cfg.Postgres.Port)
}

func TestLoadReportsAllMissingVariables(t *testing.T) {
	_, err := Load(getenv(map[string]string{}))
	require.Error(t, err)
	for _, name := range []string{EnvAdminUser, EnvAdminPassword, EnvJWTSecret, EnvBaseDomain,
		EnvCloudflareAPIToken, EnvCloudflareZoneID, EnvCloudflareTunnelToken, EnvPostgresPassword} {
		assert.Contains(t, err.Error(), name)
	}
	assert.Contains(t, err.Error(), "setup.sh")
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]struct {
		key, value, want string
	}{
		"non numeric days":    {EnvDBExpirationDays, "abc", EnvDBExpirationDays},
		"zero interval":       {EnvCheckIntervalHours, "0", EnvCheckIntervalHours},
		"bad bool":            {EnvBackupBeforeDelete, "maybe", EnvBackupBeforeDelete},
		"bad domain":          {EnvBaseDomain, "not a domain", EnvBaseDomain},
		"panel url":           {EnvPanelHostname, "http://127.0.0.1:28437", "not a URL"},
		"base domain url":     {EnvBaseDomain, "https://pachislab.com", "not a URL"},
		"foreign panel":       {EnvPanelHostname, "panel.other.com", EnvPanelHostname},
		"bad prefix chars":    {EnvClientHostPrefix, "pos_tern", EnvClientHostPrefix},
		"prefix leading dash": {EnvClientHostPrefix, "-x", EnvClientHostPrefix},
		"prefix too long":     {EnvClientHostPrefix, strings.Repeat("a", 21), EnvClientHostPrefix},
		"short jwt secret":    {EnvJWTSecret, "short", EnvJWTSecret},
		"short password":      {EnvAdminPassword, "short", EnvAdminPassword},
		"soon not smaller":    {EnvExpiringSoonDays, "30", EnvExpiringSoonDays},
		"bad tunnel token":    {EnvCloudflareTunnelToken, "%%%", EnvCloudflareTunnelToken},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			env[tc.key] = tc.value
			_, err := Load(getenv(env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadNeverEchoesTunnelToken(t *testing.T) {
	env := validEnv()
	env[EnvCloudflareTunnelToken] = base64.StdEncoding.EncodeToString([]byte("super-secret-not-json"))
	_, err := Load(getenv(env))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), env[EnvCloudflareTunnelToken])
}

func TestParseTunnelToken(t *testing.T) {
	creds, err := ParseTunnelToken(strings.TrimRight(tunnelToken("a1", "t1", "s1"), "="))
	require.NoError(t, err)
	assert.Equal(t, "a1", creds.AccountID)
	assert.Equal(t, "t1", creds.TunnelID)

	_, err = ParseTunnelToken("!!!")
	assert.ErrorContains(t, err, "base64")
	_, err = ParseTunnelToken(base64.StdEncoding.EncodeToString([]byte("nope")))
	assert.ErrorContains(t, err, "JSON")
	_, err = ParseTunnelToken(tunnelToken("a", "", "s"))
	assert.ErrorContains(t, err, "missing")
}

func TestTunnelCredentialsStringRedactsToken(t *testing.T) {
	c := TunnelCredentials{AccountID: "acc", TunnelID: "tun", Token: "secret-token"}
	assert.NotContains(t, c.String(), "secret-token")
	assert.Contains(t, c.String(), "redacted")
}

func TestDSNEscapesCredentials(t *testing.T) {
	p := PostgresConfig{User: "u", Password: "p@ss:word/#", DB: "db", Host: "db", Port: 5432}
	assert.Equal(t, "postgres://u:p%40ss%3Aword%2F%23@db:5432/db?sslmode=disable", p.DSN())
}

func TestLocalModeDoesNotRequireCloudflare(t *testing.T) {
	env := validEnv()
	for _, k := range []string{EnvBaseDomain, EnvCloudflareAPIToken, EnvCloudflareZoneID, EnvCloudflareTunnelToken} {
		delete(env, k)
	}
	env[EnvLocalMode] = "true"
	cfg, err := Load(getenv(env))
	require.NoError(t, err)
	assert.True(t, cfg.LocalMode)
	assert.Empty(t, cfg.PanelHostname)
	assert.Equal(t, DefaultLocalDBPort, cfg.LocalDBPort)
	assert.Equal(t, DefaultBackupDir, cfg.BackupDir)

	env[EnvLocalMode] = "false"
	_, err = Load(getenv(env))
	assert.ErrorContains(t, err, EnvCloudflareAPIToken)
}

func TestNotifyConfig(t *testing.T) {
	env := validEnv()
	env[EnvNotifyWebhookURL] = "https://discord.com/api/webhooks/x"
	env[EnvNotifyEmailTo] = " ops@example.com, ,me@example.com "
	env[EnvSMTPHost] = "smtp.example.com"
	env[EnvSMTPFrom] = "posterndb@example.com"
	cfg, err := Load(getenv(env))
	require.NoError(t, err)
	assert.Equal(t, []string{"ops@example.com", "me@example.com"}, cfg.Notify.EmailTo)
	assert.Equal(t, DefaultSMTPPort, cfg.Notify.SMTPPort)
	assert.True(t, cfg.Notify.EmailEnabled())

	env[EnvNotifyWebhookURL] = "discord.com/x"
	_, err = Load(getenv(env))
	assert.ErrorContains(t, err, EnvNotifyWebhookURL)
	assert.False(t, NotifyConfig{SMTPHost: "h"}.EmailEnabled())
}

func TestGoogleSignInIsOffByDefault(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	require.NoError(t, err)
	assert.False(t, cfg.Google.Enabled())
	assert.Empty(t, cfg.Google.RedirectURL)
}

func TestGoogleRedirectDefaultsToThePanelAddress(t *testing.T) {
	env := validEnv()
	env[EnvPanelHostname] = "app.pachislab.com"
	env[EnvGoogleClientID] = "id.apps.googleusercontent.com"
	env[EnvGoogleClientSecret] = "secret"
	cfg, err := Load(getenv(env))
	require.NoError(t, err)
	assert.True(t, cfg.Google.Enabled())
	assert.Equal(t, "https://app.pachislab.com/api/auth/google/callback", cfg.Google.RedirectURL)

	env[EnvGoogleRedirectURL] = "https://elsewhere.example/api/auth/google/callback"
	cfg, err = Load(getenv(env))
	require.NoError(t, err)
	assert.Equal(t, "https://elsewhere.example/api/auth/google/callback", cfg.Google.RedirectURL, "an explicit value wins")
}

func TestGoogleRedirectInLocalModeUsesTheLoopbackPort(t *testing.T) {
	env := map[string]string{
		EnvAdminUser: "admin", EnvAdminPassword: "a-very-long-password", EnvJWTSecret: strings.Repeat("s", 40),
		EnvPostgresUser: "posterndb", EnvPostgresPassword: "pw", EnvPostgresDB: "posterndb",
		EnvLocalMode: "true", EnvGoogleClientID: "id", EnvGoogleClientSecret: "secret",
	}
	cfg, err := Load(getenv(env))
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:28437/api/auth/google/callback", cfg.Google.RedirectURL)

	env[EnvPanelLocalPort] = "31000"
	cfg, err = Load(getenv(env))
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:31000/api/auth/google/callback", cfg.Google.RedirectURL)
}

func TestGoogleNeedsBothClientIDAndSecret(t *testing.T) {
	for _, only := range []string{EnvGoogleClientID, EnvGoogleClientSecret} {
		env := validEnv()
		env[only] = "x"
		_, err := Load(getenv(env))
		require.Error(t, err, only)
		assert.Contains(t, err.Error(), EnvGoogleClientID)
		assert.Contains(t, err.Error(), EnvGoogleClientSecret)
	}

	env := validEnv()
	env[EnvGoogleClientID], env[EnvGoogleClientSecret], env[EnvGoogleRedirectURL] = "id", "secret", "not-a-url"
	_, err := Load(getenv(env))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvGoogleRedirectURL)
}

func TestAuthModeDefaultsFollowGoogleCredentials(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	require.NoError(t, err)
	assert.Equal(t, AuthModePassword, cfg.AuthMode)
	assert.False(t, cfg.GoogleActive())
	assert.True(t, cfg.ClientPasswordLogin())

	env := validEnv()
	env[EnvGoogleClientID], env[EnvGoogleClientSecret] = "id", "secret"
	cfg, err = Load(getenv(env))
	require.NoError(t, err)
	assert.Equal(t, AuthModeBoth, cfg.AuthMode, "credentials present and no AUTH_MODE: both")
	assert.True(t, cfg.GoogleActive())
}

func TestAuthModeExplicitChoices(t *testing.T) {
	withGoogle := func(mode string) map[string]string {
		env := validEnv()
		env[EnvGoogleClientID], env[EnvGoogleClientSecret], env[EnvAuthMode] = "id", "secret", mode
		return env
	}

	cfg, err := Load(getenv(withGoogle("PASSWORD")))
	require.NoError(t, err)
	assert.Equal(t, AuthModePassword, cfg.AuthMode, "case does not matter")
	assert.False(t, cfg.GoogleActive(), "password mode ignores the Google credentials")

	cfg, err = Load(getenv(withGoogle(AuthModeGoogle)))
	require.NoError(t, err)
	assert.True(t, cfg.GoogleActive())
	assert.False(t, cfg.ClientPasswordLogin(), "google mode: no password for registered users")

	cfg, err = Load(getenv(withGoogle(AuthModeBoth)))
	require.NoError(t, err)
	assert.True(t, cfg.GoogleActive())
	assert.True(t, cfg.ClientPasswordLogin())
}

func TestAuthModeRejectsMistakes(t *testing.T) {
	for _, mode := range []string{AuthModeGoogle, AuthModeBoth} {
		env := validEnv()
		env[EnvAuthMode] = mode
		_, err := Load(getenv(env))
		require.Error(t, err, mode)
		assert.Contains(t, err.Error(), EnvGoogleClientID, "%s needs the Google credentials", mode)
	}
	env := validEnv()
	env[EnvAuthMode] = "facebook"
	_, err := Load(getenv(env))
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvAuthMode)
}
