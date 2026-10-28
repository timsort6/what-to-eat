package locations

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSearch_sharesFiveSecondDeadline_whenFallbackBlocks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		var deadlines []time.Time
		svc, err := NewService(scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
			calls++
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			deadlines = append(deadlines, deadline)
			if name == "amap.poi2_text" {
				<-time.After(3 * time.Second)
				return `{"status":"1","pois":[]}`, nil
			}
			<-ctx.Done()
			return "", ctx.Err()
		}))
		require.NoError(t, err)
		start := time.Now()

		_, err = svc.Search(context.Background(), searchInput(t))

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 5*time.Second, time.Since(start))
		require.Equal(t, []time.Time{start.Add(5 * time.Second), start.Add(5 * time.Second)}, deadlines)
		require.Equal(t, 2, calls)
	})
}

func TestSearch_honorsEarlierDeadline_whenParentExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		calls := 0
		svc, err := NewService(scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
			calls++
			<-ctx.Done()
			return `{"status":"1","pois":[]}`, nil
		}))
		require.NoError(t, err)
		start := time.Now()

		_, err = svc.Search(ctx, searchInput(t))

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Second, time.Since(start))
		require.Equal(t, 1, calls)
	})
}

func TestSearch_preventsFurtherCalls_whenCanceled(t *testing.T) {
	for _, cancelAt := range []int{0, 1, 2} {
		t.Run(string(rune('0'+cancelAt)), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			svc, err := NewService(scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
				calls++
				if calls == cancelAt {
					cancel()
				}
				if name == "amap.geocode" {
					return `{"status":"1","geocodes":[]}`, nil
				}
				return `{"status":"1","pois":[]}`, nil
			}))
			require.NoError(t, err)
			if cancelAt == 0 {
				cancel()
			}

			_, err = svc.Search(ctx, searchInput(t))

			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, cancelAt, calls)
		})
	}
}

func TestSearch_receivesCancellation_whenFirstCallIsInFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan error, 1)
	svc, err := NewService(scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}))
	require.NoError(t, err)
	input := searchInput(t)
	go func() {
		_, err := svc.Search(ctx, input)
		finished <- err
	}()
	<-started

	cancel()

	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("request cancellation was not propagated")
	}
}
