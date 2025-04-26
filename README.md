# datahash

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/go-sqlt/datahash)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/go-sqlt/datahash.svg?style=social)](https://github.com/go-sqlt/datahash/tags)
[![Coverage](https://img.shields.io/badge/Coverage-79.2%25-brightgreen)](https://github.com/go-sqlt/datahash/actions)

datahash provides a hashing system for arbitrary Go values with zero dependencies.  
It produces consistent 64-bit hashes by recursively traversing Go data structures.  
The package is highly customizable, efficient, and integrates with standard Go interfaces.

## Features

- Consistent 64-bit hashing of any Go value
- Handles cyclic data structures safely (pointer tracking)
- Supports struct tags and per-field options
- Slices can be treated as unordered sets (set option)
- Integrates with: encoding.BinaryMarshaler, encoding.TextMarshaler, encoding/json.Marshaler, fmt.Stringer
- Supports custom hash logic via datahash.HashWriter interface
- High performance: type caching and hasher pooling

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
	hasher := datahash.New(xxhash.New, datahash.Options{JSON: true})

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

- Tag: Struct tag to control field behavior (default: datahash)
- Marker: Add type markers to the hash (datahash:"marker")
- Set: Treat slices as unordered sets (datahash:"set")
- Binary: Prefer encoding.BinaryMarshaler (datahash:"binary")
- Text: Prefer encoding.TextMarshaler (datahash:"text")
- JSON: Prefer json.Marshaler (datahash:"json")
- String: Prefer fmt.Stringer (datahash:"string")
- ZeroNil: Treat nil pointers as zero values (datahash:"zeronil")
- IgnoreZero: Skip zero-value fields from hash (datahash:"ignorezero")

## Notes

- Only exported fields are considered
- Struct fields are hashed in their declared order
- Maps and sets are folded using XOR for order-independence
- Cyclic pointers are detected and skipped safely
- Use datahash:"-" to exclude fields from hashing
- Implement HashWriter for custom hash behavior

## Benchmark

```go
go test -bench=. -benchmem                                                  
goos: darwin
goarch: arm64
pkg: github.com/go-sqlt/datahash
cpu: Apple M3 Pro
BenchmarkHashers/Datahash+FNV_(Marker=false)-12                  1988764               573.8 ns/op           258 B/op          8 allocs/op
BenchmarkHashers/Datahash+FNV_(Marker=true)-12                   1672119               716.8 ns/op           258 B/op          8 allocs/op
BenchmarkHashers/Hashstructure+FNV-12                             346440              3367 ns/op            2544 B/op        159 allocs/op
BenchmarkHashers/JSON+FNV-12                                     1256473               954.3 ns/op           516 B/op          8 allocs/op
BenchmarkHashers/JSON_only-12                                    1847144               654.8 ns/op           516 B/op          8 allocs/op
BenchmarkHashers/FNV_only-12                                     4136947               289.7 ns/op             0 B/op          0 allocs/op
PASS
ok      github.com/go-sqlt/datahash     10.594s
```