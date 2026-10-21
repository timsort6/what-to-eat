package addresses

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStore_RejectsOutOfRangeLocation(t *testing.T) {
	s := &service{dao: newMemFavoriteAddressDAO()}
	for _, location := range []string{"180.1,0", "0,90.1", "NaN,0", "0,Inf"} {
		t.Run(location, func(t *testing.T) {
			_, err := s.Create(context.Background(), 1, CreateAddressRequest{Label: "home", Location: location})
			if err == nil || !strings.Contains(err.Error(), "invalid") {
				t.Fatalf("expected invalid location, got %v", err)
			}
		})
	}
}

func TestStore_DefaultRules_CreateAndSetDefault(t *testing.T) {
	s := &service{dao: newMemFavoriteAddressDAO()}
	uid := uint(1)

	// First address must be default even if IsDefault not provided.
	a1, err := s.Create(context.Background(), uid, CreateAddressRequest{
		Label:    "家",
		Location: "121.1,31.1",
		AMapPOI:  json.RawMessage(`{"id":"p1"}`),
	})
	if err != nil {
		t.Fatalf("create a1: %v", err)
	}
	if !a1.IsDefault {
		t.Fatalf("expected a1 default")
	}

	// Second address is not default by default.
	a2, err := s.Create(context.Background(), uid, CreateAddressRequest{
		Label:    "公司",
		Location: "121.2,31.2",
	})
	if err != nil {
		t.Fatalf("create a2: %v", err)
	}
	if a2.IsDefault {
		t.Fatalf("expected a2 not default")
	}

	// Update a2 to be default.
	trueVal := true
	_, err = s.Update(context.Background(), uid, a2.ID, UpdateAddressRequest{IsDefault: &trueVal})
	if err != nil {
		t.Fatalf("set default a2: %v", err)
	}

	list, err := s.List(context.Background(), uid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	if !list[0].IsDefault || list[0].ID != a2.ID {
		t.Fatalf("expected a2 first and default")
	}
	if list[1].IsDefault {
		t.Fatalf("expected a1 not default after switching")
	}
}

func TestStore_DeleteDefault_MigratesToMinID(t *testing.T) {
	s := &service{dao: newMemFavoriteAddressDAO()}
	uid := uint(2)

	a1, err := s.Create(context.Background(), uid, CreateAddressRequest{Label: "A1", Location: "120,30"})
	if err != nil {
		t.Fatalf("create a1: %v", err)
	}
	a2, err := s.Create(context.Background(), uid, CreateAddressRequest{Label: "A2", Location: "120.1,30.1"})
	if err != nil {
		t.Fatalf("create a2: %v", err)
	}
	a3, err := s.Create(context.Background(), uid, CreateAddressRequest{Label: "A3", Location: "120.2,30.2"})
	if err != nil {
		t.Fatalf("create a3: %v", err)
	}

	// Make a3 default.
	trueVal := true
	if _, err := s.Update(context.Background(), uid, a3.ID, UpdateAddressRequest{IsDefault: &trueVal}); err != nil {
		t.Fatalf("set default a3: %v", err)
	}

	// Delete default a3 => migrate to min id (a1).
	if err := s.Delete(context.Background(), uid, a3.ID); err != nil {
		t.Fatalf("delete a3: %v", err)
	}

	list, err := s.List(context.Background(), uid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	if list[0].ID != a1.ID || !list[0].IsDefault {
		t.Fatalf("expected a1 default after deleting default")
	}
	if list[1].ID != a2.ID {
		t.Fatalf("expected a2 second")
	}
}
