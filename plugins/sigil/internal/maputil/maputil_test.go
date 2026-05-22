package maputil

import (
	"reflect"
	"testing"
)

func TestCloneStringMap(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]string
		want map[string]string
	}{
		{"nil input", nil, nil},
		{"empty non-nil input", map[string]string{}, nil},
		{"single entry", map[string]string{"a": "1"}, map[string]string{"a": "1"}},
		{"multiple entries", map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1", "b": "2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Clone(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Clone(%v) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCloneIsIndependent(t *testing.T) {
	in := map[string]string{"a": "1"}
	out := Clone(in)
	out["b"] = "2"
	if _, ok := in["b"]; ok {
		t.Fatalf("mutating clone leaked into source: %v", in)
	}
	in["c"] = "3"
	if _, ok := out["c"]; ok {
		t.Fatalf("mutating source leaked into clone: %v", out)
	}
}

func TestCloneAnyMap(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want map[string]any
	}{
		{"nil input", nil, nil},
		{"empty non-nil input", map[string]any{}, nil},
		{"mixed values",
			map[string]any{"n": int64(7), "s": "x", "b": true},
			map[string]any{"n": int64(7), "s": "x", "b": true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Clone(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Clone(%v) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}
