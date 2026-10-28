package locations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type scriptedMCP func(context.Context, string, map[string]any) (string, error)

func (call scriptedMCP) CallToolText(ctx context.Context, name string, args map[string]any) (string, error) {
	return call(ctx, name, args)
}

func searchInput(t *testing.T) SearchInput {
	t.Helper()
	input, err := NewSearchInput("  station  ", "  sample-city  ")
	require.NoError(t, err)
	return input
}

func TestSearch_limitsFirstPage_whenPOIsAreValid(t *testing.T) {
	var calls int
	upstream := scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
		calls++
		require.Equal(t, "amap.poi2_text", name)
		require.Equal(t, map[string]any{
			"keywords": "station", "region": "sample-city", "page_size": 10, "page_num": 1,
		}, args)
		pois := make([]map[string]string, 12)
		for index := range pois {
			pois[index] = map[string]string{
				"id": fmt.Sprint(index), "name": "Transit station", "location": " 121.5 , 31.2 ",
				"address": "sample-road", "cityname": "sample-city", "typecode": "150000", "key": "private-marker",
			}
		}
		text, err := json.Marshal(map[string]any{"status": "1", "pois": pois})
		require.NoError(t, err)
		return string(text), nil
	})
	svc, err := NewService(upstream)
	require.NoError(t, err)

	items, err := svc.Search(context.Background(), searchInput(t))

	require.NoError(t, err)
	require.Len(t, items, 10)
	require.Equal(t, Location{ID: "0", Name: "Transit station", Address: "sample-road", Location: "121.5,31.2", City: "sample-city"}, items[0])
	require.Equal(t, 1, calls)
}

func TestSearch_fallsBackOnce_whenPOIsAreInvalid(t *testing.T) {
	var calls []string
	upstream := scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
		calls = append(calls, name)
		switch name {
		case "amap.poi2_text":
			return `{"status":"1","pois":[{"name":"bad","location":"NaN,31"},{"name":"","location":"121,31"}]}`, nil
		case "amap.geocode":
			require.Equal(t, map[string]any{"address": "station", "city": "sample-city"}, args)
			return `{"status":"1","geocodes":[{"formatted_address":"sample-road","location":"121,31","city":[]}]}`, nil
		default:
			t.Fatal("unexpected upstream tool")
			return "", ErrUpstream
		}
	})
	svc, err := NewService(upstream)
	require.NoError(t, err)

	items, err := svc.Search(context.Background(), searchInput(t))

	require.NoError(t, err)
	require.Equal(t, []Location{{Name: "station", Address: "sample-road", Location: "121,31"}}, items)
	require.Equal(t, []string{"amap.poi2_text", "amap.geocode"}, calls)
}

func TestSearch_returnsEmptyList_whenBothCallsHaveNoValidCoordinates(t *testing.T) {
	for _, coordinate := range []string{"", "121", "121,31,1", "NaN,31", "121,NaN", "+Inf,31", "121,-Inf", "180.1,0", "-180.1,0", "0,90.1", "0,-90.1", "text,31", "1e999,31"} {
		t.Run(coordinate, func(t *testing.T) {
			calls := 0
			upstream := scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
				calls++
				field := "pois"
				if name == "amap.geocode" {
					field = "geocodes"
				}
				text, err := json.Marshal(map[string]any{"status": "1", field: []map[string]string{{"name": "place", "location": coordinate}}})
				require.NoError(t, err)
				return string(text), nil
			})
			svc, err := NewService(upstream)
			require.NoError(t, err)

			items, err := svc.Search(context.Background(), searchInput(t))

			require.NoError(t, err)
			require.NotNil(t, items)
			require.Empty(t, items)
			require.Equal(t, 2, calls)
		})
	}
}

func TestSearch_preservesCoordinateBounds_whenFinite(t *testing.T) {
	for _, coordinate := range []string{"-180,-90", "180,90", "0,0"} {
		t.Run(coordinate, func(t *testing.T) {
			calls := 0
			svc, err := NewService(scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
				calls++
				return fmt.Sprintf(`{"status":"1","pois":[{"name":"place","location":%q}]}`, coordinate), nil
			}))
			require.NoError(t, err)

			items, err := svc.Search(context.Background(), searchInput(t))

			require.NoError(t, err)
			require.Len(t, items, 1)
			require.Equal(t, coordinate, items[0].Location)
			require.Equal(t, 1, calls)
		})
	}
}

func TestSearch_returnsControlledError_whenUpstreamFails(t *testing.T) {
	for _, payload := range []string{"", "not-json", "{}", "null", `{"status":"0","pois":[]}`, `{"status":"1","infocode":"10001","pois":[]}`, `{"status":"1"}`, `{"status":"1","pois":{}}`} {
		t.Run(payload, func(t *testing.T) {
			calls := 0
			svc, err := NewService(scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
				calls++
				return payload, nil
			}))
			require.NoError(t, err)

			items, err := svc.Search(context.Background(), searchInput(t))

			require.ErrorIs(t, err, ErrUpstream)
			require.Nil(t, items)
			require.Equal(t, 1, calls)
		})
	}
}

func TestSearch_neverRetries_whenEitherCallFails(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			calls := 0
			svc, err := NewService(scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
				calls++
				if calls == failAt {
					return "", errors.New("private-marker URL credential payload")
				}
				return `{"status":"1","pois":[]}`, nil
			}))
			require.NoError(t, err)

			_, err = svc.Search(context.Background(), searchInput(t))

			require.ErrorIs(t, err, ErrUpstream)
			require.NotContains(t, err.Error(), "private-marker")
			require.Equal(t, failAt, calls)
		})
	}
}

func TestSearch_stopsAtFirstTenRecords_whenProviderOverReturns(t *testing.T) {
	calls := 0
	svc, err := NewService(scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
		calls++
		if name == "amap.geocode" {
			return `{"status":"1","geocodes":null}`, nil
		}
		return `{"status":"1","pois":[null,null,null,null,null,null,null,null,null,null,{"name":"eleventh","location":"121,31"}]}`, nil
	}))
	require.NoError(t, err)

	items, err := svc.Search(context.Background(), searchInput(t))

	require.NoError(t, err)
	require.Empty(t, items)
	require.Equal(t, 2, calls)
}
