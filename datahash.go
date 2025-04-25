// Package datahash computes 64-bit hashes for arbitrary Go values using reflection.
//
// It recursively walks the structure of Go values to produce consistent hashes
// based on the actual data. Supported types include primitives, arrays, slices, maps,
// structs, pointers, and interfaces.
//
// Features:
//   - Handles cyclic data structures safely (pointer tracking).
//   - Customization via struct tags and Options.
//   - Supports slices as unordered sets ("set" option).
//   - Integrates with encoding.BinaryMarshaler, encoding.TextMarshaler, fmt.Stringer, and Hashable.
//   - Efficient through caching and hasher pooling.
//
// Usage:
//
// import (
//
//	"fmt"
//	"hash/fnv"
//	"math/big"
//
//	"github.com/go-sqlt/datahash"
//
// )
//
//	type MyStruct struct {
//			Name  string `datahash:"-"`
//			Age   int
//			Float *big.Float `datahash:"text"`
//	}
//
//	func main() {
//			hasher := datahash.New(fnv.New64a, datahash.Options{})
//
//			alice, err := hasher.Hash(MyStruct{Name: "Alice", Age: 30, Float: big.NewFloat(1.23)})
//			fmt.Println(alice, err)
//			// 10743316167976689248 <nil>
//
//				bob, err := hasher.Hash(MyStruct{Name: "Bob", Age: 30, Float: big.NewFloat(1.23)})
//				fmt.Println(bob, err)
//				// 10743316167976689248 <nil>
//		}
//
// Options:
//   - Tag: struct tag key to read options from (default "datahash").
//   - Set: treat slices as unordered sets (`datahash:"set"`).
//   - Binary/Text/JSON/String: prefer marshaling interfaces if implemented (`datahash:"binary,text,json,string"`).
//
// Notes:
//   - Use `datahash:"-"` to ignore a field.
//   - Struct fields are hashed in their declared order.
//   - Maps and sets are folded with XOR to handle unordered keys/values.
//   - Only exported fields are considered.
//   - To add custom hash logic to a type, implement the Hashable interface.
package datahash

import (
	"encoding"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unsafe"
)

// Hashable can be implemented by custom types that want to define
// their own hashing logic. This allows complete control over what
// is hashed and how.
type Hashable interface {
	Hash() ([]byte, error)
}

// Options defines how the hashing behavior should be customized.
//
// Fields:
//   - Tag: struct tag name to look for (default: "datahash").
//   - Set: if true, treats slices as unordered sets.
//   - Binary/Text/JSON/String: use corresponding interface methods
//     (e.g., encoding.BinaryMarshaler) if implemented.
type Options struct {
	Tag                        string
	Set                        bool
	Binary, Text, JSON, String bool
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
		}
	}

	return opts, nil
}

// New creates a new Hasher using the provided hash function constructor (e.g., fnv.New64a)
// and options for tag parsing and behavior customization.
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

// Hasher hashes arbitrary Go values in a consistent, customizable way.
// It is safe for concurrent use and caches reflection logic internally.
type Hasher[H hash.Hash64] struct {
	defaultOpts Options
	pool        *sync.Pool // Pool of H.
	store       *sync.Map  // Map with key reflect.Type and value hashFunc[H]
}

// Hash computes a 64-bit hash of any Go value based on its contents.
//
// It recursively walks the value using reflection, applying options
// and interfaces (e.g., MarshalJSON) when applicable.
//
// Returns a deterministic uint64 value or an error if the type cannot be hashed.
func (h *Hasher[H]) Hash(value any) (uint64, error) {
	c := h.pool.Get().(*container[H])
	c.Reset()

	v := reflect.ValueOf(value)

	if !v.IsValid() {
		return 0, nil
	}

	hasher, err := h.makeHashFunc(v.Type(), c, h.defaultOpts)
	if err != nil {
		return 0, err
	}

	err = hasher(v, c, h.defaultOpts)
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
	return binary.Write(c.hash, binary.LittleEndian, value.Float())
}

func (h *Hasher[H]) hashComplex(value reflect.Value, c *container[H], _ Options) error {
	return binary.Write(c.hash, binary.LittleEndian, value.Complex())
}

func (h *Hasher[H]) hashBool(value reflect.Value, c *container[H], _ Options) error {
	return binary.Write(c.hash, binary.LittleEndian, value.Bool())
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

		err := binary.Write(c.hash, binary.LittleEndian, result)
		if err != nil {
			return err
		}

		return nil
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
		)

		keys := value.MapKeys()
		for _, key := range keys {
			val := value.MapIndex(key)
			if !val.IsValid() || val.IsZero() {
				continue
			}

			tmp.Reset()

			err := khf(key, tmp, opts)
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

		return binary.Write(c.hash, binary.LittleEndian, result)
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

		for i, sf := range sfs {
			if i > 0 {
				_, err = c.hash.Write(comma[:])
				if err != nil {
					return err
				}
			}

			_, err = c.hash.Write(sf.name)
			if err != nil {
				return err
			}

			_, err = c.hash.Write(colon[:])
			if err != nil {
				return err
			}

			if err = sf.hf(value.Field(sf.idx), c, opts); err != nil {
				return err
			}
		}

		_, err = c.hash.Write(closeBrace[:])

		return err
	}
}

type setPair struct {
	key reflect.Value
	val reflect.Value
}

func (h *Hasher[H]) hashSeq2(value reflect.Value, c *container[H], opts Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	_, err := c.hash.Write(openBrace[:])
	if err != nil {
		return err
	}

	var pairs []setPair

	for key, val := range value.Seq2() {
		if key.IsValid() && val.IsValid() {
			pairs = append(pairs, setPair{
				key: key,
				val: val,
			})
		}
	}

	if len(pairs) == 0 {
		return nil
	}

	khf, err := h.makeHashFunc(pairs[0].key.Type(), c, opts)
	if err != nil {
		return err
	}

	vhf, err := h.makeHashFunc(pairs[0].val.Type(), c, opts)
	if err != nil {
		return err
	}

	for i, pair := range pairs {
		if i > 0 {
			_, err = c.hash.Write(comma[:])
			if err != nil {
				return err
			}
		}

		if err = khf(pair.key, c, opts); err != nil {
			return err
		}

		_, err = c.hash.Write(colon[:])
		if err != nil {
			return err
		}

		if err = vhf(pair.val, c, opts); err != nil {
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

func (h *Hasher[H]) hashInterfaceHashable(value reflect.Value, c *container[H], _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	v, err := value.Interface().(Hashable).Hash()
	if err != nil {
		return err
	}

	_, err = c.hash.Write(v)

	return err
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

	v, err := json.Marshal(value.Interface())
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

var (
	hashableType        = reflect.TypeFor[Hashable]()
	binaryMarshalerType = reflect.TypeFor[encoding.BinaryMarshaler]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshalerType   = reflect.TypeFor[json.Marshaler]()
	stringerType        = reflect.TypeFor[fmt.Stringer]()
)

func (h *Hasher[H]) makeHashFunc(t reflect.Type, c *container[H], opts Options) (hf hashFunc[H], err error) {
	if cached, ok := h.store.Load(t); ok {
		return cached.(hashFunc[H]), nil
	}

	if slices.Contains(c.visitedTypes, t) {
		return func(reflect.Value, *container[H], Options) error {
			return nil
		}, nil
	}

	c.visitedTypes = append(c.visitedTypes, t)

	switch {
	case t.Implements(hashableType):
		hf = h.hashInterfaceHashable

		h.store.Store(t, hf)

		return hf, nil
	case opts.Binary && t.Implements(binaryMarshalerType):
		hf = h.hashInterfaceBinary

		h.store.Store(t, hf)

		return hf, nil
	case opts.Text && t.Implements(textMarshalerType):
		hf = h.hashInterfaceText

		h.store.Store(t, hf)

		return hf, nil
	case opts.JSON && t.Implements(jsonMarshalerType):
		hf = h.hashInterfaceJSON

		h.store.Store(t, hf)

		return hf, nil
	case opts.String && t.Implements(stringerType):
		hf = h.hashInterfaceStringer

		h.store.Store(t, hf)

		return hf, nil
	}

	switch t.Kind() {
	case reflect.Interface:
		return h.hashInterface, nil
	case reflect.Pointer:
		hasher, err := h.makeHashFunc(t.Elem(), c, opts)
		if err != nil {
			return nil, err
		}

		hf = func(value reflect.Value, c *container[H], opts Options) error {
			if !value.IsValid() || value.IsZero() {
				return nil
			}

			addr := value.Pointer()
			if addr != 0 {
				if slices.Contains(c.visited, addr) {
					return nil
				}

				c.visited = append(c.visited, addr)
			}

			return hasher(value.Elem(), c, opts)
		}

		h.store.Store(t, hf)

		return hf, nil
	case reflect.String:
		hf = h.hashString

		h.store.Store(t, hf)

		return hf, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		hf = h.hashInt

		h.store.Store(t, hf)

		return hf, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		hf = h.hashUint

		h.store.Store(t, hf)

		return hf, nil
	case reflect.Float32, reflect.Float64:
		hf = h.hashFloat

		h.store.Store(t, hf)

		return hf, nil
	case reflect.Complex64, reflect.Complex128:
		hf = h.hashComplex

		h.store.Store(t, hf)

		return hf, nil
	case reflect.Bool:
		hf = h.hashBool

		h.store.Store(t, hf)

		return hf, nil
	case reflect.Array:
		vhf, err := h.makeHashFunc(t.Elem(), c, opts)
		if err != nil {
			return nil, err
		}

		hf = h.hashArray(vhf)

		h.store.Store(t, hf)

		return hf, nil
	case reflect.Slice:
		elem := t.Elem()

		if elem.Kind() == reflect.Uint8 {
			return h.hashByteSlice, nil
		}

		vhf, err := h.makeHashFunc(elem, c, opts)
		if err != nil {
			return nil, err
		}

		hf = h.hashSlice(vhf, opts)

		h.store.Store(t, hf)

		return hf, nil
	case reflect.Map:
		khf, err := h.makeHashFunc(t.Key(), c, opts)
		if err != nil {
			return nil, err
		}

		vhf, err := h.makeHashFunc(t.Elem(), c, opts)
		if err != nil {
			return nil, err
		}

		hf = h.hashMap(khf, vhf)

		h.store.Store(t, hf)

		return hf, nil
	case reflect.Struct:
		sfs := make([]structField[H], 0, t.NumField())

		for i := range t.NumField() {
			sf := t.Field(i)

			if !sf.IsExported() {
				continue
			}

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

		hf = h.hashStruct(sfs)

		h.store.Store(t, hf)

		return hf, nil
	}

	if t.CanSeq2() {
		hf = h.hashSeq2

		h.store.Store(t, hf)

		return hf, nil
	}

	if t.CanSeq() {
		hf = h.hashSeq

		h.store.Store(t, hf)

		return hf, nil
	}

	if opts.JSON {
		hf = h.hashInterfaceJSON

		h.store.Store(t, hf)

		return hf, nil
	}

	return nil, fmt.Errorf("datahash: unsupported type: %s: enable Options.JSON for fallback serialization", t.String())
}

func stringToBytes(s string) []byte {
	//nolint:gosec
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
