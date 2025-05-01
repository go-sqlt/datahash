package datahash_test

import (
	"fmt"
	"hash"
	"hash/fnv"
	"maps"
	"slices"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/go-sqlt/datahash"
)

type testCase struct {
	name           string
	value          any
	options        datahash.Options
	expectedFNV    uint64
	expectedXXHash uint64
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
		{"int", 42, datahash.Options{}, 18391255480883862255, 13066772586158965587},
		{"uint", uint64(42), datahash.Options{}, 18391255480883862255, 13066772586158965587},
		{"bool", true, datahash.Options{}, 12638152016183539244, 9962287286179718960},
		{"float64", 3.14, datahash.Options{}, 156833439713284410, 12670157684664489231},
		{"string", "hello", datahash.Options{}, 11831194018420276491, 2794345569481354659},
		{"ignored struct field", struct {
			Secret string `datahash:"-"`
			X      int
		}{"hidden", 1}, datahash.Options{}, 16533391434161719775, 5181320448927313825},
		{"json tag", struct {
			V any
		}{[]int{1, 2, 3}}, datahash.Options{}, 5608028861651753673, 13593344203319200405},
		{"stringer field", struct {
			V stringerType
		}{stringerType{V: 9}}, datahash.Options{String: true}, 1704179339678544436, 6973922210871028143},
		{"binary field", struct {
			V binaryMarshaler
		}{binaryMarshaler{N: 255}}, datahash.Options{}, 11192428154555478883, 5928437656725233329},
		{"slice vs set", []int{1, 2, 3}, datahash.Options{UnorderedSlice: true}, 17645463890579864133, 4337263566436072607},
		{"array order matters", [3]int{1, 2, 3}, datahash.Options{}, 9037388837959980876, 15299716731391107029},
		{"pointer value", ptrTo(99), datahash.Options{}, 12041394348134418438, 12663767419032247267},
		{"cyclic pointer", makeCyclic(), datahash.Options{}, 8122202391527501320, 18406638134627774035},
		{"custom hash writer", customHash{"abc"}, datahash.Options{}, 9627794456967199124, 11362593029884486877},
		{"nil pointer", (*int)(nil), datahash.Options{}, 14695981039346656037, 17241709254077376921},
		{"nil interface", (any)(nil), datahash.Options{}, 14695981039346656037, 17241709254077376921},
		{"slice with nils", []*int{nil, ptrTo(1)}, datahash.Options{}, 1378796707385414904, 1435598622177930143},
		{"map with zero value", map[string]int{"a": 0}, datahash.Options{}, 8020775391560901610, 3606100179855924115},
		{"empty map", map[int]string{}, datahash.Options{}, 586861065889900642, 9169957362658601663},
		{"map with zero value ignore zero", map[string]int{"a": 0}, datahash.Options{IgnoreZero: true}, 586861065889900642, 9169957362658601663},
		{"map with values", map[string]int{"a": 100, "b": 200}, datahash.Options{}, 11831641390546595452, 7301628112708648923},
		{"empty slice", []string{}, datahash.Options{}, 588776415145865754, 5936373637795240346},
		{"text marshal global option", textMarshaler{"global"}, datahash.Options{Text: true}, 6256686775322657367, 7874007038817112696},
		{"binary marshal global option", binaryMarshaler{5}, datahash.Options{}, 12638147618137026400, 5836407453717141263},
		{"json marshal global option", struct{ X int }{X: 1}, datahash.Options{JSON: true}, 16533391434161719775, 5181320448927313825},
		{"stringer global option", stringerType{42}, datahash.Options{String: true}, 13766696074135465618, 3853657757851777848},
		{"nil int zeronil enabled", (*int)(nil), datahash.Options{ZeroNil: true}, 12161962213042174405, 3803688792395291579},
		{"nil int", (*int)(nil), datahash.Options{}, 14695981039346656037, 17241709254077376921},
		{"ignorezero", struct {
			A int
			C int
		}{A: 1, C: 0}, datahash.Options{IgnoreZero: true}, 14952894133494373672, 13237382587658828078},
		{"ignorezero 2", struct {
			A int
			B int
		}{A: 1, B: 0}, datahash.Options{IgnoreZero: true}, 14952894133494373672, 13237382587658828078},
		{"ignorezero 2 as set", struct {
			A int
			B int
		}{A: 1, B: 0}, datahash.Options{IgnoreZero: true, UnorderedStruct: true}, 11905026311571686442, 4322134186644821414},
		{"ignorezero 2 as set", map[string]any{"A": 1}, datahash.Options{IgnoreZero: true}, 11905026311571686442, 4322134186644821414},
		{"complex128 value", complex(1.5, -2.5), datahash.Options{}, 6394125825557071332, 15864597077577832294},
		{"seq2 slice type", slices.All([]int{10, 20, 30}), datahash.Options{}, 8416685136511854477, 5615642930228160039},
		{"seq slice type", slices.Values([]int{10, 20, 30}), datahash.Options{}, 7406063137916122164, 3727773546853566122},
		{"seq slice type as set", slices.Values([]int{10, 20, 30}), datahash.Options{UnorderedSeq: true}, 653593453226122035, 2257683953387770671},
		{"seq map type", maps.All(map[int]string{1: "one", 2: "two"}), datahash.Options{UnorderedSeq2: true}, 15472268656711951149, 16081739586471367953},
		{"byte slice", []byte("hello"), datahash.Options{}, 11831194018420276491, 2794345569481354659},
		{"interface json marshal", jsonMarshaler{Val: "json"}, datahash.Options{JSON: true}, 2069784589039126867, 3804726148011779533},
		{"map equals struct set", map[string]any{"one": 1, "Two": "2"}, datahash.Options{}, 2738323115711972740, 7596626971113218840},
		{"map equals struct set 2", map[string]any{"one": 1, "two": "2"}, datahash.Options{}, 15991332270130111181, 2699984971857782748},
		{"slice", []any{1, "2", true}, datahash.Options{UnorderedSlice: true}, 1966337816571714992, 15381331533124161484},
		{"iter.Seq", slices.Values([]any{1, "2", true}), datahash.Options{UnorderedSeq: true}, 1966337816571714992, 15381331533124161484},
		{"empty slice", []any{0, false, ""}, datahash.Options{IgnoreZero: true}, 588776415145865754, 5936373637795240346},
		{"empty iter.Seq", slices.Values([]any{0, false, ""}), datahash.Options{IgnoreZero: true}, 588776415145865754, 5936373637795240346},
		{"empty iter.Seq2", slices.All([]any{0, false, ""}), datahash.Options{IgnoreZero: true}, 588776415145865754, 5936373637795240346},
		{"empty slice as set", []any{0, false, ""}, datahash.Options{IgnoreZero: true, UnorderedSlice: true}, 586861065889900642, 9169957362658601663},
		{"empty iter.Seq as set", slices.Values([]any{0, false, ""}), datahash.Options{IgnoreZero: true, UnorderedSeq: true}, 586861065889900642, 9169957362658601663},
	}

	t.Run("fnv.New64a", func(t *testing.T) {
		for _, tc := range tests {
			hasher := datahash.New(fnv.New64a, tc.options)
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := hasher.Hash(tc.value)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tc.expectedFNV {
					t.Errorf("hash mismatch:\n  got:  %d\n  want: %d", got, tc.expectedFNV)
				}
			})
		}
	})

	t.Run("xxhash.New", func(t *testing.T) {
		for _, tc := range tests {
			hasher := datahash.New(xxhash.New, tc.options)

			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := hasher.Hash(tc.value)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tc.expectedXXHash {
					t.Errorf("hash mismatch:\n  got:  %d\n  want: %d", got, tc.expectedXXHash)
				}
			})
		}
	})
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
