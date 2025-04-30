# datahash

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/go-sqlt/datahash)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/go-sqlt/datahash.svg?style=social)](https://github.com/go-sqlt/datahash/tags)
[![Coverage](https://img.shields.io/badge/Coverage-73.8%25-brightgreen)](https://github.com/go-sqlt/datahash/actions)

datahash provides high-performance, customizable hashing for arbitrary Go values with zero dependencies.  
It produces consistent 64-bit hashes by recursively traversing data structures.

## Features

- Consistent 64-bit hashing of any Go value.
- Supports structs, slices, iter.Seq, and iter.Seq2 as unordered sets (default: ordered).
- Supports custom hash logic via datahash.HashWriter or encoding.BinaryMarshaler interface.
- Integrates with: encoding.BinaryMarshaler, encoding.TextMarshaler, encoding/json.Marshaler, fmt.Stringer.
- Handles cyclic data structures safely (pointer tracking).
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
	Float *big.Float
}

func main() {
	hasher := datahash.New(xxhash.New, datahash.Options{
		Unordered:  false,
		Text:       true, // big.Float implements encoding.TextMarshaler
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
| Unordered  | Treat structs, slices, iter.Seq, and iter.Seq2 as unordered sets. |
| Text       | Prefer `encoding.TextMarshaler` if available. |
| JSON       | Prefer `json.Marshaler` if available. |
| String     | Prefer `fmt.Stringer` if available. |
| ZeroNil    | Treat nil pointers like zero values. |
| IgnoreZero | Skip zero-value fields from hashing. |

## Notes

- By default struct fields are hashed in their declared order.
- Maps and unordered sets are folded using XOR for order-independence.
- Cyclic pointers are detected and skipped safely.
- Use datahash:"-" to exclude fields from hashing.
- Implement `datahash.HashWriter` or `encoding.BinaryMarshaler` for custom hash behavior.
- Unexported fields cannot be used with custom marshalers. (!)

## Benchmark

These benchmarks demonstrate that datahash is 5–6× faster and significantly more memory-efficient than both 
mitchellh/hashstructure and gohugoio/hashstructure.

Notably, serializing to JSON and hashing the output with cespare/xxhash/v2 performs reasonably well and can 
be a viable alternative in some cases. However, JSON-based approaches cannot guarantee consistent map ordering during hashing.

By contrast, datahash always hashes maps as unordered sets and offers the same unordered (set-like) treatment 
for structs, slices, arrays, and iterators—making it more deterministic and flexible for complex data structures.

```go
go test -bench=. -benchmem               
goos: darwin
goarch: arm64
pkg: github.com/go-sqlt/datahash
cpu: Apple M3 Pro
BenchmarkHashers/Simple_struct_/Datahash+fnv____-12             20743609                57.63 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Simple_struct_/Mitchellh+fnv___-12              2947899               393.8 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/Gohugoio+fnv____-12              3012933               397.6 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/JSON+fnv________-12             12187029                96.18 ns/op           32 B/op          1 allocs/op
BenchmarkHashers/Simple_struct_/Datahash+xxhash_-12             17991476                65.04 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Simple_struct_/Mitchellh+xxhash-12              2909797               409.8 ns/op           320 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/Gohugoio+xxhash_-12              3165878               378.1 ns/op           280 B/op         13 allocs/op
BenchmarkHashers/Simple_struct_/JSON+xxhash_____-12             14335510                83.70 ns/op           32 B/op          1 allocs/op
BenchmarkHashers/Complex_struct/Datahash+fnv____-12              2552940               469.6 ns/op           112 B/op          3 allocs/op
BenchmarkHashers/Complex_struct/Mitchellh+fnv___-12               407378              2917 ns/op            1824 B/op        116 allocs/op
BenchmarkHashers/Complex_struct/Gohugoio+fnv____-12               384549              3049 ns/op            1816 B/op        115 allocs/op
BenchmarkHashers/Complex_struct/JSON+fnv________-12              1000000              1117 ns/op             402 B/op          4 allocs/op
BenchmarkHashers/Complex_struct/Datahash+xxhash_-12              2326400               510.9 ns/op           112 B/op          3 allocs/op
BenchmarkHashers/Complex_struct/Mitchellh+xxhash-12               371761              3171 ns/op            1896 B/op        116 allocs/op
BenchmarkHashers/Complex_struct/Gohugoio+xxhash_-12               417607              2821 ns/op            1632 B/op         87 allocs/op
BenchmarkHashers/Complex_struct/JSON+xxhash_____-12              1485633               809.5 ns/op           402 B/op          4 allocs/op
PASS
ok      github.com/go-sqlt/datahash     19.083s
```
