package datahash_test

import (
	"encoding/json"
	"hash/fnv"
	"math/big"
	"math/rand/v2"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/go-sqlt/datahash"
)

func generateRandomValue(r *rand.Rand, depth int) any {
	if depth > 4 {
		return r.IntN(1000)
	}

	switch r.IntN(14) {
	case 0:
		return r.IntN(1_000_000)
	case 1:
		return r.Float64()
	case 2:
		return r.IntN(2) == 0
	case 3:
		return randomString(r, 3+r.IntN(20))
	case 4:
		n := r.IntN(5)
		arr := make([]int, n)
		for i := range arr {
			arr[i] = r.IntN(5000)
		}
		return arr
	case 5:
		n := r.IntN(5)
		m := make(map[string]float64, n)
		for i := 0; i < n; i++ {
			m[randomString(r, 3+r.IntN(5))] = r.Float64()
		}
		return m
	case 6:
		return struct {
			A string
			B int
			C []byte
		}{
			A: randomString(r, 5+r.IntN(10)),
			B: r.IntN(1000),
			C: []byte(randomString(r, 5)),
		}
	case 7:
		return &struct {
			X bool
			Y *big.Int
		}{
			X: r.IntN(2) == 0,
			Y: big.NewInt(r.Int64N(1e6)),
		}
	case 8:
		u := &url.URL{
			Scheme:   "https",
			Host:     "example.com",
			Path:     "/path/" + randomString(r, 3),
			RawQuery: "query=" + randomString(r, 5),
		}
		return u
	case 9:
		type Nested struct {
			ID   int
			Name string
			Data []float64
		}
		return Nested{
			ID:   r.IntN(1000),
			Name: randomString(r, 10),
			Data: []float64{r.Float64(), r.Float64()},
		}
	case 10:
		// Array of strings
		var arr [4]string
		for i := range arr {
			arr[i] = randomString(r, 3+r.IntN(5))
		}
		return arr
	case 11:
		return []*int{ptr(r.IntN(10)), ptr(r.IntN(20)), nil}
	case 12:
		// Random JSON object
		obj := map[string]any{
			"foo": randomString(r, 5),
			"bar": r.IntN(100),
			"baz": []int{r.IntN(5), r.IntN(10)},
		}
		b, _ := json.Marshal(obj)
		return b
	case 13:
		// Recursively nested random
		return generateRandomValue(r, depth+1)
	default:
		return nil
	}
}

func FuzzHash(f *testing.F) {
	hasher := datahash.New(fnv.New64a, datahash.Options{})
	r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 42))

	seen := make(map[uint64]any)

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
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = letters[r.IntN(len(letters))]
	}
	return string(b)
}

func ptr[T any](v T) *T {
	return &v
}
