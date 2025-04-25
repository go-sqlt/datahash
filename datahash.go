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
				return init()
			},
		},
		store: &sync.Map{},
	}
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
	hash := h.pool.Get().(H)
	hash.Reset()

	v := reflect.ValueOf(value)

	hasher, err := h.makeHashFunc(v.Type(), h.defaultOpts)
	if err != nil {
		return 0, err
	}

	err = hasher(hash, v, map[unsafe.Pointer]struct{}{}, h.defaultOpts)
	if err != nil {
		h.pool.Put(hash)

		return 0, err
	}

	result := hash.Sum64()

	h.pool.Put(hash)

	return result, nil
}

type hashFunc[H hash.Hash64] func(hash H, value reflect.Value, visited map[unsafe.Pointer]struct{}, opts Options) error

var (
	openBrace    = []byte{'{'}
	closeBrace   = []byte{'}'}
	colon        = []byte{':'}
	comma        = []byte{','}
	openBracket  = []byte{'['}
	closeBracket = []byte{']'}
)

func (h *Hasher[H]) hashByteSlice(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	_, err := hash.Write(value.Bytes())

	return err
}

func (h *Hasher[H]) hashInterface(hash H, value reflect.Value, visited map[unsafe.Pointer]struct{}, opts Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	elem := value.Elem()

	hasher, err := h.makeHashFunc(elem.Type(), opts)
	if err != nil {
		return err
	}

	return hasher(hash, elem, visited, opts)
}

func (h *Hasher[H]) hashString(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	_, err := hash.Write(stringToBytes(value.String()))

	return err
}

func (h *Hasher[H]) hashInt(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	return binary.Write(hash, binary.LittleEndian, value.Int())
}

func (h *Hasher[H]) hashUint(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	return binary.Write(hash, binary.LittleEndian, value.Uint())
}

func (h *Hasher[H]) hashFloat(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	return binary.Write(hash, binary.LittleEndian, value.Float())
}

func (h *Hasher[H]) hashComplex(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	return binary.Write(hash, binary.LittleEndian, value.Complex())
}

func (h *Hasher[H]) hashBool(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	return binary.Write(hash, binary.LittleEndian, value.Bool())
}

func (h *Hasher[H]) hashArray(vhf hashFunc[H]) hashFunc[H] {
	return func(hash H, value reflect.Value, visited map[unsafe.Pointer]struct{}, opts Options) error {
		if !value.IsValid() || value.IsZero() {
			return nil
		}

		_, err := hash.Write(openBracket)
		if err != nil {
			return err
		}

		for i := range value.Len() {
			if i > 0 {
				_, err = hash.Write(comma)
				if err != nil {
					return err
				}
			}

			err := vhf(hash, value.Index(i), visited, opts)
			if err != nil {
				return err
			}
		}

		_, err = hash.Write(closeBracket)
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

	return func(hash H, value reflect.Value, visited map[unsafe.Pointer]struct{}, opts Options) error {
		if !value.IsValid() || value.IsZero() {
			return nil
		}

		var (
			result  uint64
			tmphash = h.pool.Get().(H)
		)

		for i := range value.Len() {
			val := value.Index(i)
			if !val.IsValid() || val.IsZero() {
				continue
			}

			tmphash.Reset()

			if err := vhf(tmphash, val, visited, opts); err != nil {
				h.pool.Put(tmphash)

				return err
			}

			result ^= tmphash.Sum64()
		}

		h.pool.Put(tmphash)

		err := binary.Write(hash, binary.LittleEndian, result)
		if err != nil {
			return err
		}

		return nil
	}
}

func (h *Hasher[H]) hashMap(khf, vhf hashFunc[H]) hashFunc[H] {
	return func(hash H, value reflect.Value, visited map[unsafe.Pointer]struct{}, opts Options) error {
		if !value.IsValid() || value.IsZero() {
			return nil
		}

		var (
			result  uint64
			tmphash = h.pool.Get().(H)
		)

		keys := value.MapKeys()
		for _, key := range keys {
			val := value.MapIndex(key)
			if !val.IsValid() || val.IsZero() {
				continue
			}

			tmphash.Reset()

			if err := khf(tmphash, key, visited, opts); err != nil {
				h.pool.Put(tmphash)

				return err
			}

			_, err := tmphash.Write(colon)
			if err != nil {
				h.pool.Put(tmphash)

				return err
			}

			if err := vhf(tmphash, val, visited, opts); err != nil {
				h.pool.Put(tmphash)

				return err
			}

			result ^= tmphash.Sum64()
		}

		h.pool.Put(tmphash)

		err := binary.Write(hash, binary.LittleEndian, result)
		if err != nil {
			return err
		}

		return nil
	}
}

type structField[H hash.Hash64] struct {
	name []byte
	hf   hashFunc[H]
	idx  int
}

func (h *Hasher[H]) hashStruct(sfs []structField[H]) hashFunc[H] {
	return func(hash H, value reflect.Value, visited map[unsafe.Pointer]struct{}, opts Options) error {
		if !value.IsValid() || value.IsZero() {
			return nil
		}

		if value.CanAddr() {
			ptr := value.Addr().UnsafePointer()
			if _, ok := visited[ptr]; ok {
				return nil
			}

			visited[ptr] = struct{}{}
		}

		_, err := hash.Write(openBrace)
		if err != nil {
			return err
		}

		for i, sf := range sfs {
			if i > 0 {
				_, err = hash.Write(comma)
				if err != nil {
					return err
				}
			}

			_, err = hash.Write(sf.name)
			if err != nil {
				return err
			}

			_, err := hash.Write(colon)
			if err != nil {
				h.pool.Put(hash)

				return err
			}

			if err := sf.hf(hash, value.Field(sf.idx), visited, opts); err != nil {
				return err
			}
		}

		_, err = hash.Write(closeBrace)
		if err != nil {
			return err
		}

		return nil
	}
}

type setPair struct {
	key reflect.Value
	val reflect.Value
}

func (h *Hasher[H]) hashSeq2(hash H, value reflect.Value, visited map[unsafe.Pointer]struct{}, opts Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	_, err := hash.Write(openBrace)
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

	khf, err := h.makeHashFunc(pairs[0].key.Type(), opts)
	if err != nil {
		return err
	}

	vhf, err := h.makeHashFunc(pairs[0].val.Type(), opts)
	if err != nil {
		return err
	}

	for i, pair := range pairs {
		if i > 0 {
			_, err = hash.Write(comma)
			if err != nil {
				return err
			}
		}

		if err := khf(hash, pair.key, visited, opts); err != nil {
			return err
		}

		_, err = hash.Write(colon)
		if err != nil {
			return err
		}

		if err := vhf(hash, pair.val, visited, opts); err != nil {
			return err
		}
	}

	_, err = hash.Write(closeBrace)
	if err != nil {
		return err
	}

	return nil
}

func (h *Hasher[H]) hashSeq(hash H, value reflect.Value, visited map[unsafe.Pointer]struct{}, opts Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	_, err := hash.Write(openBracket)
	if err != nil {
		return err
	}

	var (
		first = true
		vhf   hashFunc[H]
	)

	for v := range value.Seq() {
		if first {
			first = false

			vhf, err = h.makeHashFunc(v.Type(), opts)
			if err != nil {
				return err
			}
		} else {
			_, err := hash.Write(comma)
			if err != nil {
				return err
			}
		}

		if err := vhf(hash, v, visited, opts); err != nil {
			return err
		}
	}

	_, err = hash.Write(closeBracket)
	if err != nil {
		return err
	}

	return nil
}

func (h *Hasher[H]) hashInterfaceHashable(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	v, err := value.Interface().(Hashable).Hash()
	if err != nil {
		return err
	}

	_, err = hash.Write(v)

	return err
}

func (h *Hasher[H]) hashInterfaceBinary(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	v, err := value.Interface().(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return err
	}

	_, err = hash.Write(v)

	return err
}

func (h *Hasher[H]) hashInterfaceText(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	v, err := value.Interface().(encoding.TextMarshaler).MarshalText()
	if err != nil {
		return err
	}

	_, err = hash.Write(v)

	return err
}

func (h *Hasher[H]) hashInterfaceJSON(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	v, err := json.Marshal(value.Interface())
	if err != nil {
		return err
	}

	_, err = hash.Write(v)

	return err
}

func (h *Hasher[H]) hashInterfaceStringer(hash H, value reflect.Value, _ map[unsafe.Pointer]struct{}, _ Options) error {
	if !value.IsValid() || value.IsZero() {
		return nil
	}

	_, err := hash.Write(stringToBytes(value.Interface().(fmt.Stringer).String()))

	return err
}

var (
	hashableType        = reflect.TypeFor[Hashable]()
	binaryMarshalerType = reflect.TypeFor[encoding.BinaryMarshaler]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshalerType   = reflect.TypeFor[json.Marshaler]()
	stringerType        = reflect.TypeFor[fmt.Stringer]()
)

func (h *Hasher[H]) makeHashFunc(t reflect.Type, opts Options) (hf hashFunc[H], err error) {
	if cached, ok := h.store.Load(t); ok {
		return cached.(hashFunc[H]), nil
	}

	defer func() {
		if err == nil {
			h.store.Store(t, hf)
		}
	}()

	if t.Implements(hashableType) {
		return h.hashInterfaceHashable, nil
	}

	if opts.Binary && t.Implements(binaryMarshalerType) {
		return h.hashInterfaceBinary, nil
	}

	if opts.Text && t.Implements(textMarshalerType) {
		return h.hashInterfaceText, nil
	}

	if opts.JSON && t.Implements(jsonMarshalerType) {
		return h.hashInterfaceJSON, nil
	}

	if opts.String && t.Implements(stringerType) {
		return h.hashInterfaceStringer, nil
	}

	switch t.Kind() {
	case reflect.Interface:
		return h.hashInterface, nil
	case reflect.Pointer:
		hasher, err := h.makeHashFunc(t.Elem(), opts)
		if err != nil {
			return nil, err
		}

		return func(hash H, value reflect.Value, visited map[unsafe.Pointer]struct{}, opts Options) error {
			if !value.IsValid() || value.IsZero() {
				return nil
			}

			ptr := value.UnsafePointer()
			if _, ok := visited[ptr]; ok {
				return nil
			}

			visited[ptr] = struct{}{}

			return hasher(hash, value.Elem(), visited, opts)
		}, nil
	case reflect.String:
		return h.hashString, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return h.hashInt, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return h.hashUint, nil
	case reflect.Float32, reflect.Float64:
		return h.hashFloat, nil
	case reflect.Complex64, reflect.Complex128:
		return h.hashComplex, nil
	case reflect.Bool:
		return h.hashBool, nil
	case reflect.Array:
		vhf, err := h.makeHashFunc(t.Elem(), opts)
		if err != nil {
			return nil, err
		}

		return h.hashArray(vhf), nil
	case reflect.Slice:
		elem := t.Elem()

		if elem.Kind() == reflect.Uint8 {
			return h.hashByteSlice, nil
		}

		vhf, err := h.makeHashFunc(elem, opts)
		if err != nil {
			return nil, err
		}

		return h.hashSlice(vhf, opts), nil
	case reflect.Map:
		khf, err := h.makeHashFunc(t.Key(), opts)
		if err != nil {
			return nil, err
		}

		vhf, err := h.makeHashFunc(t.Elem(), opts)
		if err != nil {
			return nil, err
		}

		return h.hashMap(khf, vhf), nil
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

			opts, err = with(opts, tag)
			if err != nil {
				return nil, err
			}

			hf, err := h.makeHashFunc(sf.Type, opts)
			if err != nil {
				return nil, err
			}

			sfs = append(sfs, structField[H]{
				name: stringToBytes(sf.Name),
				idx:  i,
				hf:   hf,
			})
		}

		return h.hashStruct(sfs), nil
	}

	if t.CanSeq2() {
		return h.hashSeq2, nil
	}

	if t.CanSeq() {
		return h.hashSeq, nil
	}

	if opts.JSON {
		return h.hashInterfaceJSON, nil
	}

	return nil, fmt.Errorf("datahash: unsupported type: %s: enable Options.JSON for fallback serialization", t.String())
}

func stringToBytes(s string) []byte {
	//nolint:gosec
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
