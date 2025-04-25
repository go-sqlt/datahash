package datahash_test

import (
	"hash/fnv"
	"math/big"
	"testing"

	"github.com/go-sqlt/datahash"
)

type testCase struct {
	name     string
	value    any
	options  datahash.Options
	expected uint64
}

func TestHasher_Hash(t *testing.T) {
	tests := []testCase{
		{
			name:     "int value",
			value:    42,
			expected: mustHash(t, 42, datahash.Options{}),
		},
		{
			name: "struct with ignored field",
			value: struct {
				Name string `datahash:"-"`
				Age  int
			}{"ignored", 21},
			expected: mustHash(t, struct {
				Name string `datahash:"-"`
				Age  int
			}{"something else", 21}, datahash.Options{}),
		},
		{
			name:     "pointer value",
			value:    &[]int{1, 2, 3},
			expected: mustHash(t, &[]int{1, 2, 3}, datahash.Options{}),
		},
		{
			name:     "slice as list",
			value:    []int{1, 2, 3},
			expected: mustHash(t, []int{1, 2, 3}, datahash.Options{}),
		},
		{
			name:     "slice as set",
			value:    []int{3, 1, 2},
			options:  datahash.Options{Set: true},
			expected: mustHash(t, []int{1, 2, 3}, datahash.Options{Set: true}),
		},
		{
			name: "big.Float with text tag",
			value: struct {
				F *big.Float `datahash:"text"`
			}{F: big.NewFloat(1.23)},
			expected: mustHash(t, struct {
				F *big.Float `datahash:"text"`
			}{F: big.NewFloat(1.23)}, datahash.Options{}),
		},
		{
			name: "map value",
			value: map[string]int{
				"b": 2,
				"a": 1,
			},
			expected: mustHash(t, map[string]int{
				"a": 1,
				"b": 2,
			}, datahash.Options{}),
		},
	}

	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hasher := datahash.New(fnv.New64a, tc.options)
			got, err := hasher.Hash(tc.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.expected {
				t.Errorf("hash mismatch: got %d, want %d", got, tc.expected)
			}
		})
	}
}

func mustHash(t *testing.T, value any, opts datahash.Options) uint64 {
	t.Helper()
	h := datahash.New(fnv.New64a, opts)
	out, err := h.Hash(value)
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	return out
}
