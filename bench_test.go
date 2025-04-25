package datahash_test

import (
	"encoding/json"
	"hash/fnv"
	"testing"

	"github.com/go-sqlt/datahash"
	hashstructure "github.com/mitchellh/hashstructure/v2"
)

type BenchStruct struct {
	ID     int
	Name   string
	Tags   []string
	Nested struct {
		Score float64
		Flags map[string]bool
	}
}

func getTestValue() BenchStruct {
	return BenchStruct{
		ID:   123,
		Name: "Example",
		Tags: []string{"go", "hash", "test"},
		Nested: struct {
			Score float64
			Flags map[string]bool
		}{
			Score: 99.5,
			Flags: map[string]bool{
				"fast":  true,
				"cheap": false,
			},
		},
	}
}

func BenchmarkDatahash(b *testing.B) {
	hasher := datahash.New(fnv.New64a, datahash.Options{})
	val := getTestValue()

	b.ResetTimer()
	for b.Loop() {
		_, err := hasher.Hash(val)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashstructure(b *testing.B) {
	val := getTestValue()

	b.ResetTimer()
	for b.Loop() {
		_, err := hashstructure.Hash(val, hashstructure.FormatV2, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSON(b *testing.B) {
	val := getTestValue()

	hasher := fnv.New64a()

	b.ResetTimer()
	for b.Loop() {
		data, err := json.Marshal(val)
		if err != nil {
			b.Fatal(err)
		}

		_, err = hasher.Write(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}
