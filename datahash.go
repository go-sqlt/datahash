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
//	import (
//		"fmt"
//		"hash/fnv"
//		"math/big"
//		"github.com/go-sqlt/datahash"
//	)
//
//	type MyStruct struct {
//		Name  string `datahash:"-"`
//		Age   int
//		Float *big.Float `datahash:"text"`
//	}
//
//	func main() {
//		hasher := datahash.New(fnv.New64a, datahash.Options{
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
//		fmt.Println(alice == bob) // Output: true
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
	"fmt"
	"hash"
	"math"
	"reflect"
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

func with(opts Options, tag string) (Options, error) {
	if tag == "" {
		return opts, nil
	}

	for each := range strings.SplitSeq(tag, ",") {
		switch each {
		default:
			return opts, fmt.Errorf("datahash: unknown struct tag option: %q", each)
		case "json":
			opts.JSON = true
		case "text":
			opts.Text = true
		case "binary":
			opts.Binary = true
		case "string":
			opts.String = true
		case "set":
			opts.Set = true
		case "zeronil":
			opts.ZeroNil = true
		case "ignorezero":
			opts.IgnoreZero = true
		}
	}

	return opts, nil
}

// New creates a new Hasher that uses the given hash.Hash64 constructor and Options.
//
// The provided init function (e.g., fnv.New64a) must return a new hash.Hash64 instance.
//
// Example:
//
//	h := datahash.New(fnv.New64a, datahash.Options{JSON: true})
func New[H hash.Hash64](init func() H, opts Options) *Hasher[H] {
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
		store: &sync.Map{},
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

// Hasher hashes arbitrary Go values consistently according to configurable Options.
//
// It caches reflection logic internally for performance, is safe for concurrent use,
// and supports integration with marshaling interfaces (BinaryMarshaler, TextMarshaler, etc.).
type Hasher[H hash.Hash64] struct {
	defaultOpts Options
	pool        *sync.Pool // Pool of H.
	store       *sync.Map  // Map with key reflect.Type and value hashFunc[H]
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

	err = hf(v, c, h.defaultOpts)
	if err != nil {
		h.pool.Put(c)

		return 0, err
	}

	result := c.hash.Sum64()

	h.pool.Put(c)

	return result, nil
}

type hashFunc[H hash.Hash64] func(value reflect.Value, c *container[H], opts Options) error

var (
	openBrace    = [1]byte{'{'}
	closeBrace   = [1]byte{'}'}
	colon        = [1]byte{':'}
	comma        = [1]byte{','}
	openBracket  = [1]byte{'['}
	closeBracket = [1]byte{']'}
	byteTrue     = [1]byte{1}
	byteFalse    = [1]byte{0}
)

func (h *Hasher[H]) hashByteSlice(value reflect.Value, c *container[H], _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	_, err := c.hash.Write(value.Bytes())

	return err
}

func (h *Hasher[H]) hashInterface(value reflect.Value, c *container[H], opts Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	elem := value.Elem()

	hasher, err := h.makeHashFunc(elem.Type(), c, opts)
	if err != nil {
		return err
	}

	return hasher(elem, c, opts)
}

func (h *Hasher[H]) hashString(value reflect.Value, c *container[H], _ Options) error {
	_, err := c.hash.Write(stringToBytes(value.String()))

	return err
}

func (h *Hasher[H]) hashInt(value reflect.Value, c *container[H], _ Options) error {
	//nolint:gosec
	binary.LittleEndian.PutUint64(c.buf[:], uint64(value.Int()))
	_, err := c.hash.Write(c.buf[:])

	return err
}

func (h *Hasher[H]) hashUint(value reflect.Value, c *container[H], _ Options) error {
	binary.LittleEndian.PutUint64(c.buf[:], value.Uint())

	_, err := c.hash.Write(c.buf[:])

	return err
}

func (h *Hasher[H]) hashFloat(value reflect.Value, c *container[H], _ Options) error {
	binary.LittleEndian.PutUint64(c.buf[:], math.Float64bits(value.Float()))

	_, err := c.hash.Write(c.buf[:])

	return err
}

func (h *Hasher[H]) hashComplex(value reflect.Value, c *container[H], _ Options) error {
	v := value.Complex()

	binary.LittleEndian.PutUint64(c.buf[:], math.Float64bits(real(v)))

	_, err := c.hash.Write(c.buf[:])
	if err != nil {
		return err
	}

	binary.LittleEndian.PutUint64(c.buf[:], math.Float64bits(imag(v)))

	_, err = c.hash.Write(c.buf[:])

	return err
}

func (h *Hasher[H]) hashBool(value reflect.Value, c *container[H], _ Options) error {
	var err error

	if value.Bool() {
		_, err = c.hash.Write(byteTrue[:])
	} else {
		_, err = c.hash.Write(byteFalse[:])
	}

	return err
}

func (h *Hasher[H]) hashArray(vhf hashFunc[H]) hashFunc[H] {
	return func(value reflect.Value, c *container[H], opts Options) error {
		if !value.IsValid() || value.IsZero() {
			return nil
		}

		_, err := c.hash.Write(openBracket[:])
		if err != nil {
			return err
		}

		for i := range value.Len() {
			if i > 0 {
				_, err = c.hash.Write(comma[:])
				if err != nil {
					return err
				}
			}

			err = vhf(value.Index(i), c, opts)
			if err != nil {
				return err
			}
		}

		_, err = c.hash.Write(closeBracket[:])
		if err != nil {
			return err
		}

		return nil
	}
}

func (h *Hasher[H]) hashSlice(vhf hashFunc[H], opts Options) hashFunc[H] {
	if !opts.Set {
		return h.hashArray(vhf)
	}

	return func(value reflect.Value, c *container[H], opts Options) error {
		if !value.IsValid() || value.IsZero() {
			return nil
		}

		var (
			result uint64
			tmp    = h.pool.Get().(*container[H])
		)

		for i := range value.Len() {
			val := value.Index(i)
			if !val.IsValid() || val.IsZero() {
				continue
			}

			tmp.Reset()

			if err := vhf(val, tmp, opts); err != nil {
				h.pool.Put(tmp)

				return err
			}

			result ^= tmp.hash.Sum64()
		}

		h.pool.Put(tmp)

		binary.LittleEndian.PutUint64(c.buf[:], result)

		_, err := c.hash.Write(c.buf[:])

		return err
	}
}

func (h *Hasher[H]) hashMap(khf, vhf hashFunc[H]) hashFunc[H] {
	return func(value reflect.Value, c *container[H], opts Options) error {
		if !value.IsValid() || value.IsZero() {
			return nil
		}

		var (
			result uint64
			tmp    = h.pool.Get().(*container[H])
			err    error
		)

		keys := value.MapKeys()
		for _, key := range keys {
			val := value.MapIndex(key)
			if !val.IsValid() {
				continue
			}

			tmp.Reset()

			err = khf(key, tmp, opts)
			if err != nil {
				h.pool.Put(tmp)

				return err
			}

			_, err = tmp.hash.Write(colon[:])
			if err != nil {
				h.pool.Put(tmp)

				return err
			}

			if err = vhf(val, tmp, opts); err != nil {
				h.pool.Put(tmp)

				return err
			}

			result ^= tmp.hash.Sum64()
		}

		h.pool.Put(tmp)

		binary.LittleEndian.PutUint64(c.buf[:], result)

		_, err = c.hash.Write(c.buf[:])

		return err
	}
}

type structField[H hash.Hash64] struct {
	name []byte
	hf   hashFunc[H]
	idx  int
}

func (h *Hasher[H]) hashStruct(sfs []structField[H]) hashFunc[H] {
	return func(value reflect.Value, c *container[H], opts Options) error {
		if !value.IsValid() || value.IsZero() {
			return nil
		}

		_, err := c.hash.Write(openBrace[:])
		if err != nil {
			return err
		}

		first := true

		for _, sf := range sfs {
			fv := value.Field(sf.idx)

			if opts.IgnoreZero && fv.IsZero() {
				continue
			}

			if !first {
				_, err = c.hash.Write(comma[:])
				if err != nil {
					return err
				}
			}

			first = false

			_, err = c.hash.Write(sf.name)
			if err != nil {
				return err
			}

			_, err = c.hash.Write(colon[:])
			if err != nil {
				return err
			}

			if err = sf.hf(fv, c, opts); err != nil {
				return err
			}
		}

		_, err = c.hash.Write(closeBrace[:])

		return err
	}
}

func (h *Hasher[H]) hashSeq2(value reflect.Value, c *container[H], opts Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	if opts.Set {
		var (
			result uint64
			tmp    = h.pool.Get().(*container[H])
			err    error
		)

		var khf, vhf hashFunc[H]

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

			if err = khf(k, tmp, opts); err != nil {
				h.pool.Put(tmp)

				return err
			}

			if _, err = tmp.hash.Write(colon[:]); err != nil {
				h.pool.Put(tmp)

				return err
			}

			if err = vhf(v, tmp, opts); err != nil {
				h.pool.Put(tmp)

				return err
			}

			result ^= tmp.hash.Sum64()
		}

		h.pool.Put(tmp)

		binary.LittleEndian.PutUint64(c.buf[:], result)

		_, err = c.hash.Write(c.buf[:])

		return err
	}

	_, err := c.hash.Write(openBrace[:])
	if err != nil {
		return err
	}

	var (
		khf, vhf hashFunc[H]
		first    = true
	)

	for k, v := range value.Seq2() {
		if !k.IsValid() || !v.IsValid() {
			continue
		}

		if !first {
			if _, err = c.hash.Write(comma[:]); err != nil {
				return err
			}
		}

		first = false

		if khf == nil || vhf == nil {
			khf, err = h.makeHashFunc(k.Type(), c, opts)
			if err != nil {
				return err
			}

			vhf, err = h.makeHashFunc(v.Type(), c, opts)
			if err != nil {
				return err
			}
		}

		if err = khf(k, c, opts); err != nil {
			return err
		}

		if _, err = c.hash.Write(colon[:]); err != nil {
			return err
		}

		if err = vhf(v, c, opts); err != nil {
			return err
		}
	}

	_, err = c.hash.Write(closeBrace[:])

	return err
}

func (h *Hasher[H]) hashSeq(value reflect.Value, c *container[H], opts Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	if opts.Set {
		var (
			result uint64
			err    error
			tmp    = h.pool.Get().(*container[H])
		)

		var vhf hashFunc[H]

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

		binary.LittleEndian.PutUint64(c.buf[:], result)

		_, err = c.hash.Write(c.buf[:])

		return err
	}

	_, err := c.hash.Write(openBracket[:])
	if err != nil {
		return err
	}

	var vhf hashFunc[H]

	for v := range value.Seq() {
		if vhf == nil {
			vhf, err = h.makeHashFunc(v.Type(), c, opts)
			if err != nil {
				return err
			}
		} else {
			_, err = c.hash.Write(comma[:])
			if err != nil {
				return err
			}
		}

		if err = vhf(v, c, opts); err != nil {
			return err
		}
	}

	_, err = c.hash.Write(closeBracket[:])

	return err
}

func (h *Hasher[H]) hashInterfaceHashWriter(value reflect.Value, c *container[H], _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	return value.Interface().(HashWriter).WriteHash(c.hash)
}

func (h *Hasher[H]) hashInterfaceBinary(value reflect.Value, c *container[H], _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	v, err := value.Interface().(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return err
	}

	_, err = c.hash.Write(v)

	return err
}

func (h *Hasher[H]) hashInterfaceText(value reflect.Value, c *container[H], _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	v, err := value.Interface().(encoding.TextMarshaler).MarshalText()
	if err != nil {
		return err
	}

	_, err = c.hash.Write(v)

	return err
}

func (h *Hasher[H]) hashInterfaceJSON(value reflect.Value, c *container[H], _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	v, err := value.Interface().(json.Marshaler).MarshalJSON()
	if err != nil {
		return err
	}

	_, err = c.hash.Write(v)

	return err
}

func (h *Hasher[H]) hashInterfaceStringer(value reflect.Value, c *container[H], _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	_, err := c.hash.Write(stringToBytes(value.Interface().(fmt.Stringer).String()))

	return err
}

func (h *Hasher[H]) hashPointer(t reflect.Type, hf hashFunc[H]) hashFunc[H] {
	return func(value reflect.Value, c *container[H], opts Options) error {
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
		if contains(c.visited, addr) {
			return nil
		}

		c.visited = append(c.visited, addr)

		return hf(value.Elem(), c, opts)
	}
}

func (h *Hasher[H]) checkout(t reflect.Type, hf hashFunc[H]) (hashFunc[H], error) {
	h.store.Store(t, hf)

	return hf, nil
}

var (
	hashWriterType      = reflect.TypeFor[HashWriter]()
	binaryMarshalerType = reflect.TypeFor[encoding.BinaryMarshaler]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshalerType   = reflect.TypeFor[json.Marshaler]()
	stringerType        = reflect.TypeFor[fmt.Stringer]()
)

func (h *Hasher[H]) makeHashFunc(t reflect.Type, c *container[H], opts Options) (hf hashFunc[H], err error) {
	if cached, ok := h.store.Load(t); ok {
		return cached.(hashFunc[H]), nil
	}

	if contains(c.visitedTypes, t) {
		return func(reflect.Value, *container[H], Options) error {
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
		return h.checkout(t, h.hashString)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return h.checkout(t, h.hashInt)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return h.checkout(t, h.hashUint)
	case reflect.Float32, reflect.Float64:
		return h.checkout(t, h.hashFloat)
	case reflect.Complex64, reflect.Complex128:
		return h.checkout(t, h.hashComplex)
	case reflect.Bool:
		return h.checkout(t, h.hashBool)
	case reflect.Array:
		vhf, err := h.makeHashFunc(t.Elem(), c, opts)
		if err != nil {
			return nil, err
		}

		return h.checkout(t, h.hashArray(vhf))
	case reflect.Slice:
		elem := t.Elem()

		if elem.Kind() == reflect.Uint8 {
			return h.checkout(t, h.hashByteSlice)
		}

		vhf, err := h.makeHashFunc(elem, c, opts)
		if err != nil {
			return nil, err
		}

		return h.checkout(t, h.hashSlice(vhf, opts))
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

			localOpts, err := with(opts, tag)
			if err != nil {
				return nil, err
			}

			hf, err := h.makeHashFunc(sf.Type, c, localOpts)
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

	return nil, fmt.Errorf("datahash: unsupported type: %s (missing HashWriter or marshaling interface)", t.String())
}

func stringToBytes(s string) []byte {
	//nolint:gosec
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func contains[T comparable](in []T, t T) bool {
	for _, each := range in {
		if each == t {
			return true
		}
	}

	return false
}
