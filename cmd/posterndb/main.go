// Command posterndb is the PosternDB server. This file only wires
// dependencies together; all logic lives in internal/ packages.
//
// Subcommands:
//
//	posterndb                      start the server (default)
//	posterndb healthcheck          exit 0 if /health answers 200 (Docker healthcheck)
//	posterndb migrate-hostnames    move DBs to the hostname scheme in .env
//	          [--dry-run]          only print what would change
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tecsharp/posterndb/internal/auditor"
	"github.com/tecsharp/posterndb/internal/auth"
	"github.com/tecsharp/posterndb/internal/clients"
	"github.com/tecsharp/posterndb/internal/cloudflare"
	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/credentials"
	"github.com/tecsharp/posterndb/internal/expiry"
	"github.com/tecsharp/posterndb/internal/httpapi"
	"github.com/tecsharp/posterndb/internal/naming"
	"github.com/tecsharp/posterndb/internal/notify"
	"github.com/tecsharp/posterndb/internal/oauth"
	"github.com/tecsharp/posterndb/internal/provision"
	"github.com/tecsharp/posterndb/internal/store"
	"github.com/tecsharp/posterndb/web"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("fatal", "error", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, logger *slog.Logger) error {
	command := config.CommandServe
	if len(args) > 0 {
		command = args[0]
	}
	if command == config.CommandHealthcheck {
		return healthcheck(os.Getenv(config.EnvHTTPAddr))
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := wire(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.pool.Close()
	switch command {
	case config.CommandServe:
		return serve(ctx, cfg, app, logger)
	case config.CommandMigrateHosts:
		return migrateHostnames(ctx, app, len(args) > 1 && args[1] == config.FlagDryRun)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

type application struct {
	pool      *pgxpool.Pool
	meta      *store.Metadata
	cluster   *store.Cluster
	router    cloudflare.Router
	service   *provision.Service
	clients   *clients.Service
	notifier  *notify.Multi
	inspector *store.Inspector
}

func wire(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*application, error) {
	pool, err := store.Open(ctx, cfg.Postgres.DSN(), time.Minute)
	if err != nil {
		return nil, err
	}
	app, err := build(ctx, cfg, pool, logger)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return app, nil
}

func build(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, logger *slog.Logger) (*application, error) {
	if err := store.Migrate(ctx, pool); err != nil {
		return nil, err
	}
	cluster := store.NewCluster(pool)
	if err := cluster.Harden(ctx, cfg.Postgres.DB); err != nil {
		return nil, err
	}
	meta := store.NewMetadata(pool)
	inspector, err := setupConsole(ctx, cfg, cluster, meta)
	if err != nil {
		return nil, err
	}
	router, err := newRouter(cfg)
	if err != nil {
		return nil, err
	}
	secrets := credentials.New()
	service := provision.New(meta, cluster, router, inspector, secrets, provision.Options{
		Resolver:           naming.NewResolver(cfg),
		Policy:             expiry.NewPolicy(cfg),
		BackupBeforeDelete: cfg.BackupBeforeDelete,
		TunnelDBService:    cfg.TunnelDBService,
		ClientLocalPort:    cfg.ClientLocalPort,
		LocalDBPort:        cfg.LocalDBPort,
		BackupDir:          cfg.BackupDir,
		Logger:             logger,
	})
	// A user may not take the owner's username: it would look like the owner in
	// logs and the activity feed. (Accounts never share the owner's session.)
	reserved := func(_ context.Context, name string) (bool, error) {
		return name == cfg.AdminUser, nil
	}
	return &application{
		pool: pool, meta: meta, cluster: cluster, router: router, service: service, inspector: inspector,
		clients:  clients.New(meta, secrets, cfg.AllowSelfRegister, clients.WithReservedNames(reserved)),
		notifier: notify.New(cfg.Notify),
	}, nil
}

// setupConsole (re)creates the console role with a fresh in-memory password
// and makes sure it can reach every live client database.
func setupConsole(ctx context.Context, cfg *config.Config, cluster *store.Cluster, meta *store.Metadata) (*store.Inspector, error) {
	gen := credentials.New()
	password, err := gen.Password(config.GeneratedPasswordLn)
	if err != nil {
		return nil, err
	}
	verifier, err := gen.ScramVerifier(password)
	if err != nil {
		return nil, err
	}
	if err := cluster.EnsureConsoleRole(ctx, verifier); err != nil {
		return nil, fmt.Errorf("console role: %w", err)
	}
	active, err := meta.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	for _, db := range active {
		if err := cluster.GrantConsoleAccess(ctx, db.DBName, db.RoleName); err != nil {
			return nil, fmt.Errorf("console access to %s: %w", db.DBName, err)
		}
	}
	inspector, err := store.NewInspector(cfg.Postgres.DSN(), password)
	if err != nil {
		return nil, err
	}
	inspector.OnConnect = func(ctx context.Context, database string) {
		_ = meta.IncrementInternalSessions(ctx, database)
	}
	return inspector, nil
}

func newRouter(cfg *config.Config) (cloudflare.Router, error) {
	if cfg.LocalMode {
		return cloudflare.NoopRouter{}, nil
	}
	return cloudflare.New(cfg.CloudflareAPIToken, cfg.CloudflareZoneID, cfg.Tunnel)
}

func serve(ctx context.Context, cfg *config.Config, app *application, logger *slog.Logger) error {
	if !cfg.LocalMode {
		go publishPanel(ctx, cfg, app.router, logger)
	}
	go auditor.New(app.meta, app.cluster, app.service, app.notifier, cfg.CheckInterval, logger).Run(ctx)

	authn := auth.New(cfg.AdminUser, cfg.AdminPassword, cfg.JWTSecret, config.SessionTTL, app.clients)
	authn.SetUserPasswordLogin(cfg.ClientPasswordLogin())
	var google httpapi.GoogleAuth
	if cfg.GoogleActive() {
		google = oauth.NewGoogle(ctx, cfg.Google.ClientID, cfg.Google.ClientSecret, cfg.Google.RedirectURL)
		logger.Info("google sign-in enabled", "redirectURL", cfg.Google.RedirectURL)
	}
	handler := httpapi.NewRouter(httpapi.Deps{
		Auth:      authn,
		Limiter:   auth.NewRateLimiter(config.LoginRatePerMinute, config.LoginRateBurst),
		Databases: app.service,
		Clients:   app.clients,
		Google:    google,
		Audit:     app.meta,
		Health:    app.pool,
		Notifier:  app.notifier,
		Settings:  settingsFrom(cfg),
		Static:    web.Dist(),
		Logger:    logger,
	})
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: config.ReadHeaderTimeout}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	logger.Info("posterndb listening", "addr", cfg.HTTPAddr, "panel", cfg.PanelHostname, "localMode", cfg.LocalMode, "tunnel", cfg.Tunnel.String())

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// publishPanel makes the panel hostname reachable through the tunnel,
// retrying with backoff so a transient Cloudflare error at boot does not
// keep the app (and its /health) down.
func publishPanel(ctx context.Context, cfg *config.Config, router cloudflare.Router, logger *slog.Logger) {
	delay := 5 * time.Second
	for {
		callCtx, cancel := context.WithTimeout(ctx, config.ExternalCallTimeout)
		err := router.EnsureRoute(callCtx, cfg.PanelHostname, cfg.TunnelPanelService)
		cancel()
		if err == nil {
			logger.Info("panel published", "hostname", cfg.PanelHostname)
			return
		}
		logger.Error("publish panel hostname failed; retrying", "hostname", cfg.PanelHostname, "error", err, "retryIn", delay.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Minute)
	}
}

func settingsFrom(cfg *config.Config) httpapi.Settings {
	return httpapi.Settings{
		BaseDomain:            cfg.BaseDomain,
		PanelHostname:         cfg.PanelHostname,
		ClientHostPrefix:      cfg.ClientHostPrefix,
		ExampleHostname:       naming.NewResolver(cfg).Hostname("cliente1"),
		ExpirationDays:        int(cfg.DBExpiration.Hours() / 24),
		ExpiringSoonDays:      int(cfg.ExpiringSoon.Hours() / 24),
		CheckIntervalHours:    int(cfg.CheckInterval.Hours()),
		BackupBeforeDelete:    cfg.BackupBeforeDelete,
		LocalMode:             cfg.LocalMode,
		LocalDBPort:           cfg.LocalDBPort,
		ClientLocalPort:       cfg.ClientLocalPort,
		AllowSelfRegister:     cfg.AllowSelfRegister,
		MaxDatabasesPerClient: cfg.MaxDatabasesPerUser,
		AuthMode:              cfg.AuthMode,
		LandingURL:            cfg.LandingURL,
	}
}

func migrateHostnames(ctx context.Context, app *application, dryRun bool) error {
	changes, err := app.service.MigrateHostnames(ctx, dryRun, config.ActorSystem)
	for _, c := range changes {
		fmt.Printf("%-30s %s -> %s\n", c.Slug, c.From, c.To)
	}
	if len(changes) == 0 {
		fmt.Println("all databases already use the configured hostname scheme")
	}
	if dryRun && len(changes) > 0 {
		fmt.Println("dry run: nothing changed. Re-run without --dry-run to apply.")
	}
	return err
}

// healthcheck lets the distroless image (no shell, no curl) implement the
// Docker HEALTHCHECK with its own binary.
func healthcheck(addr string) error {
	if addr == "" {
		addr = config.DefaultHTTPAddr
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf(config.HealthcheckURLFormat, addr))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health returned %d", resp.StatusCode)
	}
	return nil
}
