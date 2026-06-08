// Command ibclientportal-flex downloads an Interactive Brokers Activity Flex
// Query report over the Flex Web Service and prints its Cash Transactions
// (deposits, withdrawals, fees, dividends, interest).
//
// Setup, done once in IBKR Account Management:
//
//	Reports > Settings > Flex Web Service: enable it and generate a token.
//	Reports > Flex Queries: build a Custom Activity Flex Query that includes
//	the "Cash Transactions" section and note its Query ID.
//
// Usage:
//
//	export IBCLIENTPORTAL_FLEX_TOKEN=...   # or pass --token
//	ibclientportal-flex --query 998877
//
//	# only deposits and withdrawals, as JSON:
//	ibclientportal-flex --query 998877 --type "Deposits/Withdrawals" --json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"text/tabwriter"

	"github.com/kevinburke/ibclientportal"
	"github.com/kevinburke/ibclientportal/flex"
)

func main() {
	token := flag.String("token", os.Getenv("IBCLIENTPORTAL_FLEX_TOKEN"),
		"Flex Web Service token (defaults to $IBCLIENTPORTAL_FLEX_TOKEN)")
	query := flag.String("query", os.Getenv("IBCLIENTPORTAL_FLEX_QUERY"),
		"Flex Query ID (defaults to $IBCLIENTPORTAL_FLEX_QUERY)")
	typeFilter := flag.String("type", "",
		"only print cash transactions of this Type (e.g. \"Deposits/Withdrawals\", \"Other Fees\")")
	asJSON := flag.Bool("json", false, "print the cash transactions as JSON")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *version {
		fmt.Println("ibclientportal-flex version " + ibclientportal.Version)
		os.Exit(0)
	}
	if *token == "" {
		slog.Error("missing Flex Web Service token; set --token or $IBCLIENTPORTAL_FLEX_TOKEN")
		os.Exit(2)
	}
	if *query == "" {
		slog.Error("missing Flex Query ID; set --query or $IBCLIENTPORTAL_FLEX_QUERY")
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	client := flex.NewClient(*token)
	report, err := client.Download(ctx, *query)
	if err != nil {
		slog.Error("could not download flex report", "error", err)
		os.Exit(1)
	}

	txns := report.CashTransactions()
	if *typeFilter != "" {
		filtered := txns[:0:0]
		for _, t := range txns {
			if t.Type == *typeFilter {
				filtered = append(filtered, t)
			}
		}
		txns = filtered
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(txns); err != nil {
			slog.Error("could not encode transactions", "error", err)
			os.Exit(1)
		}
		return
	}

	if len(txns) == 0 {
		slog.Info("no cash transactions matched", "query", *query, "type", *typeFilter)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "DATE\tTYPE\tAMOUNT\tCUR\tDESCRIPTION")
	for _, t := range txns {
		fmt.Fprintf(w, "%s\t%s\t%.2f\t%s\t%s\n",
			t.DateTime, t.Type, t.Amount, t.Currency, t.Description)
	}
	if err := w.Flush(); err != nil {
		slog.Error("could not write output", "error", err)
		os.Exit(1)
	}
}
