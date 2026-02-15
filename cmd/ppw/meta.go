package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/meta"
)

func metaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "meta",
		Short: "Meta platform utilities (legacy)",
	}
	cmd.AddCommand(metaAuthCmd())
	return cmd
}

func metaAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "auth",
		Short:      "Meta auth token management (deprecated)",
		Deprecated: "use `ppw auth login` and `ppw auth status`; `ppw meta auth` will be removed in a future release",
	}
	cmd.AddCommand(metaAuthDoctorCmd())
	cmd.AddCommand(metaAuthExchangeCmd())
	return cmd
}

func metaAuthDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose Meta API auth health",
		Long: `Checks user token validity, permissions, page accessibility,
IG account linkage, and page token derivation.

Requires environment variables:
  INSTAGRAM_ACCESS_TOKEN (user token)
  META_APP_ID, META_APP_SECRET (for debug_token)
  META_PAGE_ID (Facebook Page linked to IG account)

Never prints full tokens.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			printMetaAuthDeprecationWarning()
			cfg := buildMetaConfig()
			if cfg.UserToken == "" {
				return fmt.Errorf("INSTAGRAM_ACCESS_TOKEN is required")
			}

			tm := meta.NewTokenManager(cfg)
			report, err := tm.Doctor(context.Background())
			if err != nil {
				return fmt.Errorf("doctor: %w", err)
			}
			printDoctorReport(report)
			return nil
		},
	}
}

func metaAuthExchangeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exchange",
		Short: "Exchange short-lived token for long-lived token (~60 days)",
		Long: `Exchanges the current INSTAGRAM_ACCESS_TOKEN (short-lived, ~1 hour)
for a long-lived token (~60 days).

Requires: META_APP_ID, META_APP_SECRET, INSTAGRAM_ACCESS_TOKEN

After exchange, update your .env file with the new token.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			printMetaAuthDeprecationWarning()
			cfg := buildMetaConfig()
			tm := meta.NewTokenManager(cfg)

			token, expiresAt, err := tm.EnsureLongLivedUserToken(context.Background())
			if err != nil {
				return fmt.Errorf("exchange: %w", err)
			}

			days := int(time.Until(expiresAt).Hours() / 24)
			suffix := token[len(token)-8:]

			fmt.Println("Long-lived token obtained successfully!")
			fmt.Printf("  Expires: %s (%d days)\n", expiresAt.Format("2006-01-02"), days)
			fmt.Printf("  Token:   ...%s\n", suffix)
			fmt.Println()
			fmt.Println("Update INSTAGRAM_ACCESS_TOKEN in your .env file with the full token below:")
			fmt.Println()
			fmt.Println(token)

			return nil
		},
	}
}

func buildMetaConfig() meta.Config {
	return meta.Config{
		AppID:     os.Getenv("META_APP_ID"),
		AppSecret: os.Getenv("META_APP_SECRET"),
		PageID:    os.Getenv("META_PAGE_ID"),
		UserToken: os.Getenv("INSTAGRAM_ACCESS_TOKEN"),
	}
}

func printDoctorReport(r meta.DoctorReport) {
	fmt.Println("Meta Auth Doctor")
	fmt.Println("================")

	// User
	if r.UserID != "" {
		fmt.Printf("User:         %s (id: %s)\n", r.UserName, r.UserID)
	} else {
		fmt.Println("User:         (unable to resolve)")
	}

	// Permissions
	if len(r.Permissions) > 0 {
		fmt.Printf("Permissions:  %s\n", joinMax(r.Permissions, 5))
	} else {
		fmt.Println("Permissions:  (none granted)")
	}

	// Token
	tokenType := r.TokenDebug.Type
	if tokenType == "" {
		tokenType = "unknown"
	}
	fmt.Printf("Token:        %s", tokenType)
	if !r.TokenDebug.ExpiresAt.IsZero() {
		fmt.Printf(", expires %s (%d days)", r.TokenDebug.ExpiresAt.Format("2006-01-02"), r.DaysToExpiry)
	}
	fmt.Println()

	// Status
	switch r.TokenStatus {
	case "valid":
		fmt.Println("Status:       VALID")
	case "expiring_soon":
		fmt.Println("Status:       EXPIRING SOON - run `ppw meta auth exchange`")
	case "invalid":
		fmt.Println("Status:       INVALID - token needs replacement")
	default:
		fmt.Printf("Status:       %s\n", r.TokenStatus)
	}

	fmt.Println()

	// Page
	if r.PageID != "" {
		fmt.Printf("Page:         %s (id: %s)\n", r.PageName, r.PageID)
	} else {
		fmt.Println("Page:         (not configured or inaccessible)")
	}

	// IG account
	if r.IGUserID != "" {
		fmt.Printf("IG account:   @%s (id: %s)\n", r.IGUsername, r.IGUserID)
	} else {
		fmt.Println("IG account:   (not linked)")
	}

	// Page token
	if r.PageTokenOK {
		fmt.Println("Page token:   derivable (OK)")
	} else {
		fmt.Println("Page token:   FAILED - check page ID and user token")
	}

	fmt.Println()
	if r.TokenStatus == "valid" && r.PageTokenOK && r.IGUserID != "" {
		fmt.Println("All checks passed.")
	} else {
		fmt.Println("Some checks failed. Review the output above.")
	}
	fmt.Println("Migration: use `ppw auth login` for managed token storage/refresh.")
}

func printMetaAuthDeprecationWarning() {
	fmt.Fprintln(os.Stderr, "Warning: `ppw meta auth ...` is deprecated. Use `ppw auth login|status|logout`.")
}

// joinMax joins up to n strings with ", " and adds "+N more" if truncated.
func joinMax(items []string, n int) string {
	if len(items) <= n {
		return fmt.Sprintf("%s", joinStrings(items))
	}
	return fmt.Sprintf("%s +%d more", joinStrings(items[:n]), len(items)-n)
}

func joinStrings(items []string) string {
	result := ""
	for i, s := range items {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
