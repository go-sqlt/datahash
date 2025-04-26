package datahash_test

import (
	"encoding/json"
	"hash/fnv"
	"math/big"
	"net/url"
	"testing"

	"github.com/go-sqlt/datahash"
	hashstructure "github.com/mitchellh/hashstructure/v2"
)

type Nested struct {
	Score float64
	Flags map[string]bool
}

type BenchStruct struct {
	ID     int
	Name   string
	Tags   []string
	Nested Nested
	Big    *big.Int
	URL    *url.URL `datahash:"binary"`
}

func getTestValue() BenchStruct {
	u, _ := url.Parse("https://example.com/path?query=value")
	return BenchStruct{
		ID:   123,
		Name: "Example",
		Tags: []string{"go", "hash", "test"},
		Nested: Nested{
			Score: 99.5,
			Flags: map[string]bool{
				"fast":  true,
				"cheap": false,
			},
		},
		Big: big.NewInt(10),
		URL: u,
	}
}

func BenchmarkHashers(b *testing.B) {
	val := getTestValue()

	b.Run("Datahash+FNV/Marker=false", func(b *testing.B) {
		hasher := datahash.New(fnv.New64a, datahash.Options{Marker: false})

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := hasher.Hash(val); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Datahash+FNV/Marker=true", func(b *testing.B) {
		hasher := datahash.New(fnv.New64a, datahash.Options{Marker: true})

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := hasher.Hash(val); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Hashstructure+FNV", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := hashstructure.Hash(val, hashstructure.FormatV2, nil); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("JSON+FNV", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		hasher := fnv.New64a()

		for b.Loop() {
			data, err := json.Marshal(val)
			if err != nil {
				b.Fatal(err)
			}

			if _, err = hasher.Write(data); err != nil {
				b.Fatal(err)
			}
			_ = hasher.Sum64()
		}
	})
}
