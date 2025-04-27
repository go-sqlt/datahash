// Package datahash computes 64-bit hashes for arbitrary Go values using reflection.
//
// It recursively traverses Go values to produce deterministic hashes based on their content and structure.
// Supported types include primitives, arrays, slices, maps, structs, pointers, interfaces, and sequential types.
//
// Features:
//   - Detects and handles cyclic data structures safely (via pointer tracking).
//   - Allows customization of hashing via struct tags and Options.
//   - Supports ordered and unordered hashing of sequences (using the "set" option).
//   - Integrates with encoding.BinaryMarshaler, encoding.TextMarshaler, fmt.Stringer, and custom HashWriter interfaces.
//   - High performance through reflection caching and hasher pooling.
//
// Usage:
//
// package main

// import (
// 		"fmt"
// 		"hash/fnv"
// 		"math/big"

// 		"github.com/go-sqlt/datahash"
// )

// type MyStruct struct {
// 		Name  string `datahash:"-"`
// 		Age   int
// 		Float *big.Float `datahash:"text"`
// 	}

//	func main() {
//		hasher := datahash.New(fnv.New64a, &datahash.Options{
//			Set:        false,
//			Binary:     false,
//			Text:       false,
//			JSON:       false,
//			String:     false,
//			ZeroNil:    false,
//			IgnoreZero: false,
//		})
//
//		alice, _ := hasher.Hash(MyStruct{Name: "Alice", Age: 30, Float: big.NewFloat(1.23)})
//		bob, _ := hasher.Hash(MyStruct{Name: "Bob", Age: 30, Float: big.NewFloat(1.23)})
//
//		fmt.Println(alice, alice == bob) // Output: 13587264169994933978 true
//	}
//
// Options:
//   - Tag: struct tag key for reading field options (default "datahash").
//   - Set: treat slices, iter.Seq and iter.Seq2 as unordered sets ("set").
//   - Binary/Text/JSON/String: use marshaling interfaces if available ("binary,text,json,string").
//   - ZeroNil: treat nil pointers like zero values ("zeronil").
//   - IgnoreZero: skip fields with zero values ("ignorezero").
//
// Notes:
//   - Use `datahash:"-"` to exclude a field from hashing.
//   - Struct fields are hashed in their declared order.
//   - For custom hashing behavior, implement the HashWriter interface.
package datahash

import (
	"encoding"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unsafe"
)

// HashWriter can be implemented by types that want to define
// custom hashing behavior.
//
// The WriteHash method is responsible for writing a representation
// of the value directly into the provided hash.Hash64.
type HashWriter interface {
	WriteHash(hash hash.Hash64) error
}

// Options specifies configuration for hashing behavior.
//
// Options can be set globally when creating a Hasher, and can also
// be overridden per field via struct tags.
//
// Fields:
//   - Tag: struct tag key to read options from (default "datahash").
//   - Set: treat slices, iter.Seq and iter.Seq2 as unordered sets ("set").
//   - Binary, Text, JSON, String: prefer marshaling interfaces if available.
//   - ZeroNil: treat nil pointers like zero values ("zeronil").
//   - IgnoreZero: skip fields that have zero values ("ignorezero").
type Options struct {
	Tag                        string
	Set                        bool
	Binary, Text, JSON, String bool
	ZeroNil                    bool
	IgnoreZero                 bool
}

func (o *Options) apply(tag string) error {
	if tag == "" {
		return nil
	}

	for each := range strings.SplitSeq(tag, ",") {
		switch each {
		default:
			return fmt.Errorf("datahash: unknown struct tag option: %q", each)
		case "json":
			o.JSON = true
		case "text":
			o.Text = true
		case "binary":
			o.Binary = true
		case "string":
			o.String = true
		case "set":
			o.Set = true
		case "zeronil":
			o.ZeroNil = true
		case "ignorezero":
			o.IgnoreZero = true
		}
	}

	return nil
}

// New creates a new Hasher that uses the given hash.Hash64 constructor and Options.
//
// The provided init function (e.g., fnv.New64a) must return a new hash.Hash64 instance.
//
// Example:
//
//	h := datahash.New(fnv.New64a, datahash.Options{JSON: true})
func New[H hash.Hash64](init func() H, opts *Options) *Hasher[H] {
	if opts.Tag == "" {
		opts.Tag = "datahash"
	}

	return &Hasher[H]{
		defaultOpts: opts,
		pool: &sync.Pool{
			New: func() any {
				return &container[H]{
					hash:         init(),
					visited:      []uintptr{},
					visitedTypes: []reflect.Type{},
				}
			},
		},
	}
}

type container[H hash.Hash64] struct {
	hash         H
	visited      []uintptr
	visitedTypes []reflect.Type
	buf          [8]byte
}

func (c *container[H]) Reset() {
	c.hash.Reset()
	c.visited = c.visited[:0]
	c.visitedTypes = c.visitedTypes[:0]
}

func (c *container[H]) write(b []byte) error {
	_, err := c.hash.Write(b)

	return err
}

func (c *container[H]) writeUint64(v uint64) error {
	binary.LittleEndian.PutUint64(c.buf[:], v)

	return c.write(c.buf[:])
}

func (c *container[H]) writeFloat64(v float64) error {
	binary.LittleEndian.PutUint64(c.buf[:], math.Float64bits(v))

	return c.write(c.buf[:])
}

// Hasher hashes arbitrary Go values consistently according to configurable Options.
//
// It caches reflection logic internally for performance, is safe for concurrent use,
// and supports integration with marshaling interfaces (BinaryMarshaler, TextMarshaler, etc.).
type Hasher[H hash.Hash64] struct {
	defaultOpts *Options
	pool        *sync.Pool // Pool of H.
	types       []reflect.Type
	hashFuncs   []hashFunc[H]
	// store       *sync.Map // Map with key reflect.Type and value hashFunc[H]
}

func (h *Hasher[H]) load(t reflect.Type) (hashFunc[H], bool) {
	if i := slices.Index(h.types, t); i >= 0 {
		return h.hashFuncs[i], true
	}

	return nil, false
}

func (h *Hasher[H]) store(t reflect.Type, hf hashFunc[H]) {
	h.types = append(h.types, t)
	h.hashFuncs = append(h.hashFuncs, hf)
}

// Hash computes a 64-bit hash of the given value.
//
// It traverses the value recursively, respecting struct tags and Options,
// and applies interface-based customizations if available (e.g., MarshalJSON, HashWriter).
//
// Returns the resulting hash value or an error if the value cannot be hashed.
func (h *Hasher[H]) Hash(value any) (uint64, error) {
	c := h.pool.Get().(*container[H])
	c.Reset()

	v := reflect.ValueOf(value)

	if !v.IsValid() {
		return 0, nil
	}

	hf, err := h.makeHashFunc(v.Type(), c, h.defaultOpts)
	if err != nil {
		return 0, err
	}

	if err = hf(v, c, h.defaultOpts); err != nil {
		h.pool.Put(c)

		return 0, err
	}

	result := c.hash.Sum64()

	h.pool.Put(c)

	return result, nil
}

type hashFunc[H hash.Hash64] func(value reflect.Value, c *container[H], opts *Options) error

var (
	byteFalse = [1]byte{0x00}
	byteTrue  = [1]byte{0x01}
	colon     = [1]byte{0x02}
	comma     = [1]byte{0x03}
	startSet  = [1]byte{0x04}
	endSet    = [1]byte{0x05}
	startList = [1]byte{0x06}
	endList   = [1]byte{0x07}
)

func (h *Hasher[H]) hashByteSlice(value reflect.Value, c *container[H], opts *Options) error {
	if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	return c.write(value.Bytes())
}

func (h *Hasher[H]) hashInterface(value reflect.Value, c *container[H], opts *Options) error {
	if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	elem := value.Elem()

	if elem.Kind() == reflect.Invalid {
		return nil
	}

	hasher, err := h.makeHashFunc(elem.Type(), c, opts)
	if err != nil {
		return err
	}

	return hasher(elem, c, opts)
}

func (h *Hasher[H]) hashSliceArray(vhf hashFunc[H]) hashFunc[H] {
	return func(value reflect.Value, c *container[H], opts *Options) error {
		var err error

		if opts.Set {
			if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
				return nil
			}

			if err = c.write(startSet[:]); err != nil {
				return err
			}

			var (
				result uint64
				tmp    = h.pool.Get().(*container[H])
			)

			for i := range value.Len() {
				tmp.Reset()

				if err = vhf(value.Index(i), tmp, opts); err != nil {
					h.pool.Put(tmp)

					return err
				}

				result ^= tmp.hash.Sum64()
			}

			h.pool.Put(tmp)

			return twoErr(
				c.writeUint64(result),
				c.write(endSet[:]),
			)
		}

		if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
			return nil
		}

		if err = c.write(startList[:]); err != nil {
			return err
		}

		for i := range value.Len() {
			if i > 0 {
				if err = c.write(comma[:]); err != nil {
					return err
				}
			}

			if err = vhf(value.Index(i), c, opts); err != nil {
				return err
			}
		}

		return c.write(endList[:])
	}
}

func (h *Hasher[H]) hashMap(khf, vhf hashFunc[H]) hashFunc[H] {
	return func(value reflect.Value, c *container[H], opts *Options) error {
		if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
			return nil
		}

		var (
			result uint64
			err    error
			tmp    = h.pool.Get().(*container[H])
			iter   = value.MapRange()
		)

		if err = c.write(startSet[:]); err != nil {
			return err
		}

		for iter.Next() {
			tmp.Reset()

			if err = threeErr(
				khf(iter.Key(), tmp, opts),
				tmp.write(colon[:]),
				vhf(iter.Value(), tmp, opts),
			); err != nil {
				h.pool.Put(tmp)

				return err
			}

			result ^= tmp.hash.Sum64()
		}

		h.pool.Put(tmp)

		return twoErr(
			c.writeUint64(result),
			c.write(endSet[:]),
		)
	}
}

type structField[H hash.Hash64] struct {
	name []byte
	hf   hashFunc[H]
	idx  int
}

func (h *Hasher[H]) hashStruct(sfs []structField[H]) hashFunc[H] {
	return func(value reflect.Value, c *container[H], opts *Options) error {
		if opts.Set {
			if err := c.write(startSet[:]); err != nil {
				return err
			}

			var (
				tmp    = h.pool.Get().(*container[H])
				result uint64
			)

			for _, sf := range sfs {
				fv := value.Field(sf.idx)

				if opts.IgnoreZero && fv.IsZero() {
					continue
				}

				tmp.Reset()

				if err := threeErr(
					tmp.write(sf.name),
					tmp.write(colon[:]),
					sf.hf(fv, tmp, opts),
				); err != nil {
					h.pool.Put(tmp)

					return err
				}

				result ^= tmp.hash.Sum64()
			}

			h.pool.Put(tmp)

			return twoErr(
				c.writeUint64(result),
				c.write(endSet[:]),
			)
		}

		if !value.IsValid() {
			return nil
		}

		if err := c.write(startList[:]); err != nil {
			return err
		}

		first := true

		for _, sf := range sfs {
			fv := value.Field(sf.idx)

			if opts.IgnoreZero && fv.IsZero() {
				continue
			}

			if !first {
				if err := c.write(comma[:]); err != nil {
					return err
				}
			} else {
				first = false
			}

			if err := threeErr(
				c.write(sf.name),
				c.write(colon[:]),
				sf.hf(fv, c, opts),
			); err != nil {
				return err
			}
		}

		return c.write(endList[:])
	}
}

func (h *Hasher[H]) hashSeq2(value reflect.Value, c *container[H], opts *Options) error {
	if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	var (
		err      error
		khf, vhf hashFunc[H]
	)

	if opts.Set {
		if err = c.write(startSet[:]); err != nil {
			return err
		}

		var (
			result uint64
			tmp    = h.pool.Get().(*container[H])
		)

		for k, v := range value.Seq2() {
			if !k.IsValid() || !v.IsValid() {
				continue
			}

			tmp.Reset()

			if khf == nil || vhf == nil {
				khf, err = h.makeHashFunc(k.Type(), tmp, opts)
				if err != nil {
					h.pool.Put(tmp)

					return err
				}

				vhf, err = h.makeHashFunc(v.Type(), tmp, opts)
				if err != nil {
					h.pool.Put(tmp)

					return err
				}
			}

			if err = threeErr(
				khf(k, tmp, opts),
				tmp.write(colon[:]),
				vhf(v, tmp, opts),
			); err != nil {
				h.pool.Put(tmp)

				return err
			}

			result ^= tmp.hash.Sum64()
		}

		h.pool.Put(tmp)

		return twoErr(
			c.writeUint64(result),
			c.write(endSet[:]),
		)
	}

	if err = c.write(startList[:]); err != nil {
		return err
	}

	first := true

	for k, v := range value.Seq2() {
		if !k.IsValid() || !v.IsValid() {
			continue
		}

		if !first {
			if err = c.write(comma[:]); err != nil {
				return err
			}
		} else {
			first = false
		}

		if khf == nil || vhf == nil {
			if khf, err = h.makeHashFunc(k.Type(), c, opts); err != nil {
				return err
			}

			if vhf, err = h.makeHashFunc(v.Type(), c, opts); err != nil {
				return err
			}
		}

		if err = threeErr(
			khf(k, c, opts),
			c.write(colon[:]),
			vhf(v, c, opts),
		); err != nil {
			return err
		}
	}

	return c.write(endList[:])
}

func (h *Hasher[H]) hashSeq(value reflect.Value, c *container[H], opts *Options) error {
	if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	var (
		err error
		vhf hashFunc[H]
	)

	if opts.Set {
		if err = c.write(startSet[:]); err != nil {
			return err
		}

		var (
			result uint64
			tmp    = h.pool.Get().(*container[H])
		)

		for v := range value.Seq() {
			if vhf == nil {
				vhf, err = h.makeHashFunc(v.Type(), tmp, opts)
				if err != nil {
					h.pool.Put(tmp)

					return err
				}
			}

			tmp.Reset()

			if err = vhf(v, tmp, opts); err != nil {
				h.pool.Put(tmp)

				return err
			}

			result ^= tmp.hash.Sum64()
		}

		h.pool.Put(tmp)

		return twoErr(
			c.writeUint64(result),
			c.write(endSet[:]),
		)
	}

	if err = c.write(startList[:]); err != nil {
		return err
	}

	for v := range value.Seq() {
		if vhf == nil {
			if vhf, err = h.makeHashFunc(v.Type(), c, opts); err != nil {
				return err
			}
		} else {
			if err = c.write(comma[:]); err != nil {
				return err
			}
		}

		if err = vhf(v, c, opts); err != nil {
			return err
		}
	}

	return c.write(endList[:])
}

func (h *Hasher[H]) hashInterfaceHashWriter(value reflect.Value, c *container[H], opts *Options) error {
	if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use datahash.HashWriter from unexported fields")
	}

	i, ok := value.Interface().(HashWriter)
	if !ok || i == nil {
		return nil
	}

	return i.WriteHash(c.hash)
}

func (h *Hasher[H]) hashInterfaceBinary(value reflect.Value, c *container[H], opts *Options) error {
	if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use encoding.BinaryMarshaler from unexported fields")
	}

	i, ok := value.Interface().(encoding.BinaryMarshaler)
	if !ok || i == nil {
		return nil
	}

	v, err := i.MarshalBinary()
	if err != nil {
		return err
	}

	return c.write(v)
}

func (h *Hasher[H]) hashInterfaceText(value reflect.Value, c *container[H], opts *Options) error {
	if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use encoding.TextMarshaler from unexported fields")
	}

	i, ok := value.Interface().(encoding.TextMarshaler)
	if !ok || i == nil {
		return nil
	}

	v, err := i.MarshalText()
	if err != nil {
		return err
	}

	return c.write(v)
}

func (h *Hasher[H]) hashInterfaceJSON(value reflect.Value, c *container[H], opts *Options) error {
	if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use json.Marshaler from unexported fields")
	}

	i, ok := value.Interface().(json.Marshaler)
	if !ok || i == nil {
		return nil
	}

	v, err := i.MarshalJSON()
	if err != nil {
		return err
	}

	return c.write(v)
}

func (h *Hasher[H]) hashInterfaceStringer(value reflect.Value, c *container[H], opts *Options) error {
	if !value.IsValid() || (opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use fmt.Stringer from unexported fields")
	}

	i, ok := value.Interface().(fmt.Stringer)
	if !ok || i == nil {
		return nil
	}

	return c.write(stringToBytes(i.String()))
}

func (h *Hasher[H]) hashPointer(t reflect.Type, hf hashFunc[H]) hashFunc[H] {
	return func(value reflect.Value, c *container[H], opts *Options) error {
		if !value.IsValid() {
			return nil
		}

		if value.IsNil() {
			if opts.ZeroNil {
				return hf(reflect.Zero(t.Elem()), c, opts)
			}

			return nil
		}

		addr := value.Pointer()
		if slices.Contains(c.visited, addr) {
			return nil
		}

		c.visited = append(c.visited, addr)

		return hf(value.Elem(), c, opts)
	}
}

var (
	hashWriterType      = reflect.TypeFor[HashWriter]()
	binaryMarshalerType = reflect.TypeFor[encoding.BinaryMarshaler]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshalerType   = reflect.TypeFor[json.Marshaler]()
	stringerType        = reflect.TypeFor[fmt.Stringer]()
)

func (h *Hasher[H]) makeHashFunc(t reflect.Type, c *container[H], opts *Options) (hf hashFunc[H], err error) {
	if hf, ok := h.load(t); ok {
		return hf, nil
	}

	if slices.Contains(c.visitedTypes, t) {
		return func(reflect.Value, *container[H], *Options) error {
			return nil
		}, nil
	}

	c.visitedTypes = append(c.visitedTypes, t)

	switch {
	case t.Implements(hashWriterType):
		return h.checkout(t, h.hashInterfaceHashWriter)
	case opts.Binary && t.Implements(binaryMarshalerType):
		return h.checkout(t, h.hashInterfaceBinary)
	case opts.Text && t.Implements(textMarshalerType):
		return h.checkout(t, h.hashInterfaceText)
	case opts.JSON && t.Implements(jsonMarshalerType):
		return h.checkout(t, h.hashInterfaceJSON)
	case opts.String && t.Implements(stringerType):
		return h.checkout(t, h.hashInterfaceStringer)
	}

	switch t.Kind() {
	case reflect.Interface:
		return h.checkout(t, h.hashInterface)
	case reflect.Pointer:
		ehf, err := h.makeHashFunc(t.Elem(), c, opts)
		if err != nil {
			return nil, err
		}

		return h.checkout(t, h.hashPointer(t, ehf))
	case reflect.String:
		return h.checkout(t, func(value reflect.Value, c *container[H], opts *Options) error {
			return c.write(stringToBytes(value.String()))
		})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return h.checkout(t, func(value reflect.Value, c *container[H], opts *Options) error {
			//nolint:gosec
			return c.writeUint64(uint64(value.Int()))
		})
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return h.checkout(t, func(value reflect.Value, c *container[H], opts *Options) error {
			return c.writeUint64(value.Uint())
		})
	case reflect.Float32, reflect.Float64:
		return h.checkout(t, func(value reflect.Value, c *container[H], opts *Options) error {
			return c.writeFloat64(value.Float())
		})
	case reflect.Complex64, reflect.Complex128:
		return h.checkout(t, func(value reflect.Value, c *container[H], opts *Options) error {
			v := value.Complex()

			return twoErr(
				c.writeFloat64(real(v)),
				c.writeFloat64(imag(v)),
			)
		})
	case reflect.Bool:
		return h.checkout(t, func(value reflect.Value, c *container[H], _ *Options) error {
			if value.Bool() {
				return c.write(byteTrue[:])
			}

			return c.write(byteFalse[:])
		})
	case reflect.Array:
		vhf, err := h.makeHashFunc(t.Elem(), c, opts)
		if err != nil {
			return nil, err
		}

		return h.checkout(t, h.hashSliceArray(vhf))
	case reflect.Slice:
		elem := t.Elem()

		if elem.Kind() == reflect.Uint8 {
			return h.checkout(t, h.hashByteSlice)
		}

		vhf, err := h.makeHashFunc(elem, c, opts)
		if err != nil {
			return nil, err
		}

		return h.checkout(t, h.hashSliceArray(vhf))
	case reflect.Map:
		khf, err := h.makeHashFunc(t.Key(), c, opts)
		if err != nil {
			return nil, err
		}

		vhf, err := h.makeHashFunc(t.Elem(), c, opts)
		if err != nil {
			return nil, err
		}

		return h.checkout(t, h.hashMap(khf, vhf))
	case reflect.Struct:
		sfs := make([]structField[H], 0, t.NumField())

		for i := range t.NumField() {
			sf := t.Field(i)

			tag := sf.Tag.Get(opts.Tag)
			if tag == "-" {
				continue
			}

			if err = opts.apply(tag); err != nil {
				return nil, err
			}

			hf, err := h.makeHashFunc(sf.Type, c, opts)
			if err != nil {
				return nil, err
			}

			sfs = append(sfs, structField[H]{
				name: stringToBytes(sf.Name),
				idx:  i,
				hf:   hf,
			})
		}

		return h.checkout(t, h.hashStruct(sfs))
	}

	if t.CanSeq2() {
		return h.checkout(t, h.hashSeq2)
	}

	if t.CanSeq() {
		return h.checkout(t, h.hashSeq)
	}

	return nil, fmt.Errorf("datahash: unsupported type: %q (missing HashWriter or marshaling interface)", t)
}

func (h *Hasher[H]) checkout(t reflect.Type, hf hashFunc[H]) (hashFunc[H], error) {
	h.store(t, hf)

	return hf, nil
}

func stringToBytes(s string) []byte {
	//nolint:gosec
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func twoErr(err1, err2 error) error {
	if err1 != nil {
		return err1
	}

	return err2
}

func threeErr(err1, err2, err3 error) error {
	if err1 != nil {
		return err1
	}

	if err2 != nil {
		return err2
	}

	return err3
}
