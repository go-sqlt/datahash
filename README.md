# datahash

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/go-sqlt/datahash)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/go-sqlt/datahash.svg?style=social)](https://github.com/go-sqlt/datahash/tags)
[![Coverage](https://img.shields.io/badge/Coverage-79.3%25-brightgreen)](https://github.com/go-sqlt/datahash/actions)

datahash provides a hashing system for arbitrary Go values with zero dependencies.  
It produces consistent 64-bit hashes by recursively traversing Go data structures.  
The package is highly customizable, efficient, and integrates with standard Go interfaces.

## Features

- Consistent 64-bit hashing of any Go value.
- Handles cyclic data structures safely (pointer tracking).
- Supports struct tags and per-field options.
- Slices, iter.Seq, iter.Seq2 can be treated as unordered sets (like maps).
- Integrates with: encoding.BinaryMarshaler, encoding.TextMarshaler, encoding/json.Marshaler, fmt.Stringer.
- Supports custom hash logic via datahash.HashWriter interface.
- High performance: type caching and hasher pooling.

## Installation

```bash
go get github.com/go-sqlt/datahash
```

## Usage

```go
package main

import (
	"fmt"
	"math/big"

	"github.com/cespare/xxhash/v2"
	"github.com/go-sqlt/datahash"
)

type MyStruct struct {
	Name  string `datahash:"-"`
	Age   int
	Float *big.Float `datahash:"text"`
}

func main() {
    hasher := datahash.New(xxhash.New, datahash.Options{
        Marker:     false,
        Set:        false,
        Binary:     false,
        Text:       false,
        JSON:       false,
        String:     false,
        ZeroNil:    false,
        IgnoreZero: false,
    })

	alice, err := hasher.Hash(MyStruct{
		Name:  "Alice",
		Age:   30,
		Float: big.NewFloat(1.23),
	})
	fmt.Println(alice, err)
	// 8725273371882850098 <nil>

	bob, err := hasher.Hash(MyStruct{
		Name:  "Bob",
		Age:   30,
		Float: big.NewFloat(1.23),
	})
	fmt.Println(bob, err)
	// 8725273371882850098 <nil>
}
```

## Options

- Tag: Struct tag to control field behavior (default: datahash).
- Marker: Add type markers to the hash (datahash:"marker").
- Set: Treat slices, iter.Seq, iter.Seq2 as unordered sets (datahash:"set").
- Binary: Prefer encoding.BinaryMarshaler (datahash:"binary").
- Text: Prefer encoding.TextMarshaler (datahash:"text").
- JSON: Prefer json.Marshaler (datahash:"json").
- String: Prefer fmt.Stringer (datahash:"string").
- ZeroNil: Treat nil pointers as zero values (datahash:"zeronil").
- IgnoreZero: Skip zero-value fields from hash (datahash:"ignorezero").

## Notes

- Struct fields are hashed in their declared order.
- Maps and sets are folded using XOR for order-independence.
- Cyclic pointers are detected and skipped safely.
- Use datahash:"-" to exclude fields from hashing.
- Implement HashWriter for custom hash behavior.

## Benchmark

This benchmark demonstrates that datahash is faster and more memory-efficient than 
alternatives like hashstructure or JSON marshaling with FNV hashing:

```go
go test -bench=. -benchmem
goos: darwin
goarch: arm64
pkg: github.com/go-sqlt/datahash
cpu: Apple M3 Pro
BenchmarkHashers/Datahash+FNV/Marker=false-12            1783852               646.9 ns/op           258 B/op          8 allocs/op
BenchmarkHashers/Datahash+FNV/Marker=true-12             1420657               845.6 ns/op           258 B/op          8 allocs/op
BenchmarkHashers/Mitchellh/Hashstructure+FNV-12           340186              3466 ns/op            2544 B/op        159 allocs/op
BenchmarkHashers/Gohugoio/Hashstructure+FNV-12            325401              3611 ns/op            2472 B/op        156 allocs/op
BenchmarkHashers/JSON+FNV-12                             1260165               954.6 ns/op           516 B/op          8 allocs/op
PASS
ok      github.com/go-sqlt/datahash     6.137s
```
