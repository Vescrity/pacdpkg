package app

import (
	"reflect"
	"testing"
)

func TestKeepUnneeded(t *testing.T) {
	cases := []struct {
		name       string
		targets    []string
		requiredBy map[string][]string
		want       []string
	}{
		{
			name:    "nothing requires the targets",
			targets: []string{"a", "b"},
			want:    []string{"a", "b"},
		},
		{
			name:    "requirement inside the removal set",
			targets: []string{"a", "b"},
			requiredBy: map[string][]string{
				"b": {"a"},
			},
			want: []string{"a", "b"},
		},
		{
			name:    "requirement outside the removal set",
			targets: []string{"a"},
			requiredBy: map[string][]string{
				"a": {"c"},
			},
			want: nil,
		},
		{
			name:    "dropping a target unneeds another",
			targets: []string{"a", "b"},
			requiredBy: map[string][]string{
				"a": {"b"},
				"b": {"c"},
			},
			want: nil,
		},
		{
			name:    "only the needed target is dropped",
			targets: []string{"a", "b", "c"},
			requiredBy: map[string][]string{
				"b": {"z"},
			},
			want: []string{"a", "c"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := keepUnneeded(tc.targets, func(name string) []string {
				return tc.requiredBy[name]
			})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("keepUnneeded(%v) = %v, want %v", tc.targets, got, tc.want)
			}
		})
	}
}
