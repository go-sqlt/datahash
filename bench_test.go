package datahash_test

import (
	"encoding/json"
	"hash/fnv"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/go-sqlt/datahash"
	gohugoio "github.com/gohugoio/hashstructure"
	mitchellh "github.com/mitchellh/hashstructure/v2"
)

// Beispiele für verschiedene Typen

type SimpleStruct struct {
	Name string
	Age  int
}

type Detail struct {
	Info   string
	Number *big.Int
}

type ComplexStruct struct {
	ID      int
	Created time.Time
	Details [2]Detail
	Tags    []string
	URL     *url.URL
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
		Details: [2]Detail{
			{Info: "one", Number: big.NewInt(1)},
			{Info: "two", Number: big.NewInt(2)},
		},
		Tags: []string{"go", "benchmark", "hash"},
		URL:  u,
	}
}

func BenchmarkHashers(b *testing.B) {
	// Verschiedene Werte
	simple := getSimpleStruct()
	complex := getComplexStruct()

	cases := []struct {
		name string
		val  any
	}{
		// {"Primitive int ", primitive},
		// {"String value  ", stringValue},
		{"Simple struct ", simple},
		{"Complex struct", complex},
		// {"Map value     ", mapValue},
	}

	var (
		result uint64
		err    error
	)

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.Run("Datahash+fnv ", func(b *testing.B) {
				hasher := datahash.New(fnv.New64a, datahash.Options{IgnoreZero: true})

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if result, err = hasher.Hash(c.val); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("Mitchellh+fnv", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if result, err = mitchellh.Hash(c.val, mitchellh.FormatV2, &mitchellh.HashOptions{
						IgnoreZeroValue: true,
					}); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("Gohugoio+fnv ", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if result, err = gohugoio.Hash(c.val, &gohugoio.HashOptions{
						IgnoreZeroValue: true,
					}); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("JSON+fnv     ", func(b *testing.B) {
				hasher := fnv.New64a()

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					hasher.Reset()

					data, err := json.Marshal(c.val)
					if err != nil {
						b.Fatal(err)
					}

					if _, err = hasher.Write(data); err != nil {
						b.Fatal(err)
					}

					result = hasher.Sum64()
				}
			})

			b.Run("Datahash+xxhash ", func(b *testing.B) {
				hasher := datahash.New(xxhash.New, datahash.Options{IgnoreZero: true})

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if result, err = hasher.Hash(c.val); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("Mitchellh+xxhash", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if result, err = mitchellh.Hash(c.val, mitchellh.FormatV2, &mitchellh.HashOptions{
						IgnoreZeroValue: true,
						Hasher:          xxhash.New(),
					}); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("Gohugoio+xxhash ", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if result, err = gohugoio.Hash(c.val, &gohugoio.HashOptions{
						IgnoreZeroValue: true,
						Hasher:          xxhash.New(),
					}); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("JSON+xxhash     ", func(b *testing.B) {
				hasher := xxhash.New()

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					hasher.Reset()

					data, err := json.Marshal(c.val)
					if err != nil {
						b.Fatal(err)
					}

					if _, err = hasher.Write(data); err != nil {
						b.Fatal(err)
					}

					result = hasher.Sum64()
				}
			})
		})
	}

	_ = result
}
