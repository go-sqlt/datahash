# datahash

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/go-sqlt/datahash)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/go-sqlt/datahash.svg?style=social)](https://github.com/go-sqlt/datahash/tags)
[![Coverage](https://img.shields.io/badge/Coverage-77.1%25-brightgreen)](https://github.com/go-sqlt/datahash/actions)

datahash provides high-performance, customizable hashing for arbitrary Go values with zero dependencies.  
It produces consistent 64-bit hashes by recursively traversing data structures.

## Features

- Consistent 64-bit hashing of any Go value.
- Handles cyclic data structures safely (pointer tracking).
- Supports struct tags and per-field options.
- Supports slices, iter.Seq, and iter.Seq2 as unordered sets (optional).
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
    hasher := datahash.New(xxhash.New, datahash.Options{
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
    // The hash value is deterministic: Name field is ignored.
}
```

## Options

| Option     | Description |
|------------|-------------|
| Tag        | Struct tag key to control field behavior (default: `datahash`). |
| Set        | Treat slices, iter.Seq, and iter.Seq2 as unordered sets (like maps). |
| Binary     | Prefer `encoding.BinaryMarshaler` if available (`datahash:"binary"`). |
| Text       | Prefer `encoding.TextMarshaler` if available (`datahash:"text"`). |
| JSON       | Prefer `json.Marshaler` if available (`datahash:"json"`). |
| String     | Prefer `fmt.Stringer` if available (`datahash:"string"`). |
| ZeroNil    | Treat nil pointers like zero values (`datahash:"zeronil"`). |
| IgnoreZero | Skip zero-value fields from hashing (`datahash:"ignorezero"`). |

## Notes

- Struct fields are hashed in their declared order.
- Maps and sets are folded using XOR for order-independence.
- Cyclic pointers are detected and skipped safely.
- Use datahash:"-" to exclude fields from hashing.
- Implement HashWriter for custom hash behavior.

## Benchmark

This benchmark demonstrates that datahash is faster and more memory-efficient than 
alternatives like hashstructure or JSON marshaling with FNV hashing.

```go
go test -bench=. -benchmem                                         
goos: darwin
goarch: arm64
pkg: github.com/go-sqlt/datahash
cpu: Apple M3 Pro
BenchmarkHashers/Primitive_int_/Datahash_______________-12              38182410                29.30 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Primitive_int_/Mitchellh/Hashstructure-12              28845574                41.79 ns/op           24 B/op          3 allocs/op
BenchmarkHashers/Primitive_int_/Gohugoio/Hashstructure_-12              39140218                30.58 ns/op           16 B/op          2 allocs/op
BenchmarkHashers/Primitive_int_/JSON___________________-12              19650801                61.87 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/String_value__/Datahash_______________-12              41107209                29.63 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/String_value__/Mitchellh/Hashstructure-12              35961655                33.42 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/String_value__/Gohugoio/Hashstructure_-12              36882174                30.76 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/String_value__/JSON___________________-12              17677398                67.79 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/Simple_struct_/Datahash_______________-12              17385403                67.29 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Simple_struct_/Mitchellh/Hashstructure-12               3100363               384.9 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/Gohugoio/Hashstructure_-12               3050971               395.6 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/JSON___________________-12              11511278               102.9 ns/op            40 B/op          2 allocs/op
BenchmarkHashers/Complex_struct/Datahash_______________-12               1887534               635.5 ns/op           176 B/op          5 allocs/op
BenchmarkHashers/Complex_struct/Mitchellh/Hashstructure-12                489852              2362 ns/op            1480 B/op         92 allocs/op
BenchmarkHashers/Complex_struct/Gohugoio/Hashstructure_-12                466946              2411 ns/op            1416 B/op         90 allocs/op
BenchmarkHashers/Complex_struct/JSON___________________-12                998325              1126 ns/op             496 B/op          8 allocs/op
BenchmarkHashers/Map_value_____/Datahash_______________-12               2944309               404.8 ns/op           176 B/op          7 allocs/op
BenchmarkHashers/Map_value_____/Mitchellh/Hashstructure-12               1967696               611.4 ns/op           352 B/op         29 allocs/op
BenchmarkHashers/Map_value_____/Gohugoio/Hashstructure_-12               2128098               563.3 ns/op           208 B/op         24 allocs/op
BenchmarkHashers/Map_value_____/JSON___________________-12               3183300               374.8 ns/op           280 B/op          9 allocs/op
PASS
ok      github.com/go-sqlt/datahash     23.795s
```
