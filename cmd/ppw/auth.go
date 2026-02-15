package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/authn"
)

const (
	defaultAuthListenAddr = "localhost:8787"
	metaCallbackPath      = "/auth/meta/callback"
	threadsCallbackPath   = "/auth/threads/callback"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "OAuth login and token lifecycle helpers",
		Long: `Manages one-time OAuth login and persisted token lifecycle.

Primary flow:
  ppw auth login
  ppw auth status

Tokens are stored at auth.token_store from ppw.toml (default: ~/.ppw/tokens.json).`,
	}
	cmd.AddCommand(authLoginCmd())
	cmd.AddCommand(authStatusCmd())
	cmd.AddCommand(authRefreshCmd())
	cmd.AddCommand(authLogoutCmd())
	return cmd
}

func authLoginCmd() *cobra.Command {
	var (
		metaOnly        bool
		threadsOnly     bool
		noBrowser       bool
		listenAddr      string
		timeout         time.Duration
		metaCode        string
		metaRedirectURI string
		threadsCode     string
		threadsRedirect string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Run one-time OAuth login and persist managed tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			if metaOnly && threadsOnly {
				return fmt.Errorf("choose either --meta-only or --threads-only, not both")
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			authMgr, err := buildAuthManager(cfg)
			if err != nil {
				return err
			}
			if authMgr == nil {
				return fmt.Errorf("managed auth requires meta.app_id and meta.app_secret in ppw.toml")
			}

			enableMeta := !threadsOnly
			enableThreads := !metaOnly

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			if enableMeta {
				redirectURI := strings.TrimSpace(metaRedirectURI)
				if metaCode == "" {
					if redirectURI == "" {
						redirectURI = callbackURL(listenAddr, metaCallbackPath)
					}
					code, err := completeBrowserOAuth(ctx, authMgr, "meta", redirectURI, listenAddr, metaCallbackPath, noBrowser)
					if err != nil {
						return err
					}
					metaCode = code
				} else if redirectURI == "" {
					return fmt.Errorf("--meta-redirect-uri is required when using --meta-code")
				}

				if err := authMgr.LoginMeta(ctx, metaCode, redirectURI); err != nil {
					return fmt.Errorf("meta login: %w", err)
				}
				fmt.Println("Meta login completed.")
			}

			if enableThreads {
				redirectURI := strings.TrimSpace(threadsRedirect)
				if threadsCode == "" {
					if redirectURI == "" {
						redirectURI = callbackURL(listenAddr, threadsCallbackPath)
					}
					code, err := completeBrowserOAuth(ctx, authMgr, "threads", redirectURI, listenAddr, threadsCallbackPath, noBrowser)
					if err != nil {
						return err
					}
					threadsCode = code
				} else if redirectURI == "" {
					return fmt.Errorf("--threads-redirect-uri is required when using --threads-code")
				}

				if err := authMgr.LoginThreads(ctx, threadsCode, redirectURI); err != nil {
					return fmt.Errorf("threads login: %w", err)
				}
				fmt.Println("Threads login completed.")
			}

			status, err := authMgr.Status()
			if err != nil {
				return err
			}
			printAuthStatus(status)
			return nil
		},
	}

	cmd.Flags().BoolVar(&metaOnly, "meta-only", false, "Run Meta/Instagram OAuth only")
	cmd.Flags().BoolVar(&threadsOnly, "threads-only", false, "Run Threads OAuth only")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print OAuth URL without opening browser")
	cmd.Flags().StringVar(&listenAddr, "listen-addr", defaultAuthListenAddr, "Loopback listen address for OAuth callback")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Overall timeout for the login flow")
	cmd.Flags().StringVar(&metaCode, "meta-code", "", "Use pre-obtained Meta authorization code")
	cmd.Flags().StringVar(&metaRedirectURI, "meta-redirect-uri", "", "Redirect URI that generated --meta-code")
	cmd.Flags().StringVar(&threadsCode, "threads-code", "", "Use pre-obtained Threads authorization code")
	cmd.Flags().StringVar(&threadsRedirect, "threads-redirect-uri", "", "Redirect URI that generated --threads-code")

	return cmd
}

func authStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show stored token status from PPW token store",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			authMgr, err := buildAuthManager(cfg)
			if err != nil {
				return err
			}
			if authMgr == nil {
				return fmt.Errorf("managed auth requires meta.app_id and meta.app_secret in ppw.toml")
			}
			status, err := authMgr.Status()
			if err != nil {
				return err
			}
			printAuthStatus(status)
			return nil
		},
	}
}

func authRefreshCmd() *cobra.Command {
	var (
		metaOnly    bool
		threadsOnly bool
		timeout     time.Duration
	)
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh stored managed tokens when near expiry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if metaOnly && threadsOnly {
				return fmt.Errorf("choose either --meta-only or --threads-only, not both")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			authMgr, err := buildAuthManager(cfg)
			if err != nil {
				return err
			}
			if authMgr == nil {
				return fmt.Errorf("managed auth requires meta.app_id and meta.app_secret in ppw.toml")
			}

			enableMeta := !threadsOnly
			enableThreads := !metaOnly

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			if enableMeta {
				if _, err := authMgr.MetaUserToken(ctx); err != nil {
					return fmt.Errorf("meta refresh: %w", err)
				}
			}
			if enableThreads {
				if _, err := authMgr.ThreadsAccessToken(ctx); err != nil {
					return fmt.Errorf("threads refresh: %w", err)
				}
			}

			status, err := authMgr.Status()
			if err != nil {
				return err
			}
			printAuthStatus(status)
			return nil
		},
	}
	cmd.Flags().BoolVar(&metaOnly, "meta-only", false, "Refresh Meta/Instagram token only")
	cmd.Flags().BoolVar(&threadsOnly, "threads-only", false, "Refresh Threads token only")
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "Overall timeout for refresh operations")
	return cmd
}

func authLogoutCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Clear managed tokens from the PPW token store",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("logout requires --yes confirmation")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			authMgr, err := buildAuthManager(cfg)
			if err != nil {
				return err
			}
			if authMgr == nil {
				return fmt.Errorf("managed auth requires meta.app_id and meta.app_secret in ppw.toml")
			}
			if err := authMgr.Clear(); err != nil {
				return err
			}
			fmt.Printf("Managed tokens cleared from %s\n", authMgr.StorePath())
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm token-store reset")
	return cmd
}

func completeBrowserOAuth(
	ctx context.Context,
	authMgr *authn.Manager,
	provider string,
	redirectURI string,
	listenAddr string,
	callbackPath string,
	noBrowser bool,
) (string, error) {
	state, err := randomState()
	if err != nil {
		return "", err
	}

	var authURL string
	switch provider {
	case "meta":
		authURL, err = authMgr.MetaAuthURL(redirectURI, state, nil)
	case "threads":
		authURL, err = authMgr.ThreadsAuthURL(redirectURI, state, nil)
	default:
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
	if err != nil {
		return "", err
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server, err := startOAuthCallbackServer(listenAddr, callbackPath, state, codeCh, errCh)
	if err != nil {
		return "", err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("[%s] Authorize URL:\n%s\n", strings.ToUpper(provider), authURL)
	if !noBrowser {
		if err := openBrowser(authURL); err != nil {
			fmt.Printf("[%s] Unable to open browser automatically: %v\n", strings.ToUpper(provider), err)
			fmt.Printf("[%s] Open the URL manually.\n", strings.ToUpper(provider))
		}
	}

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("%s oauth timeout/cancelled: %w", provider, ctx.Err())
	case err := <-errCh:
		return "", err
	case code := <-codeCh:
		return code, nil
	}
}

func startOAuthCallbackServer(
	listenAddr string,
	callbackPath string,
	expectedState string,
	codeCh chan<- string,
	errCh chan<- error,
) (*http.Server, error) {
	callbackPath = ensurePath(callbackPath)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen callback %s: %w", listenAddr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != expectedState {
			http.Error(w, "invalid state", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("oauth callback state mismatch"):
			default:
			}
			return
		}
		if apiErr := strings.TrimSpace(q.Get("error")); apiErr != "" {
			desc := strings.TrimSpace(q.Get("error_description"))
			http.Error(w, "authorization denied", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("oauth callback returned %s (%s)", apiErr, desc):
			default:
			}
			return
		}
		code := strings.TrimSpace(q.Get("code"))
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("oauth callback did not include code"):
			default:
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><h3>PPW authentication completed.</h3>You can return to the terminal.</body></html>"))
		select {
		case codeCh <- code:
		default:
		}
	})

	server := &http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			select {
			case errCh <- fmt.Errorf("oauth callback server: %w", serveErr):
			default:
			}
		}
	}()
	return server, nil
}

func callbackURL(listenAddr, path string) string {
	hostPort := strings.TrimSpace(listenAddr)
	if strings.HasPrefix(hostPort, ":") {
		hostPort = "localhost" + hostPort
	}

	host, port, err := net.SplitHostPort(hostPort)
	if err == nil {
		host = strings.TrimSpace(host)
		// Meta treats localhost specially in dev OAuth flows.
		// Prefer localhost in redirect URIs even if binding on loopback IP.
		if host == "" || host == "127.0.0.1" || host == "0.0.0.0" || host == "::1" || host == "[::1]" {
			host = "localhost"
		}
		hostPort = net.JoinHostPort(host, port)
	}
	return "http://" + hostPort + ensurePath(path)
}

func ensurePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return "/"
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}

func openBrowser(rawURL string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	target := parsed.String()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func printAuthStatus(status authn.Status) {
	fmt.Println("Auth Status")
	fmt.Println("===========")
	fmt.Printf("Store:    %s\n", status.StorePath)
	printTokenStatus("Meta", status.Meta)
	printTokenStatus("Threads", status.Threads)
}

func printTokenStatus(label string, token authn.TokenStatus) {
	if !token.Present {
		fmt.Printf("%-9s not configured\n", label+":")
		return
	}

	flag := "OK"
	if token.Expiring {
		flag = "EXPIRING"
	}
	if token.ExpiresAt.IsZero() {
		fmt.Printf("%-9s user=%s expires=unknown %s\n", label+":", token.UserID, flag)
		return
	}
	fmt.Printf("%-9s user=%s expires=%s (%d days) %s\n",
		label+":", token.UserID, token.ExpiresAt.Format("2006-01-02"), token.DaysToLive, flag)
}
