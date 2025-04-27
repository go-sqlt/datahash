package datahash_test

import (
	"encoding/json"
	"hash/fnv"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/go-sqlt/datahash"
	gohugoio "github.com/gohugoio/hashstructure"
	mitchellh "github.com/mitchellh/hashstructure/v2"
)

// Beispiele für verschiedene Typen

type SimpleStruct struct {
	Name string
	Age  int
}

type ComplexStruct struct {
	ID      int
	Created time.Time
	Details map[string]*big.Int
	Tags    []string
	URL     *url.URL `datahash:"binary"`
}

func getSimpleStruct() SimpleStruct {
	return SimpleStruct{
		Name: "Alice",
		Age:  30,
	}
}

func getComplexStruct() ComplexStruct {
	u, _ := url.Parse("https://example.com/path?query=value")
	return ComplexStruct{
		ID:      123456,
		Created: time.Now(),
		Details: map[string]*big.Int{
			"one": big.NewInt(1),
			"two": big.NewInt(2),
		},
		Tags: []string{"go", "benchmark", "hash"},
		URL:  u,
	}
}

func BenchmarkHashers(b *testing.B) {
	// Verschiedene Werte
	simple := getSimpleStruct()
	complex := getComplexStruct()
	primitive := 123456789
	stringValue := "Hello, World!"
	mapValue := map[string]any{
		"key1": "value1",
		"key2": 42,
		"key3": []int{1, 2, 3},
	}

	cases := []struct {
		name string
		val  any
	}{
		{"Primitive int ", primitive},
		{"String value  ", stringValue},
		{"Simple struct ", simple},
		{"Complex struct", complex},
		{"Map value     ", mapValue},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.Run("Datahash               ", func(b *testing.B) {
				hasher := datahash.New(fnv.New64a, &datahash.Options{IgnoreZero: true})

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := hasher.Hash(c.val); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("Mitchellh/Hashstructure", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := mitchellh.Hash(c.val, mitchellh.FormatV2, &mitchellh.HashOptions{
						IgnoreZeroValue: true,
					}); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("Gohugoio/Hashstructure ", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := gohugoio.Hash(c.val, &gohugoio.HashOptions{
						IgnoreZeroValue: true,
					}); err != nil {
						b.Fatal(err)
					}
					b.N--
				}
			})

			b.Run("JSON                   ", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()

				for b.Loop() {
					hasher := fnv.New64a()

					data, err := json.Marshal(c.val)
					if err != nil {
						b.Fatal(err)
					}

					if _, err = hasher.Write(data); err != nil {
						b.Fatal(err)
					}
					_ = hasher.Sum64()
				}
			})
		})
	}
}
