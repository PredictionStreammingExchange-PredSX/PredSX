package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/predsx/predsx/cmd/predsx/client"
	"github.com/spf13/cobra"
)

var (
	apiURL   string
	jsonOut  bool
	psx      *client.PredSXClient
)

var rootCmd = &cobra.Command{
	Use:   "predsx",
	Short: "PredSX CLI for prediction market data",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		psx = client.NewClient(apiURL)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "http://localhost:8080", "PredSX API URL")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Output in JSON format")

	rootCmd.AddCommand(marketsCmd)
	rootCmd.AddCommand(tradesCmd)
	rootCmd.AddCommand(orderbookCmd)
	rootCmd.AddCommand(priceCmd)
}

func output(data interface{}, header []string, rows [][]string) {
	if jsonOut {
		b, _ := json.MarshalIndent(data, "", "  ")
		fmt.Println(string(b))
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(header)
	table.SetBorder(false)
	table.AppendBulk(rows)
	table.Render()
}

var marketsCmd = &cobra.Command{
	Use:   "markets",
	Short: "List active markets",
	Run: func(cmd *cobra.Command, args []string) {
		markets, err := psx.GetMarkets()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		rows := [][]string{}
		for _, m := range markets {
			id, _ := m["id"].(string)
			title, _ := m["title"].(string)
			rows = append(rows, []string{id, title})
		}
		output(markets, []string{"ID", "Title"}, rows)
	},
}

var tradesCmd = &cobra.Command{
	Use:   "trades <market_id>",
	Short: "Get latest trades for a market",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		trades, err := psx.GetTrades(args[0])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		rows := [][]string{}
		for _, t := range trades {
			id, _ := t["trade_id"].(string)
			price, _ := t["price"].(string) // or float
			size, _ := t["size"].(string)
			side, _ := t["side"].(string)
			rows = append(rows, []string{id, side, price, size})
		}
		output(trades, []string{"ID", "Side", "Price", "Size"}, rows)
	},
}

var orderbookCmd = &cobra.Command{
	Use:   "orderbook <market_id>",
	Short: "Get orderbook for a market",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ob, err := psx.GetOrderbook(args[0])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		// Simplified display of top bids/asks
		fmt.Println("Orderbook for", args[0])
		// Logic to format bids/asks into table rows...
		output(ob, []string{"Price", "Size", "Type"}, [][]string{}) 
	},
}

var priceCmd = &cobra.Command{
	Use:   "price <market_id>",
	Short: "Get real-time price for a market",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		price, err := psx.GetPrice(args[0])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		p, _ := price["price"].(string)
		output(price, []string{"Market ID", "Price"}, [][]string{{args[0], p}})
	},
}
