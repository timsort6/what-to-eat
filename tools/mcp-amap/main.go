package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"food-agent/internal/mcp"

	"github.com/spf13/cobra"
)

// A small interactive CLI to try AMap MCP server tools (stdio) by spawning cmd/mcp-server
// as a subprocess and connecting via internal/mcp.StdioClient.
//
// Run:
//   go run ./tools/mcp-amap
//   go run ./tools/mcp-amap geocode --address="..." --city="..."
//
// Notes:
// - cmd/mcp-server reads its own independent config file (default configs/mcp-amap.yaml).
// - This tool passes the config path via both env FOOD_AGENT_MCP_CONFIG and args --config
//   (if the user didn't specify it in server args), to make it robust.

type cfg struct {
	// Config is either a file path or a directory (dir => join with mcp-amap.yaml).
	Config string

	ServerCommand string
	ServerArgs    []string
	WorkDir       string

	CallTimeoutSeconds int
	Raw                bool
	Limit              int

	client *mcp.StdioClient
}

func main() {
	c := &cfg{
		Config:             "configs/mcp-amap.yaml",
		ServerCommand:      "go",
		ServerArgs:         []string{"run", "./cmd/mcp-server"},
		WorkDir:            ".",
		CallTimeoutSeconds: 15,
		Raw:                false,
		Limit:              3,
	}

	cmd := newRootCmd(c)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
	if c.client != nil {
		_ = c.client.Close()
	}
}

func newRootCmd(c *cfg) *cobra.Command {
	root := &cobra.Command{
		Use:   "mcp-amap-demo",
		Short: "AMap MCP demo CLI (spawns cmd/mcp-server in stdio mode)",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Allow "help" usage without spawning the server.
			if cmd.Name() == "help" || cmd.CalledAs() == "help" {
				return nil
			}
			return ensureClient(cmd.Context(), cmd, c)
		},
		// Default action: interactive mode.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runREPL(cmd.Context(), c)
		},
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&c.Config, "config", c.Config, "mcp-server config file path OR dir (dir => <dir>/mcp-amap.yaml)")
	root.PersistentFlags().StringVar(&c.ServerCommand, "server-command", c.ServerCommand, "mcp-server subprocess command")
	root.PersistentFlags().StringSliceVar(&c.ServerArgs, "server-args", c.ServerArgs, "mcp-server subprocess args (e.g. go run ./cmd/mcp-server)")
	root.PersistentFlags().StringVar(&c.WorkDir, "work-dir", c.WorkDir, "mcp-server subprocess working directory")
	root.PersistentFlags().IntVar(&c.CallTimeoutSeconds, "call-timeout-seconds", c.CallTimeoutSeconds, "tool call timeout seconds")
	root.PersistentFlags().BoolVar(&c.Raw, "raw", c.Raw, "print raw tool output (full JSON text)")
	root.PersistentFlags().IntVar(&c.Limit, "limit", c.Limit, "how many items to print for list responses")

	root.AddCommand(newToolsListCmd(c))
	root.AddCommand(newGeocodeCmd(c))
	root.AddCommand(newRegeoCmd(c))
	root.AddCommand(newPOI2TextCmd(c))
	root.AddCommand(newPOI2AroundCmd(c))

	return root
}

func newToolsListCmd(c *cfg) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools-list",
		Short: "Method tools/list: list available MCP tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := callCtx(cmd.Context(), c)
			defer cancel()

			res, err := c.client.ListTools(ctx)
			if err != nil {
				return err
			}
			if c.Raw {
				b, _ := json.MarshalIndent(res, "", "  ")
				fmt.Println(string(b))
				return nil
			}
			if len(res.Tools) == 0 {
				fmt.Println("(no tools)")
				return nil
			}
			for _, t := range res.Tools {
				if strings.TrimSpace(t.Description) != "" {
					fmt.Printf("- %s: %s\n", t.Name, t.Description)
				} else {
					fmt.Printf("- %s\n", t.Name)
				}
			}
			return nil
		},
		SilenceUsage: true,
	}
	return cmd
}

func ensureClient(ctx context.Context, cmd *cobra.Command, c *cfg) error {
	if c.client != nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cfgPath, err := resolveConfigPath(c.Config)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.ServerCommand) == "" {
		return errors.New("empty --server-command")
	}

	serverArgs := append([]string{}, c.ServerArgs...)
	if !argsContainConfig(serverArgs) {
		serverArgs = append(serverArgs, "--config", cfgPath)
	}

	env := map[string]string{
		"FOOD_AGENT_MCP_CONFIG": cfgPath,
	}

	cli, err := mcp.NewStdioClient(ctx, mcp.StdioClientOptions{
		Command: strings.TrimSpace(c.ServerCommand),
		Args:    serverArgs,
		WorkDir: strings.TrimSpace(c.WorkDir),
		Env:     env,
	})
	if err != nil {
		return err
	}
	c.client = cli
	return nil
}

func resolveConfigPath(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("empty config path")
	}

	st, err := os.Stat(s)
	if err == nil && st.IsDir() {
		return filepath.Join(s, "mcp-amap.yaml"), nil
	}
	// If file doesn't exist yet, still treat it as a file path.
	// cmd/mcp-server will error later with a clear message.
	return s, nil
}

func argsContainConfig(args []string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" {
			return true
		}
		if strings.HasPrefix(args[i], "--config=") {
			return true
		}
	}
	return false
}

func callCtx(parent context.Context, c *cfg) (context.Context, context.CancelFunc) {
	timeout := time.Duration(c.CallTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, timeout)
}

func newGeocodeCmd(c *cfg) *cobra.Command {
	var address string
	var city string

	cmd := &cobra.Command{
		Use:   "geocode",
		Short: "Tool amap.geocode: address -> lng,lat",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(address) == "" {
				return errors.New("--address is required")
			}
			ctx, cancel := callCtx(cmd.Context(), c)
			defer cancel()

			txt, err := c.client.CallToolText(ctx, "amap.geocode", map[string]any{
				"address": address,
				"city":    city,
			})
			if err != nil {
				return err
			}
			if c.Raw {
				fmt.Println(txt)
				return nil
			}

			var resp struct {
				Status   string `json:"status"`
				Info     string `json:"info"`
				Infocode string `json:"infocode"`
				Count    string `json:"count"`
				Geocodes []struct {
					FormattedAddress string `json:"formatted_address"`
					Location         string `json:"location"`
				} `json:"geocodes"`
			}
			_ = json.Unmarshal([]byte(txt), &resp)
			fmt.Printf("status=%s info=%s infocode=%s count=%s\n", resp.Status, resp.Info, resp.Infocode, resp.Count)
			if len(resp.Geocodes) > 0 {
				fmt.Printf("first.location=%s first.formatted_address=%s\n", resp.Geocodes[0].Location, resp.Geocodes[0].FormattedAddress)
			}
			return nil
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&address, "address", "", "Structured address (required)")
	cmd.Flags().StringVar(&city, "city", "", "City filter (optional)")
	return cmd
}

func newRegeoCmd(c *cfg) *cobra.Command {
	var location string
	var radius int
	var extensions string

	cmd := &cobra.Command{
		Use:   "regeo",
		Short: "Tool amap.regeo: lng,lat -> address",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(location) == "" {
				return errors.New("--location is required (lng,lat)")
			}
			ctx, cancel := callCtx(cmd.Context(), c)
			defer cancel()

			txt, err := c.client.CallToolText(ctx, "amap.regeo", map[string]any{
				"location":   location,
				"radius":     radius,
				"extensions": extensions,
			})
			if err != nil {
				return err
			}
			if c.Raw {
				fmt.Println(txt)
				return nil
			}

			var resp struct {
				Status   string `json:"status"`
				Info     string `json:"info"`
				Infocode string `json:"infocode"`
			}
			_ = json.Unmarshal([]byte(txt), &resp)
			fmt.Printf("status=%s info=%s infocode=%s\n", resp.Status, resp.Info, resp.Infocode)
			return nil
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&location, "location", "", "Location (required): lng,lat")
	cmd.Flags().IntVar(&radius, "radius", 1000, "Radius (meters)")
	cmd.Flags().StringVar(&extensions, "extensions", "base", "base or all")
	return cmd
}

func newPOI2TextCmd(c *cfg) *cobra.Command {
	var keywords string
	var types string
	var region string
	var showFields string
	var pageSize int
	var pageNum int
	var cityLimit bool

	cmd := &cobra.Command{
		Use:   "poi2-text",
		Short: "Tool amap.poi2_text: POI2.0 text search",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(keywords) == "" && strings.TrimSpace(types) == "" {
				return errors.New("--keywords or --types is required")
			}
			ctx, cancel := callCtx(cmd.Context(), c)
			defer cancel()

			argsMap := map[string]any{
				"keywords":    keywords,
				"types":       types,
				"region":      region,
				"show_fields": showFields,
				"page_size":   pageSize,
				"page_num":    pageNum,
			}
			if cmd.Flags().Changed("city-limit") {
				argsMap["city_limit"] = cityLimit
			}

			txt, err := c.client.CallToolText(ctx, "amap.poi2_text", argsMap)
			if err != nil {
				return err
			}
			if c.Raw {
				fmt.Println(txt)
				return nil
			}

			printPOI2Summary(txt, c.Limit)
			return nil
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&keywords, "keywords", "", "Keywords (either keywords or types required)")
	cmd.Flags().StringVar(&types, "types", "", "Types/typecode list joined by | (either keywords or types required)")
	cmd.Flags().StringVar(&region, "region", "", "Region/citycode/adcode/cityname (optional)")
	cmd.Flags().BoolVar(&cityLimit, "city-limit", false, "Limit results strictly within region (optional; only sent if flag is set)")
	cmd.Flags().StringVar(&showFields, "show-fields", "", "show_fields (optional)")
	cmd.Flags().IntVar(&pageSize, "page-size", 10, "page_size (1-25)")
	cmd.Flags().IntVar(&pageNum, "page-num", 1, "page_num (>=1)")
	return cmd
}

func newPOI2AroundCmd(c *cfg) *cobra.Command {
	var location string
	var keywords string
	var types string
	var radius int
	var sortRule string
	var region string
	var showFields string
	var pageSize int
	var pageNum int
	var cityLimit bool

	cmd := &cobra.Command{
		Use:   "poi2-around",
		Short: "Tool amap.poi2_around: POI2.0 around search",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(location) == "" {
				return errors.New("--location is required (lng,lat)")
			}
			ctx, cancel := callCtx(cmd.Context(), c)
			defer cancel()

			argsMap := map[string]any{
				"location":    location,
				"keywords":    keywords,
				"types":       types,
				"radius":      radius,
				"sortrule":    sortRule,
				"region":      region,
				"show_fields": showFields,
				"page_size":   pageSize,
				"page_num":    pageNum,
			}
			if cmd.Flags().Changed("city-limit") {
				argsMap["city_limit"] = cityLimit
			}

			txt, err := c.client.CallToolText(ctx, "amap.poi2_around", argsMap)
			if err != nil {
				return err
			}
			if c.Raw {
				fmt.Println(txt)
				return nil
			}

			printPOI2Summary(txt, c.Limit)
			return nil
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&location, "location", "", "Location (required): lng,lat")
	cmd.Flags().StringVar(&keywords, "keywords", "", "Keywords (optional)")
	cmd.Flags().StringVar(&types, "types", "", "Types/typecode list joined by | (optional)")
	cmd.Flags().IntVar(&radius, "radius", 5000, "Radius in meters (0-50000)")
	cmd.Flags().StringVar(&sortRule, "sortrule", "distance", "Sort rule: distance/weight")
	cmd.Flags().StringVar(&region, "region", "", "Region/citycode/adcode/cityname (optional)")
	cmd.Flags().BoolVar(&cityLimit, "city-limit", false, "Limit results strictly within region (optional; only sent if flag is set)")
	cmd.Flags().StringVar(&showFields, "show-fields", "", "show_fields (optional)")
	cmd.Flags().IntVar(&pageSize, "page-size", 10, "page_size (1-25)")
	cmd.Flags().IntVar(&pageNum, "page-num", 1, "page_num (>=1)")
	return cmd
}

func runREPL(ctx context.Context, c *cfg) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("AMap MCP demo (interactive)")
	fmt.Println("Commands:")
	fmt.Println("  0) tools-list list available tools")
	fmt.Println("  1) geocode    address -> lng,lat")
	fmt.Println("  2) regeo      lng,lat -> address")
	fmt.Println("  3) poi2-text  keyword search")
	fmt.Println("  4) poi2-around nearby search")
	fmt.Println("  q) quit")
	fmt.Println()

	for {
		fmt.Print("> ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch strings.ToLower(line) {
		case "q", "quit", "exit":
			return nil
		case "0", "tools-list":
			if err := runToolsListOnce(ctx, c); err != nil {
				fmt.Printf("error: %v\n", err)
			}
		case "1", "geocode":
			address := promptRequired(reader, "address", "")
			city := prompt(reader, "city (optional)", "")
			if err := runGeocodeOnce(ctx, c, address, city); err != nil {
				fmt.Printf("error: %v\n", err)
			}
		case "2", "regeo":
			location := promptRequired(reader, "location (lng,lat)", "")
			radius := promptInt(reader, "radius (m)", 1000)
			extensions := prompt(reader, "extensions (base/all)", "base")
			if err := runRegeoOnce(ctx, c, location, radius, extensions); err != nil {
				fmt.Printf("error: %v\n", err)
			}
		case "3", "poi2-text":
			keywords := prompt(reader, "keywords (empty if using types)", "")
			types := prompt(reader, "types (typecode, use | to join; empty if using keywords)", "")
			region := prompt(reader, "region (optional)", "")
			cityLimitSet, cityLimit := promptBoolOptional(reader, "city_limit (optional true/false, empty means not set)", "")
			showFields := prompt(reader, "show_fields (optional)", "")
			pageSize := promptInt(reader, "page_size (1-25)", 10)
			pageNum := promptInt(reader, "page_num (>=1)", 1)
			if err := runPOI2TextOnce(ctx, c, keywords, types, region, cityLimitSet, cityLimit, showFields, pageSize, pageNum); err != nil {
				fmt.Printf("error: %v\n", err)
			}
		case "4", "poi2-around":
			location := promptRequired(reader, "location (lng,lat)", "")
			keywords := prompt(reader, "keywords (optional)", "")
			types := prompt(reader, "types (typecode, use | to join; optional)", "")
			radius := promptInt(reader, "radius (m)", 5000)
			sortrule := prompt(reader, "sortrule (distance/weight)", "distance")
			region := prompt(reader, "region (optional)", "")
			cityLimitSet, cityLimit := promptBoolOptional(reader, "city_limit (optional true/false, empty means not set)", "")
			showFields := prompt(reader, "show_fields (optional)", "")
			pageSize := promptInt(reader, "page_size (1-25)", 10)
			pageNum := promptInt(reader, "page_num (>=1)", 1)
			if err := runPOI2AroundOnce(ctx, c, location, keywords, types, radius, sortrule, region, cityLimitSet, cityLimit, showFields, pageSize, pageNum); err != nil {
				fmt.Printf("error: %v\n", err)
			}
		default:
			fmt.Println("unknown command")
		}
	}
}

func runToolsListOnce(ctx context.Context, c *cfg) error {
	call, cancel := callCtx(ctx, c)
	defer cancel()

	res, err := c.client.ListTools(call)
	if err != nil {
		return err
	}
	if c.Raw {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if len(res.Tools) == 0 {
		fmt.Println("(no tools)")
		return nil
	}
	for _, t := range res.Tools {
		if strings.TrimSpace(t.Description) != "" {
			fmt.Printf("- %s: %s\n", t.Name, t.Description)
		} else {
			fmt.Printf("- %s\n", t.Name)
		}
	}
	return nil
}

func runGeocodeOnce(ctx context.Context, c *cfg, address, city string) error {
	call, cancel := callCtx(ctx, c)
	defer cancel()
	txt, err := c.client.CallToolText(call, "amap.geocode", map[string]any{"address": address, "city": city})
	if err != nil {
		return err
	}
	if c.Raw {
		fmt.Println(txt)
		return nil
	}
	var resp struct {
		Status   string `json:"status"`
		Info     string `json:"info"`
		Infocode string `json:"infocode"`
		Count    string `json:"count"`
		Geocodes []struct {
			FormattedAddress string `json:"formatted_address"`
			Location         string `json:"location"`
		} `json:"geocodes"`
	}
	_ = json.Unmarshal([]byte(txt), &resp)
	fmt.Printf("status=%s info=%s infocode=%s count=%s\n", resp.Status, resp.Info, resp.Infocode, resp.Count)
	if len(resp.Geocodes) > 0 {
		fmt.Printf("first.location=%s first.formatted_address=%s\n", resp.Geocodes[0].Location, resp.Geocodes[0].FormattedAddress)
	}
	return nil
}

func runRegeoOnce(ctx context.Context, c *cfg, location string, radius int, extensions string) error {
	call, cancel := callCtx(ctx, c)
	defer cancel()
	txt, err := c.client.CallToolText(call, "amap.regeo", map[string]any{
		"location":   location,
		"radius":     radius,
		"extensions": extensions,
	})
	if err != nil {
		return err
	}
	if c.Raw {
		fmt.Println(txt)
		return nil
	}
	var resp struct {
		Status   string `json:"status"`
		Info     string `json:"info"`
		Infocode string `json:"infocode"`
	}
	_ = json.Unmarshal([]byte(txt), &resp)
	fmt.Printf("status=%s info=%s infocode=%s\n", resp.Status, resp.Info, resp.Infocode)
	return nil
}

func runPOI2TextOnce(ctx context.Context, c *cfg, keywords, types, region string, cityLimitSet, cityLimit bool, showFields string, pageSize, pageNum int) error {
	call, cancel := callCtx(ctx, c)
	defer cancel()
	argsMap := map[string]any{
		"keywords":    keywords,
		"types":       types,
		"region":      region,
		"show_fields": showFields,
		"page_size":   pageSize,
		"page_num":    pageNum,
	}
	if cityLimitSet {
		argsMap["city_limit"] = cityLimit
	}
	txt, err := c.client.CallToolText(call, "amap.poi2_text", argsMap)
	if err != nil {
		return err
	}
	if c.Raw {
		fmt.Println(txt)
		return nil
	}
	printPOI2Summary(txt, c.Limit)
	return nil
}

func runPOI2AroundOnce(ctx context.Context, c *cfg, location, keywords, types string, radius int, sortrule, region string, cityLimitSet, cityLimit bool, showFields string, pageSize, pageNum int) error {
	call, cancel := callCtx(ctx, c)
	defer cancel()
	argsMap := map[string]any{
		"location":    location,
		"keywords":    keywords,
		"types":       types,
		"radius":      radius,
		"sortrule":    sortrule,
		"region":      region,
		"show_fields": showFields,
		"page_size":   pageSize,
		"page_num":    pageNum,
	}
	if cityLimitSet {
		argsMap["city_limit"] = cityLimit
	}
	txt, err := c.client.CallToolText(call, "amap.poi2_around", argsMap)
	if err != nil {
		return err
	}
	if c.Raw {
		fmt.Println(txt)
		return nil
	}
	printPOI2Summary(txt, c.Limit)
	return nil
}

func printPOI2Summary(txt string, limit int) {
	var resp struct {
		Status   string                   `json:"status"`
		Info     string                   `json:"info"`
		Infocode string                   `json:"infocode"`
		Count    string                   `json:"count"`
		Pois     []map[string]interface{} `json:"pois"`
	}
	_ = json.Unmarshal([]byte(txt), &resp)
	fmt.Printf("status=%s info=%s infocode=%s count=%s\n", resp.Status, resp.Info, resp.Infocode, resp.Count)

	if limit <= 0 {
		limit = 3
	}
	if len(resp.Pois) == 0 {
		fmt.Println("(no pois)")
		return
	}
	if limit > len(resp.Pois) {
		limit = len(resp.Pois)
	}
	for i := 0; i < limit; i++ {
		p := resp.Pois[i]
		fmt.Printf("poi[%d]=name=%s location=%s distance=%s address=%s type=%s typecode=%s\n",
			i,
			asString(p["name"]),
			asString(p["location"]),
			asString(p["distance"]),
			asString(p["address"]),
			asString(p["type"]),
			asString(p["typecode"]),
		)
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprintf("%v", v)
	}
}

func prompt(reader *bufio.Reader, label string, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func promptRequired(reader *bufio.Reader, label string, def string) string {
	for {
		v := prompt(reader, label, def)
		if strings.TrimSpace(v) != "" {
			return v
		}
		fmt.Println("  (required)")
	}
}

func promptInt(reader *bufio.Reader, label string, def int) int {
	for {
		v := prompt(reader, label, strconv.Itoa(def))
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
		fmt.Println("  (invalid int)")
	}
}

func promptBoolOptional(reader *bufio.Reader, label string, def string) (set bool, v bool) {
	for {
		s := prompt(reader, label, def)
		if strings.TrimSpace(s) == "" {
			return false, false
		}
		b, err := strconv.ParseBool(strings.TrimSpace(s))
		if err == nil {
			return true, b
		}
		fmt.Println("  (invalid bool)")
	}
}
