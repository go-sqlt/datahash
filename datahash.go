// Package datahash computes 64-bit hashes for arbitrary Go values using reflection.
//
// It recursively traverses Go values using reflection to produce deterministic 64-bit hashes based on their content and structure.
// Supported types include primitives, arrays, slices, maps, structs, pointers, interfaces, and types that support sequence iteration.
//
// Features:
//   - Detects and handles cyclic data structures safely (via pointer tracking).
//   - Supports ordered or unordered hashing of collections and structs via the "Unordered" option.
//   - Integrates with encoding.BinaryMarshaler, encoding.TextMarshaler, fmt.Stringer, and custom HashWriter interfaces.
//   - High performance through reflection caching and hasher pooling.
//
// Usage:
//
// package main
//
// import (
//
//	"fmt"
//	"math/big"
//
//	"github.com/cespare/xxhash/v2"
//	"github.com/go-sqlt/datahash"
//
// )
//
//	type MyStruct struct {
//		Name  string `datahash:"-"`
//		Age   int
//		Float *big.Float
//	}
//
//	func main() {
//		hasher := datahash.New(xxhash.New, datahash.Options{
//			UnorderedStruct: false,
//			UnorderedSlice:  false,
//			UnorderedArray:  false,
//			UnorderedSeq:    false,
//			UnorderedSeq2:   false,
//			Text:            true, // big.Float implements encoding.TextMarshaler
//			JSON:            false,
//			String:          false,
//			ZeroNil:         false,
//			IgnoreZero:      false,
//		})
//
//		alice, _ := hasher.Hash(MyStruct{Name: "Alice", Age: 30, Float: big.NewFloat(1.23)})
//		bob, _ := hasher.Hash(MyStruct{Name: "Bob", Age: 30, Float: big.NewFloat(1.23)})
//
//		fmt.Println(alice, alice == bob) // Output: 13125691809697640472 true
//	}
//
// Notes:
//   - For custom hashing behavior, implement the HashWriter or encoing.BinaryMarshaler interface.
//   - Text/JSON/String Option: use marshaling interfaces if available.
//   - Unordered Option: treat structs, slices, iter.Seq and iter.Seq2 as unordered sets.
//   - Use `datahash:"-"` to exclude a field from hashing.
//   - Struct fields are hashed in declared order unless Unordered is enabled, in which case order is ignored.
//   - Maps are always hashed as a unordered set.
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

// Options configures how values are hashed, including support for unordered collections, interface marshaling, and zero value handling.
type Options struct {
	UnorderedStruct, UnorderedArray, UnorderedSlice, UnorderedSeq, UnorderedSeq2 bool
	Text, JSON, String                                                           bool
	ZeroNil                                                                      bool
	IgnoreZero                                                                   bool
}

// New creates a new Hasher that uses the given hash.Hash64 constructor and Options.
//
// The init function (e.g., fnv.New64a, xxhash.New) must return a new hash.Hash64 instance on each call.
//
// Example:
//
//	fnvHasher := datahash.New(fnv.New64a, datahash.Options{})
//	xxhHasher := datahash.New(xxhash.New, datahash.Options{})
func New[H hash.Hash64](init func() H, opts Options) *Hasher {
	return &Hasher{
		opts: opts,
		containerPool: &sync.Pool{
			New: func() any {
				return &container{
					hash:    init(),
					visited: []uintptr{},
				}
			},
		},
		hashFuncMap: &sync.Map{},
		visited:     []reflect.Type{},
	}
}

// Hasher hashes arbitrary Go values consistently according to configurable Options.
//
// It caches reflection logic internally for performance, is safe for concurrent use,
// and supports integration with marshaling interfaces (BinaryMarshaler, TextMarshaler, etc.).
type Hasher struct {
	opts          Options
	containerPool *sync.Pool // Pool of *container.
	hashFuncMap   *sync.Map  // Map with key reflect.Type and value hashFunc
	visited       []reflect.Type
}

// Hash computes a 64-bit hash of the given value.
//
// It recursively traverses the value's structure using reflection, respecting the configured Options.
// Custom behavior is supported via standard marshaling interfaces (BinaryMarshaler, TextMarshaler, JSONMarshaler, fmt.Stringer)
// or the custom HashWriter interface.
//
// Returns the computed hash or an error if hashing fails.
func (h *Hasher) Hash(value any) (uint64, error) {
	c := h.containerPool.Get().(*container)
	c.Reset()

	v := reflect.ValueOf(value)

	if !v.IsValid() {
		result := c.hash.Sum64()

		h.containerPool.Put(c)

		return result, nil
	}

	hf, err := h.makeHashFunc(v.Type())
	if err != nil {
		result := c.hash.Sum64()

		h.containerPool.Put(c)

		return result, err
	}

	err = hf(v, c)
	if err != nil {
		result := c.hash.Sum64()

		h.containerPool.Put(c)

		return result, err
	}

	result := c.hash.Sum64()

	h.containerPool.Put(c)

	return result, nil
}

type hashFunc func(value reflect.Value, c *container) error

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

func (h *Hasher) hashByteSlice(value reflect.Value, c *container) error {
	if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	return c.write(value.Bytes())
}

func (h *Hasher) hashInterface(value reflect.Value, c *container) error {
	if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if value.Kind() != reflect.Interface {
		hasher, err := h.makeHashFunc(value.Type())
		if err != nil {
			return err
		}

		return hasher(value, c)
	}

	elem := value.Elem()

	if elem.Kind() == reflect.Invalid {
		return nil
	}

	hasher, err := h.makeHashFunc(elem.Type())
	if err != nil {
		return err
	}

	return hasher(elem, c)
}

func (h *Hasher) hashUnorderedSliceArray(vhf hashFunc) hashFunc {
	return func(value reflect.Value, c *container) error {
		var err error

		if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
			return nil
		}

		if err = c.write(startSet[:]); err != nil {
			return err
		}

		var (
			result uint64
			tmp    = h.containerPool.Get().(*container)
		)

		for i := range value.Len() {
			tmp.Reset()

			v := value.Index(i)

			if !v.IsValid() || (h.opts.IgnoreZero && isZero(v)) {
				continue
			}

			if err = vhf(v, tmp); err != nil {
				h.containerPool.Put(tmp)

				return err
			}

			result ^= tmp.hash.Sum64()
		}

		h.containerPool.Put(tmp)

		if result == 0 {
			return c.write(endSet[:])
		}

		return twoErr(
			c.writeUint64(result),
			c.write(endSet[:]),
		)
	}
}

func (h *Hasher) hashSliceArray(vhf hashFunc) hashFunc {
	return func(value reflect.Value, c *container) error {
		var err error

		if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
			return nil
		}

		if err = c.write(startList[:]); err != nil {
			return err
		}

		first := true

		for i := range value.Len() {
			v := value.Index(i)

			if !v.IsValid() || (h.opts.IgnoreZero && isZero(v)) {
				continue
			}

			if !first {
				if err := c.write(comma[:]); err != nil {
					return err
				}
			} else {
				first = false
			}

			if err = vhf(v, c); err != nil {
				return err
			}
		}

		return c.write(endList[:])
	}
}

func (h *Hasher) hashMap(khf, vhf hashFunc) hashFunc {
	return func(value reflect.Value, c *container) error {
		if !value.IsValid() {
			return nil
		}

		var (
			result uint64
			err    error
			tmp    = h.containerPool.Get().(*container)
			iter   = value.MapRange()
		)

		if err = c.write(startSet[:]); err != nil {
			return err
		}

		for iter.Next() {
			tmp.Reset()

			value := iter.Value()
			if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
				continue
			}

			if err = threeErr(
				khf(iter.Key(), tmp),
				tmp.write(colon[:]),
				vhf(value, tmp),
			); err != nil {
				h.containerPool.Put(tmp)

				return err
			}

			result ^= tmp.hash.Sum64()
		}

		h.containerPool.Put(tmp)

		if result == 0 {
			return c.write(endSet[:])
		}

		return twoErr(
			c.writeUint64(result),
			c.write(endSet[:]),
		)
	}
}

type structField struct {
	name []byte
	hf   hashFunc
	idx  int
}

func (h *Hasher) hashStruct(sfs []structField) hashFunc {
	if h.opts.UnorderedStruct {
		return func(value reflect.Value, c *container) error {
			var err error

			if err = c.write(startSet[:]); err != nil {
				return err
			}

			var (
				tmp    = h.containerPool.Get().(*container)
				result uint64
			)

			for _, sf := range sfs {
				fv := value.Field(sf.idx)

				if !fv.IsValid() || h.opts.IgnoreZero && isZero(fv) {
					continue
				}

				tmp.Reset()

				if err = threeErr(
					tmp.write(sf.name),
					tmp.write(colon[:]),
					sf.hf(fv, tmp),
				); err != nil {
					h.containerPool.Put(tmp)

					return err
				}

				result ^= tmp.hash.Sum64()
			}

			h.containerPool.Put(tmp)

			if result == 0 {
				return c.write(endSet[:])
			}

			return twoErr(
				c.writeUint64(result),
				c.write(endSet[:]),
			)
		}
	}

	return func(value reflect.Value, c *container) error {
		var err error

		if !value.IsValid() {
			return nil
		}

		if err = c.write(startList[:]); err != nil {
			return err
		}

		first := true

		for _, sf := range sfs {
			fv := value.Field(sf.idx)

			if !fv.IsValid() || h.opts.IgnoreZero && isZero(fv) {
				continue
			}

			if !first {
				if err := c.write(comma[:]); err != nil {
					return err
				}
			} else {
				first = false
			}

			if err = threeErr(
				c.write(sf.name),
				c.write(colon[:]),
				sf.hf(fv, c),
			); err != nil {
				return err
			}
		}

		return c.write(endList[:])
	}
}

func (h *Hasher) hashSeq2() hashFunc {
	if h.opts.UnorderedSeq2 {
		return func(value reflect.Value, c *container) error {
			if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
				return nil
			}

			var (
				err      error
				khf, vhf hashFunc
			)

			if err = c.write(startSet[:]); err != nil {
				return err
			}

			var (
				result uint64
				tmp    = h.containerPool.Get().(*container)
			)

			for k, v := range value.Seq2() {
				if !k.IsValid() || !v.IsValid() || h.opts.IgnoreZero && isZero(v) {
					continue
				}

				tmp.Reset()

				if khf == nil || vhf == nil {
					khf, err = h.makeHashFunc(k.Type())
					if err != nil {
						h.containerPool.Put(tmp)

						return err
					}

					vhf, err = h.makeHashFunc(v.Type())
					if err != nil {
						h.containerPool.Put(tmp)

						return err
					}
				}

				if err = threeErr(
					khf(k, tmp),
					tmp.write(colon[:]),
					vhf(v, tmp),
				); err != nil {
					h.containerPool.Put(tmp)

					return err
				}

				result ^= tmp.hash.Sum64()
			}

			h.containerPool.Put(tmp)

			if result == 0 {
				return c.write(endSet[:])
			}

			return twoErr(
				c.writeUint64(result),
				c.write(endSet[:]),
			)
		}
	}

	return func(value reflect.Value, c *container) error {
		if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
			return nil
		}

		var (
			err      error
			khf, vhf hashFunc
		)

		if err = c.write(startList[:]); err != nil {
			return err
		}

		for k, v := range value.Seq2() {
			if !k.IsValid() || !v.IsValid() || h.opts.IgnoreZero && isZero(v) {
				continue
			}

			if khf == nil || vhf == nil {
				if khf, err = h.makeHashFunc(k.Type()); err != nil {
					return err
				}

				if vhf, err = h.makeHashFunc(v.Type()); err != nil {
					return err
				}
			} else {
				if err = c.write(comma[:]); err != nil {
					return err
				}
			}

			if err = threeErr(
				khf(k, c),
				c.write(colon[:]),
				vhf(v, c),
			); err != nil {
				return err
			}
		}

		return c.write(endList[:])
	}
}

func (h *Hasher) hashSeq() hashFunc {
	if h.opts.UnorderedSeq {
		return func(value reflect.Value, c *container) error {
			if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
				return nil
			}

			var (
				err error
				vhf hashFunc
			)

			if err = c.write(startSet[:]); err != nil {
				return err
			}

			var (
				result uint64
				tmp    = h.containerPool.Get().(*container)
			)

			for v := range value.Seq() {
				if !v.IsValid() || h.opts.IgnoreZero && isZero(v) {
					continue
				}

				if vhf == nil {
					vhf, err = h.makeHashFunc(v.Type())
					if err != nil {
						h.containerPool.Put(tmp)

						return err
					}
				}

				tmp.Reset()

				if err = vhf(v, tmp); err != nil {
					h.containerPool.Put(tmp)

					return err
				}

				result ^= tmp.hash.Sum64()
			}

			h.containerPool.Put(tmp)

			if result == 0 {
				return c.write(endSet[:])
			}

			return twoErr(
				c.writeUint64(result),
				c.write(endSet[:]),
			)
		}
	}

	return func(value reflect.Value, c *container) error {
		if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
			return nil
		}

		var (
			err error
			vhf hashFunc
		)

		if err = c.write(startList[:]); err != nil {
			return err
		}

		for v := range value.Seq() {
			if !v.IsValid() || h.opts.IgnoreZero && isZero(v) {
				continue
			}

			if vhf == nil {
				if vhf, err = h.makeHashFunc(v.Type()); err != nil {
					return err
				}
			} else {
				if err = c.write(comma[:]); err != nil {
					return err
				}
			}

			if err = vhf(v, c); err != nil {
				return err
			}
		}

		return c.write(endList[:])
	}
}

func (h *Hasher) hashInterfaceHashWriter(value reflect.Value, c *container) error {
	if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use datahash.HashWriter on unexported fields that are not accessible via reflection")
	}

	i, ok := value.Interface().(HashWriter)
	if !ok || i == nil {
		return nil
	}

	return i.WriteHash(c.hash)
}

func (h *Hasher) hashInterfaceBinary(value reflect.Value, c *container) error {
	if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use encoding.BinaryMarshaler on unexported fields that are not accessible via reflection")
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

func (h *Hasher) hashInterfaceText(value reflect.Value, c *container) error {
	if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use encoding.TextMarshaler on unexported fields that are not accessible via reflection")
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

func (h *Hasher) hashInterfaceJSON(value reflect.Value, c *container) error {
	if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use json.Marshaler on unexported fields that are not accessible via reflection")
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

func (h *Hasher) hashInterfaceStringer(value reflect.Value, c *container) error {
	if !value.IsValid() || (h.opts.IgnoreZero && value.IsZero()) {
		return nil
	}

	if !value.CanInterface() {
		return errors.New("cannot use fmt.Stringer on unexported fields that are not accessible via reflection")
	}

	i, ok := value.Interface().(fmt.Stringer)
	if !ok || i == nil {
		return nil
	}

	return c.write(stringToBytes(i.String()))
}

func (h *Hasher) hashPointer(t reflect.Type, hf hashFunc) hashFunc {
	return func(value reflect.Value, c *container) error {
		if !value.IsValid() {
			return nil
		}

		if value.IsNil() {
			if h.opts.ZeroNil {
				return hf(reflect.Zero(t.Elem()), c)
			}

			return nil
		}

		addr := value.Pointer()
		if slices.Contains(c.visited, addr) {
			return nil
		}

		c.visited = append(c.visited, addr)

		return hf(value.Elem(), c)
	}
}

var (
	hashWriterType      = reflect.TypeFor[HashWriter]()
	binaryMarshalerType = reflect.TypeFor[encoding.BinaryMarshaler]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshalerType   = reflect.TypeFor[json.Marshaler]()
	stringerType        = reflect.TypeFor[fmt.Stringer]()
)

func (h *Hasher) makeHashFunc(t reflect.Type) (hf hashFunc, err error) {
	v, ok := h.hashFuncMap.Load(t)
	if ok {
		return v.(hashFunc), nil
	}

	if slices.Contains(h.visited, t) {
		return func(reflect.Value, *container) error {
			return nil
		}, nil
	}

	h.visited = append(h.visited, t)

	switch {
	case t.Implements(hashWriterType):
		return h.checkout(t, h.hashInterfaceHashWriter)
	case t.Implements(binaryMarshalerType):
		return h.checkout(t, h.hashInterfaceBinary)
	case h.opts.Text && t.Implements(textMarshalerType):
		return h.checkout(t, h.hashInterfaceText)
	case h.opts.JSON && t.Implements(jsonMarshalerType):
		return h.checkout(t, h.hashInterfaceJSON)
	case h.opts.String && t.Implements(stringerType):
		return h.checkout(t, h.hashInterfaceStringer)
	}

	switch t.Kind() {
	case reflect.Interface:
		return h.checkout(t, h.hashInterface)
	case reflect.Pointer:
		ehf, err := h.makeHashFunc(t.Elem())
		if err != nil {
			return nil, err
		}

		return h.checkout(t, h.hashPointer(t, ehf))
	case reflect.String:
		return h.checkout(t, func(value reflect.Value, c *container) error {
			return c.write(stringToBytes(value.String()))
		})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return h.checkout(t, func(value reflect.Value, c *container) error {
			//nolint:gosec
			return c.writeUint64(uint64(value.Int()))
		})
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return h.checkout(t, func(value reflect.Value, c *container) error {
			return c.writeUint64(value.Uint())
		})
	case reflect.Float32, reflect.Float64:
		return h.checkout(t, func(value reflect.Value, c *container) error {
			return c.writeFloat64(value.Float())
		})
	case reflect.Complex64, reflect.Complex128:
		return h.checkout(t, func(value reflect.Value, c *container) error {
			v := value.Complex()

			return twoErr(
				c.writeFloat64(real(v)),
				c.writeFloat64(imag(v)),
			)
		})
	case reflect.Bool:
		return h.checkout(t, func(value reflect.Value, c *container) error {
			if value.Bool() {
				return c.write(byteTrue[:])
			}

			return c.write(byteFalse[:])
		})
	case reflect.Array:
		vhf, err := h.makeHashFunc(t.Elem())
		if err != nil {
			return nil, err
		}

		if h.opts.UnorderedArray {
			return h.checkout(t, h.hashUnorderedSliceArray(vhf))
		}

		return h.checkout(t, h.hashSliceArray(vhf))
	case reflect.Slice:
		elem := t.Elem()

		if elem.Kind() == reflect.Uint8 {
			return h.checkout(t, h.hashByteSlice)
		}

		vhf, err := h.makeHashFunc(elem)
		if err != nil {
			return nil, err
		}

		if h.opts.UnorderedSlice {
			return h.checkout(t, h.hashUnorderedSliceArray(vhf))
		}

		return h.checkout(t, h.hashSliceArray(vhf))
	case reflect.Map:
		khf, err := h.makeHashFunc(t.Key())
		if err != nil {
			return nil, err
		}

		vhf, err := h.makeHashFunc(t.Elem())
		if err != nil {
			return nil, err
		}

		return h.checkout(t, h.hashMap(khf, vhf))
	case reflect.Struct:
		sfs := make([]structField, 0, t.NumField())

		for i := range t.NumField() {
			sf := t.Field(i)

			if sf.Tag.Get("datahash") == "-" {
				continue
			}

			hf, err := h.makeHashFunc(sf.Type)
			if err != nil {
				return nil, err
			}

			sfs = append(sfs, structField{
				name: stringToBytes(sf.Name),
				idx:  i,
				hf:   hf,
			})
		}

		return h.checkout(t, h.hashStruct(sfs))
	}

	if t.CanSeq2() {
		return h.checkout(t, h.hashSeq2())
	}

	if t.CanSeq() {
		return h.checkout(t, h.hashSeq())
	}

	return nil, fmt.Errorf("datahash: unsupported type: %q (missing HashWriter or marshaling interface)", t)
}

func (h *Hasher) checkout(t reflect.Type, hf hashFunc) (hashFunc, error) {
	h.hashFuncMap.Store(t, hf)

	return hf, nil
}

type container struct {
	hash    hash.Hash64
	visited []uintptr
	buf     [8]byte
}

func (c *container) Reset() {
	c.hash.Reset()
	c.visited = c.visited[:0]
}

func (c *container) write(b []byte) error {
	_, err := c.hash.Write(b)

	return err
}

func (c *container) writeUint64(v uint64) error {
	binary.LittleEndian.PutUint64(c.buf[:], v)

	return c.write(c.buf[:])
}

func (c *container) writeFloat64(v float64) error {
	binary.LittleEndian.PutUint64(c.buf[:], math.Float64bits(v))

	return c.write(c.buf[:])
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

func isZero(value reflect.Value) bool {
	var check = value

	for check.IsValid() && check.Kind() == reflect.Interface && !check.IsNil() {
		check = value.Elem()
	}

	return check.IsZero()
}
