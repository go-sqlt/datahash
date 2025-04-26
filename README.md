# datahash

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/go-sqlt/datahash)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/go-sqlt/datahash.svg?style=social)](https://github.com/go-sqlt/datahash/tags)
[![Coverage](https://img.shields.io/badge/Coverage-77.3%25-brightgreen)](https://github.com/go-sqlt/datahash/actions)

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
BenchmarkHashers/Primitive_int_/Datahash_______________-12              40963244                29.08 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Primitive_int_/Mitchellh/Hashstructure-12              29146521                40.21 ns/op           24 B/op          3 allocs/op
BenchmarkHashers/Primitive_int_/Gohugoio/Hashstructure_-12              38798799                31.00 ns/op           16 B/op          2 allocs/op
BenchmarkHashers/Primitive_int_/JSON___________________-12              19034953                61.94 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/String_value__/Datahash_______________-12              40841504                29.28 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/String_value__/Mitchellh/Hashstructure-12              34792482                34.56 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/String_value__/Gohugoio/Hashstructure_-12              37529221                31.39 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/String_value__/JSON___________________-12              17050468                68.67 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/Simple_struct_/Datahash_______________-12              17081565                68.48 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Simple_struct_/Mitchellh/Hashstructure-12               3075934               389.0 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/Gohugoio/Hashstructure_-12               3015672               396.9 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct_/JSON___________________-12              11449634               103.8 ns/op            40 B/op          2 allocs/op
BenchmarkHashers/Complex_struct/Datahash_______________-12               1690526               713.8 ns/op           176 B/op          5 allocs/op
BenchmarkHashers/Complex_struct/Mitchellh/Hashstructure-12                487094              2371 ns/op            1480 B/op         92 allocs/op
BenchmarkHashers/Complex_struct/Gohugoio/Hashstructure_-12                483510              2424 ns/op            1416 B/op         90 allocs/op
BenchmarkHashers/Complex_struct/JSON___________________-12               1000000              1135 ns/op             496 B/op          8 allocs/op
BenchmarkHashers/Map_value_____/Datahash_______________-12               2966040               404.3 ns/op           176 B/op          7 allocs/op
BenchmarkHashers/Map_value_____/Mitchellh/Hashstructure-12               1927134               621.4 ns/op           352 B/op         29 allocs/op
BenchmarkHashers/Map_value_____/Gohugoio/Hashstructure_-12               2116160               566.9 ns/op           208 B/op         24 allocs/op
BenchmarkHashers/Map_value_____/JSON___________________-12               3167146               376.9 ns/op           280 B/op          9 allocs/op
PASS
ok      github.com/go-sqlt/datahash     23.901s
```
