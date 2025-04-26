package datahash_test

import (
	"fmt"
	"hash"
	"hash/fnv"
	"maps"
	"slices"
	"testing"

	"github.com/go-sqlt/datahash"
)

type testCase struct {
	name     string
	value    any
	options  datahash.Options
	expected uint64
}

type customHash struct {
	Value string
}

func (c customHash) WriteHash(hash hash.Hash64) error {
	_, err := hash.Write([]byte("custom:" + c.Value))

	return err
}

type stringerType struct {
	V int
}

func (s stringerType) String() string {
	return fmt.Sprintf("S:%d", s.V)
}

type textMarshaler struct {
	V string
}

func (t textMarshaler) MarshalText() ([]byte, error) {
	return []byte("TM:" + t.V), nil
}

type binaryMarshaler struct {
	N int
}

func (b binaryMarshaler) MarshalBinary() ([]byte, error) {
	return []byte{byte(b.N)}, nil
}

type jsonMarshaler struct {
	Val string
}

func (j jsonMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"` + j.Val + `"`), nil
}

func TestHasher_Hash(t *testing.T) {
	tests := []testCase{
		{"int", 42, datahash.Options{}, mustHash(t, 42, datahash.Options{})},
		{"uint", uint64(42), datahash.Options{}, mustHash(t, uint64(42), datahash.Options{})},
		{"bool", true, datahash.Options{}, mustHash(t, true, datahash.Options{})},
		{"float64", 3.14, datahash.Options{}, mustHash(t, 3.14, datahash.Options{})},
		{"string", "hello", datahash.Options{}, mustHash(t, "hello", datahash.Options{})},
		{"ignored struct field", struct {
			Secret string `datahash:"-"`
			X      int
		}{"hidden", 1}, datahash.Options{}, mustHash(t, struct {
			Secret string `datahash:"-"`
			X      int
		}{"whatever", 1}, datahash.Options{})},
		{"json tag", struct {
			V any `datahash:"json"`
		}{[]int{1, 2, 3}}, datahash.Options{}, mustHash(t, struct {
			V any `datahash:"json"`
		}{[]int{1, 2, 3}}, datahash.Options{})},

		{"stringer field", struct {
			V stringerType `datahash:"string"`
		}{stringerType{V: 9}}, datahash.Options{}, mustHash(t, struct {
			V stringerType `datahash:"string"`
		}{stringerType{V: 9}}, datahash.Options{})},
		{"binary field", struct {
			V binaryMarshaler `datahash:"binary"`
		}{binaryMarshaler{N: 255}}, datahash.Options{}, mustHash(t, struct {
			V binaryMarshaler `datahash:"binary"`
		}{binaryMarshaler{N: 255}}, datahash.Options{})},
		{"slice vs set", []int{1, 2, 3}, datahash.Options{Set: true}, mustHash(t, []int{3, 2, 1}, datahash.Options{Set: true})},
		{"array order matters", [3]int{1, 2, 3}, datahash.Options{}, mustHash(t, [3]int{1, 2, 3}, datahash.Options{})},
		{"pointer value", ptrTo(99), datahash.Options{}, mustHash(t, ptrTo(99), datahash.Options{})},
		{"cyclic pointer", makeCyclic(), datahash.Options{}, mustHash(t, makeCyclic(), datahash.Options{})},
		{"custom hash writer", customHash{"abc"}, datahash.Options{}, mustHash(t, customHash{"abc"}, datahash.Options{})},
		{"nil pointer", (*int)(nil), datahash.Options{}, mustHash(t, (*int)(nil), datahash.Options{})},
		{"nil interface", (interface{})(nil), datahash.Options{}, mustHash(t, (interface{})(nil), datahash.Options{})},
		{"slice with nils", []*int{nil, ptrTo(1)}, datahash.Options{}, mustHash(t, []*int{nil, ptrTo(1)}, datahash.Options{})},
		{"map with zero value", map[string]int{"a": 0}, datahash.Options{}, mustHash(t, map[string]int{"a": 0}, datahash.Options{})},
		{"map with values", map[string]int{"a": 100, "b": 200}, datahash.Options{}, mustHash(t, map[string]int{"a": 100, "b": 200}, datahash.Options{})},
		{"empty map", map[int]string{}, datahash.Options{}, mustHash(t, map[int]string{}, datahash.Options{})},
		{"empty slice", []string{}, datahash.Options{}, mustHash(t, []string{}, datahash.Options{})},
		{"text marshal global option", textMarshaler{"global"}, datahash.Options{Text: true}, mustHash(t, textMarshaler{"global"}, datahash.Options{Text: true})},
		{"binary marshal global option", binaryMarshaler{5}, datahash.Options{Binary: true}, mustHash(t, binaryMarshaler{5}, datahash.Options{Binary: true})},
		{"json marshal global option", struct{ X int }{X: 1}, datahash.Options{JSON: true}, mustHash(t, struct{ X int }{X: 1}, datahash.Options{JSON: true})},
		{"stringer global option", stringerType{42}, datahash.Options{String: true}, mustHash(t, stringerType{42}, datahash.Options{String: true})},
		{"zeronil enabled", (*int)(nil), datahash.Options{ZeroNil: true}, mustHash(t, 0, datahash.Options{ZeroNil: true})},
		{"ignorezero field skipped", struct {
			A int
			B int `datahash:"ignorezero"`
		}{A: 1, B: 0}, datahash.Options{}, mustHash(t, struct {
			A int
			B int `datahash:"ignorezero"`
		}{A: 1, B: 0}, datahash.Options{})},
		{"ignorezero set globally", struct {
			A int
			B int
		}{A: 1, B: 0}, datahash.Options{IgnoreZero: true}, mustHash(t, struct {
			A int
			B int
		}{A: 1, B: 0}, datahash.Options{IgnoreZero: true})},
		{"complex128 value", complex(1.5, -2.5), datahash.Options{}, mustHash(t, complex(1.5, -2.5), datahash.Options{})},
		{"seq2 slice type", slices.All([]int{10, 20, 30}), datahash.Options{}, mustHash(t, slices.All([]int{10, 20, 30}), datahash.Options{})},
		{"seq slice type", slices.Values([]int{10, 20, 30}), datahash.Options{}, mustHash(t, slices.Values([]int{10, 20, 30}), datahash.Options{})},
		{"seq slice type as set", slices.Values([]int{10, 20, 30}), datahash.Options{Set: true}, mustHash(t, slices.Values([]int{10, 30, 20}), datahash.Options{Set: true})},
		{"seq map type", maps.All(map[int]string{1: "one", 2: "two"}), datahash.Options{Set: true}, mustHash(t, maps.All(map[int]string{1: "one", 2: "two"}), datahash.Options{Set: true})},
		{"byte slice", []byte("hello"), datahash.Options{}, mustHash(t, []byte("hello"), datahash.Options{})},
		{"interface json marshal", jsonMarshaler{Val: "json"}, datahash.Options{JSON: true}, mustHash(t, jsonMarshaler{Val: "json"}, datahash.Options{JSON: true})},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hasher := datahash.New(fnv.New64a, tc.options)
			got, err := hasher.Hash(tc.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.expected {
				t.Errorf("hash mismatch:\n  got:  %d\n  want: %d", got, tc.expected)
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

func ptrTo[T any](v T) *T {
	return &v
}

type node struct {
	Value int
	Next  *node
}

func makeCyclic() *node {
	a := &node{Value: 1}
	b := &node{Value: 2, Next: a}
	a.Next = b
	return a
}
