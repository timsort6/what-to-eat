package utils

import (
	"reflect"
	"testing"
)

func TestRemoveDuplicates(t *testing.T) {
	tests := []struct {
		name string
		args []any
		want []any
	}{
		{
			name: "test1",
			args: []any{"a", "b", "a", "c", "c", "d", "d"},
			want: []any{"a", "b", "c", "d"},
		},
		{
			name: "test2",
			args: []any{1, 2, 22, 2, 3, 4, 22},
			want: []any{1, 2, 22, 3, 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RemoveDuplicates(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RemoveDuplicates() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIntersect(t *testing.T) {
	type arg struct {
		a []any
		b []any
	}

	tests := []struct {
		name string
		args arg
		want []any
	}{
		{
			name: "test1",
			args: arg{
				[]any{"a", "b", "a", "c", "c", "d", "d"},
				[]any{"a", "b", "a", "c", "c", "d", "d", "e"},
			},
			want: []any{"a", "b", "c", "d"},
		},
		{
			name: "test2",
			args: arg{
				[]any{1, 2, 22, 2, 3, 4, 22, 33, 546, 76},
				[]any{1, 2, 22, 2, 3, 4, 22},
			},
			want: []any{1, 2, 22, 3, 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Intersect(tt.args.a, tt.args.b); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Intersect() = %v, want %v", got, tt.want)
			}
		})
	}
}
