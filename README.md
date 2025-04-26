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

This benchmark demonstrates that datahash can handle structs faster and more memory-efficient than 
alternatives like hashstructure or JSON marshaling with FNV hashing:  

3-1.2-4.9-1.4

```go
go test -bench=. -benchmem
goos: darwin
goarch: arm64
pkg: github.com/go-sqlt/datahash
cpu: Apple M3 Pro
BenchmarkHashers/Primitive_int/Datahash+FNV/Marker=false/-12            37295659                31.83 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Primitive_int/Datahash+FNV/Marker=true/-12             32563528                38.40 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Primitive_int/Mitchellh/Hashstructure+FNV/-12          30723014                37.68 ns/op           24 B/op          3 allocs/op
BenchmarkHashers/Primitive_int/Gohugoio/Hashstructure+FNV/-12           39559243                29.90 ns/op           16 B/op          2 allocs/op
BenchmarkHashers/Primitive_int/JSON+FNV/-12                             19842499                60.55 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/String_value/Datahash+FNV/Marker=false/-12             38557158                30.86 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/String_value/Datahash+FNV/Marker=true/-12              29659746                41.43 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/String_value/Mitchellh/Hashstructure+FNV/-12           35286074                32.78 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/String_value/Gohugoio/Hashstructure+FNV/-12            38238327                30.69 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/String_value/JSON+FNV/-12                              18061090                65.67 ns/op           24 B/op          2 allocs/op
BenchmarkHashers/Simple_struct/Datahash+FNV/Marker=false/-12            15978410                75.56 ns/op            0 B/op          0 allocs/op
BenchmarkHashers/Simple_struct/Datahash+FNV/Marker=true/-12             10969442               111.3 ns/op             0 B/op          0 allocs/op
BenchmarkHashers/Simple_struct/Mitchellh/Hashstructure+FNV/-12           3123708               378.9 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct/Gohugoio/Hashstructure+FNV/-12            3114404               382.8 ns/op           248 B/op         17 allocs/op
BenchmarkHashers/Simple_struct/JSON+FNV/-12                             12080455                98.74 ns/op           40 B/op          2 allocs/op
BenchmarkHashers/Complex_struct/Datahash+FNV/Marker=false/-12            1676384               715.5 ns/op           176 B/op          5 allocs/op
BenchmarkHashers/Complex_struct/Datahash+FNV/Marker=true/-12             1265469               950.8 ns/op           176 B/op          5 allocs/op
BenchmarkHashers/Complex_struct/Mitchellh/Hashstructure+FNV/-12           496767              2343 ns/op            1480 B/op         92 allocs/op
BenchmarkHashers/Complex_struct/Gohugoio/Hashstructure+FNV/-12            492592              2417 ns/op            1416 B/op         90 allocs/op
BenchmarkHashers/Complex_struct/JSON+FNV/-12                             1000000              1087 ns/op             496 B/op          8 allocs/op
BenchmarkHashers/Map_value/Datahash+FNV/Marker=false/-12                 2756678               433.8 ns/op           176 B/op          7 allocs/op
BenchmarkHashers/Map_value/Datahash+FNV/Marker=true/-12                  2096316               568.8 ns/op           176 B/op          7 allocs/op
BenchmarkHashers/Map_value/Mitchellh/Hashstructure+FNV/-12               1876640               625.5 ns/op           352 B/op         29 allocs/op
BenchmarkHashers/Map_value/Gohugoio/Hashstructure+FNV/-12                2166250               556.7 ns/op           208 B/op         24 allocs/op
BenchmarkHashers/Map_value/JSON+FNV/-12                                  3184570               373.0 ns/op           280 B/op          9 allocs/op
PASS
ok      github.com/go-sqlt/datahash     29.951s
```
