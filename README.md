# datahash

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/go-sqlt/datahash)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/go-sqlt/datahash.svg?style=social)](https://github.com/go-sqlt/datahash/tags)
[![Coverage](https://img.shields.io/badge/Coverage-55.6%25-yellow)](https://github.com/go-sqlt/datahash/actions)

datahash provides a hashing system for arbitrary Go values with zero dependencies.  
It produces consistent 64-bit hashes by recursively traversing Go data structures and considering actual content.  
The package is highly customizable, efficient, and integrates with standard Go interfaces.

## Features

- Consistent 64-bit hashing of any Go value.
- Handles cyclic data structures safely via pointer tracking.
- Supports struct tags and options to control behavior.
- Supports slices as unordered sets (set option).
- Integrates with: encoding.BinaryMarshaler, encoding.TextMarshaler, encoding/json.Marshaler and fmt.Stringer.
- Custom datahash.Hashable interface.
- High performance via: caching per type and pooling of hash.Hash64 instances.

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

- Tag: Struct Tag (default: `datahash`).
- Set: Treats slices as sets (`datahash:"set"`).
- Binary: prefer encoding.BinaryMarshaler if implemented (`datahash:"binary"`).
- Text: prefer encoding.TextMarshaler if implemented (`datahash:"text"`).
- JSON: prefer json.Marshaler if implemented (`datahash:"json"`).
- String: prefer fmt.Stringer if implemented (`datahash:"string"`).

## Notes

- Only exported fields are hashed.
- Fields are hashed in declaration order.
- Use `datahash:"-"` to ignore a field.
- Maps and sets are folded with XOR for unordered consistency.
- Pointer cycles are detected and skipped safely.
- To define custom hashing, implement the Hashable interface.

## Benchmark

```go
go test -bench=. -benchmem
goos: darwin
goarch: arm64
pkg: github.com/go-sqlt/datahash
cpu: Apple M3 Pro
BenchmarkDatahash-12             2420667               476.3 ns/op           272 B/op         12 allocs/op
BenchmarkHashstructure-12         810531              1411 ns/op             944 B/op         66 allocs/op
PASS
ok      github.com/go-sqlt/datahash     2.974s
```