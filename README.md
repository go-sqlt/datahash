# datahash

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/go-sqlt/datahash)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/go-sqlt/datahash.svg?style=social)](https://github.com/go-sqlt/datahash/tags)
[![Coverage](https://img.shields.io/badge/Coverage-71.7%25-brightgreen)](https://github.com/go-sqlt/datahash/actions)

datahash provides high-performance, customizable hashing for arbitrary Go values with zero dependencies.  
It produces consistent 64-bit hashes by recursively traversing data structures.

## Features

- Consistent 64-bit hashing of any Go value.
- Handles cyclic data structures safely (pointer tracking).
- Supports struct tags and per-field options.
- Supports structs, slices, iter.Seq, and iter.Seq2 as unordered sets (default: ordered).
- Integrates with: encoding.BinaryMarshaler, encoding.TextMarshaler, encoding/json.Marshaler, fmt.Stringer.
- Supports custom hash logic via datahash.HashWriter interface.
- High performance: type caching and hasher pooling.

## Installation

```bash
go get -u github.com/go-sqlt/datahash
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
	hasher := datahash.New(xxhash.New, &datahash.Options{
		Set:        false,
		Text:       false,
		JSON:       false,
		String:     false,
		ZeroNil:    false,
		IgnoreZero: false,
	})

	alice, _ := hasher.Hash(MyStruct{Name: "Alice", Age: 30, Float: big.NewFloat(1.23)})
	bob, _ := hasher.Hash(MyStruct{Name: "Bob", Age: 30, Float: big.NewFloat(1.23)})

	fmt.Println(alice, alice == bob) // Output: 13125691809697640472 true
}
```

## Options

| Option     | Description |
|------------|-------------|
| Tag        | Struct tag key to control field behavior (default: `datahash`). |
| Set        | Treat structs, slices, iter.Seq, and iter.Seq2 as unordered sets. |
| Text       | Prefer `encoding.TextMarshaler` if available (`datahash:"text"`). |
| JSON       | Prefer `json.Marshaler` if available (`datahash:"json"`). |
| String     | Prefer `fmt.Stringer` if available (`datahash:"string"`). |
| ZeroNil    | Treat nil pointers like zero values (`datahash:"zeronil"`). |
| IgnoreZero | Skip zero-value fields from hashing (`datahash:"ignorezero"`). |

## Notes

- By default struct fields are hashed in their declared order.
- Maps and sets are folded using XOR for order-independence.
- Cyclic pointers are detected and skipped safely.
- Use datahash:"-" to exclude fields from hashing.
- Implement HashWriter for custom hash behavior.
- Unexported fields cannot be used with custom marshalers. (!)

## Benchmark

This benchmark demonstrates that datahash is faster and more memory-efficient than 
alternatives like hashstructure or JSON marshaling with FNV hashing.

```go
go test -bench=. -benchmem                                                                
goos: darwin
goarch: arm64
pkg: github.com/go-sqlt/datahash
cpu: Apple M3 Pro
BenchmarkHashers/Simple_struct_/Datahash+fnv_-12                19501466                60.28 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Simple_struct_/Mitchellh+fnv-12                 2291349               455.9 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/Gohugoio+fnv_-12                 2988314               394.0 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/JSON+fnv_____-12                12592008                94.32 ns/op           32 B/op          1 allocs/op
BenchmarkHashers/Simple_struct_/Datahash+xxhash_-12             16792522                70.96 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Simple_struct_/Mitchellh+xxhash-12              2965380               403.0 ns/op           320 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/Gohugoio+xxhash_-12              3271189               367.8 ns/op           280 B/op         13 allocs/op
BenchmarkHashers/Simple_struct_/JSON+xxhash_____-12             14283432                82.67 ns/op           32 B/op          1 allocs/op
BenchmarkHashers/Complex_struct/Datahash+fnv_-12                 2425962               498.1 ns/op           112 B/op          3 allocs/op
BenchmarkHashers/Complex_struct/Mitchellh+fnv-12                  401911              2860 ns/op            1824 B/op        116 allocs/op
BenchmarkHashers/Complex_struct/Gohugoio+fnv_-12                  396907              2965 ns/op            1816 B/op        115 allocs/op
BenchmarkHashers/Complex_struct/JSON+fnv_____-12                 1000000              1081 ns/op             402 B/op          4 allocs/op
BenchmarkHashers/Complex_struct/Datahash+xxhash_-12              2312679               518.5 ns/op           112 B/op          3 allocs/op
BenchmarkHashers/Complex_struct/Mitchellh+xxhash-12               378866              3077 ns/op            1896 B/op        116 allocs/op
BenchmarkHashers/Complex_struct/Gohugoio+xxhash_-12               431788              2710 ns/op            1632 B/op         87 allocs/op
BenchmarkHashers/Complex_struct/JSON+xxhash_____-12              1528984               782.6 ns/op           402 B/op          4 allocs/op
PASS
ok      github.com/go-sqlt/datahash     18.852s
```
