package locations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type curlObservation struct {
	Request       string   `json:"request"`
	Command       string   `json:"command"`
	ExitCode      int      `json:"exit_code"`
	Status        int      `json:"status"`
	BodyShape     []string `json:"body_shape,omitempty"`
	UpstreamCalls int64    `json:"upstream_calls"`
	ElapsedMS     int64    `json:"elapsed_ms"`
}

type curlEvidence struct {
	Surface        string            `json:"surface"`
	Authentication string            `json:"authentication"`
	Cases          []curlObservation `json:"cases"`
	Cleanup        string            `json:"cleanup"`
}

func TestLocations_CurlSurface(t *testing.T) {
	evidence := exerciseCurlSurface(t)

	if path := os.Getenv("FOOD_AGENT_LOCATION_QA_ARTIFACT"); path != "" {
		data, err := json.MarshalIndent(evidence, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0600))
	}
}

func exerciseCurlSurface(t *testing.T) (evidence curlEvidence) {
	t.Helper()
	var calls atomic.Int64
	canceled := make(chan struct{})
	upstream := scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
		calls.Add(1)
		query, ok := args["keywords"].(string)
		if !ok {
			query, ok = args["address"].(string)
			require.True(t, ok)
		}
		switch query {
		case "error":
			return "", errors.New("private-marker tool URL credential")
		case "malformed":
			return "{}", nil
		case "timeout", "cancel":
			<-ctx.Done()
			if query == "cancel" {
				close(canceled)
			}
			return "", ctx.Err()
		case "empty":
			return `{"status":"1","pois":[],"geocodes":[]}`, nil
		case "badcoords":
			return `{"status":"1","pois":[{"name":"invalid","location":"NaN,31"}],"geocodes":[{"location":"181,31"}]}`, nil
		case "fallback":
			if name == "amap.geocode" {
				return `{"status":"1","geocodes":[{"formatted_address":"sample-road","location":"121,31","city":"sample-city"}]}`, nil
			}
			return `{"status":"1","pois":[]}`, nil
		default:
			return `{"status":"1","pois":[{"id":"one","name":"station","location":"121,31","key":"private-marker"},{"id":"two","name":"station","location":"122,32"}]}`, nil
		}
	})
	router, bearer := fixtureRouter(t, upstream)
	server := httptest.NewServer(router)
	defer func() {
		server.CloseClientConnections()
		server.Close()
		evidence.Cleanup = "httptest listener and connections closed; all curl processes joined; fixture credentials were memory-only"
	}()
	evidence.Surface = "production locations router/handler/service; local MCP response fixture"
	evidence.Authentication = "fixture JWT verified with HS256; production authentication wiring blocked by snapshot omissions; header supplied through curl stdin"
	for _, scenario := range []struct {
		query     string
		auth      bool
		status    int
		count     int
		callCount int64
	}{
		{"query=station&city=sample-city", true, 200, 2, 1},
		{"query=fallback", true, 200, 1, 2},
		{"query=empty", true, 200, 0, 2},
		{"query=badcoords", true, 200, 0, 2},
		{"query=station", false, 401, 0, 0},
		{"query=" + strings.Repeat("a", 81), true, 400, 0, 0},
		{"query=%zz", true, 400, 0, 0},
		{"query=station&location=NaN,31", true, 400, 0, 0},
		{"query=malformed", true, 502, 0, 1},
		{"query=error", true, 502, 0, 1},
		{"query=timeout", true, 504, 0, 1},
		{"query=cancel", true, 0, 0, 1},
	} {
		path := "/api/v1/locations/search?" + scenario.query
		args := []string{"-i", "--silent", "--show-error", "--max-time", "8"}
		if scenario.query == "query=cancel" {
			args[4] = "0.2"
		}
		if scenario.auth {
			args = append(args, "--header", "@-")
		}
		args = append(args, server.URL+path)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		command := exec.CommandContext(ctx, "curl.exe", args...)
		if scenario.auth {
			command.Stdin = strings.NewReader("Authorization: Bearer " + bearer + "\n")
		}
		var stderr bytes.Buffer
		command.Stderr = &stderr
		before := calls.Load()
		start := time.Now()

		output, err := command.Output()
		cancel()

		observation := curlObservation{Request: "GET " + path, Command: "curl.exe"}
		observation.ElapsedMS = time.Since(start).Milliseconds()
		for _, arg := range args {
			observation.Command += fmt.Sprintf(" %q", arg)
		}
		if scenario.status == 0 {
			var exitError *exec.ExitError
			require.ErrorAs(t, err, &exitError)
			require.Equal(t, 28, exitError.ExitCode())
			observation.ExitCode = exitError.ExitCode()
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("curl disconnect did not cancel upstream")
			}
		} else {
			require.NoError(t, err)
			response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(output)), nil)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.NotContains(t, string(body), "private-marker")
			require.Equal(t, scenario.status, response.StatusCode)
			observation.Status = response.StatusCode
			observation.BodyShape = responseShape(t, body)
			if response.StatusCode == 200 {
				var envelope struct {
					Data struct {
						Items []Location `json:"items"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(body, &envelope))
				require.NotNil(t, envelope.Data.Items)
				require.Len(t, envelope.Data.Items, scenario.count)
			}
		}
		observation.UpstreamCalls = calls.Load() - before
		require.Equal(t, scenario.callCount, observation.UpstreamCalls)
		evidence.Cases = append(evidence.Cases, observation)
	}
	return evidence
}

func responseShape(t *testing.T, body []byte) []string {
	t.Helper()
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &envelope))
	shape := make([]string, 0, len(envelope))
	for field := range envelope {
		shape = append(shape, field)
	}
	if raw, ok := envelope["data"]; ok {
		var list struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		require.NoError(t, json.Unmarshal(raw, &list))
		shape = append(shape, "data.total:number", "data.items:array")
		if len(list.Items) > 0 {
			require.Len(t, list.Items[0], 5)
			for field := range list.Items[0] {
				shape = append(shape, "data.items[]."+field+":string")
			}
		}
	}
	sort.Strings(shape)
	return shape
}
