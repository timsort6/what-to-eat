package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// A small CLI tool to test the web server Agent Chat endpoint via HTTP SSE:
// - POST /api/v1/agent/chat (text/event-stream)
//
// It is meant for quickly verifying:
// - auth works (register/login/token)
// - agent/chat streams delta events
// - done event contains answer_text + places[]

type cfg struct {
	BaseURL string
	Token   string

	Username string
	Password string

	TimeoutSeconds int
	RawEvents      bool
	PlacesLimit    int
	Summary        bool
}

func main() {
	c := &cfg{
		BaseURL:        "http://127.0.0.1:8080",
		Token:          "",
		Username:       "",
		Password:       "",
		TimeoutSeconds: 180,
		RawEvents:      false,
		PlacesLimit:    10,
		Summary:        true,
	}
	if err := newRootCmd(c).Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd(c *cfg) *cobra.Command {
	root := &cobra.Command{
		Use:   "agent-chat-demo",
		Short: "Test /api/v1/agent/chat SSE via HTTP",
	}

	root.PersistentFlags().StringVar(&c.BaseURL, "base-url", c.BaseURL, "web server base url")
	root.PersistentFlags().StringVar(&c.Token, "token", c.Token, "JWT token (or env FOOD_AGENT_TOKEN)")
	root.PersistentFlags().StringVar(&c.Username, "username", c.Username, "username for login/register (optional)")
	root.PersistentFlags().StringVar(&c.Password, "password", c.Password, "password for login/register (optional)")
	root.PersistentFlags().IntVar(&c.TimeoutSeconds, "timeout-seconds", c.TimeoutSeconds, "HTTP timeout seconds")
	root.PersistentFlags().BoolVar(&c.RawEvents, "raw-events", c.RawEvents, "print raw SSE events (debug; disables streaming text printing)")
	root.PersistentFlags().IntVar(&c.PlacesLimit, "places-limit", c.PlacesLimit, "print first N places in done event (0 disables)")
	root.PersistentFlags().BoolVar(&c.Summary, "summary", c.Summary, "print a final summary after done event")

	root.AddCommand(newRegisterCmd(c))
	root.AddCommand(newLoginCmd(c))
	root.AddCommand(newChatCmd(c))

	return root
}

func newRegisterCmd(c *cfg) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "POST /api/v1/auth/register",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(c.Username) == "" || strings.TrimSpace(c.Password) == "" {
				return errors.New("--username and --password are required")
			}
			ctx, cancel := ctxWithTimeout(cmd.Context(), c.TimeoutSeconds)
			defer cancel()
			return register(ctx, c.BaseURL, c.Username, c.Password)
		},
		SilenceUsage: true,
	}
	return cmd
}

func newLoginCmd(c *cfg) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "POST /api/v1/auth/login (prints token)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(c.Username) == "" || strings.TrimSpace(c.Password) == "" {
				return errors.New("--username and --password are required")
			}
			ctx, cancel := ctxWithTimeout(cmd.Context(), c.TimeoutSeconds)
			defer cancel()
			tok, err := login(ctx, c.BaseURL, c.Username, c.Password)
			if err != nil {
				return err
			}
			fmt.Println(tok)
			return nil
		},
		SilenceUsage: true,
	}
	return cmd
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	Messages []chatMessage `json:"messages"`

	AddressID *uint  `json:"address_id,omitempty"`
	Location  string `json:"location,omitempty"`

	Keywords  string `json:"keywords,omitempty"`
	Types     string `json:"types,omitempty"`
	Radius    int    `json:"radius,omitempty"`
	SortRule  string `json:"sortrule,omitempty"`
	Region    string `json:"region,omitempty"`
	CityLimit *bool  `json:"city_limit,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type sseDelta struct {
	Text string `json:"text"`
}

type doneData struct {
	AnswerText  string  `json:"answer_text"`
	Places      []place `json:"places"`
	PlacesCount int     `json:"places_count"`
	Location    string  `json:"location"`
	AddressID   *uint   `json:"address_id,omitempty"`
}

type place struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Location string          `json:"location,omitempty"`
	Address  string          `json:"address,omitempty"`
	Distance string          `json:"distance,omitempty"`
	Type     string          `json:"type,omitempty"`
	TypeCode string          `json:"typecode,omitempty"`
	Tel      string          `json:"tel,omitempty"`
	Tag      string          `json:"tag,omitempty"`
	Raw      json.RawMessage `json:"raw,omitempty"`
}

func newChatCmd(c *cfg) *cobra.Command {
	var prompt string
	var location string
	var addressID uint

	var keywords string
	var types string
	var radius int
	var sortRule string
	var region string
	var cityLimit bool
	var cityLimitSet bool
	var limit int

	var messagesFile string

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "POST /api/v1/agent/chat (SSE stream)",
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
			if baseURL == "" {
				return errors.New("empty --base-url")
			}

			tokenVal, err := resolveToken(cmd.Context(), c, baseURL)
			if err != nil {
				return err
			}

			var msgs []chatMessage
			if strings.TrimSpace(messagesFile) != "" {
				p, err := resolvePath(messagesFile)
				if err != nil {
					return err
				}
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(b, &msgs); err != nil {
					return fmt.Errorf("parse --messages-file: %w", err)
				}
			}
			if len(msgs) == 0 {
				if strings.TrimSpace(prompt) == "" {
					return errors.New("--prompt is required (or use --messages-file)")
				}
				msgs = []chatMessage{{Role: "user", Content: prompt}}
			}

			req := chatReq{
				Messages: msgs,
				Location: strings.TrimSpace(location),
				Keywords: strings.TrimSpace(keywords),
				Types:    strings.TrimSpace(types),
				Radius:   radius,
				SortRule: strings.TrimSpace(sortRule),
				Region:   strings.TrimSpace(region),
				Limit:    limit,
			}
			if cmd.Flags().Changed("address-id") && addressID > 0 {
				req.AddressID = &addressID
			}
			if cityLimitSet {
				req.CityLimit = &cityLimit
			}

			ctx, cancel := ctxWithTimeout(cmd.Context(), c.TimeoutSeconds)
			defer cancel()
			return chatSSE(ctx, baseURL, tokenVal, req, c.RawEvents, c.PlacesLimit, c.Summary)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "user prompt (single message)")
	cmd.Flags().StringVar(&messagesFile, "messages-file", "", "path to a JSON file containing []{role,content}")
	cmd.Flags().StringVar(&location, "location", "", "location lng,lat (recommended)")
	cmd.Flags().UintVar(&addressID, "address-id", 0, "favorite address id (alternative to --location)")

	cmd.Flags().StringVar(&keywords, "keywords", "", "amap poi2 keywords (optional)")
	cmd.Flags().StringVar(&types, "types", "", "amap poi2 types (optional)")
	cmd.Flags().IntVar(&radius, "radius", 5000, "amap poi2 radius (meters, 0-50000)")
	cmd.Flags().StringVar(&sortRule, "sortrule", "distance", "amap poi2 sort rule: distance/weight")
	cmd.Flags().StringVar(&region, "region", "", "amap poi2 region (optional)")
	cmd.Flags().BoolVar(&cityLimit, "city-limit", false, "amap poi2 city_limit (optional; only sent if --city-limit-set)")
	cmd.Flags().BoolVar(&cityLimitSet, "city-limit-set", false, "send city_limit parameter")
	cmd.Flags().IntVar(&limit, "limit", 10, "how many places to request (1-25)")

	return cmd
}

func resolveToken(ctx context.Context, c *cfg, baseURL string) (string, error) {
	if strings.TrimSpace(c.Token) != "" {
		return strings.TrimSpace(c.Token), nil
	}
	if v := strings.TrimSpace(os.Getenv("FOOD_AGENT_TOKEN")); v != "" {
		return v, nil
	}
	// Best-effort: if username/password provided, login automatically.
	if strings.TrimSpace(c.Username) != "" && strings.TrimSpace(c.Password) != "" {
		tok, err := login(ctx, baseURL, c.Username, c.Password)
		if err != nil {
			return "", err
		}
		return tok, nil
	}
	return "", errors.New("missing token: set --token or env FOOD_AGENT_TOKEN (or provide --username/--password)")
}

func ctxWithTimeout(parent context.Context, seconds int) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if seconds <= 0 {
		seconds = 180
	}
	return context.WithTimeout(parent, time.Duration(seconds)*time.Second)
}

func register(ctx context.Context, baseURL, username, password string) error {
	type req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	b, _ := json.Marshal(req{Username: username, Password: password})
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/auth/register", bytes.NewReader(b))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("register failed status=%d body=%s", resp.StatusCode, string(body))
	}
	fmt.Println(string(body))
	return nil
}

func login(ctx context.Context, baseURL, username, password string) (string, error) {
	type req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	b, _ := json.Marshal(req{Username: username, Password: password})
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/auth/login", bytes.NewReader(b))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed status=%d body=%s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse login response: %w", err)
	}
	if strings.TrimSpace(parsed.Data.Token) == "" {
		return "", fmt.Errorf("empty token in login response")
	}
	return parsed.Data.Token, nil
}

func chatSSE(ctx context.Context, baseURL, tokenVal string, req chatReq, rawEvents bool, placesLimit int, summary bool) error {
	b, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/agent/chat", bytes.NewReader(b))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+tokenVal)

	client := &http.Client{Timeout: 0} // streaming, use ctx for cancellation
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chat failed status=%d body=%s", resp.StatusCode, string(body))
	}

	reader := bufio.NewReader(resp.Body)
	var (
		curEvent string
		dataBuf  []string
		streamed strings.Builder
		gotDone  bool
	)

	printDeltaText := !rawEvents

	flushEvent := func() error {
		if curEvent == "" && len(dataBuf) == 0 {
			return nil
		}
		data := strings.Join(dataBuf, "\n")
		if rawEvents {
			fmt.Printf("\n[event=%s]\n%s\n", curEvent, data)
		}

		switch curEvent {
		case "delta":
			var d sseDelta
			if err := json.Unmarshal([]byte(data), &d); err == nil && d.Text != "" {
				streamed.WriteString(d.Text)
				if printDeltaText {
					fmt.Print(d.Text)
				}
			}
		case "done":
			var done doneData
			if err := json.Unmarshal([]byte(data), &done); err != nil {
				// still print raw
				fmt.Printf("\n(done parse error) %v\n%s\n", err, data)
				return nil
			}
			gotDone = true
			fmt.Print("\n")
			if summary {
				printDoneSummary(done, placesLimit)
			}
		case "error":
			return fmt.Errorf("server error: %s", data)
		default:
			// ignore
		}

		curEvent = ""
		dataBuf = nil
		return nil
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				_ = flushEvent()
				if summary && !gotDone {
					if txt := strings.TrimSpace(streamed.String()); txt != "" {
						fmt.Println()
						fmt.Println("===== PARTIAL OUTPUT (connection closed) =====")
						fmt.Println("answer_text:")
						fmt.Println(txt)
					}
				}
				return nil
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			curEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataBuf = append(dataBuf, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
		// ignore other fields (id:, retry:, comments)
	}
}

func resolvePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(p) {
		return p, nil
	}
	return filepath.Clean(p), nil
}

func printDoneSummary(done doneData, placesLimit int) {
	fmt.Println()
	fmt.Println("===== DONE SUMMARY =====")
	if strings.TrimSpace(done.Location) != "" {
		fmt.Printf("location=%s\n", done.Location)
	}
	if done.AddressID != nil {
		fmt.Printf("address_id=%d\n", *done.AddressID)
	}
	fmt.Println()

	if strings.TrimSpace(done.AnswerText) != "" {
		fmt.Println("answer_text:")
		fmt.Println(done.AnswerText)
		fmt.Println()
	}

	fmt.Printf("places_count=%d\n", done.PlacesCount)
	if placesLimit <= 0 {
		return
	}
	if len(done.Places) == 0 {
		return
	}
	n := placesLimit
	if n > len(done.Places) {
		n = len(done.Places)
	}
	for i := 0; i < n; i++ {
		p := done.Places[i]
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = "(no name)"
		}
		fmt.Printf("- #%d %s (id=%s distance=%s location=%s)\n", i+1, name, strings.TrimSpace(p.ID), strings.TrimSpace(p.Distance), strings.TrimSpace(p.Location))
		if addr := strings.TrimSpace(p.Address); addr != "" {
			fmt.Printf("  address=%s\n", addr)
		}
		if typ := strings.TrimSpace(p.Type); typ != "" {
			fmt.Printf("  type=%s\n", typ)
		}
	}
}

// Ensure we don't accidentally shadow stdlib strconv in older gofmt versions.
var _ = strconv.IntSize
