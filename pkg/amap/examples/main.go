package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"food-agent/pkg/amap"

	"github.com/spf13/cobra"
)

// A small interactive CLI to try AMap WebService APIs via food-agent/pkg/amap.
//
// Env (preferred for quick testing):
// - FOOD_AGENT_AMAP_KEY
// - FOOD_AGENT_AMAP_BASE_URL (optional; default https://restapi.amap.com/v3)
// - FOOD_AGENT_AMAP_TIMEOUT_SECONDS (optional; default 5)
//
// Run:
// - go run ./pkg/amap/examples                 # interactive
// - go run ./pkg/amap/examples geocode --address="..." --pretty
func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

type cfg struct {
	Key            string
	BaseURL        string
	TimeoutSeconds int

	Pretty bool
	Limit  int
}

func newRootCmd() *cobra.Command {
	c := &cfg{
		Key:            "",
		BaseURL:        "https://restapi.amap.com/v3",
		TimeoutSeconds: 5,
		Pretty:         false,
		Limit:          3,
	}

	root := &cobra.Command{
		Use:   "amap-demo",
		Short: "AMap WebService demo CLI (using food-agent/pkg/amap)",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			applyEnvDefaults(cmd, c)
			if strings.TrimSpace(c.Key) == "" {
				// Allow "help" usage without key.
				if cmd.Name() == "help" || cmd.CalledAs() == "help" {
					return nil
				}
				return errors.New("missing amap key: set FOOD_AGENT_AMAP_KEY or pass --key")
			}
			return nil
		},
		// Default action: interactive mode.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runREPL(c)
		},
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&c.Key, "key", c.Key, "AMap WebService key (env FOOD_AGENT_AMAP_KEY)")
	root.PersistentFlags().StringVar(&c.BaseURL, "base-url", c.BaseURL, "Base URL (root or /v3 or /v5). env FOOD_AGENT_AMAP_BASE_URL")
	root.PersistentFlags().IntVar(&c.TimeoutSeconds, "timeout-seconds", c.TimeoutSeconds, "Timeout seconds. env FOOD_AGENT_AMAP_TIMEOUT_SECONDS")
	root.PersistentFlags().BoolVar(&c.Pretty, "pretty", c.Pretty, "Pretty-print full JSON response")
	root.PersistentFlags().IntVar(&c.Limit, "limit", c.Limit, "How many POIs to print for list responses")

	root.AddCommand(newGeocodeCmd(c))
	root.AddCommand(newRegeoCmd(c))
	root.AddCommand(newPOI2TextCmd(c))
	root.AddCommand(newPOI2AroundCmd(c))

	return root
}

func applyEnvDefaults(cmd *cobra.Command, c *cfg) {
	fs := cmd.Flags()
	pfs := cmd.InheritedFlags()

	changed := func(name string) bool {
		if pfs.Lookup(name) != nil && pfs.Lookup(name).Changed {
			return true
		}
		if fs.Lookup(name) != nil && fs.Lookup(name).Changed {
			return true
		}
		return false
	}

	if !changed("key") {
		if v := strings.TrimSpace(os.Getenv("FOOD_AGENT_AMAP_KEY")); v != "" {
			c.Key = v
		}
	}
	if !changed("base-url") {
		if v := strings.TrimSpace(os.Getenv("FOOD_AGENT_AMAP_BASE_URL")); v != "" {
			c.BaseURL = v
		}
	}
	if !changed("timeout-seconds") {
		if v := strings.TrimSpace(os.Getenv("FOOD_AGENT_AMAP_TIMEOUT_SECONDS")); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				c.TimeoutSeconds = n
			}
		}
	}
}

func newClient(c *cfg) *amap.Client {
	opts := []amap.Option{
		amap.WithBaseURL(c.BaseURL),
		amap.WithTimeout(time.Duration(c.TimeoutSeconds) * time.Second),
	}
	return amap.NewClient(c.Key, opts...)
}

func requestContext(c *cfg) (context.Context, context.CancelFunc) {
	timeout := time.Duration(c.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

func newGeocodeCmd(c *cfg) *cobra.Command {
	var address string
	var city string

	cmd := &cobra.Command{
		Use:   "geocode",
		Short: "Geocode: address -> lng,lat (v3 /geocode/geo)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(address) == "" {
				return errors.New("--address is required")
			}
			cli := newClient(c)
			ctx, cancel := requestContext(c)
			defer cancel()

			resp, err := cli.Geo().Geocode(ctx, &amap.GeocodeRequest{
				Address: address,
				City:    city,
			})
			if err != nil {
				return err
			}

			fmt.Printf("status=%s info=%s infocode=%s count=%s\n", resp.Status, resp.Info, resp.Infocode, resp.Count)
			if len(resp.Geocodes) > 0 {
				fmt.Printf("first.location=%s first.formatted_address=%s\n", resp.Geocodes[0].Location, resp.Geocodes[0].FormattedAddress)
			}
			if c.Pretty {
				printPretty(resp)
			}
			return nil
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&address, "address", "", "Structured address (required)")
	cmd.Flags().StringVar(&city, "city", "", "City filter (optional): cityname/citycode/adcode")
	return cmd
}

func newRegeoCmd(c *cfg) *cobra.Command {
	var location string
	var radius int
	var extensions string

	cmd := &cobra.Command{
		Use:   "regeo",
		Short: "Reverse geocode: lng,lat -> address (v3 /geocode/regeo)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(location) == "" {
				return errors.New("--location is required (lng,lat)")
			}
			cli := newClient(c)
			ctx, cancel := requestContext(c)
			defer cancel()

			resp, err := cli.Geo().Regeo(ctx, &amap.RegeoRequest{
				Location:   location,
				Radius:     radius,
				Extensions: extensions,
			})
			if err != nil {
				return err
			}

			fmt.Printf("status=%s info=%s infocode=%s\n", resp.Status, resp.Info, resp.Infocode)
			if c.Pretty {
				printPretty(resp)
			}
			return nil
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&location, "location", "", "Location (required): lng,lat")
	cmd.Flags().IntVar(&radius, "radius", 1000, "POI radius in meters (0~3000 recommended)")
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
		Short: "POI2.0 text search (v5 /place/text)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(keywords) == "" && strings.TrimSpace(types) == "" {
				return errors.New("--keywords or --types is required")
			}
			cli := newClient(c)
			ctx, cancel := requestContext(c)
			defer cancel()

			var cityLimitPtr *bool
			if cmd.Flags().Changed("city-limit") {
				cityLimitPtr = &cityLimit
			}

			resp, err := cli.POI2().Text(ctx, &amap.POI2TextRequest{
				Keywords:   keywords,
				Types:      types,
				Region:     region,
				CityLimit:  cityLimitPtr,
				ShowFields: showFields,
				PageSize:   pageSize,
				PageNum:    pageNum,
			})
			if err != nil {
				return err
			}

			fmt.Printf("status=%s info=%s infocode=%s count=%s\n", resp.Status, resp.Info, resp.Infocode, resp.Count)
			printPOI2List(resp.Pois, c.Limit)
			if c.Pretty {
				printPretty(resp)
			}
			return nil
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&keywords, "keywords", "", "Keywords (either keywords or types required)")
	cmd.Flags().StringVar(&types, "types", "", "Types/typecode list joined by | (either keywords or types required)")
	cmd.Flags().StringVar(&region, "region", "", "Region/citycode/adcode/cityname (optional)")
	cmd.Flags().BoolVar(&cityLimit, "city-limit", false, "Limit results strictly within region (optional; only sent if flag is set)")
	cmd.Flags().StringVar(&showFields, "show-fields", "", "show_fields (comma separated) to request rich POI fields")
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
		Short: "POI2.0 around search (v5 /place/around)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(location) == "" {
				return errors.New("--location is required (lng,lat)")
			}
			cli := newClient(c)
			ctx, cancel := requestContext(c)
			defer cancel()

			var cityLimitPtr *bool
			if cmd.Flags().Changed("city-limit") {
				cityLimitPtr = &cityLimit
			}

			resp, err := cli.POI2().Around(ctx, &amap.POI2AroundRequest{
				Location:   location,
				Keywords:   keywords,
				Types:      types,
				Radius:     radius,
				SortRule:   sortRule,
				Region:     region,
				CityLimit:  cityLimitPtr,
				ShowFields: showFields,
				PageSize:   pageSize,
				PageNum:    pageNum,
			})
			if err != nil {
				return err
			}

			fmt.Printf("status=%s info=%s infocode=%s count=%s\n", resp.Status, resp.Info, resp.Infocode, resp.Count)
			printPOI2List(resp.Pois, c.Limit)
			if c.Pretty {
				printPretty(resp)
			}
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
	cmd.Flags().StringVar(&showFields, "show-fields", "", "show_fields (comma separated) to request rich POI fields")
	cmd.Flags().IntVar(&pageSize, "page-size", 10, "page_size (1-25)")
	cmd.Flags().IntVar(&pageNum, "page-num", 1, "page_num (>=1)")
	return cmd
}

func runREPL(c *cfg) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("AMap demo (interactive)")
	fmt.Println("Commands:")
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
		case "1", "geocode":
			address := promptRequired(reader, "address", "")
			city := prompt(reader, "city (optional)", "")
			if err := runGeocodeOnce(c, address, city); err != nil {
				fmt.Printf("error: %v\n", err)
			}
		case "2", "regeo":
			location := promptRequired(reader, "location (lng,lat)", "")
			radius := promptInt(reader, "radius (m)", 1000)
			extensions := prompt(reader, "extensions (base/all)", "base")
			if err := runRegeoOnce(c, location, radius, extensions); err != nil {
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
			if err := runPOI2TextOnce(c, keywords, types, region, cityLimitSet, cityLimit, showFields, pageSize, pageNum); err != nil {
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
			if err := runPOI2AroundOnce(c, location, keywords, types, radius, sortrule, region, cityLimitSet, cityLimit, showFields, pageSize, pageNum); err != nil {
				fmt.Printf("error: %v\n", err)
			}
		default:
			fmt.Println("unknown command; enter 1/2/3/4 or q")
		}
	}
}

func runGeocodeOnce(c *cfg, address, city string) error {
	cli := newClient(c)
	ctx, cancel := requestContext(c)
	defer cancel()

	resp, err := cli.Geo().Geocode(ctx, &amap.GeocodeRequest{Address: address, City: city})
	if err != nil {
		return err
	}
	fmt.Printf("status=%s info=%s infocode=%s count=%s\n", resp.Status, resp.Info, resp.Infocode, resp.Count)
	if len(resp.Geocodes) > 0 {
		fmt.Printf("first.location=%s first.formatted_address=%s\n", resp.Geocodes[0].Location, resp.Geocodes[0].FormattedAddress)
	}
	if c.Pretty {
		printPretty(resp)
	}
	return nil
}

func runRegeoOnce(c *cfg, location string, radius int, extensions string) error {
	cli := newClient(c)
	ctx, cancel := requestContext(c)
	defer cancel()

	resp, err := cli.Geo().Regeo(ctx, &amap.RegeoRequest{
		Location:   location,
		Radius:     radius,
		Extensions: extensions,
	})
	if err != nil {
		return err
	}
	fmt.Printf("status=%s info=%s infocode=%s\n", resp.Status, resp.Info, resp.Infocode)
	if c.Pretty {
		printPretty(resp)
	}
	return nil
}

func runPOI2TextOnce(c *cfg, keywords, types, region string, cityLimitSet, cityLimit bool, showFields string, pageSize, pageNum int) error {
	cli := newClient(c)
	ctx, cancel := requestContext(c)
	defer cancel()

	var cityLimitPtr *bool
	if cityLimitSet {
		cityLimitPtr = &cityLimit
	}

	resp, err := cli.POI2().Text(ctx, &amap.POI2TextRequest{
		Keywords:   keywords,
		Types:      types,
		Region:     region,
		CityLimit:  cityLimitPtr,
		ShowFields: showFields,
		PageSize:   pageSize,
		PageNum:    pageNum,
	})
	if err != nil {
		return err
	}
	fmt.Printf("status=%s info=%s infocode=%s count=%s\n", resp.Status, resp.Info, resp.Infocode, resp.Count)
	printPOI2List(resp.Pois, c.Limit)
	if c.Pretty {
		printPretty(resp)
	}
	return nil
}

func runPOI2AroundOnce(c *cfg, location, keywords, types string, radius int, sortrule, region string, cityLimitSet, cityLimit bool, showFields string, pageSize, pageNum int) error {
	cli := newClient(c)
	ctx, cancel := requestContext(c)
	defer cancel()

	var cityLimitPtr *bool
	if cityLimitSet {
		cityLimitPtr = &cityLimit
	}

	resp, err := cli.POI2().Around(ctx, &amap.POI2AroundRequest{
		Location:   location,
		Keywords:   keywords,
		Types:      types,
		Radius:     radius,
		SortRule:   sortrule,
		Region:     region,
		CityLimit:  cityLimitPtr,
		ShowFields: showFields,
		PageSize:   pageSize,
		PageNum:    pageNum,
	})
	if err != nil {
		return err
	}
	fmt.Printf("status=%s info=%s infocode=%s count=%s\n", resp.Status, resp.Info, resp.Infocode, resp.Count)
	printPOI2List(resp.Pois, c.Limit)
	if c.Pretty {
		printPretty(resp)
	}
	return nil
}

func printPretty(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func printPOI2List(pois []amap.POI2POI, limit int) {
	if limit <= 0 {
		limit = 3
	}
	if len(pois) == 0 {
		fmt.Println("(no pois)")
		return
	}
	if limit > len(pois) {
		limit = len(pois)
	}
	for i := 0; i < limit; i++ {
		m := mergePOI2POI(pois[i])
		b, _ := json.MarshalIndent(m, "", "  ")
		fmt.Printf("poi[%d]=%s\n", i, string(b))
	}
}

// mergePOI2POI merges typed fields and Extra fields for printing/debugging.
func mergePOI2POI(p amap.POI2POI) map[string]any {
	out := make(map[string]any)

	// Extra fields first.
	for k, raw := range p.Extra {
		var v any
		if err := json.Unmarshal(raw, &v); err == nil {
			out[k] = v
		} else {
			out[k] = string(raw)
		}
	}

	// Known fields (overwrite Extra if key conflict).
	out["id"] = p.ID
	out["name"] = p.Name
	out["location"] = p.Location
	out["distance"] = p.Distance
	out["type"] = p.Type
	out["typecode"] = p.Typecode
	out["pname"] = p.Pname
	out["cityname"] = p.Cityname
	out["adname"] = p.Adname
	out["address"] = p.Address
	out["pcode"] = p.Pcode
	out["adcode"] = p.Adcode
	out["citycode"] = p.Citycode
	out["tel"] = p.Tel
	out["tag"] = p.Tag
	out["rating"] = p.Rating
	out["cost"] = p.Cost
	out["parking_type"] = p.ParkingType
	out["alias"] = p.Alias
	out["keytag"] = p.Keytag
	out["rectag"] = p.Rectag
	out["entr_location"] = p.EntrLocation
	out["exit_location"] = p.ExitLocation
	out["gridcode"] = p.Gridcode
	out["opentime_today"] = p.OpentimeToday
	out["opentime_week"] = p.OpentimeWeek
	return out
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
		fmt.Println("  (invalid bool: use true/false)")
	}
}
