package datahash_test

import (
	"hash/fnv"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/go-sqlt/datahash"
)

func generateRandomValue(r *rand.Rand, depth int) any {
	if depth > 3 { // Limit recursion depth
		return r.IntN(1000)
	}

	switch r.IntN(8) {
	case 0:
		return r.IntN(1000)
	case 1:
		return r.Float64()
	case 2:
		return r.IntN(2) == 0
	case 3:
		return randomString(r, 5+r.IntN(10)) // <- fixed
	case 4:
		n := r.IntN(5)
		arr := make([]int, n)
		for i := range arr {
			arr[i] = r.IntN(1000)
		}
		return arr
	case 5:
		m := make(map[string]int)
		n := r.IntN(5)
		for range n {
			m[randomString(r, 5+r.IntN(5))] = r.IntN(1000)
		}
		return m
	case 6:
		type Nested struct {
			A int
			B float64
			C []string
		}
		return Nested{
			A: r.IntN(100),
			B: r.Float64(),
			C: []string{
				randomString(r, 5+r.IntN(5)),
				randomString(r, 5+r.IntN(5)),
			},
		}
	case 7:
		return &struct {
			X int
			Y []byte
		}{
			X: r.IntN(100),
			Y: []byte(randomString(r, 10)),
		}
	default:
		return nil
	}
}

func FuzzHash(f *testing.F) {
	hasher := datahash.New(fnv.New64a, datahash.Options{})
	r := rand.New(rand.NewPCG(1, 2))

	seen := make(map[uint64]any) // hash -> value

	f.Add("seed")

	f.Fuzz(func(t *testing.T, seed string) {
		_ = seed

		val := generateRandomValue(r, 0)

		h1, err := hasher.Hash(val)
		if err != nil {
			t.Fatalf("unexpected error hashing random value (type %T): %v", val, err)
		}

		if prev, exists := seen[h1]; exists {
			if !valuesAreZero(prev) && !valuesAreZero(val) && !reflect.DeepEqual(prev, val) {
				t.Fatalf("hash collision detected:\nprevious: %#v\nnew: %#v\nhash: %d", prev, val, h1)
			}
		} else {
			seen[h1] = val
		}

		rv := reflect.ValueOf(val)
		h2, err := hasher.Hash(rv.Interface())
		if err != nil {
			t.Fatalf("unexpected error hashing reflect value (type %T): %v", val, err)
		}

		if h1 != h2 {
			t.Fatalf("inconsistent hash: value vs reflect.Value; %v != %v", h1, h2)
		}
	})
}

func valuesAreZero(v any) bool {
	rv := reflect.ValueOf(v)
	return !rv.IsValid() || rv.IsZero()
}

func randomString(r *rand.Rand, length int) string {
	letters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = letters[r.IntN(len(letters))]
	}
	return string(b)
}
